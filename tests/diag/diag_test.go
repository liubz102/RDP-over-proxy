package diag_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/diag"

	"errors"
	"io/fs"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/mstsc"
	"github.com/liubz102/RDP-over-proxy/internal/rdpfile"
)

// healthy is a computer on which nothing gets in the way.
func healthy() Facts {
	return Facts{
		App:  "0.1.0",
		Xray: "26.3.27",
		Windows: Windows{Major: 10, Build: 26100, UBR: 4061, Product: "Windows 10 Pro", Edition: "Professional",
			Release: "24H2"},
		Mstsc:    "10.0.26100.1",
		MstscAx:  "10.0.26100.4061",
		WebView2: "141.0.3537.71",
		Defaults: mstsc.Defaults{Path: `C:\Users\Alice\Documents\Default.rdp`, Found: true,
			ServerAuth: rdpfile.ServerAuthWarn},
		Join:             JoinWorkgroup,
		CredentialGuard:  RunningNo,
		EncryptionOracle: -1,
		Logs:             `C:\Users\Alice\OneDrive - Contoso\Tools\RDP-over-proxy\logs`,
		LogsLocal:        true,
		Home:             `C:\Users\Alice`,
	}
}

// troubled is a computer on which much gets in the way.
func troubled() Facts {
	f := healthy()
	f.Mstsc, f.MstscAx, f.WebView2 = "", "", ""
	f.Defaults.Gateway = mstsc.Gateway{Verdict: rdpfile.GatewayUsed, Server: "gw.example.com"}
	f.Defaults.ServerAuth = rdpfile.ServerAuthRefuse
	f.Defaults.AlwaysPrompt = true
	f.Join = JoinDomain
	f.CredentialGuard = RunningYes
	f.EncryptionOracle = 2
	f.Logs, f.LogsLocal = `\\files.example.com\tools\RDP-over-proxy\logs`, false
	return f
}

func find(t *testing.T, items []Item, key string) Item {
	t.Helper()
	for _, it := range items {
		if it.Key == key {
			return it
		}
	}
	t.Fatalf("no item %q in %+v", key, items)
	return Item{}
}

func TestBuildOnAHealthyComputer(t *testing.T) {
	items := Build(healthy())
	for _, it := range items {
		if it.Status == StatusWarn || it.Status == StatusError {
			t.Errorf("%s is %s on a healthy computer: %+v", it.Key, it.Status, it)
		}
	}
	if w := find(t, items, "windows"); w.Text != "Windows 11 Professional 24H2 (26100.4061)" {
		t.Errorf("windows = %q", w.Text)
	}
	// Updates patch mstscax.dll, which does mstsc's work; both are named.
	if m := find(t, items, "mstsc"); m.Text != "10.0.26100.1 (mstscax.dll 10.0.26100.4061)" {
		t.Errorf("mstsc = %q", m.Text)
	}
	// Paths name the Windows account; the profile folder is written as
	// %USERPROFILE%, and Default.rdp's folder, which may name more (a
	// company's OneDrive), stays out of copies.
	if d := find(t, items, "defaultRdp"); d.Value != "found" || d.Detail != `%USERPROFILE%\Documents\Default.rdp` || !d.Private {
		t.Errorf("defaultRdp = %+v", d)
	}
	// The log folder is wherever the user put the app, and its path may say
	// as much: shown, but not copied.
	if l := find(t, items, "logs"); l.Value != "local" || l.Detail != `%USERPROFILE%\OneDrive - Contoso\Tools\RDP-over-proxy\logs` || !l.Private {
		t.Errorf("logs = %+v", l)
	}
	if s := find(t, items, "savedCredentials"); s.Value != SavedAllowedDefault || s.Status != StatusOK {
		t.Errorf("savedCredentials = %+v", s)
	}

	var order []string
	for _, it := range items {
		if len(order) == 0 || order[len(order)-1] != it.Group {
			order = append(order, it.Group)
		}
	}
	if want := []string{GroupVersions, GroupDefaults, GroupCredentials, GroupFiles}; !slices.Equal(order, want) {
		t.Errorf("groups in the order %q, want %q", order, want)
	}
}

func TestBuildOnATroubledComputer(t *testing.T) {
	items := Build(troubled())
	want := map[string][2]string{ // key: status, value
		"mstsc":            {StatusError, "missing"},
		"webview2":         {StatusError, "missing"},
		"gateway":          {StatusError, "used"},
		"serverAuth":       {StatusWarn, "refuse"},
		"alwaysPrompt":     {StatusWarn, "yes"},
		"domain":           {StatusInfo, "domain"},
		"savedCredentials": {StatusWarn, SavedDeniedDefault},
		"credentialGuard":  {StatusInfo, "running"},
		"encryptionOracle": {StatusWarn, "vulnerable"},
		"logs":             {StatusInfo, "network"},
	}
	for key, w := range want {
		if it := find(t, items, key); it.Status != w[0] || it.Value != w[1] {
			t.Errorf("%s = %s %q, want %s %q", key, it.Status, it.Value, w[0], w[1])
		}
	}
	// The gateway's name is the user's network: shown, but not copied.
	if g := find(t, items, "gateway"); g.Detail != "gw.example.com" || !g.Private {
		t.Errorf("gateway = %+v", g)
	}
}

func TestGatewayVerdicts(t *testing.T) {
	cases := []struct {
		g      mstsc.Gateway
		status string
		value  string
		detail string
	}{
		// A server left in the greyed-out settings is not used, so not shown.
		{mstsc.Gateway{Verdict: rdpfile.GatewayNotUsed, Server: "old-gw.example.com"}, StatusOK, "none", ""},
		{mstsc.Gateway{Verdict: rdpfile.GatewayMaybeUsed, Server: "gw.example.com"}, StatusWarn, "maybe", "gw.example.com"},
		{mstsc.Gateway{Verdict: rdpfile.GatewayMaybeUsed, Server: "gw.example.com", ByPolicy: true}, StatusWarn, "maybePolicy", "gw.example.com"},
		{mstsc.Gateway{Verdict: rdpfile.GatewayUsed}, StatusError, "used", ""},
	}
	for _, c := range cases {
		f := healthy()
		f.Defaults.Gateway = c.g
		if it := find(t, Build(f), "gateway"); it.Status != c.status || it.Value != c.value || it.Detail != c.detail {
			t.Errorf("%+v: %+v", c.g, it)
		}
	}
}

func TestBuildWithUnreadableDefaults(t *testing.T) {
	f := healthy()
	// Documents redirected to a file server that is out of reach: the path
	// names the server and the account, and goes neither into Args, which
	// are copied, nor anywhere but the private detail.
	unc := `\\fs01.corp.example\home$\jdoe\Documents\Default.rdp`
	f.Defaults.Path = unc
	f.DefaultsErr = &fs.PathError{Op: "open", Path: unc, Err: syscall.ERROR_ACCESS_DENIED}
	items := Build(f)
	d := find(t, items, "defaultRdp")
	if d.Status != StatusWarn || d.Value != "unreadable" || d.Args["error"] != syscall.ERROR_ACCESS_DENIED.Error() ||
		d.Detail != unc || !d.Private {
		t.Errorf("defaultRdp = %+v", d)
	}
	// Other errors keep their text, without the profile folder.
	f.DefaultsErr = errors.New(`c:\users\alice\Documents: something odd`)
	if d := find(t, Build(f), "defaultRdp"); d.Args["error"] != `%USERPROFILE%\Documents: something odd` {
		t.Errorf("defaultRdp error = %q", d.Args["error"])
	}
	// What the file would have said is unknown, so it is not reported.
	for _, it := range items {
		if it.Key == "gateway" || it.Key == "serverAuth" || it.Key == "alwaysPrompt" {
			t.Errorf("%s reported although Default.rdp could not be read: %+v", it.Key, it)
		}
	}
}

func TestServerAuthByPolicy(t *testing.T) {
	cases := []struct {
		auth   rdpfile.ServerAuth
		status string
		value  string
	}{
		{rdpfile.ServerAuthConnect, StatusInfo, "connectPolicy"},
		{rdpfile.ServerAuthRefuse, StatusWarn, "refusePolicy"},
		{rdpfile.ServerAuthWarn, StatusOK, "warnPolicy"},
	}
	for _, c := range cases {
		f := healthy()
		f.Defaults.ServerAuth, f.Defaults.ServerAuthByPolicy = c.auth, true
		if it := find(t, Build(f), "serverAuth"); it.Status != c.status || it.Value != c.value {
			t.Errorf("%d by policy: %+v", c.auth, it)
		}
	}
}

func TestCredentialGuardStates(t *testing.T) {
	cases := map[Running][2]string{
		RunningYes:      {StatusInfo, "running"},
		RunningNo:       {StatusOK, "off"},
		RunningUnknown:  {StatusInfo, "unknown"},
		RunningChecking: {StatusInfo, "checking"},
	}
	for state, want := range cases {
		f := healthy()
		f.CredentialGuard = state
		if it := find(t, Build(f), "credentialGuard"); it.Status != want[0] || it.Value != want[1] {
			t.Errorf("%d: %+v", state, it)
		}
	}
}

func TestEveryItemCanBeTranslated(t *testing.T) {
	codes := Codes()
	variants := []Facts{healthy(), troubled()}
	missing := healthy()
	missing.Defaults = mstsc.Defaults{Path: missing.Defaults.Path, ServerAuth: rdpfile.ServerAuthUnspecified}
	missing.Join, missing.CredentialGuard, missing.EncryptionOracle = JoinUnknown, RunningChecking, 7
	unreadable := healthy()
	unreadable.DefaultsErr = errors.New("access denied")
	variants = append(variants, missing, unreadable)
	for _, auth := range []rdpfile.ServerAuth{rdpfile.ServerAuthConnect, rdpfile.ServerAuthUnspecified} {
		f := healthy()
		f.Defaults.ServerAuth = auth
		variants = append(variants, f)
	}
	for _, auth := range []rdpfile.ServerAuth{rdpfile.ServerAuthConnect, rdpfile.ServerAuthRefuse, rdpfile.ServerAuthWarn} {
		f := healthy()
		f.Defaults.ServerAuth, f.Defaults.ServerAuthByPolicy = auth, true
		variants = append(variants, f)
	}
	for _, oracle := range []int{0, 1} {
		f := healthy()
		f.EncryptionOracle = oracle
		f.CredentialGuard = RunningUnknown
		variants = append(variants, f)
	}
	for _, byPolicy := range []bool{false, true} {
		f := healthy()
		f.Defaults.Gateway = mstsc.Gateway{Verdict: rdpfile.GatewayMaybeUsed, ByPolicy: byPolicy}
		variants = append(variants, f)
	}
	for _, d := range []Delegation{
		{AllowNTLMOnly: PolicyEnabled, AllowServers: []string{"TERMSRV/*"}},
		{AllowNTLMOnly: PolicyEnabled, AllowServers: []string{"TERMSRV/pc.example.com"}},
		{AllowNTLMOnly: PolicyDisabled},
		{Deny: PolicyEnabled, DenyServers: []string{"*"}},
	} {
		f := healthy()
		f.Delegation = d
		variants = append(variants, f)
	}

	seen := map[string]bool{}
	for _, f := range variants {
		for _, it := range Build(f) {
			need := []string{"groups." + it.Group, "items." + it.Key + ".label"}
			switch {
			case it.Value != "":
				need = append(need, "items."+it.Key+"."+it.Value)
			case it.Text == "":
				t.Errorf("%s says nothing: %+v", it.Key, it)
			}
			if it.Detail != "" {
				need = append(need, "items."+it.Key+".detail")
			}
			for _, code := range need {
				seen[code] = true
				if !slices.Contains(codes, code) {
					t.Errorf("%s uses %q, which Codes does not list", it.Key, code)
				}
			}
		}
	}
	// And every code listed can come up, so the catalogs hold nothing dead.
	for _, code := range codes {
		if !seen[code] {
			t.Errorf("Codes lists %q, which no report used", code)
		}
	}
}

func TestSavedCredentials(t *testing.T) {
	hosts := []string{"TERMSRV/*.example.com", "TERMSRV/pc.example.com"}
	enabled := func(servers []string, addDefaults bool) Delegation {
		return Delegation{AllowNTLMOnly: PolicyEnabled, AllowServers: servers, AddDefaults: addDefaults}
	}
	cases := []struct {
		name   string
		d      Delegation
		join   Join
		want   string
		listed []string
	}{
		{"no policy, workgroup", Delegation{}, JoinWorkgroup, SavedAllowedDefault, nil},
		{"no policy, domain", Delegation{}, JoinDomain, SavedDeniedDefault, nil},
		{"no policy, membership unknown", Delegation{}, JoinUnknown, SavedUnknown, nil},
		{"allows Remote Desktop anywhere", enabled([]string{"TERMSRV/*"}, false), JoinDomain, SavedAllowedPolicy, nil},
		{"service names ignore case", enabled([]string{" termsrv/* "}, false), JoinDomain, SavedAllowedPolicy, nil},
		{"allows any service anywhere", enabled([]string{"*/*"}, false), JoinUnknown, SavedAllowedPolicy, nil},
		{"names hosts only", enabled(hosts, false), JoinWorkgroup, SavedLimited, hosts},
		{"names another service", enabled([]string{"HTTP/*"}, false), JoinWorkgroup, SavedLimited, []string{"HTTP/*"}},
		{"names hosts, adds a workgroup's defaults", enabled(hosts, true), JoinWorkgroup, SavedAllowedPolicy, nil},
		{"names hosts, adds a domain member's defaults (none)", enabled(hosts, true), JoinDomain, SavedLimited, hosts},
		{"names hosts, adds defaults of an unknown kind", enabled(hosts, true), JoinUnknown, SavedUnknown, nil},
		{"disabled", Delegation{AllowNTLMOnly: PolicyDisabled}, JoinWorkgroup, SavedDeniedPolicy, nil},
		{"deny wins", Delegation{AllowNTLMOnly: PolicyEnabled, AllowServers: []string{"TERMSRV/*"},
			Deny: PolicyEnabled, DenyServers: []string{"TERMSRV/*"}}, JoinWorkgroup, SavedDeniedByDeny, nil},
		{"denies some hosts", Delegation{Deny: PolicyEnabled, DenyServers: hosts}, JoinWorkgroup, SavedAllowedDefault, nil},
		{"deny not enabled", Delegation{Deny: PolicyDisabled, DenyServers: []string{"*"}}, JoinWorkgroup, SavedAllowedDefault, nil},
	}
	for _, c := range cases {
		got, listed := c.d.SavedCredentials(c.join)
		if got != c.want || !slices.Equal(listed, c.listed) {
			t.Errorf("%s: %s %q, want %s %q", c.name, got, listed, c.want, c.listed)
		}
	}
	// The servers a limiting policy names are shown, but not copied; an
	// unknown verdict is no warning.
	f := healthy()
	f.Delegation = enabled(hosts, false)
	if it := find(t, Build(f), "savedCredentials"); it.Detail != strings.Join(hosts, ", ") || !it.Private || it.Status != StatusWarn {
		t.Errorf("savedCredentials = %+v", it)
	}
	f = healthy()
	f.Join = JoinUnknown
	if it := find(t, Build(f), "savedCredentials"); it.Value != SavedUnknown || it.Status != StatusInfo {
		t.Errorf("savedCredentials with the membership unknown = %+v", it)
	}
}

func TestWindowsNames(t *testing.T) {
	cases := map[Windows]string{
		{Major: 10, Build: 19045, UBR: 5011, Product: "Windows 10 Pro", Edition: "Professional", Release: "22H2"}: "Windows 10 Professional 22H2 (19045.5011)",
		{Major: 10, Build: 22631, UBR: 1, Product: "Windows 10 Home", Edition: "Core", Release: "23H2"}:           "Windows 11 Core 23H2 (22631.1)",
		{Major: 10, Build: 20348, UBR: 2, Product: "Windows Server 2022 Standard", Edition: "ServerStandard"}:     "Windows Server 2022 Standard (20348.2)",
		{Major: 10, Build: 26100}: "Windows 11 (26100.0)",
		{Major: 6, Minor: 3, Build: 9600, UBR: 3, Edition: "Professional"}: "Windows 6.3 Professional (9600.3)",
	}
	for w, want := range cases {
		if got := w.String(); got != want {
			t.Errorf("%+v: %q, want %q", w, got, want)
		}
	}
}
