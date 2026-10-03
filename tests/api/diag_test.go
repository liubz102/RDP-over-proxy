package api_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/api"

	"errors"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/diag"
	"github.com/liubz102/RDP-over-proxy/internal/logging"
	"github.com/liubz102/RDP-over-proxy/internal/rdpfile"
	"github.com/liubz102/RDP-over-proxy/tests/testutil"
)

func TestDefaultsAreNotedBeforeConnecting(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	h := newHarness(t)
	p := h.profile(t, "PC", h.proxy(t, "Office").ID, srv.Addr, "")
	// connect runs a session until mstsc is up, closes it, and returns the
	// keys of its log.
	connect := func() []string {
		t.Helper()
		if _, err := h.sessions.Connect(p.ID, ""); err != nil {
			t.Fatal(err)
		}
		proc := <-h.launched
		h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "running" })
		proc.Close()
		h.events.session(p.ID, func(v SessionView) bool { return v.Phase == "ended" })
		var keys []string
		for _, l := range h.sessions.Log(p.ID) {
			keys = append(keys, l.Msg)
		}
		return keys
	}

	if keys := connect(); slices.Contains(keys, MsgServerAuthRefuse) || slices.Contains(keys, MsgAlwaysPrompt) {
		t.Fatalf("notes about the defaults mstsc would use anyway: %q", keys)
	}

	// "Do not connect" when the computer cannot be verified; "always ask",
	// with no password to pass over.
	h.defaults.ServerAuth = rdpfile.ServerAuthRefuse
	h.defaults.AlwaysPrompt = true
	keys := connect()
	if !slices.Contains(keys, MsgServerAuthRefuse) || slices.Contains(keys, MsgAlwaysPrompt) {
		t.Fatalf("log: %q", keys)
	}
	// "Always ask" passes over a saved password.
	h.defaults.ServerAuth = rdpfile.ServerAuthWarn
	if err := h.vault.Save(p.Loopback, p.Username, "secret", false); err != nil {
		t.Fatal(err)
	}
	if keys := connect(); !slices.Contains(keys, MsgAlwaysPrompt) || slices.Contains(keys, MsgServerAuthRefuse) {
		t.Fatalf("log with a saved password: %q", keys)
	}

	// A policy that says "do not connect" is named as such: only an
	// administrator can change it.
	h.defaults.ServerAuth, h.defaults.ServerAuthByPolicy = rdpfile.ServerAuthRefuse, true
	if keys := connect(); !slices.Contains(keys, MsgServerAuthRefusePolicy) || slices.Contains(keys, MsgServerAuthRefuse) {
		t.Fatalf("log with the policy: %q", keys)
	}

	// Defaults that cannot be read do not stop the connection.
	h.defaultsErr = errors.New("access denied")
	if keys := connect(); !slices.Contains(keys, MsgDefaultsUnknown) {
		t.Fatalf("log with unreadable defaults: %q", keys)
	}
}

func TestDiagService(t *testing.T) {
	answer := make(chan struct{})
	var asked atomic.Int32
	ev := &events{changed: make(chan struct{}, 1)}
	var edits, opens int
	c := NewCore(Deps{
		Diagnose: func() diag.Facts {
			return diag.Facts{App: "0.1.0", Join: diag.JoinWorkgroup, EncryptionOracle: -1}
		},
		// WMI answers when the test lets it.
		CredentialGuard: func() (bool, error) {
			asked.Add(1)
			<-answer
			return true, nil
		},
		EditDefaults: func() error { edits++; return nil },
		OpenLogs:     func() error { opens++; return errors.New("no explorer") },
		Log:          logging.New(nil, logging.LevelInfo, 10),
		Emit:         ev.emit,
	})
	s := NewDiagService(c)
	find := func(items []diag.Item, key string) diag.Item {
		for _, it := range items {
			if it.Key == key {
				return it
			}
		}
		t.Fatalf("no %s in %+v", key, items)
		return diag.Item{}
	}

	// The report does not wait for WMI.
	report := s.Report()
	if app := find(report, "app"); app.Text != "0.1.0" {
		t.Errorf("app = %+v", app)
	}
	if g := find(report, "credentialGuard"); g.Value != "checking" {
		t.Fatalf("credentialGuard before WMI answered = %+v", g)
	}
	if g := find(s.Report(), "credentialGuard"); g.Value != "checking" {
		t.Fatalf("credentialGuard asked again = %+v", g)
	}
	// Its answer brings the report again, and is kept.
	close(answer)
	for len(ev.named(EventDiagChanged)) == 0 {
		<-ev.changed
	}
	if g := find(ev.named(EventDiagChanged)[0].([]diag.Item), "credentialGuard"); g.Value != "running" {
		t.Fatalf("credentialGuard in the event = %+v", g)
	}
	if g := find(s.Report(), "credentialGuard"); g.Value != "running" {
		t.Fatalf("credentialGuard after WMI answered = %+v", g)
	}
	if n := asked.Load(); n != 1 {
		t.Errorf("WMI was asked %d times, want once", n)
	}

	if err := s.EditDefaults(); err != nil || edits != 1 {
		t.Errorf("EditDefaults = %v, %d calls", err, edits)
	}
	if err := s.OpenLogs(); err == nil || opens != 1 {
		t.Errorf("OpenLogs = %v, %d calls", err, opens)
	}

	// Without them (a test's Core) the calls fail instead of panicking, and
	// the report is empty rather than null.
	none := NewDiagService(NewCore(Deps{Log: logging.New(nil, logging.LevelInfo, 10)}))
	if got := none.Report(); got == nil || len(got) != 0 {
		t.Errorf("Report without Diagnose = %#v", got)
	}
	if none.EditDefaults() == nil || none.OpenLogs() == nil {
		t.Error("EditDefaults and OpenLogs without a way to do them should fail")
	}
	// Without a way to ask WMI, Credential Guard is unknown and no event comes.
	quiet := &events{changed: make(chan struct{}, 1)}
	noWMI := NewDiagService(NewCore(Deps{
		Diagnose: func() diag.Facts { return diag.Facts{} },
		Log:      logging.New(nil, logging.LevelInfo, 10),
		Emit:     quiet.emit,
	}))
	if g := find(noWMI.Report(), "credentialGuard"); g.Value != "unknown" || len(quiet.named(EventDiagChanged)) != 0 {
		t.Errorf("credentialGuard without WMI = %+v", g)
	}
}
