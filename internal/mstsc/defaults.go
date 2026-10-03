package mstsc

import "github.com/liubz102/RDP-over-proxy/internal/rdpfile"

// Defaults is what the user's Default.rdp and Group Policy decide for every
// connection the app starts: mstsc started with /v: takes everything but the
// address and the display switches from Default.rdp.
type Defaults struct {
	// Path is Default.rdp in the user's Documents folder, and Found whether
	// it exists. Without it mstsc uses its built-in defaults.
	Path  string
	Found bool
	// Gateway is whether mstsc would hand connections to an RD Gateway.
	Gateway Gateway
	// ServerAuth is what mstsc does when it cannot verify the computer,
	// which through the tunnel it never can by name (see rdpfile.ServerAuth).
	// ServerAuthByPolicy: Group Policy decides it, not Default.rdp.
	ServerAuth         rdpfile.ServerAuth
	ServerAuthByPolicy bool
	// AlwaysPrompt: mstsc asks for the password even when one is saved.
	AlwaysPrompt bool
}

// Policies are the Group Policy settings that decide over Default.rdp.
type Policies struct {
	Gateway GatewayPolicy
	// ServerAuth is "Configure server authentication for client" (Computer
	// Configuration > Administrative Templates > Windows Components > Remote
	// Desktop Services > Remote Desktop Connection Client), when enabled
	// (ServerAuthSet). It applies instead of what mstsc or the .rdp file
	// says.
	ServerAuth    rdpfile.ServerAuth
	ServerAuthSet bool
}

// DecideDefaults combines Default.rdp, nil when there is none, with the
// policies (see DecideGateway). Path is left for the caller.
func DecideDefaults(file *rdpfile.File, p Policies) Defaults {
	// Without Default.rdp mstsc uses its own defaults: it warns when it
	// cannot verify a computer (and writes that into a new Default.rdp),
	// uses no gateway and defers to the administrator's gateway settings.
	d := Defaults{ServerAuth: rdpfile.ServerAuthWarn}
	g := rdpfile.Gateway{AdminDefaults: true}
	if file != nil {
		d.Found = true
		g = file.Gateway()
		d.ServerAuth = file.ServerAuth()
		d.AlwaysPrompt = file.AlwaysPrompt()
	}
	if p.ServerAuthSet {
		d.ServerAuth, d.ServerAuthByPolicy = p.ServerAuth, true
	}
	d.Gateway = DecideGateway(g, p.Gateway)
	return d
}
