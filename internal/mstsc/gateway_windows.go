//go:build windows

package mstsc

import (
	"errors"
	"io/fs"
	"os"

	"golang.org/x/sys/windows/registry"

	"github.com/liubz102/RDP-over-proxy/internal/rdpfile"
)

// gatewayPolicyKey holds the RD Gateway policies. TerminalServer.admx
// defines them for users only, so only HKEY_CURRENT_USER is read.
const gatewayPolicyKey = `SOFTWARE\Policies\Microsoft\Windows NT\Terminal Services`

// CheckGateway tells whether mstsc, started with /v:, would hand the
// connection to an RD Gateway, going by the user's Default.rdp and the RD
// Gateway Group Policy (see DecideGateway).
func CheckGateway() (Gateway, error) {
	path, err := DefaultRDPPath()
	if err != nil {
		return Gateway{}, err
	}
	data, err := os.ReadFile(path)
	var file rdpfile.Gateway
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// No Default.rdp: mstsc's own defaults, which use no gateway and
		// defer to the administrator's settings.
		file = rdpfile.Gateway{AdminDefaults: true}
	case err != nil:
		return Gateway{}, err
	default:
		file = rdpfile.Parse(data).Gateway()
	}
	return DecideGateway(file, readGatewayPolicy()), nil
}

// readGatewayPolicy reads the policy values. Anything missing or unreadable
// counts as not configured.
func readGatewayPolicy() GatewayPolicy {
	k, err := registry.OpenKey(registry.CURRENT_USER, gatewayPolicyKey, registry.QUERY_VALUE)
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
