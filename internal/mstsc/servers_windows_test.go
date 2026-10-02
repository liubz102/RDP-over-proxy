//go:build windows

package mstsc_test

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/mstsc"
)

// testServers uses a key of the test's own under HKEY_CURRENT_USER, never
// mstsc's, and removes it afterwards.
func testServers(t *testing.T) mstsc.Servers {
	t.Helper()
	parent := `Software\RDP-over-proxy-test`
	key := parent + `\` + model.NewID()
	t.Cleanup(func() {
		if k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.ENUMERATE_SUB_KEYS); err == nil {
			names, _ := k.ReadSubKeyNames(-1)
			k.Close()
			for _, n := range names {
				_ = registry.DeleteKey(registry.CURRENT_USER, key+`\`+n)
			}
		}
		_ = registry.DeleteKey(registry.CURRENT_USER, key)
		_ = registry.DeleteKey(registry.CURRENT_USER, parent) // fails while other tests use it
	})
	return mstsc.Servers{Key: key}
}

func TestUsernameHint(t *testing.T) {
	s := testServers(t)
	if err := s.SetUsernameHint("127.1.2.3", `EXAMPLE\alice`); err != nil {
		t.Fatalf("SetUsernameHint: %v", err)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, s.Key+`\127.1.2.3`, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := k.GetStringValue("UsernameHint")
	k.Close()
	if err != nil || got != `EXAMPLE\alice` {
		t.Fatalf("UsernameHint = %q, %v", got, err)
	}

	if err := s.Forget("127.1.2.3"); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	_, err = registry.OpenKey(registry.CURRENT_USER, s.Key+`\127.1.2.3`, registry.QUERY_VALUE)
	if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		t.Fatalf("the key is still there: %v", err)
	}
	if err := s.Forget("127.1.2.3"); err != nil {
		t.Fatalf("forgetting nothing: %v", err)
	}
}

func TestForgetTakesEveryPort(t *testing.T) {
	s := testServers(t)
	for _, name := range []string{"127.1.2.3", "127.1.2.3:13389", "127.1.2.3:23389", "127.1.2.30:13389"} {
		if err := s.SetUsernameHint(name, "alice"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Forget("127.1.2.3"); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, s.Key, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		t.Fatal(err)
	}
	left, err := k.ReadSubKeyNames(-1)
	k.Close()
	if err != nil || len(left) != 1 || left[0] != "127.1.2.30:13389" {
		t.Fatalf("left after Forget: %q, %v; want only another address", left, err)
	}
}

func TestDefaultRDPPath(t *testing.T) {
	p, err := mstsc.DefaultRDPPath()
	if err != nil {
		t.Fatal(err)
	}
	if len(p) < len(`C:\Default.rdp`) || p[len(p)-len(`\Default.rdp`):] != `\Default.rdp` {
		t.Fatalf("DefaultRDPPath = %q", p)
	}
}
