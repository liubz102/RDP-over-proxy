package api

import (
	"errors"

	"github.com/liubz102/RDP-over-proxy/internal/diag"
)

// DiagService reports what on this computer affects Remote Desktop
// connections, and opens the places where it is changed. It never changes a
// setting itself.
type DiagService struct{ c *Core }

// NewDiagService returns the service.
func NewDiagService(c *Core) *DiagService { return &DiagService{c: c} }

// errUnavailable: the build has no way to do it (a test's Core).
var errUnavailable = errors.New("not available")

// Report gathers the environment report anew, item by item in the order of
// the groups. It only reads. Until WMI has said whether Credential Guard
// runs, that item says it is being checked, and EventDiagChanged brings the
// report again once it has.
func (s *DiagService) Report() []diag.Item {
	return s.c.report()
}

// EditDefaults opens Remote Desktop Connection on Default.rdp, where the
// settings every connection shares are changed and saved. It returns once
// mstsc has started.
func (s *DiagService) EditDefaults() error {
	if s.c.d.EditDefaults == nil {
		return errUnavailable
	}
	return s.c.d.EditDefaults()
}

// OpenLogs shows the log folder in File Explorer, for attaching the log to
// a bug report. The log file has names and addresses masked.
func (s *DiagService) OpenLogs() error {
	return s.c.openFolder(s.c.d.Folders.Logs)
}

// openFolder shows one of the app's folders in File Explorer.
func (c *Core) openFolder(path string) error {
	if c.d.OpenFolder == nil || path == "" {
		return errUnavailable
	}
	return c.d.OpenFolder(path)
}

// report gathers the environment report.
func (c *Core) report() []diag.Item {
	if c.d.Diagnose == nil {
		return []diag.Item{}
	}
	f := c.d.Diagnose()
	f.CredentialGuard = c.credentialGuard()
	return diag.Build(f)
}

// credentialGuard is what is known about Credential Guard. WMI says whether
// it runs, and WMI may take long or not answer at all (a damaged repository
// is just when people look at diagnostics), so it is asked once, in the
// background, and nothing waits for it: EventDiagChanged brings the report
// once the answer is in. Credential Guard changes only with a restart, so
// the answer is kept.
func (c *Core) credentialGuard() diag.Running {
	c.guardOnce.Do(func() {
		if c.d.CredentialGuard == nil {
			c.guard.Store(int32(diag.RunningUnknown))
			return
		}
		c.guard.Store(int32(diag.RunningChecking))
		go func() {
			running, err := c.d.CredentialGuard()
			state := diag.RunningNo
			switch {
			case err != nil:
				state = diag.RunningUnknown
				c.d.Log.Warnf("ask WMI whether Credential Guard runs: %v", err)
			case running:
				state = diag.RunningYes
			}
			c.guard.Store(int32(state))
			c.d.Emit(EventDiagChanged, c.report())
		}()
	})
	return diag.Running(c.guard.Load())
}
