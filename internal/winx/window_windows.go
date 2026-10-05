//go:build windows

package winx

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                    = windows.NewLazySystemDLL("user32.dll")
	procGetAncestor           = user32.NewProc("GetAncestor")
	procGetWindow             = user32.NewProc("GetWindow")
	procInternalGetWindowText = user32.NewProc("InternalGetWindowText")
	procIsIconic              = user32.NewProc("IsIconic")
	procIsWindowEnabled       = user32.NewProc("IsWindowEnabled")
	procPostMessageW          = user32.NewProc("PostMessageW")
	procSendMessageW          = user32.NewProc("SendMessageW")
	procSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	procShowWindow            = user32.NewProc("ShowWindow")
)

const (
	gaRoot    = 2
	gwOwner   = 4
	swRestore = 9
	wmSetText = 0x000C
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
		if _, err := windows.GetWindowThreadProcessId(hwnd, &pid); err == nil && pid == enumPID {
			enumFound = append(enumFound, hwnd)
		}
		return 1 // keep enumerating
	})
)

// processWindows returns every top-level window of the process, hidden ones
// included.
func processWindows(pid int) []windows.HWND {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumPID, enumFound = uint32(pid), nil
	_ = windows.EnumWindows(enumCallback, nil)
	found := enumFound
	enumFound = nil
	return found
}

// MainWindows returns the process's visible top-level windows that have no
// owner: its main windows, without the dialogs they own.
func MainWindows(pid int) []windows.HWND {
	var main []windows.HWND
	for _, w := range processWindows(pid) {
		if windows.IsWindowVisible(w) && owner(w) == 0 {
			main = append(main, w)
		}
	}
	return main
}

// Title is the title of a top-level window (its window text). It reads what
// Windows keeps for the window instead of asking the program, so a program
// that has stopped responding cannot hold up the caller; and unlike
// GetWindowText it also reads another program's window that has no title
// bar, such as a full-screen one. "" when the window has none or is gone.
func Title(hwnd windows.HWND) string {
	buf := make([]uint16, 256)
	for {
		n, _, _ := procInternalGetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if int(n) < len(buf)-1 {
			return windows.UTF16ToString(buf[:n])
		}
		buf = make([]uint16, 2*len(buf)) // it filled the buffer, so it may have been cut short
	}
}

// SetTitle gives a top-level window a new title the way its own program
// does: it sends the window WM_SETTEXT, which programs leave to
// DefWindowProc, and that keeps the text, repaints the title bar and raises
// the name-change event. It returns once the program has handled the
// message. SetWindowText would not do for another program's window: it
// changes only the text Windows keeps, without sending WM_SETTEXT, and no
// name-change event comes from the program's process.
func SetTitle(hwnd windows.HWND, title string) error {
	p, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return err
	}
	r, _, err := procSendMessageW.Call(uintptr(hwnd), wmSetText, 0, uintptr(unsafe.Pointer(p)))
	if r == 0 {
		if errno, ok := err.(windows.Errno); ok && errno != 0 {
			return fmt.Errorf("WM_SETTEXT: %w", err) // the window is gone, say
		}
		return errors.New("the window did not take the title")
	}
	return nil
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

// topLevel reports whether hwnd is a top-level window rather than a part of
// one, such as a button. A window that is gone is neither.
func topLevel(hwnd windows.HWND) bool {
	root, _, _ := procGetAncestor.Call(uintptr(hwnd), gaRoot)
	return root == uintptr(hwnd)
}
