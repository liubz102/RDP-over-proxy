package session

import (
	"errors"
	"fmt"
	"net/netip"
	"sync"

	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/mstsc"
	"github.com/liubz102/RDP-over-proxy/internal/route"
)

// Request is everything needed to start a session.
type Request struct {
	Profile model.Profile
	// Proxy is the proxy Profile.ProxyID names (model.DirectProxy() for
	// direct connections).
	Proxy model.Proxy
	// Port is the port entrances listen on (Settings.LocalPort). Zero lets
	// the system pick a free one; only tests do that.
	Port int
	// CheckFirst runs the route check before mstsc starts
	// (Settings.CheckRouteBeforeConnect).
	CheckFirst bool
}

// Options are what a Manager needs from the rest of the app.
type Options struct {
	// Routes and Launch are required.
	Routes route.Provider
	Launch func(args []string) (Process, error)
	// Preflight adds local checks before a session acquires anything (the RD
	// Gateway check arrives in M4). Optional.
	Preflight func(Request) error
	// Credentials gives a session what to sign in with (M4). Without it
	// nothing is written and mstsc asks for the password itself.
	Credentials func(Request) Credentials
	// Changed receives every state of every session, and Log every log line.
	// Calls are made one at a time and, for each profile, in order. They may
	// call the Manager's methods except Quit, which waits for sessions that
	// would be waiting for the callback to return. Optional.
	Changed func(profileID string, s State)
	Log     func(profileID string, l Log)
}

// Errors from Connect.
var (
	ErrQuitting = errors.New("the app is quitting")
	// ErrEnding: the profile's previous session is still being stopped.
	ErrEnding = errors.New("the previous session of this connection is still ending")
	// ErrLoopbackDirect: a direct connection to this computer itself would
	// connect the tunnel to itself.
	ErrLoopbackDirect = errors.New("the target is this computer itself; without a proxy the tunnel would connect to itself")
)

// Manager runs at most one session per profile.
type Manager struct {
	opts Options

	// cb keeps the callbacks in order across sessions: an old session's last
	// report is always delivered before its successor's first.
	cb sync.Mutex

	mu       sync.Mutex
	running  map[string]*actor
	states   map[string]State // the latest state of every session, ended ones included
	quitting bool
}

// NewManager returns a Manager.
func NewManager(opts Options) *Manager {
	return &Manager{opts: opts, running: map[string]*actor{}, states: map[string]State{}}
}

// Connect starts a session for req.Profile. When the profile already has a
// session, it brings mstsc to the front instead and reports focused. Connect
// returns at once; progress arrives through Options.Changed.
func (m *Manager) Connect(req Request) (focused bool, err error) {
	if err := validate(req); err != nil {
		return false, err
	}
	id := req.Profile.ID
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.quitting {
		return false, ErrQuitting
	}
	if a := m.running[id]; a != nil {
		switch m.states[id].Phase() {
		case PhaseEnding:
			return false, ErrEnding
		case PhaseEnded:
			// It has given everything back and is only returning; a new
			// session can take its place.
		default:
			a.post(Focus{})
			return true, nil
		}
	}
	// The previous session's state goes; until the new one reports, the
	// profile counts as starting.
	delete(m.states, id)
	a := startActor(m.params(req), m.deps(req))
	m.running[id] = a
	go m.reap(id, a)
	return false, nil
}

// Stop stops the profile's session (see the Stop event). It reports whether
// there was one.
func (m *Manager) Stop(profileID string, force bool) bool {
	return m.post(profileID, Stop{Force: force})
}

// Focus brings the profile's mstsc to the front. It reports whether there
// was a session.
func (m *Manager) Focus(profileID string) bool {
	return m.post(profileID, Focus{})
}

func (m *Manager) post(profileID string, e Event) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	a := m.running[profileID]
	// A session that has published its end is over, even if reap has not
	// removed it yet.
	if a == nil || m.states[profileID].Step == StepDone {
		return false
	}
	a.post(e)
	return true
}

// States returns the latest state of every session started, ended ones
// included, by profile ID.
func (m *Manager) States() map[string]State {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]State, len(m.states))
	for id, s := range m.states {
		out[id] = s
	}
	return out
}

// Running counts the sessions that have not ended.
func (m *Manager) Running() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id := range m.running {
		if m.states[id].Step != StepDone {
			n++
		}
	}
	return n
}

// Quit stops every session by force and waits until each has given back
// everything it held. Connect fails from then on.
func (m *Manager) Quit() {
	m.mu.Lock()
	m.quitting = true
	actors := make([]*actor, 0, len(m.running))
	for _, a := range m.running {
		actors = append(actors, a)
	}
	m.mu.Unlock()
	for _, a := range actors {
		a.post(Stop{Force: true})
	}
	for _, a := range actors {
		<-a.done
	}
}

// reap forgets an actor once it has finished.
func (m *Manager) reap(id string, a *actor) {
	<-a.done
	m.mu.Lock()
	if m.running[id] == a {
		delete(m.running, id)
	}
	m.mu.Unlock()
}

func validate(req Request) error {
	if err := req.Profile.Validate(); err != nil {
		return err
	}
	if err := req.Proxy.Validate(); err != nil {
		return fmt.Errorf("proxy: %w", err)
	}
	if req.Proxy.ID != req.Profile.ProxyID {
		return fmt.Errorf("the profile uses proxy %q, but proxy %q was given", req.Profile.ProxyID, req.Proxy.ID)
	}
	if req.Port < 0 || req.Port > 65535 {
		return fmt.Errorf("port %d is outside 0-65535", req.Port)
	}
	return nil
}

func (m *Manager) params(req Request) Params {
	loopback := netip.MustParseAddr(req.Profile.Loopback) // checked by validate
	return Params{
		Entrance:   netip.AddrPortFrom(loopback, uint16(req.Port)),
		Target:     req.Profile.Target.String(),
		CheckFirst: req.CheckFirst,
		Args:       func(entrance netip.AddrPort) []string { return mstsc.Args(entrance, req.Profile) },
	}
}

func (m *Manager) deps(req Request) Deps {
	id := req.Profile.ID
	var creds Credentials = noCredentials{}
	if m.opts.Credentials != nil {
		creds = m.opts.Credentials(req)
	}
	return Deps{
		Preflight: func() error {
			if req.Proxy.Kind == model.KindDirect && req.Profile.Target.IsLoopback() {
				return ErrLoopbackDirect
			}
			if m.opts.Preflight != nil {
				return m.opts.Preflight(req)
			}
			return nil
		},
		Route:       func() (route.Dialer, func(), error) { return m.opts.Routes.Acquire(req.Proxy) },
		Credentials: creds,
		Launch:      m.opts.Launch,
		Changed: func(s State) {
			m.cb.Lock()
			defer m.cb.Unlock()
			m.mu.Lock()
			m.states[id] = s
			m.mu.Unlock()
			if m.opts.Changed != nil {
				m.opts.Changed(id, s)
			}
		},
		Log: func(l Log) {
			if m.opts.Log == nil {
				return
			}
			m.cb.Lock()
			defer m.cb.Unlock()
			m.opts.Log(id, l)
		},
	}
}
