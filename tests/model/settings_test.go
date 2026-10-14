package model_test

import (
	"strings"
	"testing"

	. "github.com/liubz102/RDP-over-proxy/internal/model"
)

func TestDefaultSettingsAreValid(t *testing.T) {
	d := DefaultSettings()
	if err := d.Validate(); err != nil {
		t.Fatalf("defaults do not validate: %v", err)
	}
	if d.Language != "" {
		t.Fatalf("default language must be empty so the first-run picker shows, got %q", d.Language)
	}
}

func TestNormalizeReplacesInvalidValues(t *testing.T) {
	got := Settings{Language: LangEn, Theme: "purple", CheckRouteBeforeConnect: false, CleanHistory: "sometimes"}.Normalize()
	want := DefaultSettings()
	want.Language = LangEn
	// Booleans are taken as given: Normalize cannot tell "missing" from
	// "switched off". Missing fields get their defaults when the file is
	// decoded on top of DefaultSettings (see store.SettingsStore.Load).
	want.CheckRouteBeforeConnect = false
	if got != want {
		t.Fatalf("Normalize() = %+v, want %+v", got, want)
	}
}

func TestNormalizeKeepsValidChoices(t *testing.T) {
	in := Settings{Language: LangZhCN, Theme: ThemeDark, CloseBehavior: CloseQuit, LocalPort: 23389,
		TestURL: "https://example.com/204", LogLevel: LogDebug, CleanHistory: HistoryLoopback}
	got := in.Normalize()
	in.Schema = SettingsSchema
	if got != in {
		t.Fatalf("Normalize() changed valid values: %+v", got)
	}
}

func TestRepairFixesWhatValidateRejects(t *testing.T) {
	got := Settings{Language: "fr", LocalPort: 70000}.Repair()
	if got.Language != "" {
		t.Errorf("unknown language should be cleared, got %q", got.Language)
	}
	if got.LocalPort != DefaultLocalPort {
		t.Errorf("out-of-range port should reset to %d, got %d", DefaultLocalPort, got.LocalPort)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("repaired settings do not validate: %v", err)
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	cases := map[string]Settings{
		"unknown language": {Language: "fr", LocalPort: DefaultLocalPort},
		"port zero":        {LocalPort: 0},
		"port too high":    {LocalPort: 70000},
	}
	for name, s := range cases {
		if err := s.Validate(); err == nil {
			t.Errorf("%s: Validate() = nil, want an error", name)
		}
	}
}

func TestHistoryMatch(t *testing.T) {
	if DefaultSettings().CleanHistory != HistoryApp {
		t.Errorf("default CleanHistory = %q, want %q", DefaultSettings().CleanHistory, HistoryApp)
	}
	for _, mode := range []string{HistoryKeep, "", "sometimes"} {
		if HistoryMatch(mode) != nil {
			t.Errorf("HistoryMatch(%q) removes something", mode)
		}
	}
	app, all := HistoryMatch(HistoryApp), HistoryMatch(HistoryLoopback)
	cases := []struct {
		entry    string
		app, all bool
	}{
		{"127.1.2.3", true, true},
		{"127.1.2.3:13389", true, true},
		{"127.254.254.254", true, true},
		{"127.20.161.131:13389", true, true},
		{"127.0.0.1", false, true},
		{"127.0.0.1:3390", false, true},
		{"127.255.1.1", false, true},
		{"127.1.1.0", false, true},
		{"pc.example.com", false, false},
		{"pc.example.com:3390", false, false},
		{"192.0.2.1:3389", false, false},
		{"localhost", false, false},
		{"localhost:3390", false, false},
		{"[::1]:3389", false, false},
		{"::1", false, false},
		{"", false, false},
		{"127.1.2.3:99999", false, false},
		{strings.Repeat("x", 300), false, false},
	}
	for _, c := range cases {
		if got := app(c.entry); got != c.app {
			t.Errorf("app range removes %q = %v, want %v", c.entry, got, c.app)
		}
		if got := all(c.entry); got != c.all {
			t.Errorf("all of 127/8 removes %q = %v, want %v", c.entry, got, c.all)
		}
	}
}
