package api

import (
	"cmp"
	"context"
	"slices"

	"github.com/liubz102/RDP-over-proxy/internal/logging"
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/probe"
	"github.com/liubz102/RDP-over-proxy/internal/session"
	"github.com/liubz102/RDP-over-proxy/internal/store"
)

// SessionService connects, disconnects and checks routes. Progress arrives
// as EventSessionsChanged and EventSessionLog.
type SessionService struct{ c *Core }

// NewSessionService returns the service.
func NewSessionService(c *Core) *SessionService { return &SessionService{c: c} }

// Connect starts a session for the profile and returns at once. When the
// profile is already connected, its window comes to the front instead.
//
// password, when set, is used for this connection: a profile that remembers
// passwords saves it as its password; otherwise it is kept for this session
// only and deleted when the session ends.
func (s *SessionService) Connect(profileID, password string) (ConnectResult, error) {
	if err := checkPassword(password); err != nil {
		return ConnectResult{}, err
	}
	s.c.lifecycle.Lock()
	defer s.c.lifecycle.Unlock()
	req, err := s.request(profileID)
	if err != nil {
		return ConnectResult{}, err
	}
	if password != "" {
		if req.Profile.RememberPassword {
			if err := s.c.creds.remember(s.c.server(req.Profile), req.Profile.Username, password); err != nil {
				return ConnectResult{}, err
			}
			defer s.c.dataChanged()
		} else {
			req.Password = password
		}
	}
	focused, err := s.c.manager.Connect(req)
	return ConnectResult{Focused: focused}, err
}

// request gathers what a session of the profile needs.
func (s *SessionService) request(profileID string) (session.Request, error) {
	p, ok := s.c.d.Data.Profile(profileID)
	if !ok {
		return session.Request{}, store.ErrNotFound
	}
	px, ok := s.c.d.Data.Proxy(p.ProxyID)
	if !ok {
		return session.Request{}, ErrProxyMissing
	}
	st := s.c.d.Settings.Get()
	return session.Request{Profile: p, Proxy: px, Port: st.LocalPort, CheckFirst: st.CheckRouteBeforeConnect}, nil
}

// Disconnect ends the profile's session. Before mstsc runs it cancels;
// afterwards it asks mstsc to close, which may ask the user to confirm.
// force ends mstsc instead.
func (s *SessionService) Disconnect(profileID string, force bool) error {
	if !s.c.manager.Stop(profileID, force) {
		return ErrNoSession
	}
	return nil
}

// Focus brings the profile's Remote Desktop window to the front.
func (s *SessionService) Focus(profileID string) error {
	if !s.c.manager.Focus(profileID) {
		return ErrNoSession
	}
	return nil
}

// States returns the latest state of every session since the app started,
// ended ones included.
func (s *SessionService) States() []SessionView {
	out := []SessionView{}
	for id, st := range s.c.manager.States() {
		out = append(out, sessionView(id, st))
	}
	slices.SortFunc(out, func(a, b SessionView) int { return cmp.Compare(a.ProfileID, b.ProfileID) })
	return out
}

// Log returns the log of the profile's latest session, oldest line first.
func (s *SessionService) Log(profileID string) []logging.Line {
	s.c.mu.Lock()
	ring := s.c.logs[profileID]
	s.c.mu.Unlock()
	if ring == nil {
		return []logging.Line{}
	}
	return ring.Lines()
}

// CheckRoute checks that the profile's target answers as an RDP server
// through its proxy, without starting Remote Desktop. It waits as long as
// the route takes; the frontend cancels the call to stop it.
func (s *SessionService) CheckRoute(ctx context.Context, profileID string) (CheckView, error) {
	req, err := s.request(profileID)
	if err != nil {
		return CheckView{}, err
	}
	if req.Proxy.Kind == model.KindDirect && req.Profile.Target.IsLoopback() {
		return CheckView{}, session.ErrLoopbackDirect
	}
	d, release, err := s.c.d.Routes.Acquire(req.Proxy)
	if err != nil {
		return CheckView{}, err
	}
	defer release()
	r, err := probe.Check(ctx, d, req.Profile.Target.String())
	if err != nil {
		return CheckView{}, err
	}
	return *checkView(r), nil
}
