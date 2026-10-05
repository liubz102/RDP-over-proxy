//go:build windows

package diag

import (
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/liubz102/RDP-over-proxy/internal/mstsc"
	"github.com/liubz102/RDP-over-proxy/internal/winx"
)

// Options are what Gather cannot find out by itself.
type Options struct {
	// App and Xray are the versions of the app and of Xray-core.
	App, Xray string
	// Logs is the app's log folder.
	Logs string
}

// Gather reads the facts from Windows. Whatever cannot be read stays
// unknown; reading never fails as a whole. Whether Credential Guard runs is
// left out (see Facts.CredentialGuard).
func Gather(o Options) Facts {
	f := Facts{App: o.App, Xray: o.Xray, Logs: o.Logs, LogsLocal: winx.OnLocalDisk(o.Logs), EncryptionOracle: -1}
	f.Home, _ = os.UserHomeDir()
	f.Windows = windowsVersion()
	if exe, err := mstsc.Path(); err == nil {
		f.Mstsc, _ = winx.FileVersion(exe)
		f.MstscAx, _ = winx.FileVersion(filepath.Join(filepath.Dir(exe), "mstscax.dll"))
	}
	f.WebView2 = winx.WebView2Version()
	f.Defaults, f.DefaultsErr = mstsc.ReadDefaults()
	f.Join = joinState()
	f.Delegation = readDelegation()
	f.EncryptionOracle = encryptionOracle()
	return f
}

func windowsVersion() Windows {
	v := windows.RtlGetVersion()
	w := Windows{Major: v.MajorVersion, Minor: v.MinorVersion, Build: v.BuildNumber}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return w
	}
	defer k.Close()
	if ubr, _, err := k.GetIntegerValue("UBR"); err == nil {
		w.UBR = uint32(ubr)
	}
	w.Product, _, _ = k.GetStringValue("ProductName")
	w.Edition, _, _ = k.GetStringValue("EditionID")
	w.Release, _, _ = k.GetStringValue("DisplayVersion")
	if w.Release == "" {
		w.Release, _, _ = k.GetStringValue("ReleaseId") // before 20H2
	}
	return w
}

// joinState tells whether the computer is in a domain. The domain's name is
// not kept: the report has no use for it.
func joinState() Join {
	var name *uint16
	var typ uint32
	if err := windows.NetGetJoinInformation(nil, &name, &typ); err != nil {
		return JoinUnknown
	}
	_ = windows.NetApiBufferFree((*byte)(unsafe.Pointer(name)))
	switch typ {
	case windows.NetSetupDomainName:
		return JoinDomain
	case windows.NetSetupWorkgroupName, windows.NetSetupUnjoined:
		return JoinWorkgroup
	}
	return JoinUnknown
}

// delegationKey holds the Credentials Delegation policies; each list of
// servers is a subkey of the same name with one value per server.
const delegationKey = `SOFTWARE\Policies\Microsoft\Windows\CredentialsDelegation`

func readDelegation() Delegation {
	var d Delegation
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, delegationKey, registry.QUERY_VALUE)
	if err != nil {
		return d
	}
	defer k.Close()
	d.AllowNTLMOnly = policyState(k, "AllowSavedCredentialsWhenNTLMOnly")
	if d.AllowNTLMOnly == PolicyEnabled {
		d.AllowServers = serverList("AllowSavedCredentialsWhenNTLMOnly")
		d.AddDefaults = policyState(k, "ConcatenateDefaults_AllowSavedNTLMOnly") == PolicyEnabled
	}
	d.Deny = policyState(k, "DenySavedCredentials")
	if d.Deny == PolicyEnabled {
		d.DenyServers = serverList("DenySavedCredentials")
	}
	return d
}

// policyState reads a policy's switch: 1 enabled, 0 disabled.
func policyState(k registry.Key, name string) PolicyState {
	v, _, err := k.GetIntegerValue(name)
	switch {
	case err != nil:
		return PolicyNotConfigured
	case v == 0:
		return PolicyDisabled
	default:
		return PolicyEnabled
	}
}

func serverList(name string) []string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, delegationKey+`\`+name, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	names, err := k.ReadValueNames(-1)
	if err != nil {
		return nil
	}
	var servers []string
	for _, n := range names {
		if s, _, err := k.GetStringValue(n); err == nil && s != "" {
			servers = append(servers, s)
		}
	}
	return servers
}

// encryptionOracle reads the CredSSP "Encryption Oracle Remediation"
// policy; -1 when it is not configured.
func encryptionOracle() int {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System\CredSSP\Parameters`, registry.QUERY_VALUE)
	if err != nil {
		return -1
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AllowEncryptionOracle")
	if err != nil {
		return -1
	}
	return int(v)
}
