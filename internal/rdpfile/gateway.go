package rdpfile

// GatewayUsage is the "gatewayusagemethod" property: when mstsc connects
// through an RD Gateway rather than to the computer directly.
type GatewayUsage int

// GatewayUsage values, as Microsoft documents them for .rdp files.
const (
	// UsageNone: don't use an RD Gateway. A file without the property
	// behaves the same.
	UsageNone GatewayUsage = 0
	// UsageAlways: always connect through the RD Gateway.
	UsageAlways GatewayUsage = 1
	// UsageIfDirectFails: use the RD Gateway when a direct connection can't
	// be made. Some clients read it as "use it, but bypass it for local
	// addresses" instead.
	UsageIfDirectFails GatewayUsage = 2
	// UsageDefault: use the default RD Gateway settings, which an
	// administrator may have set.
	UsageDefault GatewayUsage = 3
	// UsageNoneBypassLocal: don't use an RD Gateway (and bypass it for local
	// addresses). mstsc writes this into a new Default.rdp.
	UsageNoneBypassLocal GatewayUsage = 4
)

// Gateway is a file's RD Gateway settings.
type Gateway struct {
	// Host is "gatewayhostname", the RD Gateway server.
	Host  string
	Usage GatewayUsage
	// AdminDefaults is true unless "gatewayprofileusagemethod" is 1. The
	// file then defers to the RD Gateway settings an administrator sets
	// through Group Policy, if any; checking those is the caller's job.
	AdminDefaults bool
	// ProfileStated: the file has "gatewayprofileusagemethod". mstsc always
	// writes it; without it, a hand-written file leaves open whether Usage
	// applies.
	ProfileStated bool
}

// GatewayVerdict says whether mstsc would route a connection through an RD
// Gateway.
type GatewayVerdict int

const (
	// GatewayNotUsed: mstsc connects to the address directly.
	GatewayNotUsed GatewayVerdict = iota
	// GatewayMaybeUsed: the settings leave it to conditions the file cannot
	// show (default settings, or whether the direct attempt fails).
	GatewayMaybeUsed
	// GatewayUsed: mstsc hands every connection to the RD Gateway.
	GatewayUsed
)

// Gateway returns the file's RD Gateway settings.
func (f *File) Gateway() Gateway {
	g := Gateway{AdminDefaults: true}
	g.Host, _ = f.String(propGatewayHost)
	if n, ok := f.Int(propGatewayUsage); ok {
		g.Usage = GatewayUsage(n)
	}
	if n, ok := f.Int(propGatewayProfile); ok {
		g.ProfileStated = true
		g.AdminDefaults = n != 1
	}
	return g
}

// Verdict tells whether mstsc, using these settings, would send a connection
// to this app's tunnel through the RD Gateway. That would break it: the
// gateway would try to reach 127.x.y.z on its own side. Group Policy is not
// considered (see AdminDefaults).
//
// With the administrator's settings chosen ("Automatically detect RD Gateway
// server settings" in mstsc), Usage and Host are what the greyed-out "Use
// these RD Gateway server settings" would use, and mstsc ignores them; a
// Default.rdp often keeps "always" there from earlier. When the file does
// not say which settings apply, Usage may or may not count, so even
// "always" is only a maybe.
func (g Gateway) Verdict() GatewayVerdict {
	if g.AdminDefaults && g.ProfileStated {
		return GatewayNotUsed
	}
	if g.AdminDefaults && g.Usage == UsageAlways {
		return GatewayMaybeUsed
	}
	switch g.Usage {
	case UsageNone, UsageNoneBypassLocal:
		return GatewayNotUsed
	case UsageAlways:
		return GatewayUsed
	default:
		return GatewayMaybeUsed
	}
}
