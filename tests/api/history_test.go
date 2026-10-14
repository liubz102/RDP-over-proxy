package api_test

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	. "github.com/liubz102/RDP-over-proxy/internal/api"
	"github.com/liubz102/RDP-over-proxy/internal/logging"
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/mstsc"
	"github.com/liubz102/RDP-over-proxy/tests/testutil"
)

// fakeHistory stands in for Remote Desktop's memory of recent computers: a
// list that Forget prunes, and the number of calls. jump is how many jump
// list entries each call says it removed.
type fakeHistory struct {
	mu    sync.Mutex
	list  []string
	jump  int
	calls int
	err   error
}

func (f *fakeHistory) Forget(drop func(entry string) bool) (mstsc.Forgotten, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	var out mstsc.Forgotten
	var kept []string
	for _, e := range f.list {
		if drop(e) {
			out.Dropped = append(out.Dropped, e)
		} else {
			kept = append(kept, e)
		}
	}
	f.list = kept
	out.JumpList = f.jump
	return out, f.err
}

func (f *fakeHistory) set(list []string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.list, f.err = list, err
}

func (f *fakeHistory) state() ([]string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.list), f.calls
}

func (h *harness) cleanHistory(t *testing.T, mode string) {
	t.Helper()
	st := h.settings.Get()
	st.CleanHistory = mode
	if err := h.settings.Save(st); err != nil {
		t.Fatal(err)
	}
}

// The app starts by cleaning up, and cleans up again once each session has
// ended, before the frontend hears that it has.
func TestHistoryIsCleanedWhenASessionEnds(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	h := newHarness(t)
	if _, calls := h.history.state(); calls != 1 {
		t.Fatalf("Start cleaned up %d times, want once", calls)
	}
	px := h.proxy(t, "Office")
	p := h.profile(t, "PC", px.ID, srv.Addr, "")
	if _, err := h.sessions.Connect(p.ID, ""); err != nil {
		t.Fatal(err)
	}
	<-h.launched
	running := h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "running" })
	// mstsc remembers the entrance as it connects.
	h.history.set([]string{running.Addr, "pc.example.com:3390"}, nil)
	h.history.mu.Lock()
	h.history.jump = 1
	h.history.mu.Unlock()
	if err := h.sessions.Disconnect(p.ID, false); err != nil {
		t.Fatal(err)
	}
	h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "ended" })
	if list, _ := h.history.state(); !slices.Equal(list, []string{"pc.example.com:3390"}) {
		t.Fatalf("after the session the list is %q", list)
	}
	if !logged(h.log, logging.LevelInfo, "removed "+running.Addr+" from Remote Desktop's list") {
		t.Fatalf("the removal was not logged: %+v", h.log.Recent())
	}
	if !logged(h.log, logging.LevelInfo, "removed 1 entries from Remote Desktop's jump list") {
		t.Fatalf("the jump list was not logged: %+v", h.log.Recent())
	}
}

func TestHistoryFollowsTheSetting(t *testing.T) {
	h := newHarness(t)
	list := []string{"127.1.2.3:13389", "127.0.0.1:3390", "pc.example.com"}

	h.history.set(list, nil)
	h.core.ForgetHistory() // the default: this app's addresses
	if got, _ := h.history.state(); !slices.Equal(got, list[1:]) {
		t.Fatalf("app range: %q", got)
	}

	h.cleanHistory(t, model.HistoryLoopback)
	h.core.ForgetHistory()
	if got, _ := h.history.state(); !slices.Equal(got, list[2:]) {
		t.Fatalf("all of 127/8: %q", got)
	}

	h.cleanHistory(t, model.HistoryKeep)
	h.history.set(list, nil)
	_, before := h.history.state()
	h.core.ForgetHistory()
	if got, calls := h.history.state(); !slices.Equal(got, list) || calls != before {
		t.Fatalf("keep: %q after %d calls, %d before", got, calls, before)
	}
}

// A failure is logged once, until it changes or goes away.
func TestHistoryFailureIsLoggedOnce(t *testing.T) {
	h := newHarness(t)
	h.history.set(nil, errors.New("open Default.rdp: Access is denied."))
	for range 3 {
		h.core.ForgetHistory()
	}
	if n := countLogged(h.log, logging.LevelWarn, "Access is denied"); n != 1 {
		t.Fatalf("the failure was logged %d times", n)
	}
	h.history.set(nil, errors.New("open Default.rdp: The process cannot access the file"))
	h.core.ForgetHistory()
	h.history.set(nil, nil)
	h.core.ForgetHistory()
	h.core.ForgetHistory()
	if n := countLogged(h.log, logging.LevelWarn, "cannot access"); n != 1 {
		t.Fatalf("the other failure was logged %d times", n)
	}
	if n := countLogged(h.log, logging.LevelInfo, "works again"); n != 1 {
		t.Fatalf("the recovery was logged %d times", n)
	}
}

func logged(l *logging.Logger, level, text string) bool { return countLogged(l, level, text) > 0 }

func countLogged(l *logging.Logger, level, text string) int {
	n := 0
	for _, line := range l.Recent() {
		if line.Level == level && strings.Contains(line.Msg, text) {
			n++
		}
	}
	return n
}
