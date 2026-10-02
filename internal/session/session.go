// Package session is the life of a remote desktop connection.
//
// The decisions are a pure state machine (this file and reduce.go): Reduce
// takes the current State and one Event and returns the next State and the
// Effects to carry out. The imperative shell is separate: an actor per
// session (actor.go) carries out the effects (open the tunnel, start mstsc,
// ...) and feeds their outcomes back in as events, and the Manager
// (manager.go) runs at most one session per profile.
//
// One step runs at a time. Every step ends in exactly one event: its own
// success event or StepFailed. A stop request never interrupts a step's
// bookkeeping; it is noted, the step in flight finishes (the route check is
// actively cancelled, the other steps are quick), and then the session gives
// back whatever it holds. That one rule covers every ordering of a stop
// racing a step, and each ordering is a plain sequence of events to test.
//
// Steps, in order:
//
//	preflight   local checks that need no network (RD Gateway settings)
//	route       get the route to the target: direct, or an Xray outbound
//	listen      open the tunnel entrance on the profile's loopback address
//	check       optional: an X.224 Connection Request through the route
//	credential  write the password or user name hint mstsc will use
//	launch      start mstsc
//	run         mstsc is running; the session ends when it exits
//	done        everything has been given back; see Outcome
package session

import (
	"github.com/liubz102/RDP-over-proxy/internal/probe"
)

// Step is the step a session is in.
type Step string

// Steps.
const (
	StepPreflight  Step = "preflight"
	StepRoute      Step = "route"
	StepListen     Step = "listen"
	StepCheck      Step = "check"
	StepCredential Step = "credential"
	StepLaunch     Step = "launch"
	StepRun        Step = "run"
	StepDone       Step = "done"
)

// Phase is the coarse stage shown in the UI.
type Phase string

// Phases.
const (
	PhasePreparing Phase = "preparing"
	PhaseChecking  Phase = "checking"
	PhaseLaunching Phase = "launching"
	PhaseRunning   Phase = "running"
	// PhaseEnding: a stop was requested and the session is waiting for the
	// step in flight to finish, or for mstsc to exit after being killed.
	PhaseEnding Phase = "ending"
	PhaseEnded  Phase = "ended"
)

// Outcome is how a finished session ended.
type Outcome string

// Outcomes.
const (
	// OutcomeClosed: mstsc ran and has exited, whoever closed it.
	OutcomeClosed Outcome = "closed"
	// OutcomeCancelled: the user stopped the session before mstsc ran.
	OutcomeCancelled Outcome = "cancelled"
	// OutcomeFailed: a step failed before mstsc ran; see State.Failure.
	OutcomeFailed Outcome = "failed"
)

// Upstream is what the tunnel last learned about the way to the target.
type Upstream string

// Upstream states. The session logs a change only when the state flips, so
// mstsc retrying a broken route does not flood the log.
const (
	UpstreamUnknown Upstream = "unknown" // no connection has got anywhere yet
	UpstreamOK      Upstream = "ok"      // the target answered through the route
	UpstreamFailing Upstream = "failing" // the last connection could not reach the target
)

// Failure is the step that ended a session and why.
type Failure struct {
	Step Step
	Err  error
}

// State is everything known about a session.
type State struct {
	Step Step
	// CheckFirst includes StepCheck.
	CheckFirst bool
	// StopRequested: the user stopped the session before mstsc ran.
	StopRequested bool
	// Killing: the session asked Windows to end mstsc and waits for it to
	// exit.
	Killing bool
	// Failure is set when a step failed (and the user had not already asked
	// to stop).
	Failure *Failure
	// Outcome is set once Step is StepDone.
	Outcome Outcome

	// What the session holds and must give back when it ends.
	HasRoute          bool
	TunnelOpen        bool
	OneTimeCredential bool

	// Addr is the tunnel entrance mstsc connects to, once listening.
	Addr string
	// Check is the route check's result, when it ran and passed.
	Check *probe.Result
	// PID is mstsc's process ID once it has started; ExitCode is set when it
	// has exited.
	PID      int
	ExitCode int

	// Conns counts the tunnel's open connections.
	Conns         int
	Upstream      Upstream
	UpstreamError string
	// TunnelError is set when the entrance stopped accepting connections.
	TunnelError string
}

// Phase derives the coarse stage from the step.
func (s State) Phase() Phase {
	switch {
	case s.Step == StepDone:
		return PhaseEnded
	case s.StopRequested || s.Killing:
		return PhaseEnding
	}
	switch s.Step {
	case StepCheck:
		return PhaseChecking
	case StepCredential, StepLaunch:
		return PhaseLaunching
	case StepRun:
		return PhaseRunning
	default:
		return PhasePreparing
	}
}

// Event is something that happened to a session.
type Event interface{ isEvent() }

// Step results. Each step ends with its own success event or StepFailed.
type (
	PreflightPassed struct{}
	RouteReady      struct{}
	Listening       struct{ Addr string }
	CheckPassed     struct{ Result probe.Result }
	// CredentialReady: OneTime is true when a password was stored for this
	// session only and must be deleted when it ends.
	CredentialReady struct{ OneTime bool }
	ClientStarted   struct{ PID int }
	StepFailed      struct {
		Step Step
		Err  error
	}
	// ClientExited: mstsc has exited.
	ClientExited struct{ ExitCode int }
)

// Requests from the user.
type (
	// Stop ends the session. Before mstsc runs it cancels; while mstsc runs
	// it asks mstsc to close (which may ask the user to confirm), and Force
	// ends the process instead.
	Stop  struct{ Force bool }
	Focus struct{}
)

// Reports from the tunnel.
type (
	ConnOpened struct{}
	ConnClosed struct{}
	// UpstreamAnswered: bytes came back from the target through the route.
	UpstreamAnswered struct{}
	// UpstreamFailed: a connection could not reach the target, or the route
	// closed it before the target answered.
	UpstreamFailed struct{ Err error }
	// TunnelFailed: the entrance stopped accepting connections. Open
	// connections carry on, but mstsc cannot reconnect.
	TunnelFailed struct{ Err error }
)

func (PreflightPassed) isEvent()  {}
func (RouteReady) isEvent()       {}
func (Listening) isEvent()        {}
func (CheckPassed) isEvent()      {}
func (CredentialReady) isEvent()  {}
func (ClientStarted) isEvent()    {}
func (StepFailed) isEvent()       {}
func (ClientExited) isEvent()     {}
func (Stop) isEvent()             {}
func (Focus) isEvent()            {}
func (ConnOpened) isEvent()       {}
func (ConnClosed) isEvent()       {}
func (UpstreamAnswered) isEvent() {}
func (UpstreamFailed) isEvent()   {}
func (TunnelFailed) isEvent()     {}

// Effect is something the actor must do.
type Effect interface{ isEffect() }

// Steps to start. Each ends with an event (see Event).
type (
	Preflight         struct{}
	AcquireRoute      struct{}
	Listen            struct{}
	RunCheck          struct{}
	PrepareCredential struct{}
	LaunchClient      struct{}
)

// Actions without a result event of their own.
type (
	// CancelCheck aborts the route check in flight; the check then ends with
	// StepFailed.
	CancelCheck struct{}
	// CloseClient asks mstsc to close (WM_CLOSE to its window).
	CloseClient struct{}
	// KillClient ends the mstsc process this session started; ClientExited
	// follows.
	KillClient  struct{}
	FocusClient struct{}
)

// Clean-up, issued in this order when the session ends: the reverse of the
// order things were acquired in.
type (
	DeleteCredential struct{}
	CloseTunnel      struct{}
	ReleaseRoute     struct{}
)

// Log records a line in the session log. Msg is a stable key that the UI
// translates; Args fill in its details.
type Log struct {
	Level Level
	Msg   string
	Args  map[string]any
}

func (Preflight) isEffect()         {}
func (AcquireRoute) isEffect()      {}
func (Listen) isEffect()            {}
func (RunCheck) isEffect()          {}
func (PrepareCredential) isEffect() {}
func (LaunchClient) isEffect()      {}
func (CancelCheck) isEffect()       {}
func (CloseClient) isEffect()       {}
func (KillClient) isEffect()        {}
func (FocusClient) isEffect()       {}
func (DeleteCredential) isEffect()  {}
func (CloseTunnel) isEffect()       {}
func (ReleaseRoute) isEffect()      {}
func (Log) isEffect()               {}

// Level is a log line's severity.
type Level string

// Levels.
const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Log message keys.
const (
	MsgStarting        = "session.starting"
	MsgListening       = "session.listening"
	MsgCheckPassed     = "session.checkPassed"
	MsgClientStarted   = "session.clientStarted"
	MsgUpstreamOK      = "session.upstreamOk"
	MsgUpstreamFailing = "session.upstreamFailing"
	MsgStepFailed      = "session.stepFailed"
	MsgCancelling      = "session.cancelling"
	MsgClosing         = "session.closing"
	MsgKilling         = "session.killing"
	MsgClientExited    = "session.clientExited"
	MsgTunnelFailed    = "session.tunnelFailed"
	MsgEnded           = "session.ended"
	// MsgActionFailed: an action without a result event of its own (closing
	// or focusing mstsc, cleaning up) failed. The session carries on.
	MsgActionFailed = "session.actionFailed"
	// MsgUnexpected: an event arrived that does not fit the current step. It
	// is ignored; the line exists to make such a bug visible.
	MsgUnexpected = "session.unexpectedEvent"
)
