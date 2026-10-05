//go:build windows

package store_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/store"

	"errors"
	"io/fs"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

// Where Windows will not let this account create anything, as in Program
// Files, Prepare says so with an error that is a permission error: the app
// asks for administrator rights on just that.
func TestPrepareReportsAccessDenied(t *testing.T) {
	t.Run("the folders cannot be created", func(t *testing.T) {
		base := t.TempDir()
		denyAdding(t, base)
		d := DirsIn(base)
		err := d.Prepare()
		var fe *FolderError
		if !errors.As(err, &fe) || fe.Dir != d.Data || !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("Prepare() = %v, want access to %s denied", err, d.Data)
		}
	})
	t.Run("the folders exist but take no files", func(t *testing.T) {
		d := DirsIn(t.TempDir())
		if err := os.MkdirAll(d.Data, 0o700); err != nil {
			t.Fatal(err)
		}
		denyAdding(t, d.Data)
		err := d.Prepare()
		var fe *FolderError
		if !errors.As(err, &fe) || fe.Dir != d.Data || !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("Prepare() = %v, want access to %s denied", err, d.Data)
		}
	})
}

// denyAdding keeps this account from creating files and folders in dir
// until the test ends.
func denyAdding(t *testing.T, dir string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	denied, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		// On a folder these are "create files" and "create folders".
		AccessPermissions: windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA,
		AccessMode:        windows.DENY_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(u.User.Sid),
		},
	}}, dacl)
	if err != nil {
		t.Fatal(err)
	}
	set := func(acl *windows.ACL) error {
		return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
	}
	if err := set(denied); err != nil {
		t.Fatal(err)
	}
	// The owner may always change the permissions back, so the temporary
	// folder can be removed.
	t.Cleanup(func() {
		if err := set(dacl); err != nil {
			t.Errorf("restore the permissions of %s: %v", dir, err)
		}
	})
}
