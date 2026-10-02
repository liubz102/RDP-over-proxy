//go:build windows

package testutil

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// EnvHelper names the role a test binary plays when a test runs it as a
// child process (see HelperCommand).
const EnvHelper = "RDP_OVER_PROXY_TEST_HELPER"

// Helper roles.
const (
	// HelperWindow opens a window of class HelperWindowClass, standing in
	// for mstsc's session window. It prints "ready" once the window exists
	// and exits with code 0 when the window is closed. The window is a tool
	// window (no taskbar button), shown far off-screen without being
	// activated, so it never takes the focus or appears in front of the
	// person at the desk.
	HelperWindow = "window"
	// HelperDisabledWindow is HelperWindow with its window disabled, as
	// mstsc's session window is while a dialog of its own is open.
	HelperDisabledWindow = "disabled-window"
	// HelperOtherWindow is HelperWindow with a window of another class,
	// standing in for mstsc's credential prompt.
	HelperOtherWindow = "other-window"
	// HelperBlock has no window and runs until its standard input closes.
	HelperBlock = "block"
)

// HelperWindowClass is the class of HelperWindow's window.
const HelperWindowClass = "RDPOverProxyTestHelper"

// RunHelper plays the role EnvHelper names, if any, and exits. Call it first
// thing in TestMain.
func RunHelper() {
	switch os.Getenv(EnvHelper) {
	case "":
		return
	case HelperWindow:
		os.Exit(windowHelper(HelperWindowClass, false))
	case HelperDisabledWindow:
		os.Exit(windowHelper(HelperWindowClass, true))
	case HelperOtherWindow:
		os.Exit(windowHelper(otherWindowClass, false))
	case HelperBlock:
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown %s %q\n", EnvHelper, os.Getenv(EnvHelper))
		os.Exit(2)
	}
}

// HelperCommand returns a command that runs this test binary as a helper.
// The test starts it; it is killed when the test ends if it is still
// running. Only this process is ever killed.
func HelperCommand(t testing.TB, role string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// "-test.run=^$" runs no tests, should RunHelper not be wired up.
	cmd := exec.Command(exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), EnvHelper+"="+role)
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill() // fails harmlessly once it has exited and been waited for
		}
	})
	return cmd
}

const (
	wmDestroy        = 0x0002
	wsPopup          = 0x80000000
	wsExToolWindow   = 0x00000080
	wsExNoActivate   = 0x08000000
	swShowNoActivate = 4
	offscreen        = -32000
	otherWindowClass = "RDPOverProxyTestPrompt"
)

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   uintptr
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSm     uintptr
}

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
	private uint32
}

func windowHelper(class string, disabled bool) int {
	runtime.LockOSThread() // a window belongs to the thread that created it

	user32 := windows.NewLazySystemDLL("user32.dll")
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	var (
		registerClassEx  = user32.NewProc("RegisterClassExW")
		createWindowEx   = user32.NewProc("CreateWindowExW")
		showWindow       = user32.NewProc("ShowWindow")
		enableWindow     = user32.NewProc("EnableWindow")
		defWindowProc    = user32.NewProc("DefWindowProcW")
		getMessage       = user32.NewProc("GetMessageW")
		translateMessage = user32.NewProc("TranslateMessage")
		dispatchMessage  = user32.NewProc("DispatchMessageW")
		postQuitMessage  = user32.NewProc("PostQuitMessage")
		getModuleHandle  = kernel32.NewProc("GetModuleHandleW")
	)

	instance, _, _ := getModuleHandle.Call(0)
	className := windows.StringToUTF16Ptr(class)
	wndProc := windows.NewCallback(func(hwnd, message, wParam, lParam uintptr) uintptr {
		if message == wmDestroy {
			postQuitMessage.Call(0)
			return 0
		}
		r, _, _ := defWindowProc.Call(hwnd, message, wParam, lParam)
		return r
	})
	wc := wndClassEx{wndProc: wndProc, instance: instance, className: className}
	wc.size = uint32(unsafe.Sizeof(wc))
	if r, _, err := registerClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		fmt.Fprintln(os.Stderr, "RegisterClassEx:", err)
		return 2
	}
	x := int32(offscreen)
	hwnd, _, err := createWindowEx.Call(wsExToolWindow|wsExNoActivate, uintptr(unsafe.Pointer(className)), 0,
		wsPopup, uintptr(x), uintptr(x), 1, 1, 0, 0, instance, 0)
	if hwnd == 0 {
		fmt.Fprintln(os.Stderr, "CreateWindowEx:", err)
		return 2
	}
	if disabled {
		enableWindow.Call(hwnd, 0)
	}
	showWindow.Call(hwnd, swShowNoActivate)
	fmt.Println("ready")

	var m msg
	for {
		r, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // WM_QUIT, or an error
			break
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&m)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	runtime.KeepAlive(className)
	return 0
}
