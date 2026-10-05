package store_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/store"

	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// Without RDP_OVER_PROXY_HOME, everything goes next to the exe: here, the
// test binary.
func TestDefaultDirsAreNextToTheExe(t *testing.T) {
	t.Setenv(EnvHome, "")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	dir, err := ExeDir()
	if err != nil || dir != filepath.Dir(exe) {
		t.Fatalf("ExeDir() = %q, %v; want %q", dir, err, filepath.Dir(exe))
	}
	d, err := DefaultDirs()
	if err != nil {
		t.Fatalf("DefaultDirs: %v", err)
	}
	if want := (Dirs{Data: filepath.Join(dir, "data"), Logs: filepath.Join(dir, "logs")}); d != want {
		t.Fatalf("DefaultDirs() = %+v, want %+v", d, want)
	}
}

func TestPrepareCreatesWritableFolders(t *testing.T) {
	d := DirsIn(filepath.Join(t.TempDir(), "RDP-over-proxy"))
	// Again once they exist: starting the app a second time.
	for range 2 {
		if err := d.Prepare(); err != nil {
			t.Fatalf("Prepare: %v", err)
		}
	}
	for _, dir := range []string{d.Data, d.Logs} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		// The file written to check is gone again.
		if len(entries) != 0 {
			t.Errorf("%s holds %d entries after Prepare, want none", dir, len(entries))
		}
	}
}

func TestPrepareNamesTheFolderItCannotUse(t *testing.T) {
	base := t.TempDir()
	d := DirsIn(base)
	// A file where the logs folder should be.
	if err := os.WriteFile(d.Logs, []byte("not a folder"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := d.Prepare()
	var fe *FolderError
	if !errors.As(err, &fe) || fe.Dir != d.Logs {
		t.Fatalf("Prepare() = %v, want a FolderError for %s", err, d.Logs)
	}
	// The reason is the system's, without the path a second time; it is not
	// one that administrator rights would get past.
	if fe.Err == nil || errors.Is(err, fs.ErrPermission) {
		t.Errorf("reason = %v", fe.Err)
	}
	if info, err := os.Stat(d.Data); err != nil || !info.IsDir() {
		t.Errorf("the data folder, which comes first, was not created: %v", err)
	}
}
