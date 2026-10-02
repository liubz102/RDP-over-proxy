//go:build windows

package secret

import (
	"errors"
	"fmt"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// targetPrefix starts the target name mstsc looks up in Credential Manager.
// Windows accepts domain-password credentials only under names it knows,
// such as this one.
const targetPrefix = "TERMSRV/"

const (
	credTypeGeneric        = 1
	credTypeDomainPassword = 2

	credPersistSession      = 1
	credPersistLocalMachine = 2

	// commentText marks the credentials this app wrote, for anyone looking
	// at them with "cmdkey /list" or in Credential Manager.
	commentText = "Saved by RDP over Proxy"
)

var (
	advapi32        = windows.NewLazySystemDLL("advapi32.dll")
	procCredWriteW  = advapi32.NewProc("CredWriteW")
	procCredReadW   = advapi32.NewProc("CredReadW")
	procCredDeleteW = advapi32.NewProc("CredDeleteW")
	procCredFree    = advapi32.NewProc("CredFree")
)

// credential is CREDENTIALW.
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

// Vault keeps the Remote Desktop passwords mstsc signs in with, under the
// target name "TERMSRV/<server>".
//
// The app stores generic credentials ("cmdkey /generic" makes the same kind),
// which it can read back. When the user ticks "Remember me" in mstsc's own
// prompt, mstsc stores a domain-password credential under the same name;
// the vault reports and deletes those too, but never writes them.
type Vault struct{}

// Save stores the password for server. A one-time password lasts until the
// session that needs it deletes it (DeleteOneTime), or at the latest until
// the user signs out of Windows.
func (v Vault) Save(server, user, password string, oneTime bool) error {
	units := utf16.Encode([]rune(password))
	if len(units) > MaxPasswordLen {
		return fmt.Errorf("%w: the password is longer than %d characters", ErrVault, MaxPasswordLen)
	}
	target, err := windows.UTF16PtrFromString(targetPrefix + server)
	if err != nil {
		return err
	}
	comment, _ := windows.UTF16PtrFromString(commentText)
	userPtr, err := windows.UTF16PtrFromString(user)
	if err != nil {
		return err
	}
	// The password is UTF-16LE without a terminator, as cmdkey stores it.
	blob := make([]byte, 2*len(units))
	for i, u := range units {
		blob[2*i], blob[2*i+1] = byte(u), byte(u>>8)
	}
	c := credential{
		Type:       credTypeGeneric,
		TargetName: target,
		Comment:    comment,
		Persist:    credPersistLocalMachine,
		UserName:   userPtr,
	}
	if oneTime {
		c.Persist = credPersistSession
	}
	if len(blob) > 0 {
		c.CredentialBlobSize = uint32(len(blob))
		c.CredentialBlob = &blob[0]
	}
	r, _, callErr := procCredWriteW.Call(uintptr(unsafe.Pointer(&c)), 0)
	clear(blob)
	if r == 0 {
		return fmt.Errorf("%w: %w", ErrVault, callErr)
	}
	return nil
}

// Lookup reports what is saved for server.
func (v Vault) Lookup(server string) (Saved, error) {
	var s Saved
	err := v.read(server, credTypeGeneric, func(c *credential) {
		s.Ours = true
		s.OneTime = c.Persist == credPersistSession
		s.User = windows.UTF16PtrToString(c.UserName)
	})
	if err != nil {
		return Saved{}, err
	}
	err = v.read(server, credTypeDomainPassword, func(c *credential) {
		s.ByMstsc = true
		if !s.Ours {
			s.User = windows.UTF16PtrToString(c.UserName)
		}
	})
	if err != nil {
		return Saved{}, err
	}
	return s, nil
}

// Password reads back the password this app saved for server. found is
// false when there is none.
func (v Vault) Password(server string) (user, password string, found bool, err error) {
	err = v.read(server, credTypeGeneric, func(c *credential) {
		found = true
		user = windows.UTF16PtrToString(c.UserName)
		blob := unsafe.Slice(c.CredentialBlob, c.CredentialBlobSize)
		units := make([]uint16, len(blob)/2)
		for i := range units {
			units[i] = uint16(blob[2*i]) | uint16(blob[2*i+1])<<8
		}
		password = string(utf16.Decode(units))
	})
	return user, password, found, err
}

// Delete removes every password saved for server: the app's own and the one
// mstsc remembered. Nothing saved is not an error.
func (v Vault) Delete(server string) error {
	return errors.Join(v.remove(server, credTypeGeneric), v.remove(server, credTypeDomainPassword))
}

// DeleteOneTime removes the app's password for server if it is the
// one-time kind. A password the user chose to remember in the meantime
// stays.
func (v Vault) DeleteOneTime(server string) error { return v.removeOurs(server, true) }

// DeleteRemembered removes the app's remembered password for server. A
// one-time password, which a running session may still need, stays, and so
// does the one mstsc remembered.
func (v Vault) DeleteRemembered(server string) error { return v.removeOurs(server, false) }

func (v Vault) removeOurs(server string, oneTime bool) error {
	match := false
	err := v.read(server, credTypeGeneric, func(c *credential) {
		match = (c.Persist == credPersistSession) == oneTime
	})
	if err != nil || !match {
		return err
	}
	return v.remove(server, credTypeGeneric)
}

// read calls f with the credential of the given type, if there is one.
func (v Vault) read(server string, typ uint32, f func(*credential)) error {
	target, err := windows.UTF16PtrFromString(targetPrefix + server)
	if err != nil {
		return err
	}
	var c *credential
	r, _, callErr := procCredReadW.Call(uintptr(unsafe.Pointer(target)), uintptr(typ), 0, uintptr(unsafe.Pointer(&c)))
	if r == 0 {
		if errors.Is(callErr, windows.ERROR_NOT_FOUND) {
			return nil
		}
		return fmt.Errorf("%w: %w", ErrVault, callErr)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(c)))
	f(c)
	return nil
}

func (v Vault) remove(server string, typ uint32) error {
	target, err := windows.UTF16PtrFromString(targetPrefix + server)
	if err != nil {
		return err
	}
	r, _, callErr := procCredDeleteW.Call(uintptr(unsafe.Pointer(target)), uintptr(typ), 0)
	if r == 0 && !errors.Is(callErr, windows.ERROR_NOT_FOUND) {
		return fmt.Errorf("%w: %w", ErrVault, callErr)
	}
	return nil
}
