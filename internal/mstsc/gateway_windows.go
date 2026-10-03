//go:build windows

package mstsc

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"golang.org/x/sys/windows/registry"

	"github.com/liubz102/RDP-over-proxy/internal/rdpfile"
)

// policyKey holds the Remote Desktop Connection policies. TerminalServer.admx
// puts the RD Gateway ones under HKEY_CURRENT_USER only, and "Configure
// server authentication for client" under HKEY_LOCAL_MACHINE only.
const policyKey = `SOFTWARE\Policies\Microsoft\Windows NT\Terminal Services`

// ReadDefaults reads the user's Default.rdp and the Group Policy settings
// that decide over it (see DecideDefaults).
//
// An error says why Default.rdp could not be read, without its path: the
// error goes into the log file and into diagnostics people copy, and a
// redirected Documents folder can name a file server or a company.
func ReadDefaults() (Defaults, error) {
	path, err := DefaultRDPPath()
	if err != nil {
		return Defaults{}, err
	}
	var file *rdpfile.File
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		var pe *fs.PathError
		if errors.As(err, &pe) {
			err = fmt.Errorf("read Default.rdp: %w", pe.Err)
		}
		return Defaults{Path: path}, err
	default:
		file = rdpfile.Parse(data)
	}
	d := DecideDefaults(file, Policies{Gateway: readGatewayPolicy()}.withServerAuth())
	d.Path = path
	return d, nil
}

// readGatewayPolicy reads the RD Gateway policy values. Anything missing or
// unreadable counts as not configured.
func readGatewayPolicy() GatewayPolicy {
	k, err := registry.OpenKey(registry.CURRENT_USER, policyKey, registry.QUERY_VALUE)
	if err != nil {
		return GatewayPolicy{}
	}
	defer k.Close()
	var p GatewayPolicy
	if v, _, err := k.GetIntegerValue("UseProxy"); err == nil && v == 1 {
		p.Enabled = true
		// "Allow users to change this setting" writes 1 when ticked; the
		// policy is enforced by default.
		allow, _, err := k.GetIntegerValue("AllowExplicitUseProxy")
		p.Enforced = err != nil || allow != 1
	}
	p.Server, _, _ = k.GetStringValue("ProxyName")
	return p
}

// withServerAuth adds "Configure server authentication for client", whose
// value AuthenticationLevel holds 0 (always connect), 1 (do not connect) or
// 2 (warn) while the policy is enabled.
func (p Policies) withServerAuth() Policies {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, policyKey, registry.QUERY_VALUE)
	if err != nil {
		return p
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AuthenticationLevel")
	if err == nil && v <= uint64(rdpfile.ServerAuthWarn) {
		p.ServerAuth, p.ServerAuthSet = rdpfile.ServerAuth(v), true
	}
	return p
}
