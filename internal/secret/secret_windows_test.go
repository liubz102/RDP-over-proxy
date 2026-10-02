//go:build windows

package secret

import (
	"errors"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/model"
)

func TestSealOpen(t *testing.T) {
	var d DPAPI
	for _, plain := range []string{"s3cret", "密码 with spaces ", `{"protocol":"vmess","settings":{}}`} {
		sealed, err := d.Seal(plain)
		if err != nil {
			t.Fatalf("Seal: %v", err)
		}
		if !strings.HasPrefix(sealed, sealedPrefix) || strings.Contains(sealed, plain) {
			t.Fatalf("Seal(%q) = %q", plain, sealed)
		}
		got, err := d.Open(sealed)
		if err != nil || got != plain {
			t.Fatalf("Open = %q, %v; want %q", got, err, plain)
		}
	}
	if s, err := d.Seal(""); s != "" || err != nil {
		t.Fatalf("Seal(\"\") = %q, %v", s, err)
	}
	if s, err := d.Open(""); s != "" || err != nil {
		t.Fatalf("Open(\"\") = %q, %v", s, err)
	}
}

func TestOpenRejectsWhatItCannotOpen(t *testing.T) {
	var d DPAPI
	sealed, _ := d.Seal("s3cret")
	broken := sealed[:len(sealed)-8] + "AAAAAAA="
	for _, s := range []string{"plain text", "dpapi:not base64!", broken} {
		if _, err := d.Open(s); !errors.Is(err, ErrUnreadable) || errcode.Of(err) != "secret.unreadable" {
			t.Errorf("Open(%q) = %v, want ErrUnreadable", s, err)
		}
	}
}

// testVault returns a server name that no real connection can have: the
// .invalid top-level domain is reserved (RFC 2606) and never resolves, so
// mstsc never looks up these credentials and the user's own are untouched.
func testVault(t *testing.T) (Vault, string) {
	t.Helper()
	var v Vault
	server := "rdp-over-proxy-test-" + model.NewID() + ".invalid"
	t.Cleanup(func() { _ = v.Delete(server) })
	return v, server
}

func TestVaultRemembered(t *testing.T) {
	v, server := testVault(t)
	if s, err := v.Lookup(server); err != nil || s.Any() {
		t.Fatalf("Lookup before saving = %+v, %v", s, err)
	}
	if err := v.Save(server, `EXAMPLE\alice`, "p@ss wörd", false); err != nil {
		t.Fatalf("Save: %v", err)
	}
	s, err := v.Lookup(server)
	if err != nil || !s.Ours || s.OneTime || s.ByMstsc || s.User != `EXAMPLE\alice` {
		t.Fatalf("Lookup = %+v, %v", s, err)
	}
	user, pw, found, err := v.Password(server)
	if err != nil || !found || user != `EXAMPLE\alice` || pw != "p@ss wörd" {
		t.Fatalf("Password = %q, %q, %v, %v", user, pw, found, err)
	}
	// A remembered password is not one-time: DeleteOneTime leaves it.
	if err := v.DeleteOneTime(server); err != nil {
		t.Fatal(err)
	}
	if s, _ := v.Lookup(server); !s.Ours {
		t.Fatal("DeleteOneTime removed a remembered password")
	}
	if err := v.Delete(server); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if s, _ := v.Lookup(server); s.Any() {
		t.Fatalf("after Delete: %+v", s)
	}
	if err := v.Delete(server); err != nil {
		t.Fatalf("deleting nothing: %v", err)
	}
}

func TestVaultOneTime(t *testing.T) {
	v, server := testVault(t)
	if err := v.Save(server, "alice", "pw", true); err != nil {
		t.Fatal(err)
	}
	if s, _ := v.Lookup(server); !s.Ours || !s.OneTime {
		t.Fatalf("Lookup = %+v, want a one-time password", s)
	}
	if err := v.DeleteOneTime(server); err != nil {
		t.Fatal(err)
	}
	if s, _ := v.Lookup(server); s.Any() {
		t.Fatalf("after DeleteOneTime: %+v", s)
	}
	if err := v.DeleteOneTime(server); err != nil {
		t.Fatalf("deleting nothing: %v", err)
	}
}

func TestDeleteRememberedLeavesOneTime(t *testing.T) {
	v, server := testVault(t)
	if err := v.Save(server, "alice", "pw", true); err != nil {
		t.Fatal(err)
	}
	if err := v.DeleteRemembered(server); err != nil {
		t.Fatal(err)
	}
	if s, _ := v.Lookup(server); !s.OneTime {
		t.Fatal("DeleteRemembered removed a one-time password a session may need")
	}
	if err := v.Save(server, "alice", "pw", false); err != nil {
		t.Fatal(err)
	}
	if err := v.DeleteRemembered(server); err != nil {
		t.Fatal(err)
	}
	if s, _ := v.Lookup(server); s.Any() {
		t.Fatalf("after DeleteRemembered: %+v", s)
	}
}

func TestVaultRejectsLongPasswords(t *testing.T) {
	v, server := testVault(t)
	if err := v.Save(server, "alice", strings.Repeat("x", MaxPasswordLen+1), false); err == nil {
		t.Fatal("Save accepted a password longer than the limit")
	}
	if err := v.Save(server, "alice", strings.Repeat("x", MaxPasswordLen), false); err != nil {
		t.Fatalf("Save at the limit: %v", err)
	}
}

func TestVaultSeesWhatMstscRemembered(t *testing.T) {
	v, server := testVault(t)
	// What mstsc writes when the user ticks "Remember me".
	writeDomainCredential(t, targetPrefix+server, `EXAMPLE\bob`)
	s, err := v.Lookup(server)
	if err != nil || s.Ours || !s.ByMstsc || s.User != `EXAMPLE\bob` {
		t.Fatalf("Lookup = %+v, %v", s, err)
	}
	if _, _, found, _ := v.Password(server); found {
		t.Fatal("Password read mstsc's credential")
	}
	if err := v.Delete(server); err != nil {
		t.Fatal(err)
	}
	if s, _ := v.Lookup(server); s.Any() {
		t.Fatalf("after Delete: %+v", s)
	}
}

func writeDomainCredential(t *testing.T, target, user string) {
	t.Helper()
	pw := []byte{'p', 0, 'w', 0}
	c := credential{
		Type:               credTypeDomainPassword,
		TargetName:         windows.StringToUTF16Ptr(target),
		UserName:           windows.StringToUTF16Ptr(user),
		Persist:            credPersistSession,
		CredentialBlobSize: uint32(len(pw)),
		CredentialBlob:     &pw[0],
	}
	if r, _, err := procCredWriteW.Call(uintptr(unsafe.Pointer(&c)), 0); r == 0 {
		t.Fatalf("CredWrite(domain password): %v", err)
	}
}
