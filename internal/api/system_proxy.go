package api

import (
	"context"
	"net"
	"net/url"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/liubz102/RDP-over-proxy/internal/logging"
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/session"
	"github.com/liubz102/RDP-over-proxy/internal/store"
	"github.com/liubz102/RDP-over-proxy/internal/sysproxy"
)

// The built-in entry that follows Windows' proxy setting (model.KindSystem)
// is turned into the proxy that setting names for each target, or direct,
// right before a route is taken: by the session in its route step
// (Core.resolveProxy), and by the route check and the latency test. Running
// Windows' automatic configuration can take as long as the network does, so
// each of them can be cancelled meanwhile.

// EventSystemProxyChanged carries a SystemProxyView whenever Windows' proxy
// setting changes.
const EventSystemProxyChanged = "systemProxy:changed"

func init() {
	application.RegisterEvent[SystemProxyView](EventSystemProxyChanged)
}

// Session log keys of following the system proxy.
const (
	// MsgSystemManual: the manual proxy server of Windows' setting is used.
	MsgSystemManual = "session.systemManual"
	// MsgSystemConfig: the automatic configuration names a proxy server.
	MsgSystemConfig = "session.systemConfig"
	// MsgSystemConfigDirect: the automatic configuration says to connect
	// directly.
	MsgSystemConfigDirect = "session.systemConfigDirect"
	// MsgSystemBypass: the target is one of the manual proxy's exceptions.
	MsgSystemBypass = "session.systemBypass"
	// MsgSystemNone: Windows has no proxy set.
	MsgSystemNone = "session.systemNone"
	// MsgSystemConfigFailed: the automatic configuration gave no answer, so
	// the manual setting decided.
	MsgSystemConfigFailed = "session.systemConfigFailed"
)

// SystemProxyView is Windows' proxy setting now, for the entry that follows
// it.
type SystemProxyView struct {
	Settings sysproxy.Settings `json:"settings"`
	// Manual is the server of the manual setting a connection goes through
	// (sysproxy.Pick), when one is set.
	Manual *sysproxy.Server `json:"manual"`
	// Error is why the setting could not be read.
	Error string `json:"error,omitempty"`
}

// RouteView is how a connection goes when it follows Windows' proxy
// setting: directly, or through a proxy server.
type RouteView struct {
	// Kind is model.KindDirect, model.KindHTTP or model.KindSocks.
	Kind   string `json:"kind"`
	Server string `json:"server,omitempty"`
	Port   int    `json:"port,omitempty"`
	// By says how the setting decided (sysproxy.By*).
	By string `json:"by"`
	// ConfigError is why the automatic configuration gave no answer, when
	// it was asked; ConfigCode is its errcode code.
	ConfigError string `json:"configError,omitempty"`
	ConfigCode  string `json:"configCode,omitempty"`
}

// systemWatch is what Core keeps of Windows' proxy setting.
type systemWatch struct {
	mu    sync.Mutex
	last  SystemProxyView // the view last sent, once known
	known bool
	stop  func()
}

// systemProxyView reads Windows' proxy setting now.
func (c *Core) systemProxyView() SystemProxyView {
	if c.d.SystemProxy == nil {
		return SystemProxyView{Error: sysproxy.ErrUnreadable.Error()}
	}
	s, err := c.d.SystemProxy.Settings()
	if err != nil {
		return SystemProxyView{Error: err.Error()}
	}
	v := SystemProxyView{Settings: s}
	if srv, ok := sysproxy.Pick(s.Proxy); ok {
		v.Manual = &srv
	}
	return v
}

// watchSystemProxy starts telling the frontend when Windows' proxy setting
// changes (EventSystemProxyChanged). The registry says when something under
// the Internet settings changed; only a change of the proxy setting is sent.
func (c *Core) watchSystemProxy() {
	if c.d.WatchSystemProxy == nil {
		return
	}
	w := &c.system
	stop, err := c.d.WatchSystemProxy(func() {
		v := c.systemProxyView()
		w.mu.Lock()
		same := w.known && sameSystemView(w.last, v)
		w.last, w.known = v, true
		w.mu.Unlock()
		if !same {
			c.d.Emit(EventSystemProxyChanged, v)
		}
	})
	if err != nil {
		c.d.Log.Warnf("watch Windows' proxy setting: %v", err)
		return
	}
	// Read once the watch is on, so no change goes unseen. A change seen
	// meanwhile was read later than this, and stands.
	v := c.systemProxyView()
	w.mu.Lock()
	if !w.known {
		w.last, w.known = v, true
	}
	w.stop = stop
	w.mu.Unlock()
}

// stopSystemWatch ends watchSystemProxy.
func (c *Core) stopSystemWatch() {
	w := &c.system
	w.mu.Lock()
	stop := w.stop
	w.stop = nil
	w.mu.Unlock()
	if stop != nil {
		stop()
	}
}

func sameSystemView(a, b SystemProxyView) bool {
	if a.Settings != b.Settings || a.Error != b.Error || (a.Manual == nil) != (b.Manual == nil) {
		return false
	}
	return a.Manual == nil || *a.Manual == *b.Manual
}

// decide asks how Windows' proxy setting takes a connection to target
// ("host:port"). The proxy server it names is masked in the log file like
// the user's own proxies.
func (c *Core) decide(ctx context.Context, target string) (sysproxy.Decision, error) {
	if c.d.SystemProxy == nil {
		return sysproxy.Decision{}, sysproxy.ErrUnreadable
	}
	d, err := sysproxy.Decide(ctx, c.d.SystemProxy, target)
	if err == nil && d.Server != nil {
		c.mask(d.Server.Host)
	}
	return d, err
}

// systemRoute is the proxy a connection to target goes through when it
// follows Windows' proxy setting: the server the setting names, or the
// direct entry.
func (c *Core) systemRoute(ctx context.Context, target string) (model.Proxy, sysproxy.Decision, error) {
	d, err := c.decide(ctx, target)
	if err != nil {
		return model.Proxy{}, d, err
	}
	if d.Server == nil {
		return model.DirectProxy(), d, nil
	}
	return serverProxy(*d.Server), d, nil
}

// serverProxy is a proxy server Windows' setting names, as a proxy the
// engine can take. The engine keys its outbounds by ID and settings, so
// each server gets its own.
func serverProxy(s sysproxy.Server) model.Proxy {
	p := model.Proxy{Schema: model.ProxySchema, ID: model.SystemProxyID, Name: model.SystemProxyID,
		Kind: s.Kind, Server: s.Host, Port: s.Port}
	return p.Normalize()
}

// viaProxy is the proxy a route the UI was shown (SystemRoute) goes
// through.
func viaProxy(v RouteView) (model.Proxy, error) {
	switch v.Kind {
	case model.KindDirect:
		return model.DirectProxy(), nil
	case model.KindHTTP, model.KindSocks:
		p := serverProxy(sysproxy.Server{Kind: v.Kind, Host: v.Server, Port: v.Port})
		return p, p.Validate()
	}
	return model.Proxy{}, model.FieldErrors{{Field: "kind", Code: model.CodeUnsupported}}
}

// resolveProxy is the session's Options.Resolve: a profile that follows
// Windows' proxy setting goes the way it says for the profile's target,
// which the session log tells.
func (c *Core) resolveProxy(ctx context.Context, req session.Request) (model.Proxy, error) {
	if req.Proxy.Kind != model.KindSystem {
		return req.Proxy, nil
	}
	p, d, err := c.systemRoute(ctx, req.Profile.Target.String())
	if err != nil {
		return model.Proxy{}, err
	}
	id := req.Profile.ID
	if d.ConfigError != "" {
		c.warnSession(id, MsgSystemConfigFailed, map[string]any{"error": d.ConfigError, "code": d.ConfigCode})
	}
	var msg string
	args := map[string]any{}
	switch {
	case d.Server != nil && d.By == sysproxy.ByConfig:
		msg = MsgSystemConfig
	case d.Server != nil:
		msg = MsgSystemManual
	case d.By == sysproxy.ByConfig:
		msg = MsgSystemConfigDirect
	case d.By == sysproxy.ByBypass:
		msg = MsgSystemBypass
	default:
		msg = MsgSystemNone
	}
	if d.Server != nil {
		args["server"] = d.Server.Address()
		args["kind"] = protocolName(d.Server.Kind)
	}
	c.addSessionLine(id, logging.Line{Level: logging.LevelInfo, Msg: msg, Args: args}, false)
	return p, nil
}

// protocolName is how a proxy kind is written for people: the same in
// every language.
func protocolName(kind string) string {
	if kind == model.KindSocks {
		return "SOCKS5"
	}
	return "HTTP"
}

// routeView describes a decision for the UI.
func routeView(d sysproxy.Decision) RouteView {
	v := RouteView{Kind: model.KindDirect, By: d.By, ConfigError: d.ConfigError, ConfigCode: d.ConfigCode}
	if d.Server != nil {
		v.Kind, v.Server, v.Port = d.Server.Kind, d.Server.Host, d.Server.Port
	}
	return v
}

// urlTarget is the host:port a URL connects to.
func urlTarget(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	return net.JoinHostPort(u.Hostname(), port), nil
}

// SystemProxy returns Windows' proxy setting now, for the entry that
// follows it. Later changes come as EventSystemProxyChanged.
func (s *ProxyService) SystemProxy() SystemProxyView {
	return s.c.systemProxyView()
}

// SystemRoute says how Windows' proxy setting takes the profile's
// connection now. It is for profiles that follow the setting; the route
// check shows it, then checks that way (CheckRouteVia). Running the
// automatic configuration, when one is set, takes as long as the network
// does; the frontend cancels the call to stop it.
func (s *SessionService) SystemRoute(ctx context.Context, profileID string) (RouteView, error) {
	p, ok := s.c.d.Data.Profile(profileID)
	if !ok {
		return RouteView{}, store.ErrNotFound
	}
	_, d, err := s.c.systemRoute(ctx, p.Target.String())
	if err != nil {
		return RouteView{}, err
	}
	return routeView(d), nil
}

// CheckRouteVia is CheckRoute along the way SystemRoute said Windows' proxy
// setting takes the profile's connection. The route check shows that way
// and checks it, rather than asking Windows again, which could take as long
// once more and answer differently.
func (s *SessionService) CheckRouteVia(ctx context.Context, profileID string, via RouteView) (CheckView, error) {
	p, ok := s.c.d.Data.Profile(profileID)
	if !ok {
		return CheckView{}, store.ErrNotFound
	}
	px, err := viaProxy(via)
	if err != nil {
		return CheckView{}, err
	}
	return s.check(ctx, p, px)
}
