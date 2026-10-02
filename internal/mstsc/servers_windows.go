//go:build windows

package mstsc

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// ServersKey is where mstsc keeps what it remembers about each computer it
// has connected to, one subkey per address under HKEY_CURRENT_USER:
// UsernameHint (the user name its sign-in prompt suggests) and CertHash
// (the certificate the user chose to trust).
const ServersKey = `Software\Microsoft\Terminal Server Client\Servers`

// Servers edits mstsc's memory of computers. Every profile has a loopback
// address of its own, so the subkeys the app touches belong to its profiles
// alone.
type Servers struct {
	// Key is the registry key under HKEY_CURRENT_USER. The app uses
	// ServersKey; tests use one of their own.
	Key string
}

// SetUsernameHint makes mstsc suggest user when it asks for the password
// for server.
func (s Servers) SetUsernameHint(server, user string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, s.Key+`\`+server, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue("UsernameHint", user)
}

// Forget removes everything mstsc remembers about server, under the bare
// address and under the address with any port: the user name it suggests
// and the certificate trust. Nothing remembered is not an error.
func (s Servers) Forget(server string) error {
	parent, err := registry.OpenKey(registry.CURRENT_USER, s.Key, registry.ENUMERATE_SUB_KEYS)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return err
	}
	names, err := parent.ReadSubKeyNames(-1)
	parent.Close()
	if err != nil {
		return err
	}
	bare, withPort := strings.ToLower(server), strings.ToLower(server)+":"
	var errs []error
	for _, name := range names {
		lower := strings.ToLower(name)
		if lower != bare && !strings.HasPrefix(lower, withPort) {
			continue
		}
		err := registry.DeleteKey(registry.CURRENT_USER, s.Key+`\`+name)
		if err != nil && !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// DefaultRDPPath is Default.rdp in the user's Documents folder. mstsc reads
// it for every connection opened with /v: and saves to it when the user
// changes options in its window.
func DefaultRDPPath() (string, error) {
	docs, err := windows.KnownFolderPath(windows.FOLDERID_Documents, 0)
	if err != nil {
		return "", err
	}
	return filepath.Join(docs, "Default.rdp"), nil
}
