package engine_test

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/engine"
	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/loopback"
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/probe"
	"github.com/liubz102/RDP-over-proxy/internal/route"
	"github.com/liubz102/RDP-over-proxy/internal/session"
	"github.com/liubz102/RDP-over-proxy/internal/tunnel"
	"github.com/liubz102/RDP-over-proxy/tests/testutil"
	"github.com/liubz102/RDP-over-proxy/tests/testutil/xraytest"
)

func startEngine(t *testing.T, opts engine.Options) *engine.Engine {
	t.Helper()
	e, err := engine.Start(opts)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { e.Close() })
	return e
}

func acquire(t *testing.T, e *engine.Engine, p model.Proxy) route.Dialer {
	t.Helper()
	d, release, err := e.Acquire(p)
	if err != nil {
		t.Fatalf("Acquire(%s): %v", p.Kind, err)
	}
	t.Cleanup(release)
	return d
}

func TestRoutesThroughEachKindOfProxy(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm, Selected: probe.ProtocolHybridEx})
	cases := []xraytest.Options{
		{Protocol: model.KindSocks},
		{Protocol: model.KindSocks, User: "alice", Pass: "s3cret pass"},
		{Protocol: model.KindHTTP},
		{Protocol: model.KindHTTP, User: "bob", Pass: "p@ss:word"},
	}
	e := startEngine(t, engine.Options{})
	for i, o := range cases {
		px := xraytest.Start(t, o)
		d := acquire(t, e, px.Model("p"+strconv.Itoa(i), o))
		res, err := probe.Check(t.Context(), d, srv.Addr)
		if err != nil {
			t.Fatalf("%s (user %q): Check: %v", o.Protocol, o.User, err)
		}
		if res.Confirm.Selected != probe.ProtocolHybridEx {
			t.Fatalf("%s: confirm %+v", o.Protocol, res.Confirm)
		}
		if req := <-srv.Requests(); req.Protocols != probe.Requested {
			t.Fatalf("%s: the target received %+v", o.Protocol, req)
		}
	}
}

func TestOutboundFailuresCarryXraysReason(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	socks := xraytest.Options{Protocol: model.KindSocks, User: "alice", Pass: "right"}
	httpo := xraytest.Options{Protocol: model.KindHTTP, User: "alice", Pass: "right"}
	socksProxy := xraytest.Start(t, socks)
	httpProxy := xraytest.Start(t, httpo)
	e := startEngine(t, engine.Options{})

	wrongSocks := socksProxy.Model("ws", socks)
	wrongSocks.Secret = "wrong"
	wrongHTTP := httpProxy.Model("wh", httpo)
	wrongHTTP.Secret = "wrong"
	down := model.Proxy{ID: "down", Name: "Down", Kind: model.KindSocks, Server: "127.0.0.1", Port: testutil.FreePort(t)}
	downHTTP := model.Proxy{ID: "downh", Name: "Down", Kind: model.KindHTTP, Server: "127.0.0.1", Port: testutil.FreePort(t)}

	cases := []struct {
		proxy model.Proxy
		code  string
	}{
		{wrongSocks, engine.CodeProxyAuth},
		{wrongHTTP, engine.CodeProxyAuth},
		{down, engine.CodeProxyUnreachable},
		{downHTTP, engine.CodeProxyUnreachable},
	}
	for _, c := range cases {
		p := c.proxy
		_, err := probe.Check(t.Context(), acquire(t, e, p), srv.Addr)
		if !errors.Is(err, probe.ErrNoAnswer) {
			t.Fatalf("%s: err = %v, want ErrNoAnswer", p.ID, err)
		}
		// The bare end of the stream would only say "EOF"; the engine passes
		// on what Xray knows.
		if msg := err.Error(); !strings.Contains(msg, "outbound") || strings.HasSuffix(msg, "(EOF)") {
			t.Fatalf("%s: err = %q, want Xray's reason", p.ID, msg)
		}
		if got := errcode.Of(err); got != c.code {
			t.Fatalf("%s: code %q, want %q (%v)", p.ID, got, c.code, err)
		}
		t.Logf("%s: %v", p.ID, err)
	}
}

func TestProxyThatCannotReachTheTarget(t *testing.T) {
	// A target port nothing listens on: the proxy is fine, the target is not.
	target := "127.0.0.1:" + strconv.Itoa(testutil.FreePort(t))
	e := startEngine(t, engine.Options{})

	// Xray's own SOCKS and HTTP servers (v2rayN's local port is one) accept
	// the request at once and drop the connection when the target turns out
	// to be unreachable.
	for i, o := range []xraytest.Options{{Protocol: model.KindSocks}, {Protocol: model.KindHTTP}} {
		px := xraytest.Start(t, o)
		_, err := probe.Check(t.Context(), acquire(t, e, px.Model("x"+strconv.Itoa(i), o)), target)
		if got := errcode.Of(err); got != engine.CodeProxyDropped {
			t.Fatalf("Xray %s server: code %q, want %q (%v)", o.Protocol, got, engine.CodeProxyDropped, err)
		}
	}

	// Other servers answer the request with an error.
	refusing := []model.Proxy{
		{ID: "rs", Name: "Refusing SOCKS", Kind: model.KindSocks, Server: "127.0.0.1", Port: refusingProxy(t, socksRefusal)},
		{ID: "rh", Name: "Refusing HTTP", Kind: model.KindHTTP, Server: "127.0.0.1", Port: refusingProxy(t, httpRefusal)},
	}
	for _, p := range refusing {
		_, err := probe.Check(t.Context(), acquire(t, e, p), target)
		if got := errcode.Of(err); got != engine.CodeProxyTargetFailed {
			t.Fatalf("%s: code %q, want %q (%v)", p.Name, got, engine.CodeProxyTargetFailed, err)
		}
	}
}

// refusingProxy runs a proxy server that turns down every CONNECT with the
// given handler, and returns its port.
func refusingProxy(t *testing.T, serve func(net.Conn)) int {
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
				serve(c)
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// socksRefusal speaks SOCKS5 without authentication and answers the CONNECT
// with reply 5, "connection refused" (RFC 1928).
func socksRefusal(c net.Conn) {
	hello := make([]byte, 2)
	if _, err := io.ReadFull(c, hello); err != nil {
		return
	}
	if _, err := io.ReadFull(c, make([]byte, hello[1])); err != nil {
		return
	}
	if _, err := c.Write([]byte{5, 0}); err != nil {
		return
	}
	head := make([]byte, 4) // VER CMD RSV ATYP
	if _, err := io.ReadFull(c, head); err != nil {
		return
	}
	var addrLen int
	switch head[3] {
	case 1:
		addrLen = 4
	case 4:
		addrLen = 16
	case 3:
		n := make([]byte, 1)
		if _, err := io.ReadFull(c, n); err != nil {
			return
		}
		addrLen = int(n[0])
	}
	if _, err := io.ReadFull(c, make([]byte, addrLen+2)); err != nil {
		return
	}
	_, _ = c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
	_, _ = io.Copy(io.Discard, c)
}

// httpRefusal answers a CONNECT with 502 Bad Gateway.
func httpRefusal(c net.Conn) {
	req, err := http.ReadRequest(bufio.NewReader(c))
	if err != nil {
		return
	}
	req.Body.Close()
	_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
	_, _ = io.Copy(io.Discard, c)
}

func TestOutboundsAreSharedAndReleased(t *testing.T) {
	o := xraytest.Options{Protocol: model.KindSocks}
	px := xraytest.Start(t, o)
	e := startEngine(t, engine.Options{})
	p := px.Model("shared", o)

	_, release1, err := e.Acquire(p)
	if err != nil {
		t.Fatal(err)
	}
	_, release2, err := e.Acquire(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.Outbounds(); len(got) != 1 {
		t.Fatalf("two sessions on one proxy hold %d outbounds: %q", len(got), got)
	}

	// The proxy is edited while both sessions use it: a new session gets
	// the new settings, the running ones keep theirs.
	edited := p
	edited.Port = testutil.FreePort(t)
	_, release3, err := e.Acquire(edited)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.Outbounds(); len(got) != 2 {
		t.Fatalf("after editing the proxy: %d outbounds %q, want 2", len(got), got)
	}

	release1()
	release1() // releasing twice counts once
	if got := e.Outbounds(); len(got) != 2 {
		t.Fatalf("one of two users released: %d outbounds %q, want 2", len(got), got)
	}
	release2()
	release3()
	if got := e.Outbounds(); len(got) != 0 {
		t.Fatalf("everything released, still %q", got)
	}
}

func TestDirectNeedsNoOutbound(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	e := startEngine(t, engine.Options{})
	d := acquire(t, e, model.DirectProxy())
	if _, err := probe.Check(t.Context(), d, srv.Addr); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got := e.Outbounds(); len(got) != 0 {
		t.Fatalf("the direct route added outbounds %q", got)
	}
}

func TestKindsOfLaterMilestonesAreRefused(t *testing.T) {
	e := startEngine(t, engine.Options{})
	p := model.Proxy{ID: "v", Name: "VMess", Kind: model.KindVMess, Server: "192.0.2.1", Port: 443,
		Outbound: `{"protocol":"vmess"}`}
	if _, _, err := e.Acquire(p); !errors.Is(err, route.ErrUnsupported) {
		t.Fatalf("Acquire(vmess) = %v, want ErrUnsupported", err)
	}
}

func TestClose(t *testing.T) {
	o := xraytest.Options{Protocol: model.KindSocks}
	px := xraytest.Start(t, o)
	e, err := engine.Start(engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, release, err := e.Acquire(px.Model("c", o))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	release() // harmless after Close
	if _, _, err := e.Acquire(px.Model("c", o)); !errors.Is(err, engine.ErrClosed) {
		t.Fatalf("Acquire after Close = %v, want ErrClosed", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("a second Close: %v", err)
	}
}

func TestDialChecksTheAddressFirst(t *testing.T) {
	o := xraytest.Options{Protocol: model.KindSocks}
	px := xraytest.Start(t, o)
	e := startEngine(t, engine.Options{})
	d := acquire(t, e, px.Model("bad", o))
	cases := []struct{ network, address string }{
		{"udp", "192.0.2.1:3389"},
		{"tcp", "192.0.2.1"},
		{"tcp", "bad host:3389"},
		{"tcp", ":3389"},
		{"tcp", "192.0.2.1:0"},
		{"tcp", "192.0.2.1:70000"},
	}
	for _, c := range cases {
		// Xray would panic on some of these; the dialer must refuse them.
		if conn, err := d.DialContext(t.Context(), c.network, c.address); err == nil {
			conn.Close()
			t.Errorf("DialContext(%q, %q) succeeded", c.network, c.address)
		}
	}
}

func TestLatencyThroughTheEngine(t *testing.T) {
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(web.Close)
	o := xraytest.Options{Protocol: model.KindHTTP}
	px := xraytest.Start(t, o)
	e := startEngine(t, engine.Options{})
	if _, err := probe.Latency(t.Context(), acquire(t, e, px.Model("lat", o)), web.URL+"/generate_204"); err != nil {
		t.Fatalf("Latency: %v", err)
	}
	down := model.Proxy{ID: "down", Name: "Down", Kind: model.KindHTTP, Server: "127.0.0.1", Port: testutil.FreePort(t)}
	if _, err := probe.Latency(t.Context(), acquire(t, e, down), web.URL+"/generate_204"); err == nil {
		t.Fatal("Latency through a proxy that is not running succeeded")
	}
}

func TestLogBridge(t *testing.T) {
	// Proxy servers first: creating an Xray instance takes over the
	// process-wide logger, and the engine must be the last to do so.
	o := xraytest.Options{Protocol: model.KindSocks, User: "alice", Pass: "right"}
	px := xraytest.Start(t, o)
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	lines := make(chan string, 1024)
	e := startEngine(t, engine.Options{Verbose: func() bool { return true }, Log: func(level, msg string) {
		select {
		case lines <- level + " " + msg:
		default:
		}
	}})
	wrong := px.Model("log", o)
	wrong.Secret = "wrong"
	if _, err := probe.Check(t.Context(), acquire(t, e, wrong), srv.Addr); err == nil {
		t.Fatal("Check with a wrong password succeeded")
	}
	// Xray logs the failure before it ends the connection, so the line is
	// already here by the time Check has returned.
	var seen []string
	for {
		select {
		case line := <-lines:
			if strings.Contains(line, "failed to process outbound traffic") {
				return
			}
			seen = append(seen, line)
		default:
			t.Fatalf("no line about the failed outbound; got %q", seen)
		}
	}
}

// reporter collects tunnel reports.
type reporter struct {
	mu       sync.Mutex
	reports  []string
	answered chan struct{}
	once     sync.Once
}

func (r *reporter) add(s string) { r.mu.Lock(); r.reports = append(r.reports, s); r.mu.Unlock() }
func (r *reporter) ConnOpened()  { r.add("opened") }
func (r *reporter) ConnClosed()  { r.add("closed") }
func (r *reporter) UpstreamAnswered() {
	r.add("answered")
	r.once.Do(func() { close(r.answered) })
}
func (r *reporter) UpstreamFailed(err error) { r.add("failed: " + err.Error()) }
func (r *reporter) ListenerFailed(err error) { r.add("listener: " + err.Error()) }

func TestTunnelThroughTheEngine(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	o := xraytest.Options{Protocol: model.KindSocks}
	px := xraytest.Start(t, o)
	e := startEngine(t, engine.Options{})
	rep := &reporter{answered: make(chan struct{})}
	tun, err := tunnel.Listen(netip.MustParseAddrPort("127.201.202.204:0"), srv.Addr, acquire(t, e, px.Model("tun", o)), rep)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tun.Close)

	c, err := net.Dial("tcp", tun.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	req, _ := probe.ConnectionRequest{Cookie: "mstshash=alice", Negotiate: true, Protocols: probe.Requested}.Marshal()
	if _, err := c.Write(req); err != nil {
		t.Fatal(err)
	}
	if _, err := probe.ReadTPKT(c); err != nil {
		t.Fatalf("no confirm through tunnel and engine: %v", err)
	}
	<-rep.answered
	payload := []byte(strings.Repeat("remote desktop ", 5000))
	go func() { _, _ = c.Write(payload) }()
	echo := make([]byte, len(payload))
	if _, err := io.ReadFull(c, echo); err != nil || string(echo) != string(payload) {
		t.Fatalf("echo through tunnel and engine: %v", err)
	}
	c.Close()
	tun.Close() // waits for the connection's goroutines
	rep.mu.Lock()
	defer rep.mu.Unlock()
	if !slices.Equal(rep.reports, []string{"opened", "answered", "closed"}) {
		t.Fatalf("reports = %q", rep.reports)
	}
}

// fakeMstsc stands in for mstsc in the session test.
type fakeMstsc struct {
	exit chan int
	once sync.Once
}

func (p *fakeMstsc) PID() int             { return 4242 }
func (p *fakeMstsc) Wait() (int, error)   { return <-p.exit, nil }
func (p *fakeMstsc) Close() (bool, error) { p.once.Do(func() { p.exit <- 0 }); return true, nil }
func (p *fakeMstsc) Kill() error          { p.once.Do(func() { p.exit <- 1 }); return nil }
func (p *fakeMstsc) Focus() error         { return nil }

func TestSessionThroughSocks(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	o := xraytest.Options{Protocol: model.KindSocks, User: "alice", Pass: "pw"}
	px := xraytest.Start(t, o)
	e := startEngine(t, engine.Options{})

	states := make(chan session.State, 64)
	launched := make(chan *fakeMstsc, 1)
	m := session.NewManager(session.Options{
		Routes: e,
		Launch: func([]string) (session.Process, error) {
			p := &fakeMstsc{exit: make(chan int, 1)}
			launched <- p
			return p, nil
		},
		Changed: func(_ string, s session.State) { states <- s },
	})
	t.Cleanup(m.Quit)

	ap := netip.MustParseAddrPort(srv.Addr)
	proxy := px.Model("socksproxy", o)
	p := model.DefaultProfile()
	p.ID, p.Name, p.ProxyID = "viasocks", "Via SOCKS", proxy.ID
	p.Target = model.Target{Host: ap.Addr().String(), Port: int(ap.Port())}
	p.Loopback = loopback.Derive(p.ID).String()
	if _, err := m.Connect(session.Request{Profile: p, Proxy: proxy, CheckFirst: true}); err != nil {
		t.Fatal(err)
	}
	mstsc := <-launched
	var running session.State
	for running = range states {
		if running.Phase() == session.PhaseRunning {
			break
		}
	}
	if running.Check == nil {
		t.Fatal("the route check did not run")
	}
	if got := e.Outbounds(); len(got) != 1 {
		t.Fatalf("a running session holds %d outbounds", len(got))
	}
	mstsc.Close() // the user closes Remote Desktop
	for s := range states {
		if s.Phase() == session.PhaseEnded {
			if s.Outcome != session.OutcomeClosed {
				t.Fatalf("outcome %s", s.Outcome)
			}
			break
		}
	}
	if got := e.Outbounds(); len(got) != 0 {
		t.Fatalf("the ended session still holds outbounds %q", got)
	}
}
