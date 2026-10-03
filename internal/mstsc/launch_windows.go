//go:build windows

package mstsc

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"golang.org/x/sys/windows"

	"github.com/liubz102/RDP-over-proxy/internal/winx"
)

// Path is mstsc.exe in the Windows system folder. It is never looked up on
// PATH, where something else could be called mstsc.
func Path() (string, error) {
	dir, err := windows.GetSystemDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "mstsc.exe"), nil
}

// SessionWindowClass is the window class of mstsc's remote session window:
// the window that, asked to close, offers to disconnect the session. It has
// been the same since Windows XP; tools that automate Remote Desktop rely on
// it too. Until the connection is made mstsc shows other windows only (the
// progress dialog, the credential prompt, the certificate warning).
const SessionWindowClass = "TscShellContainerClass"

// ErrNoWindow is returned when the process shows no window at all.
var ErrNoWindow = errors.New("the process has no window")

// Process is a program the app started.
type Process struct {
	cmd *exec.Cmd
	// sessionClass is the class of the window Close asks to close.
	sessionClass string

	// handle is the app's own handle to the process. Windows does not reuse
	// a process ID while a handle to the process is open, so holding it
	// guarantees that the windows found by PID belong to this process and
	// not to a later one that got the same ID. Wait closes it.
	mu     sync.Mutex
	handle windows.Handle
}

// Launch starts mstsc with args.
func Launch(args []string) (*Process, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	return Start(exec.Command(path, args...), SessionWindowClass)
}

// EditDefaults opens Remote Desktop Connection on Default.rdp with its
// options shown ("mstsc /edit"), where the user changes what every
// connection shares (clipboard, drives, sound, experience) and saves it.
// Without a Default.rdp, plain mstsc opens on its built-in defaults, and
// saving there creates the file. The app neither waits for it nor ends it.
func EditDefaults() error {
	exe, err := Path()
	if err != nil {
		return err
	}
	file, err := DefaultRDPPath()
	if err != nil {
		return err
	}
	var args []string
	if _, err := os.Stat(file); !errors.Is(err, fs.ErrNotExist) {
		args = []string{"/edit", file}
	}
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// Start starts cmd, whose session window has the class sessionClass. Launch
// uses it for mstsc; tests use it for a stand-in.
func Start(cmd *exec.Cmd, sessionClass string) (*Process, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// The process cannot have been reaped yet (nothing has waited for it),
	// so its ID still refers to it here.
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("open the started process: %w", err)
	}
	return &Process{cmd: cmd, sessionClass: sessionClass, handle: h}, nil
}

// PID is the process ID.
func (p *Process) PID() int { return p.cmd.Process.Pid }

// Wait blocks until the process exits and returns its exit code. Call it
// once.
func (p *Process) Wait() (int, error) {
	err := p.cmd.Wait()
	p.mu.Lock()
	_ = windows.CloseHandle(p.handle)
	p.handle = 0
	p.mu.Unlock()
	var exit *exec.ExitError
	if err == nil || errors.As(err, &exit) {
		return p.cmd.ProcessState.ExitCode(), nil
	}
	return -1, err
}

// Close asks the remote session window to close, as its close button does;
// mstsc then asks the user to confirm. It reports false when there is no
// such window to ask: mstsc is still connecting or asking for a password, or
// a dialog of its own is open over the window (a modal dialog disables it).
// Closing those other windows instead can leave mstsc running with no
// window at all, so then only Kill ends it. Once the process has exited,
// Close does nothing and reports true: Wait reports the exit.
func (p *Process) Close() (closing bool, err error) {
	closing = true
	err = p.whileRunning(func(pid int) error {
		var asked bool
		for _, w := range winx.MainWindows(pid) {
			if winx.ClassName(w) != p.sessionClass || !winx.Enabled(w) {
				continue
			}
			if err := winx.PostClose(w); err != nil {
				return err
			}
			asked = true
		}
		closing = asked
		return nil
	})
	return closing, err
}

// Focus brings mstsc to the front: the remote session window if there is
// one, otherwise whatever it shows, such as the credential prompt.
func (p *Process) Focus() error {
	return p.whileRunning(func(pid int) error {
		ws := winx.MainWindows(pid)
		if len(ws) == 0 {
			return ErrNoWindow
		}
		target := ws[0]
		for _, w := range ws {
			if winx.ClassName(w) == p.sessionClass {
				target = w
				break
			}
		}
		return winx.BringToFront(target)
	})
}

// Kill ends the process: only this one, through the handle from starting it,
// never by name. Killing a process that has exited is not an error.
func (p *Process) Kill() error {
	return p.whileRunning(func(int) error {
		err := windows.TerminateProcess(p.handle, 1)
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) && !p.running() {
			return nil // it exited just now; Windows refuses to terminate it twice
		}
		return err
	})
}

// whileRunning calls f with the PID if the process is still running, while
// holding the handle that keeps the PID from being reused. Once the process
// has exited it does nothing: Wait reports the exit.
func (p *Process) whileRunning(f func(pid int) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.handle == 0 || !p.running() {
		return nil
	}
	return f(p.PID())
}

// running reports whether the process has not exited yet. p.mu must be held
// and p.handle open.
func (p *Process) running() bool {
	ev, err := windows.WaitForSingleObject(p.handle, 0)
	return err == nil && ev == uint32(windows.WAIT_TIMEOUT)
}
