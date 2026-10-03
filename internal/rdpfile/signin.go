package rdpfile

// ServerAuth is the "authentication level" property: what mstsc does when it
// cannot verify the remote computer's identity ("If server authentication
// fails" on its Advanced tab).
//
// Through the app's tunnel it never can by name: mstsc connects to
// 127.x.y.z, which is not the name on the computer's certificate, and
// Kerberos knows no such computer. Only a certificate the user chose to
// trust for that address ("Don't ask me again") gets past it.
type ServerAuth int

// ServerAuth values, as Microsoft documents them for .rdp files.
const (
	// ServerAuthConnect: connect without warning.
	ServerAuthConnect ServerAuth = 0
	// ServerAuthRefuse: do not connect.
	ServerAuthRefuse ServerAuth = 1
	// ServerAuthWarn: warn, and let the user connect or not. mstsc's own
	// default.
	ServerAuthWarn ServerAuth = 2
	// ServerAuthUnspecified: no requirement; also a file without the
	// property, or with a value mstsc does not define.
	ServerAuthUnspecified ServerAuth = 3
)

// ServerAuth returns the file's "authentication level".
func (f *File) ServerAuth() ServerAuth {
	n, ok := f.Int(propServerAuth)
	if !ok || n < int(ServerAuthConnect) || n > int(ServerAuthUnspecified) {
		return ServerAuthUnspecified
	}
	return ServerAuth(n)
}

// AlwaysPrompt reports whether mstsc asks for the password at every
// connection, even when one is saved ("prompt for credentials" is 1:
// "Always ask for credentials" on its General tab).
func (f *File) AlwaysPrompt() bool {
	return f.flag(propPromptCreds)
}
