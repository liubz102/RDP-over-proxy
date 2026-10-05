//go:build windows && !server

package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"

	"github.com/liubz102/RDP-over-proxy/internal/i18n"
	"github.com/liubz102/RDP-over-proxy/internal/store"
	"github.com/liubz102/RDP-over-proxy/internal/winx"
)

// The data and logs folders are next to the exe. Where only an administrator
// may write, as in Program Files, the app asks for administrator rights once
// (UAC) and has an elevated copy of itself create the folders and let the
// user's account write to them. The app itself never runs elevated: when a
// standard user borrows an administrator's sign-in, an elevated process runs
// as that administrator, and its encrypted proxy passwords and Remote Desktop
// passwords would belong to the wrong account.

// prepareFlag starts the app as that elevated copy. The one argument after
// it is the SID of the account to let write.
const prepareFlag = "--prepare-folders"

// elevatedTask does the elevated copy's work when args ask for it. ok says
// whether they did; code is the exit code: 0, or the Windows error code that
// stopped it.
func elevatedTask(args []string) (code int, ok bool) {
	if len(args) == 0 || args[0] != prepareFlag {
		return 0, false
	}
	if len(args) != 2 {
		return int(windows.ERROR_INVALID_PARAMETER), true
	}
	return int(errnoOf(prepareElevated(args[1]))), true
}

// prepareElevated creates the folders next to this exe and lets sid write
// in them. It works out the folders itself rather than being told: an
// elevated process acts only where it can vouch for. (RDP_OVER_PROXY_HOME
// would not reach it anyway: UAC starts it with the account's own
// environment.)
func prepareElevated(sid string) error {
	base, err := store.ExeDir()
	if err != nil {
		return err
	}
	d := store.DirsIn(base)
	for _, dir := range []string{d.Data, d.Logs} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := winx.AllowModify(dir, sid); err != nil {
			return err
		}
	}
	return nil
}

// errnoOf is the Windows error code in err, as an exit code.
func errnoOf(err error) uint32 {
	if err == nil {
		return 0
	}
	var errno windows.Errno
	if errors.As(err, &errno) && errno != 0 {
		return uint32(errno)
	}
	return uint32(windows.ERROR_GEN_FAILURE)
}

// prepareDirs makes sure the app can write its folders. When Windows
// refuses because only an administrator may write there, it explains and
// asks for administrator rights; whatever else stops it, it explains. A
// non-nil error means the app cannot run, and the user has been told why.
// All this happens before the language setting can be read, perhaps before
// it could ever have been saved, so the messages are in both languages.
func prepareDirs(d store.Dirs) error {
	err := d.Prepare()
	if err == nil {
		return nil
	}
	base := filepath.Dir(d.Data)
	if !elevationHelps(err, base) {
		showFolderError(err)
		return err
	}
	if !winx.Confirm(both("folders.elevate.title", " / "), both("folders.elevate.body", "\n\n", "{dir}", base)) {
		return err // the message said what to do instead
	}
	exe, err := os.Executable()
	if err != nil {
		showFolderError(err)
		return err
	}
	sid, err := winx.CurrentUserSID()
	if err != nil {
		showFolderError(err)
		return err
	}
	code, err := winx.RunElevated(exe, prepareFlag, sid)
	switch {
	case errors.Is(err, winx.ErrCancelled):
		winx.ShowError(both("folders.elevate.title", " / "), both("folders.declined.body", "\n\n", "{dir}", base))
		return err
	case err != nil:
		err = &store.FolderError{Dir: base, Err: err}
	case code != 0:
		err = &store.FolderError{Dir: base, Err: windows.Errno(code)}
	default:
		// The elevated copy is done. Whether this account can write now
		// is for this account to find out.
		err = d.Prepare()
	}
	if err != nil {
		showFolderError(err)
	}
	return err
}

// elevationHelps reports whether administrator rights could get past err:
// Windows denied access on a disk of this PC to a process without them, in
// the exe's own folder (the elevated copy knows nothing of
// RDP_OVER_PROXY_HOME).
func elevationHelps(err error, base string) bool {
	return errors.Is(err, fs.ErrPermission) && os.Getenv(store.EnvHome) == "" &&
		!winx.Elevated() && winx.OnLocalDisk(base)
}

// showFolderError says which folder cannot be used and why, the reason in
// each language as Windows words it.
func showFolderError(err error) {
	dir, reason := "", err
	var fe *store.FolderError
	if errors.As(err, &fe) {
		dir, reason = fe.Dir, fe.Err
	}
	var parts []string
	for _, lang := range i18n.Order(i18n.Detect()) {
		r := strings.NewReplacer("{dir}", dir, "{reason}", winx.ErrorText(reason, lang))
		parts = append(parts, r.Replace(i18n.T(lang, "folders.unusable.body")))
	}
	winx.ShowError(both("folders.unusable.title", " / "), strings.Join(parts, "\n\n"))
}

// both is a message in both languages, the system's first.
func both(key, sep string, replace ...string) string {
	return i18n.Both(i18n.Detect(), key, sep, replace...)
}
