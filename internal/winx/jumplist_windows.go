//go:build windows

package winx

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

// JumpLink is a shortcut in an app's jump list: the program it starts, with
// the arguments.
type JumpLink struct {
	Path string
	Args string
}

// The shell's objects for an app's automatic jump list (Recent and Frequent,
// two views of one list) and its interfaces.
var (
	clsidApplicationDocumentLists = ole.NewGUID("{86BEC222-30F2-47E0-9F25-60D11CD75C28}")
	iidApplicationDocumentLists   = ole.NewGUID("{3C594F9F-9F30-47A1-979A-C9E83D3D0A06}")
	clsidApplicationDestinations  = ole.NewGUID("{86C14003-4D6B-4EF3-A7B4-0506663B2E68}")
	iidApplicationDestinations    = ole.NewGUID("{12337D35-94C6-48A0-BCE7-6A9C69D4D600}")
	iidObjectArray                = ole.NewGUID("{92CA9DCD-5622-4BBA-A805-5E9F541BD8C9}")
	iidShellLinkW                 = ole.NewGUID("{000214F9-0000-0000-C000-000000000046}")
)

// Methods by their place in the interfaces' tables, after IUnknown's three.
const (
	listsSetAppID          = 3 // IApplicationDocumentLists
	listsGetList           = 4
	destinationsSetAppID   = 3 // IApplicationDestinations
	destinationsRemove     = 4
	objectArrayGetCount    = 3 // IObjectArray
	objectArrayGetAt       = 4
	shellLinkGetPath       = 3 // IShellLinkW
	shellLinkGetArguments  = 10
	adltRecent             = 0 // APPDOCLISTTYPE: the whole list, latest first
	linkArgumentsMaxLength = 1 << 15
)

// PruneJumpList removes from the automatic jump list of the app appID (its
// Recent and Frequent items) the shortcuts remove picks, and returns how many
// it removed. Entries that are not shortcuts stay. Each removal is a call
// to the shell, which keeps the list; a program adding to the list
// meanwhile, such as mstsc as it connects, goes through the shell too.
func PruneJumpList(appID string, remove func(JumpLink) bool) (removed int, err error) {
	err = withCOM(windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE, func() error {
		removed, err = pruneJumpList(appID, remove)
		return err
	})
	return removed, err
}

func pruneJumpList(appID string, remove func(JumpLink) bool) (int, error) {
	id, err := windows.UTF16PtrFromString(appID)
	if err != nil {
		return 0, err
	}
	lists, err := ole.CreateInstance(clsidApplicationDocumentLists, iidApplicationDocumentLists)
	if err != nil {
		return 0, fmt.Errorf("read the jump list: %w", err)
	}
	defer lists.Release()
	r, _, _ := syscall.SyscallN(method(lists, listsSetAppID), uintptr(unsafe.Pointer(lists)), uintptr(unsafe.Pointer(id)))
	if err := hresult(r); err != nil {
		return 0, fmt.Errorf("read the jump list: %w", err)
	}
	var items *ole.IUnknown
	r, _, _ = syscall.SyscallN(method(lists, listsGetList), uintptr(unsafe.Pointer(lists)), adltRecent, 0,
		uintptr(unsafe.Pointer(iidObjectArray)), uintptr(unsafe.Pointer(&items)))
	if err := hresult(r); err != nil {
		return 0, fmt.Errorf("read the jump list: %w", err)
	}
	defer items.Release()
	var count uint32
	r, _, _ = syscall.SyscallN(method(items, objectArrayGetCount), uintptr(unsafe.Pointer(items)), uintptr(unsafe.Pointer(&count)))
	if err := hresult(r); err != nil {
		return 0, fmt.Errorf("read the jump list: %w", err)
	}

	var picked []*ole.IUnknown
	defer func() {
		for _, p := range picked {
			p.Release()
		}
	}()
	for i := range count {
		var link *ole.IUnknown
		r, _, _ := syscall.SyscallN(method(items, objectArrayGetAt), uintptr(unsafe.Pointer(items)), uintptr(i),
			uintptr(unsafe.Pointer(iidShellLinkW)), uintptr(unsafe.Pointer(&link)))
		if hresult(r) != nil {
			continue // not a shortcut
		}
		l, err := readLink(link)
		if err != nil || !remove(l) {
			link.Release()
			continue
		}
		picked = append(picked, link)
	}
	if len(picked) == 0 {
		return 0, nil
	}

	dest, err := ole.CreateInstance(clsidApplicationDestinations, iidApplicationDestinations)
	if err != nil {
		return 0, fmt.Errorf("change the jump list: %w", err)
	}
	defer dest.Release()
	r, _, _ = syscall.SyscallN(method(dest, destinationsSetAppID), uintptr(unsafe.Pointer(dest)), uintptr(unsafe.Pointer(id)))
	if err := hresult(r); err != nil {
		return 0, fmt.Errorf("change the jump list: %w", err)
	}
	removed := 0
	var errs []error
	for _, p := range picked {
		r, _, _ := syscall.SyscallN(method(dest, destinationsRemove), uintptr(unsafe.Pointer(dest)), uintptr(unsafe.Pointer(p)))
		if err := hresult(r); err != nil {
			errs = append(errs, fmt.Errorf("remove an entry from the jump list: %w", err))
			continue
		}
		removed++
	}
	return removed, errors.Join(errs...)
}

// readLink reads a shortcut's program and arguments (IShellLinkW).
func readLink(link *ole.IUnknown) (JumpLink, error) {
	path := make([]uint16, windows.MAX_LONG_PATH)
	r, _, _ := syscall.SyscallN(method(link, shellLinkGetPath), uintptr(unsafe.Pointer(link)),
		uintptr(unsafe.Pointer(&path[0])), uintptr(len(path)), 0, 0)
	if err := hresult(r); err != nil {
		return JumpLink{}, err
	}
	args := make([]uint16, linkArgumentsMaxLength)
	r, _, _ = syscall.SyscallN(method(link, shellLinkGetArguments), uintptr(unsafe.Pointer(link)),
		uintptr(unsafe.Pointer(&args[0])), uintptr(len(args)))
	if err := hresult(r); err != nil {
		return JumpLink{}, err
	}
	return JumpLink{Path: windows.UTF16ToString(path), Args: windows.UTF16ToString(args)}, nil
}

// method returns the address of an interface's method by its place in the
// interface's table.
func method(obj *ole.IUnknown, index int) uintptr {
	return (*[16]uintptr)(unsafe.Pointer(obj.RawVTable))[index]
}

// hresult turns a failing HRESULT into an error.
func hresult(r uintptr) error {
	if int32(r) < 0 {
		return syscall.Errno(r)
	}
	return nil
}
