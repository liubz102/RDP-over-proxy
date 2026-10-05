package session_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/session"

	"errors"
	"io"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/loopback"
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/probe"
	"github.com/liubz102/RDP-over-proxy/internal/route"
	"github.com/liubz102/RDP-over-proxy/tests/testutil"
)

// These tests run whole sessions: a real tunnel on a loopback address, a fake
// RDP server as the target, and the test itself playing mstsc. mstsc is never
// started.

// fakeProcess stands in for mstsc.
type fakeProcess struct {
	pid          int
	args         []string
	exit         chan int
	once         sync.Once
	declineClose atomic.Bool // Close does nothing, like a user answering "no" in mstsc
	noWindow     atomic.Bool // Close finds no window to ask, like mstsc asking for a password
	closes       atomic.Int32
	kills        atomic.Int32
	focuses      atomic.Int32
	name         atomic.Pointer[string] // the name ShowName was given
	nameErr      error                  // what ShowName returns
}

func (p *fakeProcess) PID() int           { return p.pid }
func (p *fakeProcess) Wait() (int, error) { return <-p.exit, nil }
func (p *fakeProcess) exitWith(code int)  { p.once.Do(func() { p.exit <- code }) }
func (p *fakeProcess) Kill() error        { p.kills.Add(1); p.exitWith(1); return nil }
func (p *fakeProcess) Focus() error       { p.focuses.Add(1); return nil }
func (p *fakeProcess) ShowName(name string) error {
	p.name.Store(&name)
	return p.nameErr
}
func (p *fakeProcess) Close() (bool, error) {
	if p.noWindow.Load() {
		return false, nil
	}
	p.closes.Add(1)
	if !p.declineClose.Load() {
		p.exitWith(0)
	}
	return true, nil
}

type launcher struct {
	pids    atomic.Int32
	started chan *fakeProcess
	fail    error
	// nameFails is what the processes' ShowName returns.
	nameFails error
}

func (l *launcher) launch(args []string) (Process, error) {
	if l.fail != nil {
		return nil, l.fail
	}
	p := &fakeProcess{pid: 1000 + int(l.pids.Add(1)), args: args, exit: make(chan int, 1), nameErr: l.nameFails}
	l.started <- p
	return p, nil
}

// routes reaches every target directly, whatever the proxy; it stands in for
// the Xray engine.
type routes struct {
	acquired, released atomic.Int32
	fail               error
}

func (r *routes) Acquire(model.Proxy) (route.Dialer, func(), error) {
	if r.fail != nil {
		return nil, nil, r.fail
	}
	r.acquired.Add(1)
	return route.Direct(), func() { r.released.Add(1) }, nil
}

type creds struct {
	oneTime           bool
	prepared, deleted atomic.Int32
}

func (c *creds) Prepare() (bool, error) { c.prepared.Add(1); return c.oneTime, nil }
func (c *creds) Delete() error          { c.deleted.Add(1); return nil }

// recorder keeps every state and log line, per profile.
type recorder struct {
	mu      sync.Mutex
	states  map[string][]State
	logs    map[string][]Log
	changed chan struct{}
}

func newRecorder() *recorder {
	return &recorder{states: map[string][]State{}, logs: map[string][]Log{}, changed: make(chan struct{}, 1)}
}

func (r *recorder) state(id string, s State) {
	r.mu.Lock()
	r.states[id] = append(r.states[id], s)
	r.mu.Unlock()
	select {
	case r.changed <- struct{}{}:
	default:
	}
}

func (r *recorder) log(id string, l Log) {
	r.mu.Lock()
	r.logs[id] = append(r.logs[id], l)
	r.mu.Unlock()
}

// wait waits until the profile's latest state satisfies ok.
func (r *recorder) wait(id string, ok func(State) bool) State {
	for {
		r.mu.Lock()
		ss := r.states[id]
		r.mu.Unlock()
		if len(ss) > 0 && ok(ss[len(ss)-1]) {
			return ss[len(ss)-1]
		}
		<-r.changed
	}
}

func (r *recorder) ended(id string) State {
	return r.wait(id, func(s State) bool { return s.Step == StepDone })
}

func (r *recorder) logKeys(id string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var keys []string
	for _, l := range r.logs[id] {
		keys = append(keys, l.Msg)
	}
	return keys
}

func (r *recorder) lines(id string) []Log {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.logs[id])
}

func (r *recorder) phases(id string) []Phase {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Phase
	for _, s := range r.states[id] {
		if p := s.Phase(); len(out) == 0 || out[len(out)-1] != p {
			out = append(out, p)
		}
	}
	return out
}

type harness struct {
	m        *Manager
	launcher *launcher
	routes   *routes
	creds    *creds
	rec      *recorder
}

func newHarness(t *testing.T) *harness {
	h := &harness{
		launcher: &launcher{started: make(chan *fakeProcess, 16)},
		routes:   &routes{},
		creds:    &creds{},
		rec:      newRecorder(),
	}
	h.m = NewManager(Options{
		Routes:      h.routes,
		Launch:      h.launcher.launch,
		Credentials: func(Request) Credentials { return h.creds },
		Changed:     h.rec.state,
		Log:         h.rec.log,
	})
	t.Cleanup(h.m.Quit) // nothing outlives the test
	return h
}

var testProxy = model.Proxy{Schema: model.ProxySchema, ID: "testproxy", Name: "Test route",
	Kind: model.KindSocks, Server: "192.0.2.1", Port: 1080}

// request is a profile whose target is srv, through testProxy, with its own
// loopback address and port 0.
func request(t *testing.T, id string, srv *testutil.RDPServer) Request {
	t.Helper()
	ap := netip.MustParseAddrPort(srv.Addr)
	p := model.DefaultProfile()
	p.ID = id
	p.Name = "Test " + id
	p.Target = model.Target{Host: ap.Addr().String(), Port: int(ap.Port())}
	p.ProxyID = testProxy.ID
	p.Loopback = loopback.Derive(id).String()
	p.Display = model.Display{Mode: model.DisplayWindow, Width: 1024, Height: 768}
	return Request{Profile: p, Proxy: testProxy, Port: 0, CheckFirst: true}
}

func connect(t *testing.T, h *harness, req Request) {
	t.Helper()
	focused, err := h.m.Connect(req)
	if err != nil || focused {
		t.Fatalf("Connect = %v, %v", focused, err)
	}
}

// playMstsc connects to the tunnel the way mstsc does and returns the
// connection once the target's Connection Confirm has come back through it.
func playMstsc(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("connect to the tunnel: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	req, _ := probe.ConnectionRequest{Cookie: "mstshash=alice", Negotiate: true, Protocols: probe.Requested}.Marshal()
	if _, err := c.Write(req); err != nil {
		t.Fatal(err)
	}
	pkt, err := probe.ReadTPKT(c)
	if err != nil {
		t.Fatalf("no Connection Confirm through the tunnel: %v", err)
	}
	if _, err := probe.ParseConnectionConfirm(pkt); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSessionEndToEnd(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm, Selected: probe.ProtocolHybridEx})
	h := newHarness(t)
	h.creds.oneTime = true
	req := request(t, "e2e", srv)
	connect(t, h, req)

	p := <-h.launcher.started
	st := h.rec.wait("e2e", func(s State) bool { return s.Phase() == PhaseRunning })
	if want := []string{"/v:" + st.Addr, "/w:1024", "/h:768"}; !slices.Equal(p.args, want) {
		t.Fatalf("mstsc arguments = %q, want %q", p.args, want)
	}
	if name := p.name.Load(); name == nil || *name != req.Profile.Name {
		t.Fatalf("mstsc's window shows the name %v, want the profile's %q", name, req.Profile.Name)
	}
	if ap := netip.MustParseAddrPort(st.Addr); ap.Addr().String() != req.Profile.Loopback {
		t.Fatalf("the tunnel listens on %s, not on the profile's loopback address %s", st.Addr, req.Profile.Loopback)
	}
	// The route check went first, with the probe's request (no user name).
	if check := <-srv.Requests(); check.Cookie != "" || st.Check == nil || st.Check.Confirm.Selected != probe.ProtocolHybridEx {
		t.Fatalf("route check: request %+v, result %+v", check, st.Check)
	}

	c := playMstsc(t, st.Addr)
	if got := <-srv.Requests(); got.Cookie != "mstshash=alice" {
		t.Fatalf("the target received %+v, not mstsc's request", got)
	}
	if _, err := c.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	echo := make([]byte, 5)
	if _, err := io.ReadFull(c, echo); err != nil || string(echo) != "hello" {
		t.Fatalf("echo = %q, %v", echo, err)
	}
	h.rec.wait("e2e", func(s State) bool { return s.Upstream == UpstreamOK && s.Conns == 1 })
	c.Close()
	h.rec.wait("e2e", func(s State) bool { return s.Conns == 0 })

	p.exitWith(0) // the user closes Remote Desktop
	end := h.rec.ended("e2e")
	if end.Outcome != OutcomeClosed || end.Failure != nil {
		t.Fatalf("final state %+v", end)
	}
	if h.routes.acquired.Load() != 1 || h.routes.released.Load() != 1 {
		t.Fatalf("route acquired %d times, released %d", h.routes.acquired.Load(), h.routes.released.Load())
	}
	if h.creds.prepared.Load() != 1 || h.creds.deleted.Load() != 1 {
		t.Fatalf("credential prepared %d times, deleted %d", h.creds.prepared.Load(), h.creds.deleted.Load())
	}
	if c, err := net.Dial("tcp", st.Addr); err == nil {
		c.Close()
		t.Fatal("the tunnel still accepts connections after the session ended")
	}
	want := []Phase{PhasePreparing, PhaseChecking, PhaseLaunching, PhaseRunning, PhaseEnded}
	if got := h.rec.phases("e2e"); !slices.Equal(got, want) {
		t.Fatalf("phases = %v, want %v", got, want)
	}
	logs := h.rec.logKeys("e2e")
	for _, key := range []string{MsgStarting, MsgListening, MsgCheckPassed, MsgClientStarted, MsgUpstreamOK, MsgClientExited, MsgEnded} {
		if !slices.Contains(logs, key) {
			t.Errorf("log has no %s: %q", key, logs)
		}
	}
	if h.m.Running() != 0 {
		t.Fatalf("Running = %d after the end", h.m.Running())
	}
}

func TestConnectingARunningProfileFocusesIt(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	h := newHarness(t)
	req := request(t, "focus", srv)
	connect(t, h, req)
	p := <-h.launcher.started
	h.rec.wait("focus", func(s State) bool { return s.Phase() == PhaseRunning })

	focused, err := h.m.Connect(req)
	if err != nil || !focused {
		t.Fatalf("second Connect = %v, %v; want focused", focused, err)
	}
	if !h.m.Focus("focus") || h.m.Focus("nobody") {
		t.Fatal("Focus should report whether the profile has a session")
	}
	h.m.Stop("focus", false)
	h.rec.ended("focus")
	if n := p.focuses.Load(); n != 2 {
		t.Fatalf("mstsc was focused %d times, want 2", n)
	}
	if len(h.launcher.started) != 0 {
		t.Fatal("a second mstsc was started")
	}
}

func TestActiveFromConnectToTheEnd(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	h := newHarness(t)
	if h.m.Active("act") {
		t.Fatal("Active before Connect")
	}
	connect(t, h, request(t, "act", srv))
	if !h.m.Active("act") { // before the session has reported anything
		t.Fatal("not Active right after Connect")
	}
	p := <-h.launcher.started
	h.rec.wait("act", func(s State) bool { return s.Phase() == PhaseRunning })
	p.exitWith(0)
	h.rec.ended("act")
	if h.m.Active("act") {
		t.Fatal("Active after the session ended")
	}
}

func TestStopAsksMstscToClose(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	h := newHarness(t)
	connect(t, h, request(t, "stop", srv))
	p := <-h.launcher.started
	p.declineClose.Store(true)
	h.rec.wait("stop", func(s State) bool { return s.Phase() == PhaseRunning })

	// The user answers "no" in mstsc's confirmation: still running.
	if !h.m.Stop("stop", false) {
		t.Fatal("Stop found no session")
	}
	// Force is the way out.
	h.m.Stop("stop", true)
	end := h.rec.ended("stop")
	if end.Outcome != OutcomeClosed || p.closes.Load() != 1 || p.kills.Load() != 1 {
		t.Fatalf("outcome %s, closes %d, kills %d", end.Outcome, p.closes.Load(), p.kills.Load())
	}
	if h.m.Stop("stop", false) {
		t.Fatal("Stop reported a session after it ended")
	}
}

func TestStopWithNoWindowToAskEndsMstsc(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	h := newHarness(t)
	connect(t, h, request(t, "prompt", srv))
	p := <-h.launcher.started
	p.noWindow.Store(true)
	h.rec.wait("prompt", func(s State) bool { return s.Phase() == PhaseRunning })

	// mstsc is asking for a password: nothing can ask the user to confirm,
	// so a plain stop ends it.
	h.m.Stop("prompt", false)
	end := h.rec.ended("prompt")
	if end.Outcome != OutcomeClosed || p.kills.Load() != 1 {
		t.Fatalf("outcome %s, kills %d", end.Outcome, p.kills.Load())
	}
	if !slices.Contains(h.rec.logKeys("prompt"), MsgNothingToClose) {
		t.Fatalf("log %q does not say why mstsc was ended", h.rec.logKeys("prompt"))
	}
}

// A window title without the profile's name is worth a warning, not the
// session.
func TestNameNotShownOnlyWarns(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	h := newHarness(t)
	h.launcher.nameFails = errors.New("no window events")
	connect(t, h, request(t, "title", srv))
	p := <-h.launcher.started
	h.rec.wait("title", func(s State) bool { return s.Phase() == PhaseRunning })

	warned := slices.ContainsFunc(h.rec.lines("title"), func(l Log) bool {
		return l.Msg == MsgActionFailed && l.Level == LevelWarn && l.Args["action"] == "showName" &&
			l.Args["error"] == "no window events"
	})
	if !warned {
		t.Fatalf("log %+v has no warning about the window title", h.rec.lines("title"))
	}
	p.exitWith(0)
	if end := h.rec.ended("title"); end.Outcome != OutcomeClosed || end.Failure != nil {
		t.Fatalf("final state %+v", end)
	}
}

func TestCancelWhileChecking(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerNothing})
	h := newHarness(t)
	connect(t, h, request(t, "cancel", srv))
	<-srv.Requests() // the check reached the target, which never answers
	h.rec.wait("cancel", func(s State) bool { return s.Phase() == PhaseChecking })

	h.m.Stop("cancel", false)
	end := h.rec.ended("cancel")
	if end.Outcome != OutcomeCancelled || end.Failure != nil {
		t.Fatalf("final state %+v", end)
	}
	if len(h.launcher.started) != 0 {
		t.Fatal("mstsc was started after the session was cancelled")
	}
	if h.routes.released.Load() != 1 {
		t.Fatal("the route was not released")
	}
	if slices.Contains(h.rec.logKeys("cancel"), MsgStepFailed) {
		t.Fatal("a cancelled check was logged as a failure")
	}
}

func TestFailures(t *testing.T) {
	cases := []struct {
		name   string
		answer testutil.Answer
		setup  func(*harness, *Request)
		step   Step
		is     error
	}{
		{"target is not RDP", testutil.AnswerNotRDP, nil, StepCheck, probe.ErrNotRDP},
		{"route refuses", testutil.AnswerConfirm, func(h *harness, _ *Request) { h.routes.fail = route.ErrUnsupported },
			StepRoute, route.ErrUnsupported},
		{"mstsc does not start", testutil.AnswerConfirm, func(h *harness, _ *Request) { h.launcher.fail = errors.New("no mstsc") },
			StepLaunch, nil},
		{"direct to this computer", testutil.AnswerConfirm, func(_ *harness, r *Request) {
			r.Proxy = model.DirectProxy()
			r.Profile.ProxyID = model.DirectProxyID
		}, StepPreflight, ErrLoopbackDirect},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: c.answer})
			h := newHarness(t)
			id := "fail" + strconv.Itoa(i)
			req := request(t, id, srv)
			if c.setup != nil {
				c.setup(h, &req)
			}
			connect(t, h, req)
			end := h.rec.ended(id)
			if end.Outcome != OutcomeFailed || end.Failure == nil || end.Failure.Step != c.step {
				t.Fatalf("final state %+v, failure %+v; want a failure at %s", end, end.Failure, c.step)
			}
			if c.is != nil && !errors.Is(end.Failure.Err, c.is) {
				t.Fatalf("failure %v, want %v", end.Failure.Err, c.is)
			}
			if h.routes.acquired.Load() != h.routes.released.Load() {
				t.Fatalf("route acquired %d times, released %d", h.routes.acquired.Load(), h.routes.released.Load())
			}
			if end.Addr != "" {
				if c, err := net.Dial("tcp", end.Addr); err == nil {
					c.Close()
					t.Fatal("the tunnel is still open after the failure")
				}
			}
		})
	}
}

func TestEntranceAlreadyInUse(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	h := newHarness(t)
	req := request(t, "busy", srv)
	other, err := net.Listen("tcp", net.JoinHostPort(req.Profile.Loopback, "0"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	req.Port = int(other.Addr().(*net.TCPAddr).AddrPort().Port())

	connect(t, h, req)
	end := h.rec.ended("busy")
	if end.Failure == nil || end.Failure.Step != StepListen {
		t.Fatalf("final state %+v; want a failure to listen", end)
	}
	if h.routes.released.Load() != 1 {
		t.Fatal("the route was not released")
	}
}

func TestPreflightHook(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	gateway := errors.New("Default.rdp sends connections to an RD Gateway")
	rec := newRecorder()
	m := NewManager(Options{
		Routes:    &routes{},
		Launch:    (&launcher{started: make(chan *fakeProcess, 1)}).launch,
		Preflight: func(Request) error { return gateway },
		Changed:   rec.state,
	})
	t.Cleanup(m.Quit)
	if _, err := m.Connect(request(t, "gw", srv)); err != nil {
		t.Fatal(err)
	}
	if end := rec.ended("gw"); end.Failure == nil || !errors.Is(end.Failure.Err, gateway) {
		t.Fatalf("final state %+v", end)
	}
}

func TestWithoutCheckTheTargetSeesOnlyMstsc(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	h := newHarness(t)
	req := request(t, "nocheck", srv)
	req.CheckFirst = false
	connect(t, h, req)
	p := <-h.launcher.started
	st := h.rec.wait("nocheck", func(s State) bool { return s.Phase() == PhaseRunning })
	playMstsc(t, st.Addr)
	if got := <-srv.Requests(); got.Cookie != "mstshash=alice" {
		t.Fatalf("the first request at the target was %+v; no route check should have run", got)
	}
	p.exitWith(0)
	if end := h.rec.ended("nocheck"); end.Check != nil || end.Outcome != OutcomeClosed {
		t.Fatalf("final state %+v", end)
	}
	if slices.Contains(h.rec.phases("nocheck"), PhaseChecking) {
		t.Fatal("the session went through checking")
	}
}

func TestReconnectAfterTheSessionEnded(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	h := newHarness(t)
	req := request(t, "again", srv)
	for round := range 2 {
		connect(t, h, req)
		p := <-h.launcher.started
		h.rec.wait("again", func(s State) bool { return s.Phase() == PhaseRunning })
		p.exitWith(0)
		h.rec.ended("again")
		if got := h.routes.released.Load(); got != int32(round+1) {
			t.Fatalf("round %d: route released %d times", round, got)
		}
	}
}

func TestQuitStopsEverySession(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	h := newHarness(t)
	var procs []*fakeProcess
	for _, id := range []string{"q1", "q2"} {
		connect(t, h, request(t, id, srv))
		procs = append(procs, <-h.launcher.started)
		h.rec.wait(id, func(s State) bool { return s.Phase() == PhaseRunning })
	}
	if n := h.m.Running(); n != 2 {
		t.Fatalf("Running = %d, want 2", n)
	}

	h.m.Quit() // returns only when everything is given back
	for i, id := range []string{"q1", "q2"} {
		st := h.m.States()[id]
		if st.Step != StepDone || procs[i].kills.Load() != 1 {
			t.Fatalf("%s after Quit: %+v, kills %d", id, st, procs[i].kills.Load())
		}
	}
	if h.routes.acquired.Load() != 2 || h.routes.released.Load() != 2 {
		t.Fatalf("routes acquired %d, released %d", h.routes.acquired.Load(), h.routes.released.Load())
	}
	if _, err := h.m.Connect(request(t, "q3", srv)); !errors.Is(err, ErrQuitting) {
		t.Fatalf("Connect after Quit = %v, want ErrQuitting", err)
	}
}

func TestConnectRejectsBadRequests(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	h := newHarness(t)
	cases := map[string]func(*Request){
		"invalid profile":   func(r *Request) { r.Profile.Target.Host = "" },
		"invalid proxy":     func(r *Request) { r.Proxy.Server = "" },
		"another proxy":     func(r *Request) { r.Profile.ProxyID = "otherproxy" },
		"port out of range": func(r *Request) { r.Port = 70000 },
	}
	for name, change := range cases {
		req := request(t, "bad", srv)
		change(&req)
		if _, err := h.m.Connect(req); err == nil {
			t.Errorf("%s: Connect accepted it", name)
		}
	}
	if len(h.m.States()) != 0 {
		t.Fatal("a rejected request started a session")
	}
}
