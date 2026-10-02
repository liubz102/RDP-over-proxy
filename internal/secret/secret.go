// Package secret keeps secrets out of plain sight: proxy passwords and Xray
// outbounds are sealed with DPAPI before they are written to a file, and the
// Remote Desktop password lives in Windows Credential Manager, where mstsc
// reads it.
package secret

import "github.com/liubz102/RDP-over-proxy/internal/errcode"

// MaxPasswordLen is the longest Remote Desktop password, in UTF-16 code
// units, the vault stores. Windows passwords are at most 256 characters,
// well within Credential Manager's limit of 2560 bytes.
const MaxPasswordLen = 256

// Errors.
var (
	// ErrUnreadable: a sealed value cannot be opened, typically because the
	// file was copied from another Windows user or computer.
	ErrUnreadable = errcode.New("secret.unreadable", "the saved secret cannot be decrypted by this Windows user")
	// ErrVault labels failures to read or change Credential Manager.
	ErrVault = errcode.New("credential.failed", "Windows Credential Manager refused the change")
)

// Saved is what Credential Manager holds for a server.
type Saved struct {
	// Ours: a password this app stored.
	Ours bool
	// OneTime: ours is only for the session running now; it is deleted when
	// the session ends, and Windows drops it at sign-out in any case.
	OneTime bool
	// ByMstsc: a password mstsc itself remembered ("Remember me").
	ByMstsc bool
	// User is the user name stored with the password, ours first.
	User string
}

// Any reports whether some password is saved for the server.
func (s Saved) Any() bool { return s.Ours || s.ByMstsc }
