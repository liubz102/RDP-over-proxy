package store_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/store"

	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/model"
)

func TestDefaultDirsHonoursEnvHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, home)
	d, err := DefaultDirs()
	if err != nil {
		t.Fatalf("DefaultDirs: %v", err)
	}
	if d.Data != filepath.Join(home, "data") || d.Logs != filepath.Join(home, "logs") {
		t.Fatalf("DefaultDirs() = %+v, want data and logs in %s", d, home)
	}
}

func TestLoadMissingFileGivesDefaults(t *testing.T) {
	s := NewSettingsStore(t.TempDir())
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != model.DefaultSettings() {
		t.Fatalf("Load() = %+v, want defaults", got)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := model.DefaultSettings()
	want.Language = model.LangZhCN
	want.Theme = model.ThemeDark
	want.LocalPort = 23389

	if err := NewSettingsStore(dir).Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := NewSettingsStore(dir).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("round trip: got %+v, want %+v", got, want)
	}
	// No temporary files are left behind by the atomic write.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected only settings.json in %s, found %d entries", dir, len(entries))
	}
}

func TestSaveRejectsInvalidSettings(t *testing.T) {
	s := NewSettingsStore(t.TempDir())
	bad := model.DefaultSettings()
	bad.Language = "fr"
	if err := s.Save(bad); err == nil {
		t.Fatal("Save accepted an unsupported language")
	}
	if _, err := os.Stat(s.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a rejected Save must not write the file (stat err = %v)", err)
	}
}

func TestLoadCorruptFileRecoversWithDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := NewSettingsStore(dir).Load()
	if !errors.Is(err, ErrSettingsRecovered) {
		t.Fatalf("Load error = %v, want ErrSettingsRecovered", err)
	}
	if got != model.DefaultSettings() {
		t.Fatalf("Load() = %+v, want defaults", got)
	}
	if _, err := os.Stat(path + ".corrupt"); err != nil {
		t.Fatalf("the unreadable file should be kept as settings.json.corrupt: %v", err)
	}
}

func TestLoadKeepsDefaultsForMissingFields(t *testing.T) {
	dir := t.TempDir()
	// A file from a build that knew fewer settings, with one boolean switched off.
	data := `{"schema":1,"language":"en","theme":"dark"}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := NewSettingsStore(dir).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := model.DefaultSettings()
	want.Language = model.LangEn
	want.Theme = model.ThemeDark
	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}

	off := `{"schema":1,"language":"en","checkRouteBeforeConnect":false}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(off), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = NewSettingsStore(dir).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.CheckRouteBeforeConnect {
		t.Fatal("an explicit false must survive loading")
	}
}

func TestLoadRepairsUnknownValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"schema":1,"language":"fr","theme":"neon","localPort":99999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := NewSettingsStore(dir).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != model.DefaultSettings() {
		t.Fatalf("Load() = %+v, want every unknown value repaired to its default", got)
	}
}

func TestPeekChangesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewSettingsStore(dir)
	if got := s.Peek(); got != model.DefaultSettings() {
		t.Fatalf("Peek of an unreadable file = %+v, want defaults", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("Peek moved the unreadable file aside")
	}
	if err := os.WriteFile(path, []byte(`{"schema":1,"language":"en","theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := s.Peek(); got.Language != model.LangEn || got.Theme != model.ThemeDark {
		t.Fatalf("Peek = %+v", got)
	}
	if s.Get() != model.DefaultSettings() {
		t.Fatal("Peek changed the settings in effect")
	}
}
