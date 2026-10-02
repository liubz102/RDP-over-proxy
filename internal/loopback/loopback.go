// Package loopback gives every connection profile its own tunnel entrance on
// the loopback network. Windows delivers all of 127.0.0.0/8 to this computer,
// so any 127.a.b.c can be listened on without setting anything up. mstsc
// remembers credentials and certificate trust per address; one fixed address
// per profile keeps those separate for each computer and stable over time.
//
// A profile's address is derived from its ID once, when the profile is
// created, and then stored with it. Changing the derivation would only affect
// profiles created afterwards.
package loopback

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net/netip"
)

// The last three octets each run from 1 to 254. Leaving out 0 and 255 avoids
// addresses that look like network or broadcast addresses to people and to
// some tools, and keeps 127.0.0.1 out of the range.
const (
	octetValues = 254
	poolSize    = octetValues * octetValues * octetValues // 16,387,064 addresses
)

// ErrExhausted is returned by Assign when every address in the range is taken.
var ErrExhausted = errors.New("every loopback address in 127.1.1.1-127.254.254.254 is taken")

// Valid reports whether a is a per-profile address: 127.a.b.c with a, b and
// c each from 1 to 254.
func Valid(a netip.Addr) bool {
	if !a.Is4() {
		return false
	}
	b := a.As4()
	return b[0] == 127 && inRange(b[1]) && inRange(b[2]) && inRange(b[3])
}

// ValidString is Valid for an address in text form. Only the canonical
// dotted-decimal form is accepted.
func ValidString(s string) bool {
	a, err := netip.ParseAddr(s)
	return err == nil && Valid(a) && a.String() == s
}

func inRange(b byte) bool { return b >= 1 && b <= octetValues }

// Derive returns the address the profile ID maps to before conflicts with
// other profiles are considered.
func Derive(id string) netip.Addr {
	return addrAt(indexOf(id))
}

// Assign returns the address for a new profile: the one its ID derives to,
// or, when taken reports that one as in use, the next free address after it
// (wrapping around at the end of the range). The search covers the whole
// range once, so it always ends; it fails only if every address is taken.
func Assign(id string, taken func(netip.Addr) bool) (netip.Addr, error) {
	start := indexOf(id)
	for i := range poolSize {
		a := addrAt((start + i) % poolSize)
		if !taken(a) {
			return a, nil
		}
	}
	return netip.Addr{}, ErrExhausted
}

// indexOf maps an ID to a position in the range with SHA-256, so IDs spread
// evenly and the same ID always gives the same position.
func indexOf(id string) int {
	sum := sha256.Sum256([]byte(id))
	return int(binary.BigEndian.Uint64(sum[:8]) % poolSize)
}

func addrAt(i int) netip.Addr {
	return netip.AddrFrom4([4]byte{
		127,
		byte(i/(octetValues*octetValues)) + 1,
		byte(i/octetValues%octetValues) + 1,
		byte(i%octetValues) + 1,
	})
}
