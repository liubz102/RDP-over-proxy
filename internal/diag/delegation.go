package diag

import (
	"slices"
	"strings"
)

// PolicyState is how a Group Policy setting is set.
type PolicyState int

const (
	PolicyNotConfigured PolicyState = iota
	PolicyEnabled
	PolicyDisabled
)

// Delegation is the Credentials Delegation policy on saved credentials
// (Computer Configuration > Administrative Templates > System > Credentials
// Delegation). CredSSP applies it when mstsc hands a saved password to the
// remote computer.
//
// Through the tunnel the remote computer is authenticated with NTLM only:
// mstsc connects to 127.x.y.z, a name Kerberos has no computer for. So
// "Allow delegating saved credentials with NTLM-only server authentication"
// is the setting that counts, and "Deny delegating saved credentials"
// overrides it.
type Delegation struct {
	// AllowNTLMOnly is "Allow delegating saved credentials with NTLM-only
	// server authentication", and AllowServers the servers it names, as
	// service principal names such as "TERMSRV/*". AddDefaults: "Concatenate
	// OS defaults with input above".
	AllowNTLMOnly PolicyState
	AllowServers  []string
	AddDefaults   bool
	// Deny is "Deny delegating saved credentials", and DenyServers the
	// servers it names.
	Deny        PolicyState
	DenyServers []string
}

// What SavedCredentials finds ("diag.items.savedCredentials.<verdict>").
const (
	// SavedAllowedDefault: no policy, and the computer is in no domain;
	// Windows then allows Remote Desktop on any computer (TERMSRV/*).
	SavedAllowedDefault = "allowedDefault"
	// SavedAllowedPolicy: the policy allows Remote Desktop on any computer.
	SavedAllowedPolicy = "allowedPolicy"
	// SavedDeniedDefault: no policy, and the computer is in a domain;
	// Windows then allows no computer at all.
	SavedDeniedDefault = "deniedDefault"
	// SavedDeniedPolicy: the policy is disabled, which allows no computer.
	SavedDeniedPolicy = "deniedPolicy"
	// SavedDeniedByDeny: "Deny delegating saved credentials" covers every
	// computer.
	SavedDeniedByDeny = "deniedByDeny"
	// SavedLimited: the policy names some servers only; the tunnel
	// entrances are hardly among them.
	SavedLimited = "limited"
	// SavedUnknown: no policy decides, and whether the computer is in a
	// domain is unknown.
	SavedUnknown = "unknown"
)

// SavedCredentials tells whether CredSSP lets mstsc use saved passwords for
// the app's tunnel entrances (TERMSRV/127.x.y.z) on a computer with the
// given domain membership, and, when the policy names some servers only,
// which.
func (d Delegation) SavedCredentials(join Join) (verdict string, listed []string) {
	if d.Deny == PolicyEnabled && slices.ContainsFunc(d.DenyServers, coversEntrances) {
		return SavedDeniedByDeny, nil
	}
	switch d.AllowNTLMOnly {
	case PolicyDisabled:
		return SavedDeniedPolicy, nil
	case PolicyEnabled:
		if slices.ContainsFunc(d.AllowServers, coversEntrances) {
			return SavedAllowedPolicy, nil
		}
		if !d.AddDefaults {
			return SavedLimited, d.AllowServers
		}
	}
	// The OS defaults decide, alone or added to the policy's list: Remote
	// Desktop on any computer (TERMSRV/*) for a computer in no domain,
	// nothing for one in a domain.
	enabled := d.AllowNTLMOnly == PolicyEnabled
	switch {
	case join == JoinUnknown:
		return SavedUnknown, nil
	case join == JoinWorkgroup && enabled:
		return SavedAllowedPolicy, nil
	case join == JoinWorkgroup:
		return SavedAllowedDefault, nil
	case enabled:
		return SavedLimited, d.AllowServers
	default:
		return SavedDeniedDefault, nil
	}
}

// coversEntrances reports whether a server named in the policy, a service
// principal name such as "TERMSRV/*", covers every tunnel entrance: Remote
// Desktop ("TERMSRV", or any service) on any computer. A name allows one
// wildcard, so one that names hosts ("TERMSRV/*.example.com") never covers
// them all.
func coversEntrances(spn string) bool {
	service, host, found := strings.Cut(strings.TrimSpace(spn), "/")
	if !found {
		return service == "*"
	}
	return (service == "*" || strings.EqualFold(service, "TERMSRV")) && host == "*"
}
