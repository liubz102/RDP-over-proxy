package mstsc

import (
	"cmp"

	"github.com/liubz102/RDP-over-proxy/internal/rdpfile"
)

// Gateway is whether mstsc would hand a connection to an RD Gateway, and
// why. A gateway would try to reach the app's 127.x.y.z entrance from its
// own side of the network and fail.
type Gateway struct {
	Verdict rdpfile.GatewayVerdict
	// Server is the RD Gateway server, when known.
	Server string
	// ByPolicy: Group Policy decides, not Default.rdp.
	ByPolicy bool
}

// GatewayPolicy is the user's RD Gateway Group Policy (User Configuration >
// Administrative Templates > Windows Components > Remote Desktop Services >
// RD Gateway).
type GatewayPolicy struct {
	// Enabled: "Enable connection through RD Gateway" is on. mstsc then
	// uses the gateway when it cannot connect to the computer directly.
	Enabled bool
	// Enforced: "Allow users to change this setting" is off, so the user's
	// own settings cannot turn the gateway off.
	Enforced bool
	// Server is "Set RD Gateway server address".
	Server string
}

// DecideGateway combines Default.rdp's settings with the policy.
//
// An enabled policy applies when it is enforced, or when Default.rdp defers
// to the administrator's settings (gatewayprofileusagemethod 0, the value
// mstsc writes into a new Default.rdp). The policy only sends connections
// to the gateway when a direct connection fails; mstsc reaches the tunnel
// directly, so the gateway is at most a fallback: "maybe used". Otherwise
// Default.rdp's own setting decides (see rdpfile.Gateway.Verdict).
func DecideGateway(file rdpfile.Gateway, policy GatewayPolicy) Gateway {
	if policy.Enabled && (policy.Enforced || file.AdminDefaults) {
		return Gateway{Verdict: rdpfile.GatewayMaybeUsed, Server: policy.Server, ByPolicy: true}
	}
	return Gateway{Verdict: file.Verdict(), Server: cmp.Or(file.Host, policy.Server)}
}
