//go:build windows

package mstsc_test

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/liubz102/RDP-over-proxy/internal/mstsc"
	"github.com/liubz102/RDP-over-proxy/internal/winx"
	"github.com/liubz102/RDP-over-proxy/tests/testutil"
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

// startWindow starts a window helper (testutil.HelperWindow and the like)
// and waits until its window exists. The helper's session window class
// stands in for mstsc's.
func startWindow(t *testing.T, role string) *mstsc.Process {
	t.Helper()
	cmd := testutil.HelperCommand(t, role)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	p, err := mstsc.Start(cmd, testutil.HelperWindowClass)
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
	p := startWindow(t, testutil.HelperWindow)
	if got := winx.MainWindows(p.PID()); len(got) != 1 || winx.ClassName(got[0]) != testutil.HelperWindowClass {
		t.Fatalf("found %d main windows of the helper, want 1 of its class", len(got))
	}
	if closing, err := p.Close(); err != nil || !closing {
		t.Fatalf("Close = %v, %v", closing, err)
	}
	code, err := p.Wait()
	if err != nil || code != 0 {
		t.Fatalf("Wait = %d, %v; want a clean exit after WM_CLOSE", code, err)
	}
	// Once it has exited, closing and killing do nothing.
	if closing, err := p.Close(); err != nil || !closing {
		t.Errorf("Close after exit = %v, %v; want true: the exit is on its way", closing, err)
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
	p, err := mstsc.Start(cmd, testutil.HelperWindowClass)
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

// Only the session window is asked to close. Closing what mstsc shows
// before it (the credential prompt) can leave it running with no window.
func TestCloseLeavesOtherWindowsAlone(t *testing.T) {
	for _, role := range []string{testutil.HelperOtherWindow, testutil.HelperDisabledWindow} {
		p := startWindow(t, role)
		if closing, err := p.Close(); err != nil || closing {
			t.Errorf("%s: Close = %v, %v; want false, nothing to ask", role, closing, err)
		}
		if got := winx.MainWindows(p.PID()); len(got) != 1 {
			t.Errorf("%s: the window is gone after Close", role)
		}
		if err := p.Kill(); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Wait(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCloseAndFocusWithoutAWindow(t *testing.T) {
	p := startBlocking(t)
	if closing, err := p.Close(); err != nil || closing {
		t.Errorf("Close = %v, %v; want false, nothing to ask", closing, err)
	}
	if err := p.Focus(); !errors.Is(err, mstsc.ErrNoWindow) {
		t.Errorf("Focus = %v, want ErrNoWindow", err)
	}
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Wait(); err != nil {
		t.Fatal(err)
	}
}

// titled is a testutil.HelperTitledWindows, started as mstsc is.
type titled struct {
	p                         *mstsc.Process
	in                        io.WriteCloser
	session, stubborn, prompt windows.HWND
}

func startTitled(t *testing.T) *titled {
	t.Helper()
	cmd := testutil.HelperCommand(t, testutil.HelperTitledWindows)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	p, err := mstsc.Start(cmd, testutil.HelperWindowClass)
	if err != nil {
		t.Fatal(err)
	}
	h := &titled{p: p, in: in}
	line, err := bufio.NewReader(out).ReadString('\n')
	if _, serr := fmt.Sscanf(line, "ready %d %d %d", &h.session, &h.stubborn, &h.prompt); serr != nil {
		t.Fatalf("the helper said %q (%v) instead of ready and its windows", line, err)
	}
	return h
}

// retitle has the helper set the title of one of its windows itself, as
// mstsc sets its own.
func (h *titled) retitle(t *testing.T, which, title string) {
	t.Helper()
	if _, err := fmt.Fprintf(h.in, "%s %s\n", which, title); err != nil {
		t.Fatal(err)
	}
}

// waitTitle waits until the window's title is want.
func waitTitle(t *testing.T, pid int, hwnd windows.HWND, want string) {
	t.Helper()
	reached := make(chan struct{})
	var once sync.Once
	stop, err := winx.WatchWindows(pid, func(w windows.HWND) {
		if w == hwnd && winx.Title(w) == want {
			once.Do(func() { close(reached) })
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	<-reached
}

func TestShowNameKeepsTheNameInFront(t *testing.T) {
	h := startTitled(t)
	pid := h.p.PID()
	if err := h.p.ShowName("Office"); err != nil {
		t.Fatal(err)
	}
	waitTitle(t, pid, h.session, "Office - "+testutil.HelperSessionTitle)
	if err := h.p.ShowName("Again"); err == nil {
		t.Error("a second ShowName should fail")
	}

	// mstsc sets its own title again, say when it reconnects: the name goes
	// back in front. Its windows of another class, such as the credential
	// prompt, keep their titles.
	h.retitle(t, "prompt", "Windows Security")
	// The watch takes the changes in order, so by the end of the second
	// round it has seen all that came before the first.
	for _, own := range []string{"127.0.0.2:13389 - Reconnecting", testutil.HelperSessionTitle} {
		h.retitle(t, "session", own)
		waitTitle(t, pid, h.session, "Office - "+own)
	}
	if got := winx.Title(h.prompt); got != "Windows Security" {
		t.Errorf("the prompt's title became %q", got)
	}
	// The window that puts its own title back was given the name once, and
	// then left alone instead of the two taking turns for ever.
	if n := testutil.HelperRefusals(h.stubborn); n != 1 {
		t.Errorf("the stubborn window was given a title %d times, want 1", n)
	}
	if got := winx.Title(h.stubborn); got != testutil.HelperStubbornTitle {
		t.Errorf("the stubborn window's title is %q", got)
	}

	h.in.Close() // the helper exits
	if code, err := h.p.Wait(); err != nil || code != 0 {
		t.Fatalf("Wait = %d, %v", code, err)
	}
	if err := h.p.ShowName("Office"); err != nil {
		t.Errorf("ShowName after the exit: %v", err)
	}
}

func TestStartFailure(t *testing.T) {
	cmd := testutil.HelperCommand(t, testutil.HelperBlock)
	cmd.Path = filepath.Join(t.TempDir(), "missing.exe")
	if _, err := mstsc.Start(cmd, testutil.HelperWindowClass); err == nil {
		t.Fatal("starting a missing program should fail")
	}
}
