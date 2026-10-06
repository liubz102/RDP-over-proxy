package session

import (
	"context"
	"net/netip"
	"sync"

	"github.com/liubz102/RDP-over-proxy/internal/probe"
	"github.com/liubz102/RDP-over-proxy/internal/route"
	"github.com/liubz102/RDP-over-proxy/internal/tunnel"
)

// Process is a running mstsc that the session started.
type Process interface {
	PID() int
	// Wait blocks until the process exits and returns its exit code. The
	// session calls it once.
	Wait() (exitCode int, err error)
	// Close asks mstsc to close (WM_CLOSE to its session window), which
	// may ask the user to confirm. closing is false when there was no window
	// that could ask; only Kill ends mstsc then.
	Close() (closing bool, err error)
	// Kill ends the process. Killing a process that has already exited is
	// not an error.
	Kill() error
	// Focus brings mstsc's window to the front.
	Focus() error
	// ShowName keeps name (the profile's) in the title of mstsc's window
	// while it runs. Started with /v:, mstsc titles it with the tunnel
	// entrance only, which says nothing about the computer behind it.
	ShowName(name string) error
}

// Credentials writes and removes what mstsc signs in with.
type Credentials interface {
	// Prepare stores the password or the user name hint for the session.
	// oneTime reports a password stored for this session only, which Delete
	// removes when the session ends.
	Prepare() (oneTime bool, err error)
	Delete() error
}

// noCredentials writes nothing: mstsc asks for the password itself.
type noCredentials struct{}

func (noCredentials) Prepare() (bool, error) { return false, nil }
func (noCredentials) Delete() error          { return nil }

// Params describe one session.
type Params struct {
	// Entrance is where the tunnel listens: the profile's loopback address
	// and the configured port.
	Entrance netip.AddrPort
	// Target is where the tunnel's connections go, as "host:port".
	Target     string
	CheckFirst bool
	// Args returns mstsc's arguments for the tunnel's actual address.
	Args func(entrance netip.AddrPort) []string
	// Name is the profile's name, for mstsc's window title.
	Name string
}

// Deps are the parts of the outside world a session uses. Tests replace
// them; Manager builds the real ones.
type Deps struct {
	Preflight func() error
	// Route takes the session's route. It may wait on the network (Windows'
	// automatic proxy configuration) and ends when ctx is cancelled.
	Route       func(ctx context.Context) (d route.Dialer, release func(), err error)
	Credentials Credentials
	Launch      func(args []string) (Process, error)
	// Changed receives every new state and Log every log line. Both are
	// called from the session's goroutine; they must not wait on the
	// session.
	Changed func(State)
	Log     func(Log)
}

// actor runs one session: it performs the effects Reduce asks for, one at a
// time, and posts their outcomes back to itself as events.
type actor struct {
	params Params
	deps   Deps
	inbox  *mailbox
	// done is closed once the session has ended and everything it held has
	// been given back.
	done chan struct{}

	// The rest belongs to the actor's goroutine.
	state       State
	dialer      route.Dialer
	release     func()
	tunnel      *tunnel.Tunnel
	cancelRoute context.CancelFunc
	cancelCheck context.CancelFunc
	proc        Process
}

func startActor(p Params, d Deps) *actor {
	a := &actor{params: p, deps: d, inbox: newMailbox(), done: make(chan struct{})}
	go a.run()
	return a
}

// post hands the session an event. It never blocks, so the tunnel and the
// waiting goroutines can report at any time, even while the actor is busy
// closing them.
func (a *actor) post(e Event) { a.inbox.post(e) }

func (a *actor) run() {
	defer close(a.done)
	state, effects := Start(a.params.CheckFirst)
	a.state = state
	a.perform(effects)
	a.deps.Changed(a.state)
	for a.state.Step != StepDone {
		for _, e := range a.inbox.take() {
			if r, ok := e.(RouteReady); ok && r.release != nil {
				// The route is held from here on; Reduce decides what is
				// done with it (a stop meanwhile gives it back at once).
				a.dialer, a.release = r.dialer, r.release
			}
			prev := a.state
			a.state, effects = Reduce(a.state, e)
			// Effects run before the new state is published, so a published
			// "ended" means everything has already been given back.
			a.perform(effects)
			if a.state != prev {
				a.deps.Changed(a.state)
			}
			if a.state.Step == StepDone {
				break // anything still queued would be ignored
			}
		}
	}
}

func (a *actor) perform(effects []Effect) {
	for _, e := range effects {
		switch e := e.(type) {
		case Log:
			a.deps.Log(e)

		case Preflight:
			a.result(StepPreflight, PreflightPassed{}, a.deps.Preflight())

		case AcquireRoute:
			// Its own goroutine: following Windows' proxy setting may wait
			// for the network, and a stop must not wait for that.
			ctx, cancel := context.WithCancel(context.Background())
			a.cancelRoute = cancel
			go func() {
				defer cancel()
				d, release, err := a.deps.Route(ctx)
				if err != nil {
					a.post(StepFailed{Step: StepRoute, Err: err})
					return
				}
				a.post(RouteReady{dialer: d, release: release})
			}()

		case CancelRoute:
			a.cancelRoute()

		case Listen:
			t, err := tunnel.Listen(a.params.Entrance, a.params.Target, a.dialer, reporter{a.inbox})
			if err != nil {
				a.post(StepFailed{Step: StepListen, Err: err})
				continue
			}
			a.tunnel = t
			a.post(Listening{Addr: t.Addr().String()})

		case RunCheck:
			ctx, cancel := context.WithCancel(context.Background())
			a.cancelCheck = cancel
			d, target := a.dialer, a.params.Target
			go func() {
				defer cancel()
				r, err := probe.Check(ctx, d, target)
				if err != nil {
					a.post(StepFailed{Step: StepCheck, Err: err})
					return
				}
				a.post(CheckPassed{Result: r})
			}()

		case CancelCheck:
			a.cancelCheck()

		case PrepareCredential:
			oneTime, err := a.deps.Credentials.Prepare()
			a.result(StepCredential, CredentialReady{OneTime: oneTime}, err)

		case LaunchClient:
			p, err := a.deps.Launch(a.params.Args(a.tunnel.Addr()))
			if err != nil {
				a.post(StepFailed{Step: StepLaunch, Err: err})
				continue
			}
			a.proc = p
			// A title without the name is no reason to stop: the warning is
			// enough.
			a.action("showName", p.ShowName(a.params.Name))
			a.post(ClientStarted{PID: p.PID()})
			go func() {
				code, err := p.Wait()
				if err != nil {
					code = -1 // the exit status could not be read
				}
				a.post(ClientExited{ExitCode: code})
			}()

		case CloseClient:
			closing, err := a.proc.Close()
			a.action("close", err)
			if err == nil && !closing {
				a.post(NothingToClose{})
			}
		case KillClient:
			a.action("kill", a.proc.Kill())
		case FocusClient:
			a.action("focus", a.proc.Focus())

		case DeleteCredential:
			a.action("deleteCredential", a.deps.Credentials.Delete())
		case CloseTunnel:
			a.tunnel.Close()
		case ReleaseRoute:
			a.release()
		}
	}
}

// result posts a step's success event, or StepFailed when err is set.
func (a *actor) result(step Step, ok Event, err error) {
	if err != nil {
		a.post(StepFailed{Step: step, Err: err})
		return
	}
	a.post(ok)
}

// action logs the failure of an action that has no event of its own.
func (a *actor) action(name string, err error) {
	if err != nil {
		a.deps.Log(logLine(LevelWarn, MsgActionFailed, withError(map[string]any{"action": name}, err)))
	}
}

// reporter turns the tunnel's reports into events.
type reporter struct{ m *mailbox }

func (r reporter) ConnOpened()              { r.m.post(ConnOpened{}) }
func (r reporter) ConnClosed()              { r.m.post(ConnClosed{}) }
func (r reporter) UpstreamAnswered()        { r.m.post(UpstreamAnswered{}) }
func (r reporter) UpstreamFailed(err error) { r.m.post(UpstreamFailed{Err: err}) }
func (r reporter) ListenerFailed(err error) { r.m.post(TunnelFailed{Err: err}) }

// mailbox is an unbounded queue of events. Posting never blocks; take waits
// until there is something to take.
type mailbox struct {
	mu     sync.Mutex
	queue  []Event
	signal chan struct{}
}

func newMailbox() *mailbox { return &mailbox{signal: make(chan struct{}, 1)} }

func (m *mailbox) post(e Event) {
	m.mu.Lock()
	m.queue = append(m.queue, e)
	m.mu.Unlock()
	select {
	case m.signal <- struct{}{}:
	default: // a wake-up is already pending
	}
}

// take returns every queued event, in order, waiting for one if none is
// queued.
func (m *mailbox) take() []Event {
	for {
		m.mu.Lock()
		q := m.queue
		m.queue = nil
		m.mu.Unlock()
		if len(q) > 0 {
			return q
		}
		<-m.signal
	}
}
