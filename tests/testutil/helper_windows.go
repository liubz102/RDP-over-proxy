//go:build windows

package testutil

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	// person at the desk. The other window helpers' windows are the same.
	HelperWindow = "window"
	// HelperDisabledWindow is HelperWindow with its window disabled, as
	// mstsc's session window is while a dialog of its own is open.
	HelperDisabledWindow = "disabled-window"
	// HelperOtherWindow is HelperWindow with a window of another class,
	// standing in for mstsc's credential prompt.
	HelperOtherWindow = "other-window"
	// HelperTitledWindows opens three windows with titles: a session window
	// (class HelperWindowClass, title HelperSessionTitle); a session window
	// that puts its own title (HelperStubbornTitle) back while handling
	// every WM_SETTEXT it is sent, counting the times (HelperRefusals); and
	// a window of another class (HelperPromptTitle), standing in for the
	// credential prompt. It prints "ready" and the three window handles, in that order,
	// then reads lines from its standard input: "session <title>" or
	// "prompt <title>" sets that window's title from inside the program, as
	// mstsc sets its own. It exits when its standard input closes.
	HelperTitledWindows = "titled-windows"
	// HelperBlock has no window and runs until its standard input closes.
	HelperBlock = "block"
	// HelperSocks stands in for a proxy program: it listens on 127.0.0.1
	// and answers every SOCKS5 greeting with "no authentication", then
	// closes the connection. It prints "ready <port>" once it listens, and
	// exits when its standard input closes. Run it under a proxy program's
	// file name with HelperCommandNamed.
	HelperSocks = "socks"
)

// HelperWindowClass is the class of HelperWindow's window.
const HelperWindowClass = "RDPOverProxyTestHelper"

// The titles HelperTitledWindows' windows start with.
const (
	HelperSessionTitle  = "127.0.0.2:13389 - Remote Desktop Connection"
	HelperStubbornTitle = "127.0.0.3:13389 - Remote Desktop Connection"
	HelperPromptTitle   = "Remote Desktop Connection"
)

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
	case HelperTitledWindows:
		os.Exit(titledWindowsHelper())
	case HelperBlock:
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	case HelperSocks:
		os.Exit(socksHelper())
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

// HelperCommandNamed is HelperCommand with the test binary copied to a
// temporary folder as name, such as "xray.exe", for tests that tell
// programs by their file names.
func HelperCommandNamed(t testing.TB, role, name string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	named := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(named, data, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(named, "-test.run=^$")
	cmd.Env = append(os.Environ(), EnvHelper+"="+role)
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			// The copy can be removed only once its process has exited.
			_, _ = cmd.Process.Wait()
		}
	})
	return cmd
}

// HelperRefusals counts the times the stubborn window of HelperTitledWindows
// put its own title back. The count is a property of the window, which any
// program can read.
func HelperRefusals(hwnd windows.HWND) int {
	r, _, _ := getProp.Call(uintptr(hwnd), uintptr(unsafe.Pointer(refusalsProp)))
	return int(r)
}

const (
	wmDestroy        = 0x0002
	wmSetText        = 0x000C
	wsPopup          = 0x80000000
	wsExToolWindow   = 0x00000080
	wsExNoActivate   = 0x08000000
	swShowNoActivate = 4
	offscreen        = -32000
	otherWindowClass = "RDPOverProxyTestPrompt"
)

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	registerClassEx  = user32.NewProc("RegisterClassExW")
	createWindowEx   = user32.NewProc("CreateWindowExW")
	showWindow       = user32.NewProc("ShowWindow")
	enableWindow     = user32.NewProc("EnableWindow")
	defWindowProc    = user32.NewProc("DefWindowProcW")
	getMessage       = user32.NewProc("GetMessageW")
	translateMessage = user32.NewProc("TranslateMessage")
	dispatchMessage  = user32.NewProc("DispatchMessageW")
	postQuitMessage  = user32.NewProc("PostQuitMessage")
	setWindowText    = user32.NewProc("SetWindowTextW")
	setProp          = user32.NewProc("SetPropW")
	getProp          = user32.NewProc("GetPropW")
	getModuleHandle  = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")

	refusalsProp = windows.StringToUTF16Ptr("RDPOverProxyTestRefusals")
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

func socksHelper() int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("ready %d\n", ln.Addr().(*net.TCPAddr).Port)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				var greeting [2]byte
				if _, err := io.ReadFull(c, greeting[:]); err != nil || greeting[0] != 5 {
					return
				}
				methods := make([]byte, greeting[1])
				if _, err := io.ReadFull(c, methods); err != nil {
					return
				}
				_, _ = c.Write([]byte{5, 0})
			}()
		}
	}()
	_, _ = io.Copy(io.Discard, os.Stdin)
	return 0
}

func windowHelper(class string, disabled bool) int {
	runtime.LockOSThread() // a window belongs to the thread that created it
	if err := registerClass(class, windows.NewCallback(wndProc)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	hwnd, err := createWindow(class, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if disabled {
		enableWindow.Call(hwnd, 0)
	}
	showWindow.Call(hwnd, swShowNoActivate)
	fmt.Println("ready")
	pumpMessages()
	return 0
}

func titledWindowsHelper() int {
	runtime.LockOSThread() // a window belongs to the thread that created it
	proc := windows.NewCallback(wndProc)
	for _, class := range []string{HelperWindowClass, otherWindowClass} {
		if err := registerClass(class, proc); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	var hwnds [3]uintptr
	for i, w := range []struct{ class, title string }{
		{HelperWindowClass, HelperSessionTitle},
		{HelperWindowClass, HelperStubbornTitle},
		{otherWindowClass, HelperPromptTitle},
	} {
		hwnd, err := createWindow(w.class, w.title)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		showWindow.Call(hwnd, swShowNoActivate)
		hwnds[i] = hwnd
	}
	stubborn = hwnds[1]
	fmt.Printf("ready %d %d %d\n", hwnds[0], hwnds[1], hwnds[2])

	byName := map[string]uintptr{"session": hwnds[0], "prompt": hwnds[2]}
	go func() {
		// SetWindowText from this goroutine's thread hands the window's own
		// thread WM_SETTEXT and waits until it has been handled, so each
		// line's change is made before the next line is read.
		in := bufio.NewScanner(os.Stdin)
		for in.Scan() {
			which, title, _ := strings.Cut(in.Text(), " ")
			text, err := windows.UTF16PtrFromString(title)
			if err != nil || byName[which] == 0 {
				fmt.Fprintf(os.Stderr, "bad line %q\n", in.Text())
				os.Exit(2)
			}
			setWindowText.Call(byName[which], uintptr(unsafe.Pointer(text)))
		}
		os.Exit(0) // standard input closed: the test is done
	}()
	pumpMessages()
	return 0
}

// stubborn is HelperTitledWindows' window that puts its own title back.
// Only the thread of the windows uses it, and its count of refusals.
var (
	stubborn      uintptr
	refusals      uintptr
	stubbornTitle = windows.StringToUTF16Ptr(HelperStubbornTitle)
)

func wndProc(hwnd, message, wParam, lParam uintptr) uintptr {
	switch message {
	case wmDestroy:
		postQuitMessage.Call(0)
		return 0
	case wmSetText:
		r, _, _ := defWindowProc.Call(hwnd, message, wParam, lParam)
		if hwnd == stubborn {
			// Right away, before the sender gets its answer.
			refusals++
			setProp.Call(hwnd, uintptr(unsafe.Pointer(refusalsProp)), refusals)
			defWindowProc.Call(hwnd, wmSetText, 0, uintptr(unsafe.Pointer(stubbornTitle)))
		}
		return r
	}
	r, _, _ := defWindowProc.Call(hwnd, message, wParam, lParam)
	return r
}

func registerClass(name string, wndProc uintptr) error {
	instance, _, _ := getModuleHandle.Call(0)
	wc := wndClassEx{wndProc: wndProc, instance: instance, className: windows.StringToUTF16Ptr(name)}
	wc.size = uint32(unsafe.Sizeof(wc))
	if r, _, err := registerClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx: %w", err)
	}
	return nil
}

// createWindow creates a hidden window of the class, far off-screen.
func createWindow(class, title string) (uintptr, error) {
	instance, _, _ := getModuleHandle.Call(0)
	var text *uint16
	if title != "" {
		text = windows.StringToUTF16Ptr(title)
	}
	x := int32(offscreen)
	hwnd, _, err := createWindowEx.Call(wsExToolWindow|wsExNoActivate,
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(class))), uintptr(unsafe.Pointer(text)),
		wsPopup, uintptr(x), uintptr(x), 1, 1, 0, 0, instance, 0)
	if hwnd == 0 {
		return 0, fmt.Errorf("CreateWindowEx: %w", err)
	}
	return hwnd, nil
}

func pumpMessages() {
	var m msg
	for {
		r, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // WM_QUIT, or an error
			return
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&m)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}
