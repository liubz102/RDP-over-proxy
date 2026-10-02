package model

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// DefaultRDPPort is the port Remote Desktop listens on unless it was changed.
const DefaultRDPPort = 3389

// Target is the remote computer a connection reaches through its proxy.
type Target struct {
	// Host is a DNS name or an IP address, without brackets or a port.
	Host string `json:"host"`
	Port int    `json:"port"`
}

// String formats the target the way people write it: host:port, with an IPv6
// address in brackets.
func (t Target) String() string {
	return net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
}

// IsLoopback reports whether the target is this computer itself: localhost
// or a loopback address.
func (t Target) IsLoopback() bool {
	h := strings.ToLower(strings.TrimSuffix(t.Host, "."))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	a, err := netip.ParseAddr(h)
	return err == nil && a.Unmap().IsLoopback()
}

// ParseTarget reads an address the way people type it and .rdp files store
// it: "host", "host:port", "[IPv6]", "[IPv6]:port" or a bare IPv6 address.
// A missing port becomes defaultPort.
func ParseTarget(s string, defaultPort int) (Target, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Target{}, errors.New("the address is empty")
	}
	host, port, hasPort := s, "", false
	switch {
	case strings.HasPrefix(s, "["):
		end := strings.IndexByte(s, ']')
		if end < 0 {
			return Target{}, fmt.Errorf("address %q has no closing ']'", s)
		}
		host = s[1:end]
		if rest := s[end+1:]; rest != "" {
			p, ok := strings.CutPrefix(rest, ":")
			if !ok {
				return Target{}, fmt.Errorf("address %q has text after ']'", s)
			}
			port, hasPort = p, true
		}
		if a, err := netip.ParseAddr(host); err != nil || !a.Is6() {
			return Target{}, fmt.Errorf("address %q: only IPv6 addresses go in brackets", s)
		}
	case strings.Count(s, ":") == 1:
		host, port, _ = strings.Cut(s, ":")
		hasPort = true
	}
	// More than one colon without brackets is a bare IPv6 address.

	t := Target{Host: host, Port: defaultPort}
	if hasPort {
		p, ok := parsePort(port)
		if !ok {
			return Target{}, fmt.Errorf("address %q: the port must be a number from 1 to 65535", s)
		}
		t.Port = p
	}
	if !ValidHost(t.Host) {
		return Target{}, fmt.Errorf("address %q: %q is not a valid host name or IP address", s, t.Host)
	}
	return t, nil
}

// parsePort accepts only plain digits (strconv.Atoi would also take "+80").
func parsePort(s string) (int, bool) {
	if s == "" || len(s) > 5 {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, n >= 1 && n <= 65535
}

// ValidHost reports whether h is an IP address or a DNS host name.
//
// IP addresses must be usable as a destination: not unspecified, not
// multicast and without an IPv6 zone (a zone means nothing to a proxy).
// Names use ASCII letters, digits, '-' and '_' (internationalized names must
// be entered in their xn-- form), and the last label cannot be all digits, so
// a mistyped IPv4 address such as "10.0.0.256" is not taken for a name.
func ValidHost(h string) bool {
	if a, err := netip.ParseAddr(h); err == nil {
		return a.Zone() == "" && !a.IsUnspecified() && !a.IsMulticast()
	}
	name := strings.TrimSuffix(h, ".")
	if name == "" || len(name) > 253 {
		return false
	}
	labels := strings.Split(name, ".")
	for _, l := range labels {
		if !validLabel(l) {
			return false
		}
	}
	return !allDigits(labels[len(labels)-1])
}

func validLabel(l string) bool {
	if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
		return false
	}
	for i := 0; i < len(l); i++ {
		c := l[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
		if !ok {
			return false
		}
	}
	return true
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// normalizeHost trims spaces and the brackets people put around IPv6
// addresses.
func normalizeHost(h string) string {
	h = strings.TrimSpace(h)
	if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		h = h[1 : len(h)-1]
	}
	return h
}
