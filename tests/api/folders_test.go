package api_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/api"

	"slices"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/logging"
)

func TestAppServiceFolders(t *testing.T) {
	folders := Folders{Data: `D:\Tools\RDP-over-proxy\data`, Logs: `D:\Tools\RDP-over-proxy\logs`}
	var opened []string
	s := NewAppService(NewCore(Deps{
		Folders:    folders,
		OpenFolder: func(path string) error { opened = append(opened, path); return nil },
		Log:        logging.New(nil, logging.LevelInfo, 10),
	}), func() {})

	if got := s.Folders(); got != folders {
		t.Errorf("Folders() = %+v, want %+v", got, folders)
	}
	for _, name := range []string{FolderData, FolderLogs} {
		if err := s.OpenFolder(name); err != nil {
			t.Errorf("OpenFolder(%q): %v", name, err)
		}
	}
	if want := []string{folders.Data, folders.Logs}; !slices.Equal(opened, want) {
		t.Errorf("opened %q, want %q", opened, want)
	}
	// Only the app's own folders: the page cannot have any path opened.
	if err := s.OpenFolder(`C:\Windows`); err == nil || len(opened) != 2 {
		t.Errorf("OpenFolder of another path = %v, opened %q", err, opened)
	}

	// Without a way to open them (a test's Core), it fails instead of
	// panicking.
	none := NewAppService(NewCore(Deps{Log: logging.New(nil, logging.LevelInfo, 10)}), func() {})
	if none.OpenFolder(FolderData) == nil {
		t.Error("OpenFolder without a way to open folders should fail")
	}
}
