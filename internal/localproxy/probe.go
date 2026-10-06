package localproxy

import (
	"context"
	"io"
	"net"
	"net/netip"
	"strconv"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
)

// ErrNotLocal: Probe was asked about an address that is not a loopback
// address of this computer.
var ErrNotLocal = errcode.New("localproxy.notLocal", "only ports on this computer's loopback addresses are probed")

// Result is how a port answered a SOCKS5 greeting.
type Result struct {
	// Socks5: it is a SOCKS5 server the app can use, one that lets anyone
	// in or one that wants a user name and password (Password).
	Socks5   bool `json:"socks5"`
	Password bool `json:"password"`
	// Host is the address that took the connection, one of those asked.
	Host string `json:"host"`
}

// The SOCKS5 greeting (RFC 1928): version 5, two methods, no
// authentication and user name and password (RFC 1929).
var greeting = []byte{5, 2, methodNone, methodPassword}

const (
	methodNone     = 0x00
	methodPassword = 0x02
)

// Probe greets the port at the first of hosts that takes the connection,
// as a SOCKS5 client would, and reads the method the server picks. It goes
// no further: the connection closes before any request, so a proxy
// connects nowhere. A port that forwards everything to a fixed target
// (Xray's dokodemo-door, a Clash tunnel) passes the greeting on, and that
// target sees four stray bytes. Only loopback addresses are probed
// (ErrNotLocal).
//
// A server of another kind may wait for more before it says anything, so
// there is no telling how long an answer takes, and there is no timeout.
// Cancel ctx to stop Probe; it then returns ctx's error.
func Probe(ctx context.Context, hosts []string, port int) (Result, error) {
	if len(hosts) == 0 || port < 1 || port > 65535 {
		return Result{}, ErrNotLocal
	}
	for _, h := range hosts {
		if a, err := netip.ParseAddr(h); err != nil || !a.IsLoopback() {
			return Result{}, ErrNotLocal
		}
	}
	var err error
	for _, h := range hosts {
		var r Result
		if r, err = probe(ctx, h, port); err == nil {
			return r, nil
		}
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		// Refused: the socket does not listen on this address; try the next.
	}
	return Result{}, err
}

// probe greets the port at one address. It fails only when it could not
// connect; whatever the server does after that is its answer.
func probe(ctx context.Context, host string, port int) (Result, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return Result{}, err
	}
	defer conn.Close()
	// Cancelling ctx closes the connection, which ends a blocked write or
	// read at once.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	r := Result{Host: host}
	var reply [2]byte
	if _, err := conn.Write(greeting); err != nil {
		return r, ctx.Err()
	}
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		// Closed, or too short an answer: not a SOCKS5 server.
		return r, ctx.Err()
	}
	if reply[0] == 5 {
		switch reply[1] {
		case methodNone:
			r.Socks5 = true
		case methodPassword:
			r.Socks5, r.Password = true, true
		}
	}
	return r, nil
}
