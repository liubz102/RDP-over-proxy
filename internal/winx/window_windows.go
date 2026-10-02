//go:build windows

package winx

import (
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sys/windows"
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procGetWindow           = user32.NewProc("GetWindow")
	procIsIconic            = user32.NewProc("IsIconic")
	procIsWindowEnabled     = user32.NewProc("IsWindowEnabled")
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

// ClassName is the window's class name, such as "TscShellContainerClass";
// "" when it cannot be read.
func ClassName(hwnd windows.HWND) string {
	buf := make([]uint16, 256)
	n, err := windows.GetClassName(hwnd, &buf[0], int32(len(buf)))
	if err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

// Enabled reports whether the window accepts input. A window is disabled
// while a modal dialog it owns is open.
func Enabled(hwnd windows.HWND) bool {
	r, _, _ := procIsWindowEnabled.Call(uintptr(hwnd))
	return r != 0
}

// PostClose asks the window to close, as its close button does. The program
// may ask the user to confirm.
func PostClose(hwnd windows.HWND) error {
	if r, _, err := procPostMessageW.Call(uintptr(hwnd), wmClose, 0, 0); r == 0 {
		return fmt.Errorf("PostMessage(WM_CLOSE): %w", err)
	}
	return nil
}

// BringToFront brings the window to the front, restoring it first if it is
// minimized. Windows only allows this while the calling app is in the
// foreground, which it is when the user has just clicked in it.
func BringToFront(hwnd windows.HWND) error {
	w := uintptr(hwnd)
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
