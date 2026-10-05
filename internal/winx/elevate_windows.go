//go:build windows

package winx

import (
	"fmt"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ErrCancelled is RunElevated's error when the user said no to the UAC
// prompt.
var ErrCancelled error = windows.ERROR_CANCELLED

// Elevated reports whether this process runs with administrator rights.
func Elevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// CurrentUserSID is the SID of the account this process runs as, such as
// "S-1-5-21-…-1001".
func CurrentUserSID() (string, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String(), nil
}

// OnLocalDisk reports whether path is on a disk of this PC, which
// administrator rights on this PC can open up; a network share or a CD they
// cannot.
func OnLocalDisk(path string) bool {
	root, err := windows.UTF16PtrFromString(filepath.VolumeName(path) + `\`)
	if err != nil {
		return false
	}
	switch windows.GetDriveType(root) {
	case windows.DRIVE_FIXED, windows.DRIVE_REMOVABLE, windows.DRIVE_RAMDISK:
		return true
	}
	return false
}

// modify is File Explorer's "Modify" permission: read, write, run and
// delete, but not change permissions or take ownership.
const modify = windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.FILE_GENERIC_EXECUTE | windows.DELETE

// AllowModify lets the account sid create, change and delete files in dir
// and everything in it, in addition to what dir's permissions allow already.
// sid must name a user account, not a group such as Everyone: the elevated
// process that calls this does what its command line says.
func AllowModify(dir, sid string) error {
	s, err := windows.StringToSid(sid)
	if err != nil {
		return err
	}
	_, _, kind, err := s.LookupAccount("")
	if err != nil {
		return err
	}
	if kind != windows.SidTypeUser {
		return fmt.Errorf("%s is not a user account: %w", sid, windows.ERROR_INVALID_SID)
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: modify,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(s),
		},
	}}, dacl)
	if err != nil {
		return err
	}
	// The folder keeps what it inherits; Windows passes the new entry on to
	// what is in it already.
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}

// shellExecuteInfo is SHELLEXECUTEINFOW.
type shellExecuteInfo struct {
	size          uint32
	mask          uint32
	hwnd          windows.HWND
	verb          *uint16
	file          *uint16
	parameters    *uint16
	directory     *uint16
	show          int32
	instApp       windows.Handle
	idList        uintptr
	class         *uint16
	keyClass      windows.Handle
	hotKey        uint32
	iconOrMonitor windows.Handle
	process       windows.Handle
}

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
)

var procShellExecuteExW = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

// RunElevated starts exe with administrator rights, which Windows asks the
// user for (UAC), and waits for it to exit. It returns the exit code, or
// ErrCancelled when the user said no.
func RunElevated(exe string, args ...string) (exitCode uint32, err error) {
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return 0, err
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return 0, err
	}
	params, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(args))
	if err != nil {
		return 0, err
	}
	info := shellExecuteInfo{
		mask:       seeMaskNoCloseProcess | seeMaskNoAsync,
		verb:       verb,
		file:       file,
		parameters: params,
		show:       windows.SW_HIDE,
	}
	info.size = uint32(unsafe.Sizeof(info))
	// ShellExecuteEx may hand the work to shell extensions, so it gets the
	// COM set-up Microsoft asks for.
	err = withCOM(windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE, func() error {
		if ok, _, e := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info))); ok == 0 {
			if errno, isErrno := e.(windows.Errno); isErrno && errno != 0 {
				return errno
			}
			return windows.ERROR_GEN_FAILURE
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if info.process == 0 {
		return 0, fmt.Errorf("%s started without a process to wait for: %w", exe, windows.ERROR_INVALID_HANDLE)
	}
	defer windows.CloseHandle(info.process)
	// Only the process knows when it is done; its handle is signalled when it
	// exits.
	if _, err := windows.WaitForSingleObject(info.process, windows.INFINITE); err != nil {
		return 0, err
	}
	if err := windows.GetExitCodeProcess(info.process, &exitCode); err != nil {
		return 0, err
	}
	return exitCode, nil
}
