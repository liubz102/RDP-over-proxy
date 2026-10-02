package session

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/liubz102/RDP-over-proxy/internal/probe"
)

const addr = "127.10.20.30:13389"

var checkResult = probe.Result{
	Elapsed: 320 * time.Millisecond,
	Confirm: probe.ConnectionConfirm{Negotiated: true, Selected: probe.ProtocolHybridEx},
}

// happyPath is every step succeeding, in order, with the route check on.
// Event i completes the step that is in flight after events 0..i-1.
var happyPath = []Event{
	PreflightPassed{},
	RouteReady{},
	Listening{Addr: addr},
	CheckPassed{Result: checkResult},
	CredentialReady{OneTime: true},
	ClientStarted{PID: 4242},
}

// stepsInFlight[i] is the step in flight before happyPath[i].
var stepsInFlight = []Step{StepPreflight, StepRoute, StepListen, StepCheck, StepCredential, StepLaunch}

// play starts a session and applies events in order. It returns the final
// state and every effect, log lines included.
func play(t *testing.T, checkFirst bool, events ...Event) (State, []Effect) {
	t.Helper()
	s, all := Start(checkFirst)
	for _, e := range events {
		var effects []Effect
		s, effects = Reduce(s, e)
		all = append(all, effects...)
	}
	return s, all
}

// actions drops the log lines.
func actions(effects []Effect) []Effect {
	return slices.DeleteFunc(slices.Clone(effects), func(e Effect) bool {
		_, isLog := e.(Log)
		return isLog
	})
}

// logs returns the message key of each log line.
func logs(effects []Effect) []string {
	var keys []string
	for _, e := range effects {
		if l, ok := e.(Log); ok {
			keys = append(keys, l.Msg)
		}
	}
	return keys
}

func assertActions(t *testing.T, got []Effect, want ...Effect) {
	t.Helper()
	if got := actions(got); !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
		t.Fatalf("actions =\n %#v\nwant\n %#v", got, want)
	}
}

func TestHappyPath(t *testing.T) {
	events := append(slices.Clone(happyPath), ConnOpened{}, UpstreamAnswered{}, ConnClosed{}, ClientExited{ExitCode: 0})
	s, effects := play(t, true, events...)
	assertActions(t, effects,
		Preflight{}, AcquireRoute{}, Listen{}, RunCheck{}, PrepareCredential{}, LaunchClient{},
		DeleteCredential{}, CloseTunnel{}, ReleaseRoute{})
	want := State{
		Step:       StepDone,
		CheckFirst: true,
		Outcome:    OutcomeClosed,
		Addr:       addr,
		Check:      &checkResult,
		PID:        4242,
		Upstream:   UpstreamOK,
	}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("final state =\n %+v\nwant\n %+v", s, want)
	}
	wantLogs := []string{MsgStarting, MsgListening, MsgCheckPassed, MsgClientStarted, MsgUpstreamOK,
		MsgClientExited, MsgEnded}
	if got := logs(effects); !slices.Equal(got, wantLogs) {
		t.Fatalf("logs = %q, want %q", got, wantLogs)
	}
}

func TestWithoutRouteCheck(t *testing.T) {
	events := []Event{PreflightPassed{}, RouteReady{}, Listening{Addr: addr}, CredentialReady{}, ClientStarted{PID: 1},
		ClientExited{ExitCode: 0}}
	s, effects := play(t, false, events...)
	// No check, and no one-time credential to delete.
	assertActions(t, effects,
		Preflight{}, AcquireRoute{}, Listen{}, PrepareCredential{}, LaunchClient{}, CloseTunnel{}, ReleaseRoute{})
	if s.Outcome != OutcomeClosed || s.Check != nil {
		t.Fatalf("final state = %+v", s)
	}
}

func TestPhases(t *testing.T) {
	want := []Phase{PhasePreparing, PhasePreparing, PhasePreparing, PhaseChecking, PhaseLaunching, PhaseLaunching, PhaseRunning}
	s, _ := Start(true)
	if s.Phase() != want[0] {
		t.Fatalf("after Start: phase %s, want %s", s.Phase(), want[0])
	}
	for i, e := range happyPath {
		s, _ = Reduce(s, e)
		if s.Phase() != want[i+1] {
			t.Fatalf("after %T: phase %s, want %s", e, s.Phase(), want[i+1])
		}
	}
	s, _ = Reduce(s, ClientExited{})
	if s.Phase() != PhaseEnded {
		t.Fatalf("after exit: phase %s", s.Phase())
	}
}

func TestStopWhileEachStepRuns(t *testing.T) {
	// What has to be given back depends on how far the session got.
	cleanup := [][]Effect{
		{},
		{ReleaseRoute{}},
		{CloseTunnel{}, ReleaseRoute{}},
		{CloseTunnel{}, ReleaseRoute{}},
		{DeleteCredential{}, CloseTunnel{}, ReleaseRoute{}},
	}
	for i, step := range stepsInFlight[:5] {
		s, _ := play(t, true, happyPath[:i]...)
		if s.Step != step {
			t.Fatalf("setup: step %s, want %s", s.Step, step)
		}
		s, effects := Reduce(s, Stop{})
		if step == StepCheck {
			assertActions(t, effects, CancelCheck{}) // the check is the one step that is aborted
		} else {
			assertActions(t, effects)
		}
		if s.Phase() != PhaseEnding || s.Step != step {
			t.Fatalf("stop during %s: phase %s, step %s; want ending while the step finishes", step, s.Phase(), s.Step)
		}
		// The step finishes anyway (a quick step, or a check that won the
		// race against its cancellation).
		s, effects = Reduce(s, happyPath[i])
		assertActions(t, effects, cleanup[i]...)
		if s.Step != StepDone || s.Outcome != OutcomeCancelled || s.Failure != nil {
			t.Fatalf("stop during %s: final state %+v", step, s)
		}
	}
}

func TestStopDuringCheckEndsWithTheCancelledCheck(t *testing.T) {
	s, _ := play(t, true, happyPath[:3]...)
	s, _ = Reduce(s, Stop{})
	s, effects := Reduce(s, StepFailed{Step: StepCheck, Err: context.Canceled})
	assertActions(t, effects, CloseTunnel{}, ReleaseRoute{})
	if s.Outcome != OutcomeCancelled || s.Failure != nil {
		t.Fatalf("final state = %+v; a cancelled check is not a failure", s)
	}
	if slices.Contains(logs(effects), MsgStepFailed) {
		t.Fatal("the cancelled check should not be logged as a failure")
	}
}

func TestStopWhileMstscStarts(t *testing.T) {
	s, _ := play(t, true, happyPath[:5]...)
	s, _ = Reduce(s, Stop{})
	// mstsc started anyway; with no window to close yet, it is ended.
	s, effects := Reduce(s, ClientStarted{PID: 7})
	assertActions(t, effects, KillClient{})
	if s.Step != StepRun || s.Phase() != PhaseEnding {
		t.Fatalf("state = %+v", s)
	}
	// Nothing more to do until it has exited.
	if _, effects = Reduce(s, Stop{Force: true}); len(effects) != 0 {
		t.Fatalf("a second stop issued %#v", effects)
	}
	if _, effects = Reduce(s, Focus{}); len(effects) != 0 {
		t.Fatalf("focus while killing issued %#v", effects)
	}
	s, effects = Reduce(s, ClientExited{ExitCode: 1})
	assertActions(t, effects, DeleteCredential{}, CloseTunnel{}, ReleaseRoute{})
	if s.Outcome != OutcomeCancelled || s.ExitCode != 1 {
		t.Fatalf("final state = %+v", s)
	}
}

func TestFailureAtEachStep(t *testing.T) {
	cleanup := [][]Effect{
		{},
		{},
		{ReleaseRoute{}},
		{CloseTunnel{}, ReleaseRoute{}},
		{CloseTunnel{}, ReleaseRoute{}},
		{DeleteCredential{}, CloseTunnel{}, ReleaseRoute{}},
	}
	cause := errors.New("boom")
	for i, step := range stepsInFlight {
		s, _ := play(t, true, happyPath[:i]...)
		s, effects := Reduce(s, StepFailed{Step: step, Err: cause})
		assertActions(t, effects, cleanup[i]...)
		if s.Step != StepDone || s.Outcome != OutcomeFailed {
			t.Fatalf("failure at %s: state %+v", step, s)
		}
		if s.Failure == nil || s.Failure.Step != step || s.Failure.Err != cause {
			t.Fatalf("failure at %s: Failure = %+v", step, s.Failure)
		}
		if s.HasRoute || s.TunnelOpen || s.OneTimeCredential {
			t.Fatalf("failure at %s: still holding something: %+v", step, s)
		}
		var failLog *Log
		for _, e := range effects {
			if l, ok := e.(Log); ok && l.Msg == MsgStepFailed {
				failLog = &l
			}
		}
		if failLog == nil || failLog.Level != LevelError || failLog.Args["step"] != string(step) || failLog.Args["error"] != "boom" {
			t.Fatalf("failure at %s: log %+v", step, failLog)
		}
	}
}

func TestOneTimeCredentialOnlyDeletedWhenWritten(t *testing.T) {
	events := slices.Clone(happyPath)
	events[4] = CredentialReady{OneTime: false} // remembered password, or only a user name hint
	_, effects := play(t, true, append(events, ClientExited{})...)
	if slices.Contains(actions(effects), Effect(DeleteCredential{})) {
		t.Fatal("a remembered credential must not be deleted")
	}
}

func TestStopWhileRunning(t *testing.T) {
	s, _ := play(t, true, happyPath...)

	// A plain stop asks mstsc to close. mstsc may ask the user to confirm,
	// so the session keeps running until mstsc really exits.
	s, effects := Reduce(s, Stop{})
	assertActions(t, effects, CloseClient{})
	if s.Phase() != PhaseRunning {
		t.Fatalf("phase %s after asking mstsc to close", s.Phase())
	}
	// The user declined in mstsc and asks again.
	s, effects = Reduce(s, Stop{})
	assertActions(t, effects, CloseClient{})

	// Force ends the process, once.
	s, effects = Reduce(s, Stop{Force: true})
	assertActions(t, effects, KillClient{})
	if s.Phase() != PhaseEnding {
		t.Fatalf("phase %s after kill", s.Phase())
	}
	s, effects = Reduce(s, Stop{Force: true})
	assertActions(t, effects)

	s, effects = Reduce(s, ClientExited{ExitCode: 1})
	assertActions(t, effects, DeleteCredential{}, CloseTunnel{}, ReleaseRoute{})
	if s.Outcome != OutcomeClosed {
		t.Fatalf("outcome %s; mstsc ran, so it was closed, not cancelled", s.Outcome)
	}
}

func TestStopTwiceBeforeMstscRuns(t *testing.T) {
	s, _ := play(t, true, happyPath[:3]...)
	s, _ = Reduce(s, Stop{})
	s, effects := Reduce(s, Stop{Force: true})
	if len(effects) != 0 || !s.StopRequested {
		t.Fatalf("second stop: %+v, %#v", s, effects)
	}
}

func TestFocus(t *testing.T) {
	s, _ := play(t, true, happyPath[:4]...)
	if _, effects := Reduce(s, Focus{}); len(effects) != 0 {
		t.Fatalf("focus before mstsc runs issued %#v", effects)
	}
	s, _ = play(t, true, happyPath...)
	if _, effects := Reduce(s, Focus{}); !reflect.DeepEqual(effects, []Effect{FocusClient{}}) {
		t.Fatalf("focus while running issued %#v", effects)
	}
}

func TestUpstreamIsLoggedOnlyWhenItFlips(t *testing.T) {
	s, _ := play(t, true, happyPath...)
	var all []Effect
	reasons := []error{errors.New("refused"), errors.New("reset")}
	for _, e := range []Event{
		UpstreamAnswered{}, UpstreamAnswered{},
		UpstreamFailed{Err: reasons[0]}, UpstreamFailed{Err: reasons[1]},
		UpstreamAnswered{}, UpstreamFailed{Err: reasons[0]},
	} {
		var effects []Effect
		s, effects = Reduce(s, e)
		all = append(all, effects...)
	}
	want := []string{MsgUpstreamOK, MsgUpstreamFailing, MsgUpstreamOK, MsgUpstreamFailing}
	if got := logs(all); !slices.Equal(got, want) {
		t.Fatalf("logs = %q, want %q", got, want)
	}
	if s.Upstream != UpstreamFailing || s.UpstreamError != "refused" {
		t.Fatalf("upstream = %s %q", s.Upstream, s.UpstreamError)
	}
	// The reason shown is the latest, even when it was not logged.
	s, _ = Reduce(s, UpstreamFailed{Err: reasons[1]})
	if s.UpstreamError != "reset" {
		t.Fatalf("UpstreamError = %q, want the latest reason", s.UpstreamError)
	}
	s, _ = Reduce(s, UpstreamAnswered{})
	if s.UpstreamError != "" {
		t.Fatalf("UpstreamError = %q after recovering", s.UpstreamError)
	}
}

func TestTunnelEventsBeforeTheTunnelOpensAreIgnored(t *testing.T) {
	s, _ := play(t, true, happyPath[:2]...)
	for _, e := range []Event{ConnOpened{}, UpstreamAnswered{}, UpstreamFailed{Err: errors.New("x")}} {
		next, effects := Reduce(s, e)
		if !reflect.DeepEqual(next, s) || len(effects) != 0 {
			t.Fatalf("%T before the tunnel opened changed %+v into %+v (%#v)", e, s, next, effects)
		}
	}
}

func TestTunnelFailureIsLoggedOnce(t *testing.T) {
	s, _ := play(t, true, happyPath[:2]...)
	if next, effects := Reduce(s, TunnelFailed{Err: errors.New("x")}); !reflect.DeepEqual(next, s) || len(effects) != 0 {
		t.Fatal("a tunnel failure before the tunnel opened should be ignored")
	}
	s, _ = play(t, true, happyPath...)
	s, effects := Reduce(s, TunnelFailed{Err: errors.New("accept: too many open files")})
	if !slices.Equal(logs(effects), []string{MsgTunnelFailed}) || s.TunnelError != "accept: too many open files" {
		t.Fatalf("first failure: %+v, %#v", s, effects)
	}
	if s.Phase() != PhaseRunning {
		t.Fatalf("phase %s; mstsc keeps its open connection, so the session keeps running", s.Phase())
	}
	if _, effects = Reduce(s, TunnelFailed{Err: errors.New("again")}); len(effects) != 0 {
		t.Fatalf("a second failure was logged: %#v", effects)
	}
	_, effects = Reduce(s, ClientExited{})
	assertActions(t, effects, DeleteCredential{}, CloseTunnel{}, ReleaseRoute{})
}

func TestConnectionCount(t *testing.T) {
	s, _ := play(t, true, happyPath...)
	for _, e := range []Event{ConnOpened{}, ConnOpened{}, ConnClosed{}} {
		s, _ = Reduce(s, e)
	}
	if s.Conns != 1 {
		t.Fatalf("Conns = %d, want 1", s.Conns)
	}
	for range 3 {
		s, _ = Reduce(s, ConnClosed{})
	}
	if s.Conns != 0 {
		t.Fatalf("Conns = %d, want it never to go below 0", s.Conns)
	}
}

func TestCheckLogDetails(t *testing.T) {
	failed := probe.Result{Elapsed: 2 * time.Second,
		Confirm: probe.ConnectionConfirm{Negotiated: true, Failure: probe.SSLNotAllowedByServer}}
	cases := []struct {
		result probe.Result
		want   map[string]any
	}{
		{checkResult, map[string]any{"elapsedMs": int64(320), "protocol": "HYBRID_EX"}},
		{probe.Result{Elapsed: time.Millisecond}, map[string]any{"elapsedMs": int64(1), "protocol": "RDP"}},
		{failed, map[string]any{"elapsedMs": int64(2000), "protocol": "", "negotiationFailure": "SSL_NOT_ALLOWED_BY_SERVER"}},
	}
	for _, c := range cases {
		s, _ := play(t, true, happyPath[:3]...)
		_, effects := Reduce(s, CheckPassed{Result: c.result})
		l, ok := effects[0].(Log)
		if !ok || l.Msg != MsgCheckPassed || !reflect.DeepEqual(l.Args, c.want) {
			t.Errorf("check log = %#v, want args %v", effects[0], c.want)
		}
	}
}

func TestUnexpectedEventsChangeNothing(t *testing.T) {
	s, _ := play(t, true, happyPath[:3]...) // the check is in flight
	for _, e := range []Event{
		PreflightPassed{},
		RouteReady{},
		Listening{Addr: addr},
		CredentialReady{},
		ClientStarted{PID: 1},
		ClientExited{},
		StepFailed{Step: StepListen, Err: errors.New("late")},
	} {
		next, effects := Reduce(s, e)
		if !reflect.DeepEqual(next, s) {
			t.Errorf("%T changed the state", e)
		}
		if got := logs(effects); !slices.Equal(got, []string{MsgUnexpected}) || len(actions(effects)) != 0 {
			t.Errorf("%T: effects %#v, want only an unexpected-event warning", e, effects)
		}
	}
	running, _ := play(t, true, happyPath...)
	if _, effects := Reduce(running, StepFailed{Step: StepRun, Err: errors.New("x")}); !slices.Equal(logs(effects), []string{MsgUnexpected}) {
		t.Errorf("StepFailed while running: %#v", effects)
	}
}

func TestEventsAfterTheEndAreIgnored(t *testing.T) {
	s, _ := play(t, true, StepFailed{Step: StepPreflight, Err: errors.New("x")})
	for _, e := range []Event{Stop{}, Focus{}, ClientExited{}, ConnOpened{}, UpstreamFailed{Err: errors.New("y")}, PreflightPassed{}} {
		next, effects := Reduce(s, e)
		if !reflect.DeepEqual(next, s) || effects != nil {
			t.Errorf("%T after the end: %+v, %#v", e, next, effects)
		}
	}
}
