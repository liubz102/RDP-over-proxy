// Package tunnel is a profile's entrance on the loopback network. It accepts
// mstsc's connections on 127.a.b.c:port and carries each one to the target
// through a route, counting the bytes and reporting what it learns about the
// way to the target.
//
// When either direction of a connection ends, both are closed. RDP never
// half-closes a connection, and keeping one direction open after the other
// ended would need a timeout to clean up after peers that never finish.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"

	"github.com/liubz102/RDP-over-proxy/internal/route"
)

// Reporter receives what the tunnel observes. Its methods are called from the
// tunnel's goroutines, possibly at the same time, and must return promptly
// without waiting on anything: Close waits for those goroutines.
type Reporter interface {
	// ConnOpened: mstsc opened a connection to the entrance.
	ConnOpened()
	// ConnClosed: a connection ended. It is the last report about it.
	ConnClosed()
	// UpstreamAnswered: the first bytes of a connection came back from the
	// target.
	UpstreamAnswered()
	// UpstreamFailed: a connection could not reach the target, or the route
	// closed it before the target answered.
	UpstreamFailed(err error)
	// ListenerFailed: the entrance stopped accepting connections for a
	// reason other than Close. Connections already open carry on.
	ListenerFailed(err error)
}

// ErrNoAnswer is what UpstreamFailed reports when the route closed a
// connection before the target sent anything back.
var ErrNoAnswer = errors.New("the connection closed before the target answered")

// copyBuffer is the size of each direction's copy buffer.
const copyBuffer = 32 << 10

// Tunnel is an open entrance.
type Tunnel struct {
	ln     net.Listener
	target string
	dialer route.Dialer
	report Reporter

	// ctx ends when Close is called, which aborts dials in flight.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	sent, received atomic.Int64

	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
}

// Listen opens the entrance at addr; mstsc can connect as soon as it
// returns. Each connection is carried to target ("host:port") through d.
func Listen(addr netip.AddrPort, target string, d route.Dialer, r Reporter) (*Tunnel, error) {
	ln, err := net.Listen("tcp", addr.String())
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	t := &Tunnel{
		ln:     ln,
		target: target,
		dialer: d,
		report: r,
		ctx:    ctx,
		cancel: cancel,
		conns:  map[net.Conn]struct{}{},
	}
	t.wg.Go(t.accept)
	return t, nil
}

// Addr is the address mstsc connects to.
func (t *Tunnel) Addr() netip.AddrPort {
	return t.ln.Addr().(*net.TCPAddr).AddrPort()
}

// Bytes returns how many bytes have gone to the target and come back from it
// so far, over all connections.
func (t *Tunnel) Bytes() (sent, received int64) {
	return t.sent.Load(), t.received.Load()
}

// Close stops accepting, ends every connection and waits until all of the
// tunnel's goroutines have returned. Calling it again does nothing more.
func (t *Tunnel) Close() {
	t.mu.Lock()
	t.closed = true
	conns := make([]net.Conn, 0, len(t.conns))
	for c := range t.conns {
		conns = append(conns, c)
	}
	t.mu.Unlock()

	t.cancel()
	t.ln.Close()
	for _, c := range conns {
		c.Close()
	}
	t.wg.Wait()
}

func (t *Tunnel) accept() {
	for {
		c, err := t.ln.Accept()
		if err != nil {
			if !t.isClosed() {
				t.report.ListenerFailed(err)
			}
			return
		}
		if !t.track(c) {
			c.Close()
			return
		}
		t.report.ConnOpened()
		t.wg.Go(func() { t.serve(c) })
	}
}

// serve carries one connection from mstsc to the target and back.
func (t *Tunnel) serve(client net.Conn) {
	defer t.report.ConnClosed()
	defer t.untrack(client)

	up, err := t.dialer.DialContext(t.ctx, "tcp", t.target)
	if err != nil {
		if t.ctx.Err() == nil { // not just the tunnel closing
			t.report.UpstreamFailed(err)
		}
		return
	}
	if !t.track(up) {
		up.Close()
		return
	}
	defer t.untrack(up)

	// The first direction to end closes both and records which one it was.
	var once sync.Once
	clientEnded := false
	end := func(byClient bool) {
		once.Do(func() {
			clientEnded = byClient
			client.Close()
			up.Close()
		})
	}

	toTarget := make(chan struct{})
	go func() {
		defer close(toTarget)
		_, _ = pump(up, client, &t.sent, nil)
		end(true)
	}()
	n, err := pump(client, up, &t.received, t.report.UpstreamAnswered)
	end(false)
	<-toTarget

	// The target never answered, and neither mstsc nor Close ended the
	// connection: the route gave up on it.
	if n == 0 && !clientEnded && t.ctx.Err() == nil {
		if err == nil {
			err = ErrNoAnswer
		} else {
			err = fmt.Errorf("%w (%w)", ErrNoAnswer, err)
		}
		t.report.UpstreamFailed(err)
	}
}

// pump copies src to dst until either side fails or src ends, adding to
// counter as it goes. first, if not nil, is called once, when the first
// bytes have been read. A clean end of src returns a nil error.
func pump(dst io.Writer, src io.Reader, counter *atomic.Int64, first func()) (int64, error) {
	buf := make([]byte, copyBuffer)
	var total int64
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if total == 0 && first != nil {
				first()
			}
			total += int64(n)
			counter.Add(int64(n))
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return total, werr
			}
		}
		if rerr == io.EOF {
			return total, nil
		}
		if rerr != nil {
			return total, rerr
		}
	}
}

// track registers a connection so Close can end it; it refuses once the
// tunnel is closing.
func (t *Tunnel) track(c net.Conn) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return false
	}
	t.conns[c] = struct{}{}
	return true
}

func (t *Tunnel) untrack(c net.Conn) {
	c.Close()
	t.mu.Lock()
	delete(t.conns, c)
	t.mu.Unlock()
}

func (t *Tunnel) isClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}
