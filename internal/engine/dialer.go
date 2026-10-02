package engine

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"

	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/core"

	"github.com/liubz102/RDP-over-proxy/internal/model"
)

// dialer connects through one outbound.
type dialer struct {
	instance *core.Instance
	tag      string
}

// DialContext returns at once: Xray connects in the background. If the
// outbound cannot carry the connection, the first Read fails with the reason
// Xray gave. Cancelling ctx ends the connection.
func (d *dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dest, err := destination(network, address)
	if err != nil {
		return nil, err
	}
	t := &tracker{}
	ctx = session.TrackedConnectionError(ctx, t)
	ctx = session.ContextWithContent(ctx, &session.Content{})
	ctx = session.SetForcedOutboundTagToContext(ctx, d.tag)
	conn, err := core.Dial(ctx, d.instance, dest)
	if err != nil {
		return nil, err
	}
	return &trackedConn{Conn: conn, tracker: t}, nil
}

// destination converts "host:port" for Xray, checking everything first:
// Xray panics on some malformed destinations instead of returning an error.
func destination(network, address string) (xnet.Destination, error) {
	if network != "tcp" {
		return xnet.Destination{}, fmt.Errorf("the proxy engine carries TCP only, not %q", network)
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return xnet.Destination{}, err
	}
	if !model.ValidHost(host) {
		return xnet.Destination{}, fmt.Errorf("%q is not a valid host name or IP address", host)
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return xnet.Destination{}, fmt.Errorf("%q is not a valid port", portText)
	}
	return xnet.TCPDestination(xnet.ParseAddress(host), xnet.Port(port)), nil
}

// tracker receives the error Xray reports when an outbound fails a
// connection. Xray submits it before it ends the connection, so it is in
// place by the time a Read sees the end.
type tracker struct {
	mu  sync.Mutex
	err error
}

// SubmitError implements session.TrackedRequestErrorFeedback.
func (t *tracker) SubmitError(err error) {
	t.mu.Lock()
	if t.err == nil {
		t.err = err
	}
	t.mu.Unlock()
}

func (t *tracker) get() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

// trackedConn reports the outbound's error, when there is one, in place of
// the bare end of the stream.
type trackedConn struct {
	net.Conn
	tracker *tracker
}

func (c *trackedConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if err != nil {
		if cause := c.tracker.get(); cause != nil {
			return n, cause
		}
	}
	return n, err
}
