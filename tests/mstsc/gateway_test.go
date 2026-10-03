package mstsc_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/mstsc"

	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/rdpfile"
)

func TestDecideGateway(t *testing.T) {
	explicit := func(usage rdpfile.GatewayUsage) rdpfile.Gateway {
		return rdpfile.Gateway{Host: "gw.example.com", Usage: usage, ProfileStated: true}
	}
	adminDefaults := rdpfile.Gateway{Usage: rdpfile.UsageNoneBypassLocal, AdminDefaults: true, ProfileStated: true} // a new Default.rdp
	// "Automatically detect", with "always" left over from earlier settings.
	autoDetect := rdpfile.Gateway{Host: "gw.example.com", Usage: rdpfile.UsageAlways, AdminDefaults: true, ProfileStated: true}
	none := GatewayPolicy{}
	enforced := GatewayPolicy{Enabled: true, Enforced: true, Server: "policy-gw.example.com"}
	optional := GatewayPolicy{Enabled: true, Server: "policy-gw.example.com"}

	cases := []struct {
		name   string
		file   rdpfile.Gateway
		policy GatewayPolicy
		want   Gateway
	}{
		{"fresh Default.rdp, no policy", adminDefaults, none, Gateway{Verdict: rdpfile.GatewayNotUsed}},
		{"automatically detect, no policy", autoDetect, none,
			Gateway{Verdict: rdpfile.GatewayNotUsed, Server: "gw.example.com"}},
		{"automatically detect, with a policy", autoDetect, optional,
			Gateway{Verdict: rdpfile.GatewayMaybeUsed, Server: "policy-gw.example.com", ByPolicy: true}},
		{"always use the gateway", explicit(rdpfile.UsageAlways), none,
			Gateway{Verdict: rdpfile.GatewayUsed, Server: "gw.example.com"}},
		{"gateway if direct fails", explicit(rdpfile.UsageIfDirectFails), none,
			Gateway{Verdict: rdpfile.GatewayMaybeUsed, Server: "gw.example.com"}},
		{"explicitly none", explicit(rdpfile.UsageNone), none, Gateway{Verdict: rdpfile.GatewayNotUsed, Server: "gw.example.com"}},
		{"enforced policy beats the user's none", explicit(rdpfile.UsageNone), enforced,
			Gateway{Verdict: rdpfile.GatewayMaybeUsed, Server: "policy-gw.example.com", ByPolicy: true}},
		{"policy the user may override, overridden", explicit(rdpfile.UsageNone), optional,
			Gateway{Verdict: rdpfile.GatewayNotUsed, Server: "gw.example.com"}},
		{"policy the user may override, as default", adminDefaults, optional,
			Gateway{Verdict: rdpfile.GatewayMaybeUsed, Server: "policy-gw.example.com", ByPolicy: true}},
	}
	for _, c := range cases {
		if got := DecideGateway(c.file, c.policy); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}
