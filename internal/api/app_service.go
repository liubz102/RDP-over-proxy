package api

import (
	"slices"

	"github.com/liubz102/RDP-over-proxy/internal/logging"
)

// AppService is about the app as a whole: notices and its own log.
type AppService struct{ c *Core }

// NewAppService returns the service.
func NewAppService(c *Core) *AppService { return &AppService{c: c} }

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
