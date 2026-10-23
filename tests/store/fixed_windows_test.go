//go:build windows

package store_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/store"

	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/secret"
)

// What earlier versions saved, sealed with DPAPI for this Windows user, is
// sealed again with the app's own key on loading, and from then on opens
// without DPAPI, as on another computer.
func TestDPAPIFilesMoveToAnotherComputer(t *testing.T) {
	dir := t.TempDir()
	const id = "0123456789abcdef"
	sealed, err := secret.DPAPI{}.Seal("s3cret pass")
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "proxies/"+id+".json", `{"schema":1,"name":"Office","kind":"socks","server":"192.0.2.10","port":1080,`+
		`"username":"alice","secret":"`+sealed+`"}`)
	if _, problems := OpenData(dir, secret.Fixed{Legacy: secret.DPAPI{}}); len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ProxiesDir, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"secret": "aes:`) || strings.Contains(string(raw), "dpapi:") || strings.Contains(string(raw), "s3cret") {
		t.Fatalf("the file was not sealed again:\n%s", raw)
	}
	elsewhere, problems := OpenData(dir, secret.Fixed{})
	if len(problems) != 0 {
		t.Fatalf("problems without DPAPI: %v", problems)
	}
	if p, _ := elsewhere.Proxy(id); p.Secret != "s3cret pass" || elsewhere.SecretsLost(id) {
		t.Fatalf("loaded without DPAPI: %+v", p)
	}
}
