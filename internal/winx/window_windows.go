//go:build windows

package winx

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sys/windows"
)

// ErrNoWindow is returned when a process has no main window, for example
// because it is still starting.
var ErrNoWindow = errors.New("the process has no window")

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procGetWindow           = user32.NewProc("GetWindow")
	procIsIconic            = user32.NewProc("IsIconic")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procShowWindow          = user32.NewProc("ShowWindow")
)

const (
	gwOwner   = 4
	swRestore = 9
	wmClose   = 0x0010
)

// EnumWindows calls back synchronously on the calling thread. A Go callback
// can never be freed and Windows allows only a limited number of them, so a
// single callback serves every enumeration, one at a time.
var (
	enumMu       sync.Mutex
	enumPID      uint32
	enumFound    []windows.HWND
	enumCallback = windows.NewCallback(func(hwnd windows.HWND, _ uintptr) uintptr {
		var pid uint32
		if _, err := windows.GetWindowThreadProcessId(hwnd, &pid); err == nil && pid == enumPID &&
			windows.IsWindowVisible(hwnd) && owner(hwnd) == 0 {
			enumFound = append(enumFound, hwnd)
		}
		return 1 // keep enumerating
	})
)

// MainWindows returns the process's visible top-level windows that have no
// owner: its main windows, without the dialogs they own.
func MainWindows(pid int) []windows.HWND {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumPID, enumFound = uint32(pid), nil
	_ = windows.EnumWindows(enumCallback, nil)
	found := enumFound
	enumFound = nil
	return found
}

// CloseWindows asks each of the process's main windows to close, as its
// close button does. The program may ask the user to confirm.
func CloseWindows(pid int) error {
	ws := MainWindows(pid)
	if len(ws) == 0 {
		return ErrNoWindow
	}
	for _, w := range ws {
		if r, _, err := procPostMessageW.Call(uintptr(w), wmClose, 0, 0); r == 0 {
			return fmt.Errorf("PostMessage(WM_CLOSE): %w", err)
		}
	}
	return nil
}

// FocusWindow brings the process's main window to the front, restoring it
// first if it is minimized. Windows only allows this while the calling app
// is in the foreground, which it is when the user has just clicked in it.
func FocusWindow(pid int) error {
	ws := MainWindows(pid)
	if len(ws) == 0 {
		return ErrNoWindow
	}
	w := uintptr(ws[0])
	if minimized, _, _ := procIsIconic.Call(w); minimized != 0 {
		procShowWindow.Call(w, swRestore)
	}
	if ok, _, _ := procSetForegroundWindow.Call(w); ok == 0 {
		return errors.New("Windows did not let the app bring the window to the front")
	}
	return nil
}

func owner(hwnd windows.HWND) uintptr {
	r, _, _ := procGetWindow.Call(uintptr(hwnd), gwOwner)
	return r
}
