package winx_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/winx"

	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestCurrentUserSID(t *testing.T) {
	sid, err := CurrentUserSID()
	if err != nil || !strings.HasPrefix(sid, "S-1-") {
		t.Fatalf("CurrentUserSID() = %q, %v", sid, err)
	}
	// Whether the tests run elevated depends on how they were started; the
	// question itself must get an answer.
	t.Logf("elevated: %v", Elevated())
}

func TestOnLocalDisk(t *testing.T) {
	if dir := t.TempDir(); !OnLocalDisk(dir) {
		t.Errorf("OnLocalDisk(%q) = false", dir)
	}
	// Windows answers for a share at once, reachable or not.
	if share := `\\server.invalid\share\RDP-over-proxy`; OnLocalDisk(share) {
		t.Errorf("OnLocalDisk(%q) = true", share)
	}
}

// Windows words its errors in each UI language when it has the text; the
// English one it always has.
func TestErrorText(t *testing.T) {
	if got := ErrorText(windows.ERROR_ACCESS_DENIED, "en"); got != "Access is denied." {
		t.Errorf("English: %q", got)
	}
	zh := ErrorText(windows.ERROR_ACCESS_DENIED, "zh-CN")
	if zh == "" || strings.HasSuffix(zh, "\n") {
		t.Errorf("Chinese: %q", zh)
	}
	t.Logf("Chinese: %q", zh)
	// Wrapped, as in a FolderError.
	if got := ErrorText(fmt.Errorf("mkdir: %w", windows.ERROR_ACCESS_DENIED), "en"); got != "Access is denied." {
		t.Errorf("wrapped: %q", got)
	}
	if got := ErrorText(errors.New("no Windows error"), "zh-CN"); got != "no Windows error" {
		t.Errorf("not a Windows error: %q", got)
	}
}

// AllowModify adds an entry for the account that its folders and files
// inherit, and keeps what was there.
func TestAllowModify(t *testing.T) {
	dir := t.TempDir()
	sid, err := CurrentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	before := len(aces(t, dir))
	if err := AllowModify(dir, sid); err != nil {
		t.Fatalf("AllowModify: %v", err)
	}
	after := aces(t, dir)
	if len(after) <= before {
		t.Errorf("%d entries before, %d after: the inherited ones should stay", before, len(after))
	}
	const modify = windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.FILE_GENERIC_EXECUTE | windows.DELETE
	const inherit = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
	found := false
	for _, a := range after {
		if a.sid == sid && a.kind == windows.ACCESS_ALLOWED_ACE_TYPE && a.flags&windows.INHERITED_ACE == 0 &&
			a.flags&inherit == inherit && a.mask&modify == modify {
			found = true
		}
	}
	if !found {
		t.Errorf("no inheritable Modify entry for %s in %+v", sid, after)
	}
}

// The elevated copy grants what its command line says, so it takes only a
// user account.
func TestAllowModifyTakesOnlyAUser(t *testing.T) {
	dir := t.TempDir()
	before := aces(t, dir)
	for _, sid := range []string{
		"S-1-1-0",      // Everyone
		"S-1-5-32-545", // Users
		"S-1-5-11",     // Authenticated Users
		"not a SID",
	} {
		if err := AllowModify(dir, sid); err == nil {
			t.Errorf("AllowModify(%q) gave a group or nothing the right to write", sid)
		}
	}
	if after := aces(t, dir); len(after) != len(before) {
		t.Errorf("the permissions changed: %+v", after)
	}
	sid, err := CurrentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	if err := AllowModify(filepath.Join(dir, "missing"), sid); err == nil {
		t.Error("AllowModify on a missing folder should fail")
	}
}

type ace struct {
	kind  uint8
	flags uint8
	mask  windows.ACCESS_MASK
	sid   string
}

// aces lists the entries of dir's permissions.
func aces(t *testing.T, dir string) []ace {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	var out []ace
	for i := range uint32(dacl.AceCount) {
		var a *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &a); err != nil {
			t.Fatal(err)
		}
		sid := (*windows.SID)(unsafe.Pointer(&a.SidStart))
		out = append(out, ace{kind: a.Header.AceType, flags: a.Header.AceFlags, mask: a.Mask, sid: sid.String()})
	}
	return out
}
