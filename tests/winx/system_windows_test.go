package winx_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/winx"

	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestFileVersion(t *testing.T) {
	v, err := FileVersion(filepath.Join(os.Getenv("SystemRoot"), "System32", "kernel32.dll"))
	if err != nil || !regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+$`).MatchString(v) {
		t.Fatalf("FileVersion(kernel32.dll) = %q, %v", v, err)
	}
	if _, err := FileVersion(filepath.Join(t.TempDir(), "missing.exe")); err == nil {
		t.Error("a missing file has no version")
	}
}

// Whether Credential Guard runs depends on the computer; the question
// itself must get an answer.
func TestCredentialGuardRunning(t *testing.T) {
	running, err := CredentialGuardRunning()
	if err != nil {
		t.Fatalf("CredentialGuardRunning: %v", err)
	}
	t.Logf("Credential Guard running: %v", running)
	// Asking again on another thread works as well: COM is set up and
	// undone per call.
	if again, err := CredentialGuardRunning(); err != nil || again != running {
		t.Fatalf("again: %v, %v", again, err)
	}
}
