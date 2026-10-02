// Package route is how a tunnel reaches its target: directly, or through the
// embedded Xray engine (package engine, which is the Provider the app uses).
// A session acquires a route when it starts and releases it when it ends, so
// the engine can keep one outbound per proxy for as long as some session uses
// it.
package route

import (
	"context"
	"net"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/model"
)

// Dialer opens connections to the target. *net.Dialer is one.
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Provider hands out routes for proxies.
type Provider interface {
	// Acquire returns the route through p and a function that releases it.
	// Release is called exactly once, after every connection that used the
	// route has closed.
	Acquire(p model.Proxy) (d Dialer, release func(), err error)
}

// ErrUnsupported is returned for a proxy kind this build cannot use yet.
var ErrUnsupported = errcode.New("proxy.unsupported", "this kind of proxy is not supported yet")

// Direct is the route for model.KindDirect: a plain TCP connection from this
// computer. Go enables TCP keep-alive on it, so a dead peer is noticed even
// while the remote desktop is idle.
func Direct() Dialer { return &net.Dialer{} }
