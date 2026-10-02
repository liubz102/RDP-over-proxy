//go:build windows

package mstsc

import (
	"errors"
	"fmt"
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

// Process is a program the app started.
type Process struct {
	cmd *exec.Cmd

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
	return Start(exec.Command(path, args...))
}

// Start starts cmd. Launch uses it for mstsc; tests use it for a stand-in.
func Start(cmd *exec.Cmd) (*Process, error) {
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
	return &Process{cmd: cmd, handle: h}, nil
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

// Close asks the program to close its windows (WM_CLOSE). mstsc may ask the
// user to confirm. Closing a process that has exited does nothing.
func (p *Process) Close() error { return p.whileRunning(winx.CloseWindows) }

// Focus brings the program's window to the front.
func (p *Process) Focus() error { return p.whileRunning(winx.FocusWindow) }

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
