//go:build windows

package mstsc_test

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/mstsc"
	"github.com/liubz102/RDP-over-proxy/internal/testutil"
	"github.com/liubz102/RDP-over-proxy/internal/winx"
)

// These tests never start mstsc. A copy of the test binary stands in for it
// (see testutil.RunHelper).
func TestMain(m *testing.M) {
	testutil.RunHelper()
	os.Exit(m.Run())
}

func TestPathIsInTheSystemFolder(t *testing.T) {
	p, err := mstsc.Path()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(filepath.Base(p), "mstsc.exe") || !strings.EqualFold(filepath.Base(filepath.Dir(p)), "System32") {
		t.Fatalf("Path = %q, want ...\\System32\\mstsc.exe", p)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("mstsc.exe is not where Path says: %v", err)
	}
}

// startWindow starts the window helper and waits until its window exists.
func startWindow(t *testing.T) *mstsc.Process {
	t.Helper()
	cmd := testutil.HelperCommand(t, testutil.HelperWindow)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	p, err := mstsc.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if strings.TrimSpace(line) != "ready" {
		t.Fatalf("the helper said %q (%v) instead of ready", line, err)
	}
	return p
}

func TestCloseAsksTheWindowToClose(t *testing.T) {
	p := startWindow(t)
	if got := winx.MainWindows(p.PID()); len(got) != 1 {
		t.Fatalf("found %d main windows of the helper, want 1", len(got))
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	code, err := p.Wait()
	if err != nil || code != 0 {
		t.Fatalf("Wait = %d, %v; want a clean exit after WM_CLOSE", code, err)
	}
	// Once it has exited, closing and killing do nothing.
	if err := p.Close(); err != nil {
		t.Errorf("Close after exit: %v", err)
	}
	if err := p.Kill(); err != nil {
		t.Errorf("Kill after exit: %v", err)
	}
	if err := p.Focus(); err != nil {
		t.Errorf("Focus after exit: %v", err)
	}
}

// startBlocking starts a helper without a window that runs until killed.
func startBlocking(t *testing.T) *mstsc.Process {
	t.Helper()
	cmd := testutil.HelperCommand(t, testutil.HelperBlock)
	stdin, err := cmd.StdinPipe() // held open, so the helper keeps running
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stdin.Close() })
	p, err := mstsc.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestKill(t *testing.T) {
	p := startBlocking(t)
	if err := p.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	code, err := p.Wait()
	if err != nil || code != 1 {
		t.Fatalf("Wait = %d, %v; want exit code 1 from TerminateProcess", code, err)
	}
	if err := p.Kill(); err != nil {
		t.Errorf("a second Kill: %v", err)
	}
}

func TestCloseAndFocusWithoutAWindow(t *testing.T) {
	p := startBlocking(t)
	if err := p.Close(); !errors.Is(err, winx.ErrNoWindow) {
		t.Errorf("Close = %v, want ErrNoWindow", err)
	}
	if err := p.Focus(); !errors.Is(err, winx.ErrNoWindow) {
		t.Errorf("Focus = %v, want ErrNoWindow", err)
	}
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestStartFailure(t *testing.T) {
	cmd := testutil.HelperCommand(t, testutil.HelperBlock)
	cmd.Path = filepath.Join(t.TempDir(), "missing.exe")
	if _, err := mstsc.Start(cmd); err == nil {
		t.Fatal("starting a missing program should fail")
	}
}
