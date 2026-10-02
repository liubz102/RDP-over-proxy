// Package xraytest runs Xray-core proxy servers inside the test process, so
// tests can put a real SOCKS5 or HTTP proxy between the engine and a target.
// It is a package of its own so that only the tests that need Xray link it.
package xraytest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"

	// The server side: inbounds, and the outbound that connects to targets.
	_ "github.com/xtls/xray-core/app/dispatcher"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	_ "github.com/xtls/xray-core/proxy/freedom"
	_ "github.com/xtls/xray-core/proxy/http"
	_ "github.com/xtls/xray-core/proxy/socks"
	_ "github.com/xtls/xray-core/transport/internet/tcp"

	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/testutil"
)

// Options configure a proxy server.
type Options struct {
	// Protocol is model.KindSocks or model.KindHTTP.
	Protocol string
	// User and Pass, when User is set, are the only account the server
	// accepts.
	User, Pass string
}

// Proxy is a running proxy server on 127.0.0.1. It connects to targets
// directly, and stops when the test ends.
type Proxy struct {
	Host string
	Port int
}

// Start starts a proxy server for the test.
//
// Note that creating an Xray instance makes its logger the process-wide one,
// replacing the engine's log bridge. Tests that look at the engine's log
// lines must start their proxy servers before the engine.
func Start(t testing.TB, o Options) *Proxy {
	t.Helper()
	port := testutil.FreePort(t)
	settings := map[string]any{}
	accounts := []map[string]string{}
	if o.User != "" {
		accounts = append(accounts, map[string]string{"user": o.User, "pass": o.Pass})
	}
	switch o.Protocol {
	case model.KindSocks:
		settings["auth"] = "noauth"
		if o.User != "" {
			settings["auth"] = "password"
			settings["accounts"] = accounts
		}
	case model.KindHTTP:
		if o.User != "" {
			settings["accounts"] = accounts
		}
	default:
		t.Fatalf("xraytest: unknown protocol %q", o.Protocol)
	}
	config := map[string]any{
		"log": map[string]any{"loglevel": "none"},
		"inbounds": []any{map[string]any{
			"listen": "127.0.0.1", "port": port, "protocol": o.Protocol, "settings": settings,
		}},
		"outbounds": []any{map[string]any{"protocol": "freedom"}},
	}
	text, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := serial.LoadJSONConfig(strings.NewReader(string(text)))
	if err != nil {
		t.Fatalf("xraytest: config: %v", err)
	}
	instance, err := core.New(pb)
	if err != nil {
		t.Fatalf("xraytest: %v", err)
	}
	// Start returns once the inbound is listening.
	if err := instance.Start(); err != nil {
		t.Fatalf("xraytest: start: %v", err)
	}
	t.Cleanup(func() { instance.Close() })
	return &Proxy{Host: "127.0.0.1", Port: port}
}

// Model returns a proxy entry for this server, with the server's account
// when it has one.
func (p *Proxy) Model(id string, o Options) model.Proxy {
	return model.Proxy{
		Schema:   model.ProxySchema,
		ID:       id,
		Name:     "Test " + o.Protocol,
		Kind:     o.Protocol,
		Server:   p.Host,
		Port:     p.Port,
		Username: o.User,
		Secret:   o.Pass,
	}
}
