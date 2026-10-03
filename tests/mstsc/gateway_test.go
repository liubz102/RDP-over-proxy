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

func TestDecideDefaults(t *testing.T) {
	// No Default.rdp: mstsc's built-in defaults, which warn when a computer
	// cannot be verified, under the administrator's gateway settings.
	none := DecideDefaults(nil, Policies{})
	if none.Found || none.ServerAuth != rdpfile.ServerAuthWarn || none.ServerAuthByPolicy || none.AlwaysPrompt ||
		none.Gateway != (Gateway{Verdict: rdpfile.GatewayNotUsed}) {
		t.Errorf("without Default.rdp: %+v", none)
	}
	gateway := GatewayPolicy{Enabled: true, Server: "policy-gw.example.com"}
	if d := DecideDefaults(nil, Policies{Gateway: gateway}); d.Gateway != (Gateway{Verdict: rdpfile.GatewayMaybeUsed, Server: "policy-gw.example.com", ByPolicy: true}) {
		t.Errorf("without Default.rdp, with a gateway policy: %+v", d.Gateway)
	}

	file := rdpfile.Parse([]byte("authentication level:i:1\r\nprompt for credentials:i:1\r\n" +
		"gatewayprofileusagemethod:i:1\r\ngatewayusagemethod:i:1\r\ngatewayhostname:s:gw.example.com\r\n"))
	d := DecideDefaults(file, Policies{})
	want := Defaults{Found: true, ServerAuth: rdpfile.ServerAuthRefuse, AlwaysPrompt: true,
		Gateway: Gateway{Verdict: rdpfile.GatewayUsed, Server: "gw.example.com"}}
	if d != want {
		t.Errorf("DecideDefaults = %+v, want %+v", d, want)
	}

	// "Configure server authentication for client" decides over the file,
	// either way.
	warn := Policies{ServerAuth: rdpfile.ServerAuthWarn, ServerAuthSet: true}
	if d := DecideDefaults(file, warn); d.ServerAuth != rdpfile.ServerAuthWarn || !d.ServerAuthByPolicy {
		t.Errorf("a policy that warns, over a file that refuses: %+v", d)
	}
	refuse := Policies{ServerAuth: rdpfile.ServerAuthRefuse, ServerAuthSet: true}
	if d := DecideDefaults(nil, refuse); d.ServerAuth != rdpfile.ServerAuthRefuse || !d.ServerAuthByPolicy {
		t.Errorf("a policy that refuses, without Default.rdp: %+v", d)
	}
}
