package probe_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/probe"

	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestConnectionRequestBytes(t *testing.T) {
	// TPKT (19 bytes) | X.224 CR, LI 14 | RDP_NEG_REQ asking for SSL|HYBRID|HYBRID_EX.
	want := mustHex(t, "03 00 00 13  0e e0 00 00 00 00 00  01 00 08 00 0b 00 00 00")
	got, err := ConnectionRequest{Negotiate: true, Protocols: Requested}.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal =\n % x\nwant\n % x", got, want)
	}
}

func TestConnectionRequestRoundTrip(t *testing.T) {
	cases := []ConnectionRequest{
		{},
		{Negotiate: true, Protocols: ProtocolRDP},
		{Negotiate: true, Flags: 0x01, Protocols: ProtocolHybrid | ProtocolRDSAAD},
		{Cookie: "mstshash=alice", Negotiate: true, Protocols: Requested},
		{Cookie: "msts=3640205228.15629.0000"},
	}
	for _, want := range cases {
		pkt, err := want.Marshal()
		if err != nil {
			t.Fatalf("%+v: Marshal: %v", want, err)
		}
		got, err := ParseConnectionRequest(pkt)
		if err != nil {
			t.Fatalf("%+v: Parse: %v", want, err)
		}
		if got != want {
			t.Errorf("round trip: got %+v, want %+v", got, want)
		}
	}
}

func TestConnectionRequestWithCorrelationInfo(t *testing.T) {
	const correlationInfoPresent = 0x08 // RDP_NEG_REQ flag CORRELATION_INFO_PRESENT
	pkt, err := ConnectionRequest{Negotiate: true, Flags: correlationInfoPresent, Protocols: Requested}.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	// Without the announced 36-byte correlation info the request is broken.
	if _, err := ParseConnectionRequest(pkt); !errors.Is(err, ErrMalformed) {
		t.Fatalf("missing correlation info: err = %v, want ErrMalformed", err)
	}
	// TPKT (55 bytes) | X.224 CR, LI 50 | RDP_NEG_REQ with the flag |
	// RDP_NEG_CORRELATION_INFO: type 6, length 36, then 32 bytes of ID and
	// reserved space.
	full := append(mustHex(t, "03 00 00 37  32 e0 00 00 00 00 00  01 08 08 00 0b 00 00 00  06 00 24 00"), make([]byte, 32)...)
	r, err := ParseConnectionRequest(full)
	if err != nil || r.Protocols != Requested {
		t.Fatalf("with correlation info: %+v, %v", r, err)
	}
}

func TestConnectionRequestRejectsBadCookies(t *testing.T) {
	if _, err := (ConnectionRequest{Cookie: "a\r\nb"}).Marshal(); err == nil {
		t.Error("a cookie with a line break must be refused")
	}
	if _, err := (ConnectionRequest{Cookie: strings.Repeat("x", 300)}).Marshal(); err == nil {
		t.Error("a cookie too long for the X.224 length indicator must be refused")
	}
	// TPKT (33 bytes) | X.224 CR, LI 28 | a cookie without its CR LF.
	unterminated := append(mustHex(t, "03 00 00 21  1c e0 00 00 00 00 00"), "Cookie: mstshash=alice"...)
	if _, err := ParseConnectionRequest(unterminated); !errors.Is(err, ErrMalformed) {
		t.Errorf("unterminated cookie: err = %v, want ErrMalformed", err)
	}
}

func TestConnectionConfirmBytes(t *testing.T) {
	cases := []struct {
		cc   ConnectionConfirm
		want string
	}{
		{ConnectionConfirm{Negotiated: true, Flags: 0x1f, Selected: ProtocolHybridEx},
			"03 00 00 13  0e d0 00 00 00 00 00  02 1f 08 00 08 00 00 00"},
		{ConnectionConfirm{Failure: HybridRequiredByServer},
			"03 00 00 13  0e d0 00 00 00 00 00  03 00 08 00 05 00 00 00"},
		{ConnectionConfirm{},
			"03 00 00 0b  06 d0 00 00 00 00 00"},
	}
	for _, c := range cases {
		if got := c.cc.Marshal(); !bytes.Equal(got, mustHex(t, c.want)) {
			t.Errorf("%+v: Marshal =\n % x\nwant\n %s", c.cc, got, c.want)
		}
	}
}

func TestConnectionConfirmRoundTrip(t *testing.T) {
	cases := []ConnectionConfirm{
		{},
		{Negotiated: true, Selected: ProtocolRDP},
		{Negotiated: true, Selected: ProtocolSSL},
		{Negotiated: true, Flags: 0x1f, Selected: ProtocolHybrid},
		{Negotiated: true, Selected: ProtocolHybridEx},
		{Negotiated: true, Selected: ProtocolRDSAAD},
	}
	// Every failure code the specification defines.
	for code := SSLRequiredByServer; code <= SSLWithUserAuthRequiredByServer; code++ {
		cases = append(cases, ConnectionConfirm{Negotiated: true, Failure: code})
	}
	for _, want := range cases {
		got, err := ParseConnectionConfirm(want.Marshal())
		if err != nil {
			t.Fatalf("%+v: Parse: %v", want, err)
		}
		if got != want {
			t.Errorf("round trip: got %+v, want %+v", got, want)
		}
	}
}

func TestParseRealServerConfirm(t *testing.T) {
	// Windows servers fill SRC-REF with 0x1234; the parser must not care.
	got, err := ParseConnectionConfirm(mustHex(t, "03 00 00 13 0e d0 00 00 12 34 00 02 1f 08 00 02 00 00 00"))
	if err != nil {
		t.Fatal(err)
	}
	if want := (ConnectionConfirm{Negotiated: true, Flags: 0x1f, Selected: ProtocolHybrid}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestParseConnectionConfirmErrors(t *testing.T) {
	cases := []struct {
		name string
		pkt  string
		want error
	}{
		{"empty", "", ErrTruncated},
		{"not TPKT", "48 54 54 50 2f 31 2e 31", ErrNotRDP},
		{"length too small", "03 00 00 0a 05 d0 00 00 00 00", ErrMalformed},
		{"shorter than declared", "03 00 00 13 0e d0 00 00 00 00 00 02 00 08 00", ErrTruncated},
		{"longer than declared", "03 00 00 0b 06 d0 00 00 00 00 00 ff", ErrMalformed},
		{"length indicator too small", "03 00 00 0b 05 d0 00 00 00 00 00", ErrMalformed},
		{"length indicator too large", "03 00 00 0b 07 d0 00 00 00 00 00", ErrMalformed},
		{"connection request instead", "03 00 00 0b 06 e0 00 00 00 00 00", ErrMalformed},
		{"disconnect request", "03 00 00 0b 06 80 00 00 00 00 00", ErrMalformed},
		{"partial negotiation data", "03 00 00 0f 0a d0 00 00 00 00 00 02 00 08 00", ErrMalformed},
		{"wrong negotiation length", "03 00 00 13 0e d0 00 00 00 00 00 02 00 09 00 02 00 00 00", ErrMalformed},
		{"request type in a confirm", "03 00 00 13 0e d0 00 00 00 00 00 01 00 08 00 02 00 00 00", ErrMalformed},
		{"failure without a code", "03 00 00 13 0e d0 00 00 00 00 00 03 00 08 00 00 00 00 00", ErrMalformed},
	}
	for _, c := range cases {
		_, err := ParseConnectionConfirm(mustHex(t, c.pkt))
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
}

func TestReadTPKT(t *testing.T) {
	full := ConnectionConfirm{Negotiated: true, Selected: ProtocolHybrid}.Marshal()
	got, err := ReadTPKT(bytes.NewReader(append(full, "next packet"...)))
	if err != nil || !bytes.Equal(got, full) {
		t.Fatalf("ReadTPKT = % x, %v; want exactly one packet", got, err)
	}
}

func TestReadTPKTCutAtEveryLength(t *testing.T) {
	full := ConnectionConfirm{Negotiated: true, Selected: ProtocolHybrid}.Marshal()
	for n := range len(full) {
		_, err := ReadTPKT(bytes.NewReader(full[:n]))
		want := ErrTruncated
		if n == 0 {
			want = ErrNoAnswer
		}
		if !errors.Is(err, want) {
			t.Errorf("cut after %d bytes: err = %v, want %v", n, err, want)
		}
	}
}

func TestReadTPKTNoAnswerKeepsTheCause(t *testing.T) {
	_, err := ReadTPKT(bytes.NewReader(nil))
	if !errors.Is(err, ErrNoAnswer) || !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want ErrNoAnswer wrapping io.EOF", err)
	}
}

func TestReadTPKTNotRDP(t *testing.T) {
	_, err := ReadTPKT(strings.NewReader("HTTP/1.1 400 Bad Request\r\n\r\n"))
	if !errors.Is(err, ErrNotRDP) || !strings.Contains(err.Error(), `"HTTP"`) {
		t.Fatalf("err = %v, want ErrNotRDP showing what arrived", err)
	}
	// One stray byte is enough to tell.
	if _, err := ReadTPKT(strings.NewReader("S")); !errors.Is(err, ErrNotRDP) {
		t.Fatalf("one byte: err = %v, want ErrNotRDP", err)
	}
}

func TestReadTPKTRejectsTinyPackets(t *testing.T) {
	if _, err := ReadTPKT(bytes.NewReader(mustHex(t, "03 00 00 04"))); !errors.Is(err, ErrMalformed) {
		t.Fatalf("err = %v, want ErrMalformed", err)
	}
}

func TestNames(t *testing.T) {
	protocols := map[Protocols]string{
		ProtocolRDP:      "RDP",
		ProtocolHybrid:   "HYBRID",
		Requested:        "SSL|HYBRID|HYBRID_EX",
		ProtocolRDSAAD:   "RDSAAD",
		0x40 | 0x01:      "SSL|0x40",
		ProtocolRDSTLS:   "RDSTLS",
		ProtocolHybridEx: "HYBRID_EX",
	}
	for p, want := range protocols {
		if got := p.String(); got != want {
			t.Errorf("Protocols(%#x).String() = %q, want %q", uint32(p), got, want)
		}
	}
	if got := HybridRequiredByServer.String(); got != "HYBRID_REQUIRED_BY_SERVER" {
		t.Errorf("FailureCode.String() = %q", got)
	}
	if got := FailureCode(9).String(); got != "0x00000009" {
		t.Errorf("unknown FailureCode.String() = %q", got)
	}
}
