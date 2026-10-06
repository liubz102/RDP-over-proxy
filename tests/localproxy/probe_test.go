package localproxy_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/localproxy"

	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/tests/testutil/xraytest"
)

// serve runs a server on 127.0.0.1 that hands each connection to handle,
// and returns its port.
func serve(t *testing.T, handle func(net.Conn)) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				handle(c)
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// answer reads the greeting, says reply, and reports what came after it
// before the client closed.
func answer(reply []byte, greeting chan<- []byte, after chan<- []byte) func(net.Conn) {
	return func(c net.Conn) {
		g := make([]byte, 4)
		if _, err := io.ReadFull(c, g); err != nil {
			return
		}
		greeting <- g
		_, _ = c.Write(reply)
		rest, _ := io.ReadAll(c)
		after <- rest
	}
}

func TestProbeAsksOnlyForTheMethod(t *testing.T) {
	for _, tc := range []struct {
		reply []byte
		want  Result
	}{
		{[]byte{5, 0}, Result{Socks5: true, Host: "127.0.0.1"}},
		{[]byte{5, 2}, Result{Socks5: true, Password: true, Host: "127.0.0.1"}},
		// None of the methods offered.
		{[]byte{5, 0xff}, Result{Host: "127.0.0.1"}},
		// Another kind of server.
		{[]byte("HTTP/1.1 400 Bad Request\r\n\r\n"), Result{Host: "127.0.0.1"}},
	} {
		greeting, after := make(chan []byte, 1), make(chan []byte, 1)
		port := serve(t, answer(tc.reply, greeting, after))
		got, err := Probe(context.Background(), []string{"127.0.0.1"}, port)
		if err != nil || got != tc.want {
			t.Errorf("reply % x: Probe = %+v, %v; want %+v", tc.reply, got, err, tc.want)
			continue
		}
		if g := <-greeting; !bytes.Equal(g, []byte{5, 2, 0, 2}) {
			t.Errorf("greeting % x, want no authentication and a password offered", g)
		}
		// Nothing follows the greeting: the proxy is never asked to connect.
		if rest := <-after; len(rest) != 0 {
			t.Errorf("reply % x: the client sent % x after the greeting", tc.reply, rest)
		}
	}
}

// Closing, at once or after too short an answer, is an answer too.
func TestProbeOfAServerThatCloses(t *testing.T) {
	for _, reply := range [][]byte{nil, {5}} {
		port := serve(t, func(c net.Conn) { _, _ = c.Write(reply) })
		got, err := Probe(context.Background(), []string{"127.0.0.1"}, port)
		if err != nil || got != (Result{Host: "127.0.0.1"}) {
			t.Errorf("reply % x, then closed: Probe = %+v, %v; want not SOCKS5", reply, got, err)
		}
	}
}

// A server of another kind may wait for more; cancelling ends the wait.
func TestProbeCancelled(t *testing.T) {
	got := make(chan struct{})
	port := serve(t, func(c net.Conn) {
		g := make([]byte, 4)
		if _, err := io.ReadFull(c, g); err == nil {
			close(got)
		}
		_, _ = io.Copy(io.Discard, c) // never answers
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := Probe(ctx, []string{"127.0.0.1"}, port)
		done <- err
	}()
	<-got
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Probe after cancel = %v, want context.Canceled", err)
	}
}

// The next address is tried when one refuses: a socket listening on every
// IPv6 address may be IPv6 only, or take IPv4 too.
func TestProbeTriesTheNextHost(t *testing.T) {
	greeting, after := make(chan []byte, 1), make(chan []byte, 1)
	port := serve(t, answer([]byte{5, 0}, greeting, after))
	got, err := Probe(context.Background(), []string{"::1", "127.0.0.1"}, port)
	if err != nil || got != (Result{Socks5: true, Host: "127.0.0.1"}) {
		t.Fatalf("Probe = %+v, %v", got, err)
	}
}

func TestProbeOfAClosedPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	_, err = Probe(context.Background(), []string{"127.0.0.1"}, port)
	if code := errcode.Of(err); code != errcode.NetRefused {
		t.Fatalf("Probe of a closed port = %v (%s), want %s", err, code, errcode.NetRefused)
	}
}

func TestProbeStaysOnThisComputer(t *testing.T) {
	for _, tc := range []struct {
		hosts []string
		port  int
	}{
		{[]string{"192.0.2.1"}, 1080},
		{[]string{"127.0.0.1", "192.0.2.1"}, 1080},
		{[]string{"localhost"}, 1080}, // names are not resolved
		{nil, 1080},
		{[]string{"127.0.0.1"}, 0},
		{[]string{"127.0.0.1"}, 65536},
	} {
		if _, err := Probe(context.Background(), tc.hosts, tc.port); !errors.Is(err, ErrNotLocal) {
			t.Errorf("Probe(%q, %d) = %v, want ErrNotLocal", tc.hosts, tc.port, err)
		}
	}
}

// Real SOCKS5 servers: Xray's, as v2rayN runs it, with and without an
// account.
func TestProbeOfXray(t *testing.T) {
	open := xraytest.Start(t, xraytest.Options{Protocol: model.KindSocks})
	locked := xraytest.Start(t, xraytest.Options{Protocol: model.KindSocks, User: "alice", Pass: "secret"})
	for _, tc := range []struct {
		px   *xraytest.Proxy
		want Result
	}{
		{open, Result{Socks5: true, Host: "127.0.0.1"}},
		{locked, Result{Socks5: true, Password: true, Host: "127.0.0.1"}},
	} {
		got, err := Probe(context.Background(), []string{tc.px.Host}, tc.px.Port)
		if err != nil || got != tc.want {
			t.Errorf("Probe(%s:%d) = %+v, %v; want %+v", tc.px.Host, tc.px.Port, got, err, tc.want)
		}
	}
}
