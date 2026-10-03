// Package diag reports what on this computer affects the app's Remote
// Desktop connections: the versions involved, the settings mstsc takes from
// Default.rdp for every connection, and the policies that decide whether a
// saved password is used. It only reads; it never changes a setting.
//
// Gather collects the facts from Windows. Build, which makes no system
// calls, turns them into the report the UI shows.
package diag

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/liubz102/RDP-over-proxy/internal/mstsc"
	"github.com/liubz102/RDP-over-proxy/internal/rdpfile"
)

// Statuses: how an item bears on connecting.
const (
	StatusOK    = "ok"    // as it should be
	StatusInfo  = "info"  // worth knowing, no trouble
	StatusWarn  = "warn"  // may get in the way
	StatusError = "error" // gets in the way
)

// Groups, in the order the report lists them.
const (
	GroupVersions    = "versions"
	GroupDefaults    = "defaults"
	GroupCredentials = "credentials"
	GroupFiles       = "files"
)

var groups = []string{GroupVersions, GroupDefaults, GroupCredentials, GroupFiles}

// Item is one line of the report.
type Item struct {
	Group  string `json:"group"`
	Key    string `json:"key"`
	Status string `json:"status"`
	// Value is translated ("diag.items.<key>.<value>") with Args filled in;
	// empty when Text says it all.
	Value string            `json:"value,omitempty"`
	Args  map[string]string `json:"args,omitempty"`
	// Text is shown as it is: a version, a path.
	Text string `json:"text,omitempty"`
	// Detail is shown as it is, under "diag.items.<key>.detail": the RD
	// Gateway server, the servers a policy lists.
	Detail string `json:"detail,omitempty"`
	// Private: Detail names something of the user's, such as a server on
	// their network. Copies of the report, meant for bug reports, leave it
	// out.
	Private bool `json:"private,omitempty"`
}

// Item keys.
const (
	keyApp              = "app"
	keyXray             = "xray"
	keyWindows          = "windows"
	keyMstsc            = "mstsc"
	keyWebView2         = "webview2"
	keyDefaultRDP       = "defaultRdp"
	keyGateway          = "gateway"
	keyServerAuth       = "serverAuth"
	keyAlwaysPrompt     = "alwaysPrompt"
	keyDomain           = "domain"
	keySavedCredentials = "savedCredentials"
	keyCredentialGuard  = "credentialGuard"
	keyEncryptionOracle = "encryptionOracle"
	keyLogs             = "logs"
)

// catalog lists every item with the values it takes and whether it has a
// detail line: what the UI has to translate (Codes).
var catalog = []struct {
	key    string
	values []string
	detail bool
}{
	{keyApp, nil, false},
	{keyXray, nil, false},
	{keyWindows, nil, false},
	{keyMstsc, []string{"missing"}, false},
	{keyWebView2, []string{"missing"}, false},
	{keyDefaultRDP, []string{"found", "missing", "unreadable"}, true},
	{keyGateway, []string{"none", "maybe", "maybePolicy", "used"}, true},
	{keyServerAuth, []string{"connect", "refuse", "warn", "unspecified", "connectPolicy", "refusePolicy", "warnPolicy"}, false},
	{keyAlwaysPrompt, []string{"no", "yes"}, false},
	{keyDomain, []string{"workgroup", "domain", "unknown"}, false},
	{keySavedCredentials, []string{SavedAllowedDefault, SavedAllowedPolicy, SavedDeniedDefault, SavedDeniedPolicy,
		SavedDeniedByDeny, SavedLimited, SavedUnknown}, true},
	{keyCredentialGuard, []string{"running", "off", "unknown", "checking"}, false},
	{keyEncryptionOracle, []string{"notConfigured", "forceUpdated", "mitigated", "vulnerable", "other"}, false},
	{keyLogs, nil, false},
}

// Codes lists the translation keys under "diag." that a report can use, for
// checking that the UI translates every one.
func Codes() []string {
	var out []string
	for _, g := range groups {
		out = append(out, "groups."+g)
	}
	for _, c := range catalog {
		out = append(out, "items."+c.key+".label")
		for _, v := range c.values {
			out = append(out, "items."+c.key+"."+v)
		}
		if c.detail {
			out = append(out, "items."+c.key+".detail")
		}
	}
	return out
}

// Windows is the Windows version.
type Windows struct {
	Major, Minor, Build, UBR uint32
	// Product is "ProductName", which says "Windows 10" on Windows 11 too;
	// only server names are taken from it.
	Product string
	// Edition is "EditionID", such as "Professional"; Release is
	// "DisplayVersion", such as "24H2".
	Edition, Release string
}

// String names the version the way winver does, in English, such as
// "Windows 11 Professional 24H2 (26100.4061)".
func (w Windows) String() string {
	var name string
	switch {
	case strings.HasPrefix(w.Product, "Windows Server"):
		name = w.Product
	case w.Major == 10 && w.Build >= 22000:
		// Windows 11 kept version 10.0; its builds start at 22000.
		name = "Windows 11 " + w.Edition
	case w.Major == 10:
		name = "Windows 10 " + w.Edition
	default:
		name = fmt.Sprintf("Windows %d.%d %s", w.Major, w.Minor, w.Edition)
	}
	name = strings.TrimSpace(name)
	if w.Release != "" {
		name += " " + w.Release
	}
	return fmt.Sprintf("%s (%d.%d)", name, w.Build, w.UBR)
}

// Join is whether the computer is a member of an Active Directory domain.
type Join int

const (
	JoinUnknown Join = iota
	// JoinWorkgroup: a workgroup, or not joined to anything (computers
	// joined to Microsoft Entra ID only are among them).
	JoinWorkgroup
	JoinDomain
)

// Running is whether a part of Windows runs.
type Running int

const (
	RunningUnknown Running = iota
	RunningNo
	RunningYes
	// RunningChecking: the answer has not come yet.
	RunningChecking
)

// Facts are what Gather finds out.
type Facts struct {
	// App and Xray are the versions of the app and of Xray-core.
	App, Xray string
	Windows   Windows
	// Mstsc and MstscAx are the file versions of mstsc.exe and of
	// mstscax.dll, which does its work and which updates patch; "" when
	// missing. WebView2 is the WebView2 Runtime's version.
	Mstsc, MstscAx, WebView2 string
	// Defaults are what Default.rdp and the RD Gateway policy decide;
	// DefaultsErr is set when they could not be read.
	Defaults    mstsc.Defaults
	DefaultsErr error
	Join        Join
	Delegation  Delegation
	// CredentialGuard is whether Credential Guard runs. Gather leaves it
	// unknown: asking WMI can take long, so the caller asks on its own
	// (winx.CredentialGuardRunning) and fills it in.
	CredentialGuard Running
	// EncryptionOracle is the CredSSP "Encryption Oracle Remediation"
	// policy (AllowEncryptionOracle): 0 force updated clients, 1 mitigated,
	// 2 vulnerable, -1 not configured.
	EncryptionOracle int
	// Logs is the app's log folder.
	Logs string
	// Home is the user's profile folder. Paths and messages show it as
	// %USERPROFILE%: it names the Windows account.
	Home string
}

// Build turns the facts into the report, in the order of the groups.
func Build(f Facts) []Item {
	items := []Item{
		{Group: GroupVersions, Key: keyApp, Status: StatusInfo, Text: f.App},
		{Group: GroupVersions, Key: keyXray, Status: StatusInfo, Text: f.Xray},
		{Group: GroupVersions, Key: keyWindows, Status: StatusInfo, Text: f.Windows.String()},
		version(keyMstsc, mstscVersion(f.Mstsc, f.MstscAx)),
		version(keyWebView2, f.WebView2),
	}
	items = append(items, defaults(f)...)
	items = append(items, credentials(f)...)
	return append(items, Item{Group: GroupFiles, Key: keyLogs, Status: StatusInfo, Text: f.unhome(f.Logs)})
}

func version(key, v string) Item {
	if v == "" {
		return Item{Group: GroupVersions, Key: key, Status: StatusError, Value: "missing"}
	}
	return Item{Group: GroupVersions, Key: key, Status: StatusInfo, Text: v}
}

// mstscVersion names both files when updates have left them apart.
func mstscVersion(exe, ax string) string {
	if exe == "" || ax == "" || ax == exe {
		return exe
	}
	return fmt.Sprintf("%s (mstscax.dll %s)", exe, ax)
}

func defaults(f Facts) []Item {
	d := f.Defaults
	file := Item{Group: GroupDefaults, Key: keyDefaultRDP, Detail: f.unhome(d.Path), Private: true}
	switch {
	case f.DefaultsErr != nil:
		file.Status, file.Value = StatusWarn, "unreadable"
		file.Args = map[string]string{"error": f.unhome(pathless(f.DefaultsErr))}
		return []Item{file}
	case d.Found:
		file.Status, file.Value = StatusOK, "found"
	default:
		file.Status, file.Value = StatusInfo, "missing"
	}
	return []Item{file, gateway(d.Gateway), serverAuth(d.ServerAuth, d.ServerAuthByPolicy), alwaysPrompt(d.AlwaysPrompt)}
}

// pathless is the text of err without the path a file error names: Args are
// copied, and a redirected Documents folder can name a file server or a
// company. Default.rdp's path is the item's private detail.
func pathless(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

func gateway(g mstsc.Gateway) Item {
	it := Item{Group: GroupDefaults, Key: keyGateway, Detail: g.Server, Private: true}
	switch {
	case g.Verdict == rdpfile.GatewayUsed:
		it.Status, it.Value = StatusError, "used"
	case g.Verdict == rdpfile.GatewayMaybeUsed && g.ByPolicy:
		it.Status, it.Value = StatusWarn, "maybePolicy"
	case g.Verdict == rdpfile.GatewayMaybeUsed:
		it.Status, it.Value = StatusWarn, "maybe"
	default:
		// A server left in the greyed-out settings is not used.
		it.Status, it.Value, it.Detail = StatusOK, "none", ""
	}
	return it
}

func serverAuth(a rdpfile.ServerAuth, byPolicy bool) Item {
	it := Item{Group: GroupDefaults, Key: keyServerAuth}
	switch a {
	case rdpfile.ServerAuthConnect:
		it.Status, it.Value = StatusInfo, "connect"
	case rdpfile.ServerAuthRefuse:
		it.Status, it.Value = StatusWarn, "refuse"
	case rdpfile.ServerAuthWarn:
		it.Status, it.Value = StatusOK, "warn"
	default:
		it.Status, it.Value = StatusOK, "unspecified"
	}
	if byPolicy && it.Value != "unspecified" {
		// Only an administrator changes it; the texts say so.
		it.Value += "Policy"
	}
	return it
}

func alwaysPrompt(on bool) Item {
	if on {
		return Item{Group: GroupDefaults, Key: keyAlwaysPrompt, Status: StatusWarn, Value: "yes"}
	}
	return Item{Group: GroupDefaults, Key: keyAlwaysPrompt, Status: StatusOK, Value: "no"}
}

func credentials(f Facts) []Item {
	domain := Item{Group: GroupCredentials, Key: keyDomain, Status: StatusInfo, Value: "unknown"}
	switch f.Join {
	case JoinDomain:
		domain.Value = "domain"
	case JoinWorkgroup:
		domain.Value = "workgroup"
	}

	verdict, listed := f.Delegation.SavedCredentials(f.Join)
	saved := Item{Group: GroupCredentials, Key: keySavedCredentials, Status: StatusWarn, Value: verdict,
		Detail: strings.Join(listed, ", "), Private: true}
	switch verdict {
	case SavedAllowedDefault, SavedAllowedPolicy:
		saved.Status = StatusOK
	case SavedUnknown:
		saved.Status = StatusInfo
	}

	guard := Item{Group: GroupCredentials, Key: keyCredentialGuard, Status: StatusInfo, Value: "unknown"}
	switch f.CredentialGuard {
	case RunningYes:
		guard.Value = "running"
	case RunningNo:
		guard.Status, guard.Value = StatusOK, "off"
	case RunningChecking:
		guard.Value = "checking"
	}

	oracle := Item{Group: GroupCredentials, Key: keyEncryptionOracle}
	switch f.EncryptionOracle {
	case -1:
		oracle.Status, oracle.Value = StatusOK, "notConfigured"
	case 0:
		oracle.Status, oracle.Value = StatusInfo, "forceUpdated"
	case 1:
		oracle.Status, oracle.Value = StatusOK, "mitigated"
	case 2:
		oracle.Status, oracle.Value = StatusWarn, "vulnerable"
	default:
		oracle.Status, oracle.Value = StatusInfo, "other"
		oracle.Args = map[string]string{"value": strconv.Itoa(f.EncryptionOracle)}
	}
	return []Item{domain, saved, guard, oracle}
}

// unhome writes the user's profile folder in s as %USERPROFILE%.
func (f Facts) unhome(s string) string {
	home := strings.TrimRight(f.Home, `\/`)
	if home == "" {
		return s
	}
	lower, lowerHome := strings.ToLower(s), strings.ToLower(home)
	if len(lower) != len(s) || len(lowerHome) != len(home) {
		return strings.ReplaceAll(s, home, "%USERPROFILE%") // a case change altered the length
	}
	var b strings.Builder
	for {
		i := strings.Index(lower, lowerHome)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		b.WriteString("%USERPROFILE%")
		s, lower = s[i+len(home):], lower[i+len(home):]
	}
}
