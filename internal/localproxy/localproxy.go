// Package localproxy finds proxies already running on this computer, such as
// the local port of v2rayN or Clash, so that a new user can add one with a
// click.
//
// Gather reads the facts from Windows: the listening TCP sockets, the
// processes and the proxy setting. Candidates, which makes no system calls,
// picks the ports that may be proxies, and Probe asks one whether it speaks
// SOCKS5.
package localproxy

import (
	"cmp"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Facts are what Candidates works from.
type Facts struct {
	Listeners []Listener
	Processes []Process
	// SystemProxy is the proxy server in the user's Internet settings while
	// it is on, as written ("127.0.0.1:10809", "http=127.0.0.1:7890;…");
	// empty while it is off.
	SystemProxy string
}

// Listener is a listening TCP socket and the process that owns it.
type Listener struct {
	Addr netip.AddrPort
	PID  uint32
}

// Process is a running program: its ID, the ID of the process that started
// it, the file name of its executable ("xray.exe"), and when it started
// (zero when not known).
type Process struct {
	PID       uint32
	ParentPID uint32
	Exe       string
	Created   time.Time
}

// Where a candidate was found.
const (
	// SourceProgram: a known proxy program listens on the port. Programs
	// have other ports too (an HTTP proxy, a control API); Probe tells
	// which is a SOCKS5 proxy.
	SourceProgram = "program"
	// SourceSystem: the user's Internet settings have the port as their
	// proxy server, which makes it an HTTP proxy.
	SourceSystem = "system"
)

// Candidate is a port on this computer that may be a proxy.
type Candidate struct {
	// Name is how people know the program: a known app ("v2rayN") that
	// listens itself or started the program that listens, else the
	// listening program's file name without ".exe" ("xray"); empty when
	// the program is not known.
	Name string `json:"name"`
	// Hosts are the addresses that reach the port, the one to prefer
	// first. A socket listening on every IPv6 address takes IPv4
	// connections too unless it was made IPv6-only, which the socket table
	// does not tell, so both loopback addresses are listed for it.
	Hosts  []string `json:"hosts"`
	Port   int      `json:"port"`
	Source string   `json:"source"`
}

// apps are proxy programs that people know by name, by their lower-case
// file name without ".exe". Most run a proxy core (cores) that does the
// listening; its ports are named after the app.
var apps = map[string]string{
	"v2rayn":            "v2rayN",
	"v2raya":            "v2rayA",
	"clash for windows": "Clash for Windows",
	"clash verge":       "Clash Verge",
	"clash-verge":       "Clash Verge",
	"clash nyanpasu":    "Clash Nyanpasu",
	"clash-nyanpasu":    "Clash Nyanpasu",
	"mihomo party":      "Mihomo Party",
	"flclash":           "FlClash",
	"nekoray":           "NekoRay",
	"nekobox":           "NekoBox",
	"throne":            "Throne",
	"hiddify":           "Hiddify",
	"qv2ray":            "Qv2ray",
	"shadowsocks":       "Shadowsocks",
	"karing":            "Karing",
	"gui.for.clash":     "GUI.for.Clash",
	"gui.for.singbox":   "GUI.for.SingBox",
}

// cores are how the lower-case file names of proxy programs begin: the
// cores that apps run, which people also run on their own. "clash" takes
// in clash-win64.exe and Clash.Meta-windows-amd64.exe; "wv2ray" is the
// windowless v2ray that v2rayN 3 runs.
var cores = []string{
	"xray", "wxray", "v2ray", "wv2ray", "sing-box", "mihomo", "verge-mihomo", "clash",
	"hysteria", "tuic", "naive", "trojan", "ss-local", "sslocal", "brook", "gost",
	"nekobox_core", "nekoray_core", "hiddifycli", "flclashcore",
}

// trimExe is a file name without its ".exe" ending, however it is written.
func trimExe(exe string) string {
	if n := len(exe) - len(".exe"); n >= 0 && strings.EqualFold(exe[n:], ".exe") {
		return exe[:n]
	}
	return exe
}

// isProxyProgram reports whether a program with this file name is a proxy
// program.
func isProxyProgram(exe string) bool {
	s := strings.ToLower(trimExe(exe))
	return apps[s] != "" || slices.ContainsFunc(cores, func(c string) bool { return strings.HasPrefix(s, c) })
}

// hosts are the loopback addresses that reach a socket listening at addr,
// none when it listens on another address only.
func hosts(addr netip.Addr) []string {
	addr = addr.Unmap()
	switch {
	case addr == netip.IPv4Unspecified():
		return []string{"127.0.0.1"}
	case addr.Is4() && addr.IsLoopback():
		return []string{addr.String()}
	case addr == netip.IPv6Unspecified():
		return []string{"127.0.0.1", "::1"}
	case addr == netip.IPv6Loopback():
		return []string{"::1"}
	}
	return nil
}

// Candidates picks the ports that may be proxies: each port of a known proxy
// program that a loopback address reaches, and the proxy server in the
// Internet settings when it is on this computer and a program other than a
// known proxy program listens there (a known one's ports are candidates
// already). Programs come first, then by name and port.
func Candidates(f Facts) []Candidate {
	procs := make(map[uint32]Process, len(f.Processes))
	for _, p := range f.Processes {
		procs[p.PID] = p
	}
	type key struct {
		pid  uint32
		port uint16
	}
	found := map[key]int{} // index in out
	var out []Candidate
	for _, l := range f.Listeners {
		p, ok := procs[l.PID]
		hs := hosts(l.Addr.Addr())
		if !ok || len(hs) == 0 || !isProxyProgram(p.Exe) {
			continue
		}
		k := key{l.PID, l.Addr.Port()}
		i, ok := found[k]
		if !ok {
			i = len(out)
			found[k] = i
			out = append(out, Candidate{Name: name(p, procs), Port: int(l.Addr.Port()), Source: SourceProgram})
		}
		for _, h := range hs {
			if !slices.Contains(out[i].Hosts, h) {
				out[i].Hosts = append(out[i].Hosts, h)
			}
		}
	}
	if c, ok := system(f, procs); ok {
		out = append(out, c)
	}
	for i := range out {
		// IPv4 first: what people expect to see, and certain to work
		// whenever an IPv4 socket listens.
		slices.SortStableFunc(out[i].Hosts, func(a, b string) int { return cmp.Compare(isIPv6(a), isIPv6(b)) })
	}
	slices.SortStableFunc(out, func(a, b Candidate) int {
		return cmp.Or(
			cmp.Compare(sourceRank[a.Source], sourceRank[b.Source]),
			cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
			cmp.Compare(a.Port, b.Port),
		)
	})
	return out
}

var sourceRank = map[string]int{SourceProgram: 0, SourceSystem: 1}

func isIPv6(host string) int {
	if strings.Contains(host, ":") {
		return 1
	}
	return 0
}

// name is how people know the program that p belongs to: the nearest known
// app among p and the processes that started it, else p's file name.
//
// A parent's ID may have been reused since it exited, by a process that
// started later (explorer.exe outlives the userinit.exe that started it,
// whose ID a program started at sign-in may get). So the chain goes only
// to a parent that started before its child, and stops where either start
// time is not known.
func name(p Process, procs map[uint32]Process) string {
	seen := map[uint32]bool{}
	for q := p; !seen[q.PID]; {
		seen[q.PID] = true
		if n := apps[strings.ToLower(trimExe(q.Exe))]; n != "" {
			return n
		}
		parent, ok := procs[q.ParentPID]
		if !ok || parent.Created.IsZero() || q.Created.IsZero() || parent.Created.After(q.Created) {
			break
		}
		q = parent
	}
	return trimExe(p.Exe)
}

// system is the candidate the Internet settings name: their HTTP proxy, if
// it is on this computer and a program other than a known proxy program
// listens there.
func system(f Facts, procs map[uint32]Process) (Candidate, bool) {
	host, port, ok := systemProxy(f.SystemProxy)
	if !ok {
		return Candidate{}, false
	}
	for _, l := range f.Listeners {
		if l.Addr.Port() != port || !slices.Contains(hosts(l.Addr.Addr()), host.String()) {
			continue
		}
		p, known := procs[l.PID]
		if known && isProxyProgram(p.Exe) {
			return Candidate{}, false
		}
		c := Candidate{Hosts: []string{host.String()}, Port: int(port), Source: SourceSystem}
		if known {
			c.Name = trimExe(p.Exe)
		}
		return c, true
	}
	// Nothing listens there: the program that set it is not running.
	return Candidate{}, false
}

// systemProxy reads the HTTP proxy in an Internet settings proxy server:
// the entry for http, else the one for every protocol, else the one for
// https. It is returned only when it is on this computer.
func systemProxy(setting string) (netip.Addr, uint16, bool) {
	entries := map[string]string{}
	for _, e := range strings.FieldsFunc(setting, func(r rune) bool { return r == ';' || unicode.IsSpace(r) }) {
		if scheme, value, ok := strings.Cut(e, "="); ok {
			entries[strings.ToLower(scheme)] = value
		} else {
			entries[""] = e
		}
	}
	for _, scheme := range []string{"http", "", "https"} {
		if v, ok := entries[scheme]; ok {
			return loopback(v)
		}
	}
	return netip.Addr{}, 0, false
}

// loopback parses "host:port" (or "http://host:port", as some programs
// write it) when host is a loopback address or localhost.
func loopback(v string) (netip.Addr, uint16, bool) {
	if _, rest, ok := strings.Cut(v, "://"); ok {
		v = rest
	}
	host, portText, err := net.SplitHostPort(strings.TrimSuffix(v, "/"))
	if err != nil {
		return netip.Addr{}, 0, false
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return netip.Addr{}, 0, false
	}
	if strings.EqualFold(host, "localhost") {
		return netip.AddrFrom4([4]byte{127, 0, 0, 1}), uint16(port), true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !addr.IsLoopback() {
		return netip.Addr{}, 0, false
	}
	return addr.Unmap(), uint16(port), true
}
