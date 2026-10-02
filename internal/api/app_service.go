package api

import (
	"slices"

	"github.com/liubz102/RDP-over-proxy/internal/logging"
)

// AppService is about the app as a whole: notices, its own log, quitting.
type AppService struct {
	c    *Core
	quit func()
}

// NewAppService returns the service. quit ends the app; it returns at once,
// and the shutdown ends every session.
func NewAppService(c *Core, quit func()) *AppService { return &AppService{c: c, quit: quit} }

// Quit quits the app. Quitting ends every remote desktop, so while any is
// connected an unconfirmed request only reports how many, and the UI asks
// the user before calling again with confirmed.
func (s *AppService) Quit(confirmed bool) QuitView {
	if n := s.c.Running(); n > 0 && !confirmed {
		return QuitView{Connected: n}
	}
	s.quit()
	return QuitView{}
}

// KeepRunning answers a request to quit with "no": the user cancelled the
// confirmation.
func (s *AppService) KeepRunning() {
	s.c.quitAsked.Store(false)
}

// Notices returns the notices not yet dismissed, oldest first. Notices that
// arrive later come as EventNotice.
func (s *AppService) Notices() []Notice {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	return append([]Notice{}, s.c.notices...)
}

// Dismiss removes a notice.
func (s *AppService) Dismiss(id int) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	s.c.notices = slices.DeleteFunc(s.c.notices, func(n Notice) bool { return n.ID == id })
}

// Log returns the app's recent log lines, oldest first, with nothing
// masked: they are for the user's own screen.
func (s *AppService) Log() []logging.Line {
	return s.c.d.Log.Recent()
}
