//go:build windows

package winx

import (
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
)

// FileVersion reads the file version from an executable's version resource,
// such as "10.0.26100.4061".
func FileVersion(path string) (string, error) {
	size, err := windows.GetFileVersionInfoSize(path, nil)
	if err != nil {
		return "", err
	}
	buf := make([]byte, size)
	if err := windows.GetFileVersionInfo(path, 0, size, unsafe.Pointer(&buf[0])); err != nil {
		return "", err
	}
	var fixed *windows.VS_FIXEDFILEINFO
	n := uint32(unsafe.Sizeof(*fixed))
	if err := windows.VerQueryValue(unsafe.Pointer(&buf[0]), `\`, unsafe.Pointer(&fixed), &n); err != nil {
		return "", err
	}
	ms, ls := fixed.FileVersionMS, fixed.FileVersionLS
	return fmt.Sprintf("%d.%d.%d.%d", ms>>16, ms&0xFFFF, ls>>16, ls&0xFFFF), nil
}

// withCOM runs f with COM set up on the calling thread, which it keeps for
// the duration: COM belongs to a thread. A thread that already has COM keeps
// it as it was.
func withCOM(coinit uint32, f func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	switch err := windows.CoInitializeEx(0, coinit); {
	case err == nil, errors.Is(err, sFalse):
		// Set up now, or before in the same way: undone once each.
		defer windows.CoUninitialize()
	case errors.Is(err, rpcEChangedMode):
		// Set up before in another way: usable, and not ours to undo.
	default:
		return err
	}
	return f()
}

// What CoInitializeEx says when the thread already has COM.
const (
	sFalse          = syscall.Errno(0x00000001)
	rpcEChangedMode = syscall.Errno(0x80010106)
)

// CredentialGuardRunning reports whether Credential Guard runs, the way
// Microsoft documents checking it: Win32_DeviceGuard's
// SecurityServicesRunning includes 1. The isolated LSA process (LsaIso.exe)
// does not tell: Key Guard, which guards Windows Hello keys, runs it too.
//
// WMI may take long to answer, or not answer at all when its repository is
// damaged; callers should not hold anything up waiting for it.
func CredentialGuardRunning() (running bool, err error) {
	err = withCOM(windows.COINIT_MULTITHREADED, func() error {
		running, err = credentialGuardRunning()
		return err
	})
	return running, err
}

func credentialGuardRunning() (bool, error) {
	unknown, err := oleutil.CreateObject("WbemScripting.SWbemLocator")
	if err != nil {
		return false, err
	}
	defer unknown.Release()
	locator, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return false, err
	}
	defer locator.Release()
	conn, err := oleutil.CallMethod(locator, "ConnectServer", nil, `root\Microsoft\Windows\DeviceGuard`)
	if err != nil {
		return false, err
	}
	service := conn.ToIDispatch()
	defer service.Release()
	query, err := oleutil.CallMethod(service, "ExecQuery", "SELECT SecurityServicesRunning FROM Win32_DeviceGuard")
	if err != nil {
		return false, err
	}
	rows := query.ToIDispatch()
	defer rows.Release()

	running := false
	err = oleutil.ForEach(rows, func(v *ole.VARIANT) error {
		row := v.ToIDispatch()
		defer row.Release()
		prop, err := oleutil.GetProperty(row, "SecurityServicesRunning")
		if err != nil {
			return err
		}
		defer prop.Clear()
		arr := prop.ToArray()
		if arr == nil {
			return nil // none runs
		}
		// An array of variants holding integers (VT_ARRAY|VT_VARIANT).
		for _, s := range arr.ToValueArray() {
			running = running || isOne(s)
		}
		return nil
	})
	return running, err
}

// isOne reports whether v is the integer 1, whatever its type.
func isOne(v any) bool {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() == 1
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint() == 1
	}
	return false
}

// OpenFolder shows a folder in File Explorer. ShellExecute may hand the work
// to shell extensions, so it gets the COM set-up Microsoft asks for.
func OpenFolder(path string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	dir, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return withCOM(windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE, func() error {
		return windows.ShellExecute(0, verb, dir, nil, nil, windows.SW_SHOWNORMAL)
	})
}
