//go:build windows

package secret

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// sealedPrefix marks a DPAPI-sealed value in a file.
const sealedPrefix = "dpapi:"

// entropy ties sealed values to this app: DPAPI only opens them when given
// the same bytes. It is no secret (the source is public); it only keeps a
// value sealed by this app from opening as some other app's.
var entropy = []byte("RDP-over-proxy/secret/v1")

// DPAPI seals values for the current Windows user (CryptProtectData without
// CRYPTPROTECT_LOCAL_MACHINE). Only the same user, on the same computer or
// with a roaming profile, can open them.
type DPAPI struct{}

// Seal returns plain sealed as "dpapi:<base64>". An empty value stays empty.
func (DPAPI) Seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	out, err := protect([]byte(plain), true)
	if err != nil {
		return "", fmt.Errorf("DPAPI: %w", err)
	}
	return sealedPrefix + base64.StdEncoding.EncodeToString(out), nil
}

// Open reverses Seal. An empty value opens to an empty one.
func (DPAPI) Open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	text, ok := strings.CutPrefix(sealed, sealedPrefix)
	if !ok {
		return "", fmt.Errorf("%w: the value is not sealed", ErrUnreadable)
	}
	data, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	out, err := protect(data, false)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	return string(out), nil
}

// protect seals (or, with seal false, opens) data.
func protect(data []byte, seal bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("nothing to process")
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	ent := windows.DataBlob{Size: uint32(len(entropy)), Data: &entropy[0]}
	var out windows.DataBlob
	var err error
	if seal {
		err = windows.CryptProtectData(&in, nil, &ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptUnprotectData(&in, nil, &ent, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}
