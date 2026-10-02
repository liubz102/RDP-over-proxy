package session

import (
	"fmt"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
)

// Start begins a session. checkFirst runs the route check before mstsc
// starts (Settings.CheckRouteBeforeConnect).
func Start(checkFirst bool) (State, []Effect) {
	s := State{Step: StepPreflight, CheckFirst: checkFirst, Upstream: UpstreamUnknown}
	return s, []Effect{info(MsgStarting, nil), Preflight{}}
}

// Reduce applies one event. Events after the session is done, and events
// that do not fit the current step, change nothing.
func Reduce(s State, e Event) (State, []Effect) {
	if s.Step == StepDone {
		return s, nil
	}
	switch e := e.(type) {
	case PreflightPassed:
		if s.Step != StepPreflight {
			return s.unexpected(e)
		}
		return s.stepSucceeded(nil)

	case RouteReady:
		if s.Step != StepRoute {
			return s.unexpected(e)
		}
		s.HasRoute = true
		return s.stepSucceeded(nil)

	case Listening:
		if s.Step != StepListen {
			return s.unexpected(e)
		}
		s.TunnelOpen = true
		s.Addr = e.Addr
		return s.stepSucceeded([]Effect{info(MsgListening, map[string]any{"addr": e.Addr})})

	case CheckPassed:
		if s.Step != StepCheck {
			return s.unexpected(e)
		}
		r := e.Result
		s.Check = &r
		// A server that does not negotiate uses standard RDP security, which
		// is what the zero Selected value names.
		args := map[string]any{"elapsedMs": r.Elapsed.Milliseconds(), "protocol": r.Confirm.Selected.String()}
		if r.Confirm.Failure != 0 {
			args["protocol"] = ""
			args["negotiationFailure"] = r.Confirm.Failure.String()
		}
		return s.stepSucceeded([]Effect{info(MsgCheckPassed, args)})

	case CredentialReady:
		if s.Step != StepCredential {
			return s.unexpected(e)
		}
		s.OneTimeCredential = e.OneTime
		return s.stepSucceeded(nil)

	case ClientStarted:
		if s.Step != StepLaunch {
			return s.unexpected(e)
		}
		s.Step = StepRun
		s.PID = e.PID
		effects := []Effect{info(MsgClientStarted, map[string]any{"pid": e.PID})}
		if s.StopRequested {
			// Stopped while mstsc was starting: it has no window to close
			// yet, so end the process.
			s.Killing = true
			effects = append(effects, KillClient{})
		}
		return s, effects

	case StepFailed:
		if e.Step != s.Step || s.Step == StepRun {
			return s.unexpected(e)
		}
		var effects []Effect
		// After a stop request the step's failure is usually the stop
		// itself (a cancelled check); the outcome is "cancelled" either way.
		if !s.StopRequested {
			s.Failure = &Failure{Step: e.Step, Err: e.Err}
			effects = append(effects, logLine(LevelError, MsgStepFailed,
				withError(map[string]any{"step": string(e.Step)}, e.Err)))
		}
		return s.finish(effects)

	case ClientExited:
		if s.Step != StepRun {
			return s.unexpected(e)
		}
		s.ExitCode = e.ExitCode
		return s.finish([]Effect{info(MsgClientExited, map[string]any{"exitCode": e.ExitCode})})

	case Stop:
		return s.stop(e.Force)

	case Focus:
		if s.Step == StepRun && !s.Killing {
			return s, []Effect{FocusClient{}}
		}
		return s, nil

	case ConnOpened:
		if s.TunnelOpen {
			s.Conns++
		}
		return s, nil

	case ConnClosed:
		if s.TunnelOpen && s.Conns > 0 {
			s.Conns--
		}
		return s, nil

	case UpstreamAnswered:
		if !s.TunnelOpen || s.Upstream == UpstreamOK {
			return s, nil
		}
		s.Upstream = UpstreamOK
		s.UpstreamError, s.UpstreamCode = "", ""
		return s, []Effect{info(MsgUpstreamOK, nil)}

	case UpstreamFailed:
		if !s.TunnelOpen {
			return s, nil
		}
		// Keep the latest reason for the UI, but log only the flip.
		s.UpstreamError, s.UpstreamCode = errText(e.Err), errcode.Of(e.Err)
		if s.Upstream == UpstreamFailing {
			return s, nil
		}
		s.Upstream = UpstreamFailing
		return s, []Effect{logLine(LevelWarn, MsgUpstreamFailing, withError(nil, e.Err))}

	case TunnelFailed:
		if !s.TunnelOpen || s.TunnelError != "" {
			return s, nil
		}
		s.TunnelError, s.TunnelCode = errText(e.Err), errcode.Of(e.Err)
		return s, []Effect{logLine(LevelError, MsgTunnelFailed, withError(nil, e.Err))}
	}
	return s.unexpected(e)
}

// stepSucceeded moves on after the step in flight succeeded: to the next
// step, or to the end when a stop was requested meanwhile.
func (s State) stepSucceeded(effects []Effect) (State, []Effect) {
	if s.StopRequested {
		return s.finish(effects)
	}
	s.Step = s.next()
	return s, append(effects, startEffect[s.Step])
}

// next is the step after the current one.
func (s State) next() Step {
	switch s.Step {
	case StepPreflight:
		return StepRoute
	case StepRoute:
		return StepListen
	case StepListen:
		if s.CheckFirst {
			return StepCheck
		}
		return StepCredential
	case StepCheck:
		return StepCredential
	case StepCredential:
		return StepLaunch
	}
	panic(fmt.Sprintf("session: no step after %q", s.Step))
}

var startEffect = map[Step]Effect{
	StepRoute:      AcquireRoute{},
	StepListen:     Listen{},
	StepCheck:      RunCheck{},
	StepCredential: PrepareCredential{},
	StepLaunch:     LaunchClient{},
}

func (s State) stop(force bool) (State, []Effect) {
	switch {
	case s.Killing:
		return s, nil
	case s.Step == StepRun && force:
		s.Killing = true
		return s, []Effect{info(MsgKilling, nil), KillClient{}}
	case s.Step == StepRun:
		// mstsc may ask the user to confirm and they may decline, so the
		// state stays "running" until mstsc actually exits.
		return s, []Effect{info(MsgClosing, nil), CloseClient{}}
	case s.StopRequested:
		return s, nil
	}
	s.StopRequested = true
	effects := []Effect{info(MsgCancelling, nil)}
	if s.Step == StepCheck {
		effects = append(effects, CancelCheck{})
	}
	return s, effects
}

// finish gives back everything the session holds and ends it.
func (s State) finish(effects []Effect) (State, []Effect) {
	if s.OneTimeCredential {
		effects = append(effects, DeleteCredential{})
		s.OneTimeCredential = false
	}
	if s.TunnelOpen {
		effects = append(effects, CloseTunnel{})
		s.TunnelOpen = false
		s.Conns = 0
	}
	if s.HasRoute {
		effects = append(effects, ReleaseRoute{})
		s.HasRoute = false
	}
	s.Step = StepDone
	s.Killing = false
	switch {
	case s.Failure != nil:
		s.Outcome = OutcomeFailed
	case s.StopRequested:
		s.Outcome = OutcomeCancelled
	default:
		s.Outcome = OutcomeClosed
	}
	return s, append(effects, info(MsgEnded, map[string]any{"outcome": string(s.Outcome)}))
}

func (s State) unexpected(e Event) (State, []Effect) {
	return s, []Effect{logLine(LevelWarn, MsgUnexpected, map[string]any{
		"event": fmt.Sprintf("%T", e),
		"step":  string(s.Step),
	})}
}

func info(msg string, args map[string]any) Log { return logLine(LevelInfo, msg, args) }

func logLine(level Level, msg string, args map[string]any) Log {
	return Log{Level: level, Msg: msg, Args: args}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// withError adds err's text and code to a log line's arguments, and the
// arguments its translated message needs (errcode.WithArgs) as "errorArgs".
func withError(args map[string]any, err error) map[string]any {
	if args == nil {
		args = map[string]any{}
	}
	args["error"] = errText(err)
	args["code"] = errcode.Of(err)
	if a := errcode.Args(err); a != nil {
		args["errorArgs"] = a
	}
	return args
}
