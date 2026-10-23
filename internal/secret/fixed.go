package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// Prefixes of sealed values in files.
const (
	// filePrefix marks a value sealed with the app's own key (Fixed).
	filePrefix = "aes:"
	// dpapiPrefix marks a value earlier versions sealed with DPAPI.
	dpapiPrefix = "dpapi:"
)

// label ties sealed values to this app: it is the AES-GCM additional data,
// and DPAPI's entropy. It is no secret; it only keeps a value sealed by this
// app from opening as some other app's.
var label = []byte("RDP-over-proxy/secret/v1")

// fileAEAD seals with the app's own key, AES-256. The key was drawn at
// random once, on 2026-10-23, and must never change: no file written with
// it would open any more (tests/secret holds a value sealed with it). It is
// no real secret either, since the source is public: it keeps passwords out
// of plain sight in the files, and lets them move with the files to another
// computer, which DPAPI did not.
var fileAEAD = func() cipher.AEAD {
	key, err := hex.DecodeString("10d7e7fc593c00375fcf37a79f85628762838ffaca441a8d6faaeba9e9f410a3")
	if err != nil || len(key) != 32 {
		panic("secret: the file key is not 32 bytes of hex")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return aead
}()

// Fixed seals values with the app's own key (AES-256-GCM), so they open on
// any computer, for any Windows user. Legacy opens the values earlier
// versions sealed with DPAPI ("dpapi:"); without it they are unreadable.
type Fixed struct {
	Legacy interface {
		Open(sealed string) (string, error)
	}
}

// Seal returns plain sealed as "aes:<base64>": a random nonce, then the
// sealed bytes. An empty value stays empty.
func (Fixed) Seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	n := fileAEAD.NonceSize()
	nonce := make([]byte, n, n+len(plain)+fileAEAD.Overhead())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("random nonce: %w", err)
	}
	out := fileAEAD.Seal(nonce, nonce, []byte(plain), label)
	return filePrefix + base64.StdEncoding.EncodeToString(out), nil
}

// Open reverses Seal, and opens what earlier versions sealed with DPAPI
// through Legacy. An empty value opens to an empty one.
func (f Fixed) Open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	if strings.HasPrefix(sealed, dpapiPrefix) {
		if f.Legacy == nil {
			return "", fmt.Errorf("%w: sealed with DPAPI, which only the Windows user who saved it can open", ErrUnreadable)
		}
		return f.Legacy.Open(sealed)
	}
	text, ok := strings.CutPrefix(sealed, filePrefix)
	if !ok {
		return "", fmt.Errorf("%w: the value is not sealed", ErrUnreadable)
	}
	data, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	n := fileAEAD.NonceSize()
	if len(data) < n {
		return "", fmt.Errorf("%w: the value is cut short", ErrUnreadable)
	}
	plain, err := fileAEAD.Open(nil, data[:n], data[n:], label)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	return string(plain), nil
}

// Current reports whether sealed is empty or sealed the way Seal does it
// now. Anything else, a value sealed with DPAPI, is better sealed again.
func (Fixed) Current(sealed string) bool {
	return sealed == "" || strings.HasPrefix(sealed, filePrefix)
}
