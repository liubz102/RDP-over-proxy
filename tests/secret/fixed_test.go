package secret_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/secret"

	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
)

// pinned was sealed with the app's key on 2026-10-23, by Node.js rather than
// this package, so it stands for a file saved anywhere. If it no longer
// opens, the key or the format changed and no file saved before opens any
// more: change them back.
const (
	pinned      = "aes:ckjW0JHvT9J0lPYLuhzbuc7+ooHskfsuS1hle2Y3qb8Y2v5A3lOE6Athor2aTV+II68ixsf4yJT85EdvJRrpEodsGqVKerG9GrWE3l/o"
	pinnedPlain = "sealed on 2026-10-23 by Node.js: p@ss wörd 密码"
)

func TestFixedSealOpen(t *testing.T) {
	var f Fixed
	for _, plain := range []string{"s3cret", "密码 with spaces ", `{"protocol":"vmess","settings":{}}`} {
		sealed, err := f.Seal(plain)
		if err != nil {
			t.Fatalf("Seal: %v", err)
		}
		// The file format: "aes:" and the nonce and sealed bytes in base64.
		if !strings.HasPrefix(sealed, "aes:") || strings.Contains(sealed, plain) || !f.Current(sealed) {
			t.Fatalf("Seal(%q) = %q", plain, sealed)
		}
		got, err := f.Open(sealed)
		if err != nil || got != plain {
			t.Fatalf("Open = %q, %v; want %q", got, err, plain)
		}
		// A new nonce every time: the same value never looks the same twice.
		if again, _ := f.Seal(plain); again == sealed {
			t.Fatalf("Seal(%q) gave the same text twice", plain)
		}
	}
	if s, err := f.Seal(""); s != "" || err != nil {
		t.Fatalf("Seal(\"\") = %q, %v", s, err)
	}
	if s, err := f.Open(""); s != "" || err != nil {
		t.Fatalf("Open(\"\") = %q, %v", s, err)
	}
}

func TestFixedOpensWhatWasSealedElsewhere(t *testing.T) {
	got, err := Fixed{}.Open(pinned)
	if err != nil || got != pinnedPlain {
		t.Fatalf("Open(pinned) = %q, %v: the key or the format changed, so files saved before no longer open", got, err)
	}
}

func TestFixedRejectsWhatItCannotOpen(t *testing.T) {
	var f Fixed
	sealed, _ := f.Seal("s3cret")
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, "aes:"))
	if err != nil {
		t.Fatal(err)
	}
	changed := append([]byte(nil), raw...)
	changed[len(changed)-1] ^= 1
	for name, s := range map[string]string{
		"changed":               "aes:" + base64.StdEncoding.EncodeToString(changed),
		"shorter than a nonce":  "aes:" + base64.StdEncoding.EncodeToString(raw[:8]),
		"only a nonce":          "aes:" + base64.StdEncoding.EncodeToString(raw[:12]),
		"not base64":            "aes:not base64!",
		"plain text":            "plain text",
		"DPAPI without Legacy":  "dpapi:AQAAAB",
		"another app's sealing": "sealed:" + base64.StdEncoding.EncodeToString([]byte("s3cret")),
	} {
		if _, err := f.Open(s); !errors.Is(err, ErrUnreadable) || errcode.Of(err) != "secret.unreadable" {
			t.Errorf("%s: Open(%q) = %v, want ErrUnreadable", name, s, err)
		}
	}
}

// oldSealing stands in for DPAPI, which earlier versions sealed with:
// "dpapi:" and the plain text.
type oldSealing struct{}

func (oldSealing) Open(s string) (string, error) {
	text, ok := strings.CutPrefix(s, "dpapi:")
	if !ok {
		return "", ErrUnreadable
	}
	return text, nil
}

func TestFixedOpensOldValuesThroughLegacy(t *testing.T) {
	f := Fixed{Legacy: oldSealing{}}
	if got, err := f.Open("dpapi:s3cret"); err != nil || got != "s3cret" {
		t.Fatalf("Open = %q, %v", got, err)
	}
	// Such a value is better sealed again; Seal itself never seals that way.
	if f.Current("dpapi:s3cret") || !f.Current("") || !f.Current(pinned) {
		t.Fatal("Current is wrong")
	}
	if sealed, _ := f.Seal("s3cret"); !strings.HasPrefix(sealed, "aes:") {
		t.Fatalf("Seal = %q", sealed)
	}
}
