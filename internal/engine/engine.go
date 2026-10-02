// Package engine runs the one Xray-core instance embedded in the app.
//
// Each proxy that sessions use becomes an outbound in the instance. Sessions
// using the same proxy with the same settings share it, and it is removed
// when the last of them lets go. Editing a proxy while sessions use it gives
// later sessions a new outbound; the running ones keep the old one until they
// end. Connections are handed straight to their outbound by tag, so Xray's
// routing never decides where anything goes.
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/outbound"
	"github.com/xtls/xray-core/infra/conf"
	"github.com/xtls/xray-core/infra/conf/serial"

	// The parts of Xray the engine uses. infra/conf links most of Xray in
	// any case; these imports make the registrations the engine relies on
	// explicit.
	_ "github.com/xtls/xray-core/app/dispatcher"
	_ "github.com/xtls/xray-core/app/log"
	_ "github.com/xtls/xray-core/app/policy"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	_ "github.com/xtls/xray-core/proxy/blackhole"
	_ "github.com/xtls/xray-core/proxy/http"
	_ "github.com/xtls/xray-core/proxy/socks"
	_ "github.com/xtls/xray-core/transport/internet/tcp"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/route"
)

// connIdleMax is Xray's idle timeout for a connection, set to the largest
// value its configuration takes (about 136 years). The default of 300 seconds
// would cut a remote desktop that sits idle, and nothing in this app wants
// Xray to time connections out. This is one of the exceptions to the
// event-driven rule registered in CLAUDE.md.
const connIdleMax = math.MaxUint32

// blackholeTag is the outbound that takes any connection without a tag.
const blackholeTag = "blackhole"

// baseConfig is the instance before any proxy is added. The blackhole is the
// first outbound, which makes it Xray's default: a connection that somehow
// arrives without a tag goes nowhere instead of out through some proxy.
var baseConfig = fmt.Sprintf(`{
	"log": {"loglevel": "none"},
	"policy": {"levels": {"0": {"connIdle": %d}}},
	"outbounds": [{"protocol": "blackhole", "tag": %q}]
}`, uint32(connIdleMax), blackholeTag)

// ErrClosed is returned by Acquire after Close.
var ErrClosed = errcode.New("app.quitting", "the proxy engine has stopped")

// Options configure the engine.
type Options struct {
	// Log receives Xray's own log lines: errors and warnings, and, while
	// Verbose reports true, also its informational and debug lines. Xray
	// calls both from many goroutines at once; they must be safe for that
	// and return quickly. Optional.
	Log     func(level, msg string)
	Verbose func() bool
}

// Engine is the running Xray instance.
type Engine struct {
	instance  *core.Instance
	outbounds outbound.Manager

	mu     sync.Mutex
	shared map[string]*shared // by proxy ID and a digest of its outbound
	seq    int
	closed bool
}

type shared struct {
	tag  string
	refs int
}

// Start creates and starts the Xray instance. It holds no proxies until
// sessions acquire them.
func Start(opts Options) (*Engine, error) {
	config, err := serial.LoadJSONConfig(strings.NewReader(baseConfig))
	if err != nil {
		return nil, fmt.Errorf("proxy engine: %w", err)
	}
	instance, err := core.New(config)
	if err != nil {
		return nil, fmt.Errorf("proxy engine: %w", err)
	}
	// Creating the instance made Xray's own logger the process-wide one;
	// the bridge replaces it.
	if opts.Log != nil {
		registerLogBridge(opts.Log, opts.Verbose)
	}
	if err := instance.Start(); err != nil {
		instance.Close()
		return nil, fmt.Errorf("proxy engine: %w", err)
	}
	ohm, ok := instance.GetFeature(outbound.ManagerType()).(outbound.Manager)
	if !ok {
		instance.Close()
		return nil, errors.New("proxy engine: Xray has no outbound manager")
	}
	return &Engine{instance: instance, outbounds: ohm, shared: map[string]*shared{}}, nil
}

// Close stops the instance. Routes still held become unusable; releasing
// them afterwards is harmless.
func (e *Engine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.shared = map[string]*shared{}
	e.mu.Unlock()
	return e.instance.Close()
}

// Acquire implements route.Provider. The direct entry needs no engine: it
// is a plain connection from this computer.
func (e *Engine) Acquire(p model.Proxy) (route.Dialer, func(), error) {
	if p.Kind == model.KindDirect {
		return route.Direct(), func() {}, nil
	}
	ob, err := Outbound(p)
	if err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256([]byte(ob))
	key := p.ID + "/" + hex.EncodeToString(sum[:8])

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, nil, ErrClosed
	}
	s := e.shared[key]
	if s == nil {
		e.seq++
		tag := fmt.Sprintf("proxy-%s-%d", p.ID, e.seq)
		if err := e.add(ob, tag); err != nil {
			return nil, nil, err
		}
		s = &shared{tag: tag}
		e.shared[key] = s
	}
	s.refs++
	var once sync.Once
	release := func() { once.Do(func() { e.release(key) }) }
	return &dialer{instance: e.instance, tag: s.tag}, release, nil
}

// Outbounds lists the tags of the outbounds sessions hold, for diagnostics.
func (e *Engine) Outbounds() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	tags := make([]string, 0, len(e.shared))
	for _, s := range e.shared {
		tags = append(tags, s.tag)
	}
	return tags
}

// add builds an outbound from its JSON and adds it under tag.
func (e *Engine) add(outboundJSON, tag string) error {
	var c conf.OutboundDetourConfig
	if err := json.Unmarshal([]byte(outboundJSON), &c); err != nil {
		return fmt.Errorf("the proxy's Xray outbound is not valid: %w", err)
	}
	c.Tag = tag
	built, err := c.Build()
	if err != nil {
		return fmt.Errorf("Xray rejected the proxy's settings: %w", err)
	}
	if err := core.AddOutboundHandler(e.instance, built); err != nil {
		return fmt.Errorf("Xray could not add the proxy: %w", err)
	}
	return nil
}

func (e *Engine) release(key string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.shared[key]
	if s == nil {
		return // the engine has been closed
	}
	s.refs--
	if s.refs > 0 {
		return
	}
	delete(e.shared, key)
	// Removing a handler only forgets it; closing it frees what it holds.
	h := e.outbounds.GetHandler(s.tag)
	_ = e.outbounds.RemoveHandler(context.Background(), s.tag)
	if h != nil {
		_ = h.Close()
	}
}

// Outbound returns the Xray outbound object, as JSON, for proxy p.
func Outbound(p model.Proxy) (string, error) {
	switch p.Kind {
	case model.KindSocks, model.KindHTTP:
		settings := map[string]any{"address": p.Server, "port": p.Port}
		if p.Username != "" {
			settings["user"] = p.Username
			settings["pass"] = p.Secret
		}
		b, err := json.Marshal(map[string]any{"protocol": p.Kind, "settings": settings})
		return string(b), err
	default:
		// The V2Ray-family kinds and custom outbounds arrive in M6.
		return "", fmt.Errorf("%w: %s", route.ErrUnsupported, p.Kind)
	}
}
