package loopback_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/loopback"

	"errors"
	"net/netip"
	"testing"
)

func TestValid(t *testing.T) {
	for _, s := range []string{"127.1.1.1", "127.254.254.254", "127.10.20.30"} {
		if !ValidString(s) {
			t.Errorf("ValidString(%q) = false", s)
		}
	}
	for _, s := range []string{
		"", "127.0.0.1", "127.0.1.1", "127.1.1.0", "127.1.1.255", "127.255.1.1", "126.1.1.1",
		"192.0.2.1", "::1", "::ffff:127.1.1.1", "127.1.1", "127.01.1.1", "localhost",
	} {
		if ValidString(s) {
			t.Errorf("ValidString(%q) = true", s)
		}
	}
}

func TestDeriveIsStableAndInRange(t *testing.T) {
	// Pinned so an accidental change to the derivation shows up here. Stored
	// profiles keep their address either way; only new profiles would move.
	if got, want := Derive("0123456789abcdef"), netip.MustParseAddr("127.76.204.123"); got != want {
		t.Fatalf("Derive = %s, want %s", got, want)
	}
	seen := map[netip.Addr]bool{}
	for _, id := range []string{"a", "b", "c", "0123456789abcdef", "fedcba9876543210", "direct"} {
		a := Derive(id)
		if !Valid(a) {
			t.Errorf("Derive(%q) = %s, outside the range", id, a)
		}
		if a != Derive(id) {
			t.Errorf("Derive(%q) is not deterministic", id)
		}
		seen[a] = true
	}
	if len(seen) != 6 {
		t.Errorf("six IDs derived only %d distinct addresses", len(seen))
	}
}

func TestAssignPrefersTheDerivedAddress(t *testing.T) {
	got, err := Assign("0123456789abcdef", func(netip.Addr) bool { return false })
	if err != nil || got != Derive("0123456789abcdef") {
		t.Fatalf("Assign = %s, %v; want the derived address", got, err)
	}
}

func TestAssignSkipsTakenAddresses(t *testing.T) {
	id := "0123456789abcdef"
	first := Derive(id) // 127.76.204.123
	taken := map[netip.Addr]bool{
		first:                                 true,
		netip.MustParseAddr("127.76.204.124"): true,
	}
	got, err := Assign(id, func(a netip.Addr) bool { return taken[a] })
	if err != nil {
		t.Fatal(err)
	}
	if want := netip.MustParseAddr("127.76.204.125"); got != want {
		t.Fatalf("Assign = %s, want %s", got, want)
	}
}

func TestAssignCarriesIntoTheNextOctet(t *testing.T) {
	// Find an ID whose derived address ends in .254, then take that address:
	// the next candidate is .1 in the following third octet, never .255 or .0.
	for _, id := range candidateIDs() {
		a := Derive(id).As4()
		if a[3] != 254 || a[2] == 254 {
			continue
		}
		got, err := Assign(id, func(x netip.Addr) bool { return x == Derive(id) })
		if err != nil {
			t.Fatal(err)
		}
		want := netip.AddrFrom4([4]byte{127, a[1], a[2] + 1, 1})
		if got != want {
			t.Fatalf("Assign(%q) = %s, want %s", id, got, want)
		}
		return
	}
	t.Skip("no candidate ID derived to an address ending in .254")
}

func TestTheRangeEnds(t *testing.T) {
	// With everything else taken, the search finds each end of the range.
	for _, end := range []string{"127.1.1.1", "127.254.254.254"} {
		want := netip.MustParseAddr(end)
		got, err := Assign("0123456789abcdef", func(a netip.Addr) bool { return a != want })
		if err != nil || got != want {
			t.Fatalf("Assign = %s, %v; want %s", got, err, want)
		}
	}
}

func TestAssignWrapsAroundAtTheEnd(t *testing.T) {
	// Everything from the derived address to the end of the range is taken.
	id := "0123456789abcdef"
	start := index(Derive(id))
	got, err := Assign(id, func(a netip.Addr) bool { return index(a) >= start })
	if err != nil {
		t.Fatal(err)
	}
	if got != netip.MustParseAddr("127.1.1.1") {
		t.Fatalf("Assign = %s, want the search to wrap to 127.1.1.1", got)
	}
}

func TestAssignFailsWhenEverythingIsTaken(t *testing.T) {
	_, err := Assign("x", func(netip.Addr) bool { return true })
	if !errors.Is(err, ErrExhausted) {
		t.Fatalf("Assign error = %v, want ErrExhausted", err)
	}
}

// index is an address's position in the range, which runs from 127.1.1.1
// to 127.254.254.254 with each of the last three octets from 1 to 254.
func index(a netip.Addr) int {
	const octetValues = 254
	b := a.As4()
	return (int(b[1])-1)*octetValues*octetValues + (int(b[2])-1)*octetValues + int(b[3]) - 1
}

func candidateIDs() []string {
	ids := make([]string, 0, 5000)
	for i := range 5000 {
		ids = append(ids, string(rune('a'+i%26))+string(rune('a'+i/26%26))+string(rune('a'+i/676)))
	}
	return ids
}
