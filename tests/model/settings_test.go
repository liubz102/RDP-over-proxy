package model_test

import (
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
	got := Settings{Language: LangEn, Theme: "purple", CheckRouteBeforeConnect: false}.Normalize()
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
		TestURL: "https://example.com/204", LogLevel: LogDebug}
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
