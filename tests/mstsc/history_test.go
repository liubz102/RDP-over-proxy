package mstsc_test

import (
	"bytes"
	"encoding/binary"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/liubz102/RDP-over-proxy/internal/mstsc"
)

// ours stands in for the setting's rule: tunnel entrances are 127.x.
func ours(entry string) bool { return strings.HasPrefix(entry, "127.") }

// ten returns real entries a, b, c… in the order of a full list.
func ten(n int) []string {
	var list []string
	for i := range n {
		list = append(list, string(rune('a'+i))+".example.com")
	}
	return list
}

func TestPrune(t *testing.T) {
	full := ten(10)
	cases := []struct {
		name        string
		list, known []string
		kept        []string
		dropped     []string
		restored    int
	}{
		{"nothing to do", []string{"a", "b"}, nil, []string{"a", "b"}, nil, 0},
		{"empty", nil, nil, nil, nil, 0},
		{"drops ours, keeps the order",
			[]string{"127.1.2.3:13389", "a", "127.4.5.6", "b"}, nil,
			[]string{"a", "b"}, []string{"127.1.2.3:13389", "127.4.5.6"}, 0},
		{"drops repeats, whatever the case",
			[]string{"a", "B", "b", "A"}, nil, []string{"a", "B"}, nil, 0},
		// mstsc put an entrance first and pushed the tenth computer out.
		{"puts back what ours pushed out",
			append([]string{"127.1.2.3:13389"}, full[:9]...), full,
			full, []string{"127.1.2.3:13389"}, 1},
		// Two entrances pushed two out; known's order decides.
		{"puts back in known's order",
			append([]string{"127.1.1.1", "127.2.2.2"}, full[:8]...), full,
			full, []string{"127.1.1.1", "127.2.2.2"}, 2},
		// The user connected to x.example.com meanwhile, which pushed j out,
		// then an entrance pushed i out: i comes back, j had no room anyway
		// with one dropped.
		{"one back for each dropped",
			append([]string{"127.1.2.3", "x.example.com"}, full[:8]...), full,
			append(append([]string{"x.example.com"}, full[:8]...), full[8]), []string{"127.1.2.3"}, 1},
		{"nothing back without a drop",
			full[:9], full, full[:9], nil, 0},
		{"never puts back ours or what is there",
			[]string{"127.9.9.9", "A.example.com"}, []string{"a.example.com", "127.1.1.1", "b.example.com"},
			[]string{"A.example.com", "b.example.com"}, []string{"127.9.9.9"}, 1},
		{"no more than were dropped",
			append([]string{"127.1.1.1"}, full[:9]...), append(full, "k.example.com", "l.example.com"),
			full, []string{"127.1.1.1"}, 1},
	}
	for _, c := range cases {
		kept, dropped, restored := mstsc.Prune(c.list, c.known, ours)
		if !slices.Equal(kept, c.kept) || !slices.Equal(dropped, c.dropped) || restored != c.restored {
			t.Errorf("%s: Prune = %q, %q, %d; want %q, %q, %d",
				c.name, kept, dropped, restored, c.kept, c.dropped, c.restored)
		}
		// Pruning what Prune left changes nothing.
		again, none, back := mstsc.Prune(kept, c.known, ours)
		if !slices.Equal(again, kept) || none != nil || back != 0 {
			t.Errorf("%s: pruning again = %q, %q, %d", c.name, again, none, back)
		}
	}
}

// rdp encodes text the way mstsc saves Default.rdp: UTF-16LE with a
// byte-order mark.
func rdp(text string) []byte {
	units := utf16.Encode([]rune(text))
	b := make([]byte, 2+2*len(units))
	b[0], b[1] = 0xFF, 0xFE
	for i, u := range units {
		binary.LittleEndian.PutUint16(b[2+2*i:], u)
	}
	return b
}

func TestClearAddress(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"full address",
			"screen mode id:i:2\r\nfull address:s:127.1.2.3:13389\r\nusername:s:用户\r\n",
			"screen mode id:i:2\r\nfull address:s:\r\nusername:s:用户\r\n"},
		{"both addresses, names in any case",
			"Full Address:S:127.1.2.3\r\nalternate full address:s: 127.4.5.6 \r\n",
			"Full Address:S:\r\nalternate full address:s:\r\n"},
		{"last line without a line break",
			"username:s:alice\r\nfull address:s:127.1.2.3",
			"username:s:alice\r\nfull address:s:"},
		{"LF only", "full address:s:127.1.2.3\nkeep:s:x\n", "full address:s:\nkeep:s:x\n"},
	}
	for _, c := range cases {
		got, changed := mstsc.ClearAddress(rdp(c.in), ours)
		if !changed || !bytes.Equal(got, rdp(c.want)) {
			t.Errorf("%s: ClearAddress = %q, %v; want %q", c.name, got, changed, c.want)
		}
	}

	unchanged := map[string][]byte{
		"another computer":  rdp("full address:s:pc.example.com:3390\r\n"),
		"no address":        rdp("full address:s:\r\n"),
		"a number property": rdp("full address:i:127\r\n"),
		"another property":  rdp("gatewayhostname:s:127.1.2.3\r\n"),
		"UTF-8":             []byte("full address:s:127.1.2.3\r\n"),
		"odd length":        append(rdp("full address:s:127.1.2.3\r\n"), 'x'),
		"big endian":        {0xFE, 0xFF, 0, 'f'},
		"empty":             {},
	}
	for name, in := range unchanged {
		got, changed := mstsc.ClearAddress(in, ours)
		if changed || !bytes.Equal(got, in) {
			t.Errorf("%s: ClearAddress = %q, %v; want it unchanged", name, got, changed)
		}
	}
}

// Bytes that are not valid UTF-16 elsewhere in the file stay as they were.
func TestClearAddressKeepsOtherBytes(t *testing.T) {
	in := rdp("full address:s:127.1.2.3\r\nodd:s:")
	in = append(in, 0x00, 0xD8) // a lone high surrogate
	in = append(in, rdp("\r\n")[2:]...)
	got, changed := mstsc.ClearAddress(in, ours)
	want := rdp("full address:s:\r\nodd:s:")
	want = append(want, 0x00, 0xD8)
	want = append(want, rdp("\r\n")[2:]...)
	if !changed || !bytes.Equal(got, want) {
		t.Fatalf("ClearAddress = % x, %v; want % x", got, changed, want)
	}
}
