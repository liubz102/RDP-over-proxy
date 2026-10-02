// Package probe speaks just enough of the Remote Desktop Protocol to tell
// whether an RDP server answers at the end of a route. Every RDP connection
// starts with the client's X.224 Connection Request and the server's
// Connection Confirm (MS-RDPBCGR 2.2.1.1 and 2.2.1.2); the probe sends the
// first and reads the second. Nothing is authenticated and no credentials or
// user names are sent.
package probe

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Protocols are the security protocols of RDP negotiation: a set of flags in
// a request, a single value in a response. Zero is standard RDP security.
type Protocols uint32

// Protocol values (MS-RDPBCGR 2.2.1.1.1).
const (
	ProtocolRDP      Protocols = 0x00 // standard RDP security
	ProtocolSSL      Protocols = 0x01 // TLS
	ProtocolHybrid   Protocols = 0x02 // CredSSP (Network Level Authentication)
	ProtocolRDSTLS   Protocols = 0x04
	ProtocolHybridEx Protocols = 0x08 // CredSSP with an Early User Authorization Result
	ProtocolRDSAAD   Protocols = 0x10 // Microsoft Entra ID sign-in
)

var protocolNames = []struct {
	p    Protocols
	name string
}{
	{ProtocolSSL, "SSL"},
	{ProtocolHybrid, "HYBRID"},
	{ProtocolRDSTLS, "RDSTLS"},
	{ProtocolHybridEx, "HYBRID_EX"},
	{ProtocolRDSAAD, "RDSAAD"},
}

// String names the protocols as the specification does, joined with "|".
func (p Protocols) String() string {
	if p == ProtocolRDP {
		return "RDP"
	}
	var parts []string
	rest := p
	for _, n := range protocolNames {
		if p&n.p != 0 {
			parts = append(parts, n.name)
			rest &^= n.p
		}
	}
	if rest != 0 {
		parts = append(parts, fmt.Sprintf("0x%X", uint32(rest)))
	}
	return strings.Join(parts, "|")
}

// FailureCode is why a server refused every protocol the client offered
// (RDP_NEG_FAILURE, MS-RDPBCGR 2.2.1.2.2).
type FailureCode uint32

// Failure codes.
const (
	SSLRequiredByServer             FailureCode = 1
	SSLNotAllowedByServer           FailureCode = 2
	SSLCertNotOnServer              FailureCode = 3
	InconsistentFlags               FailureCode = 4
	HybridRequiredByServer          FailureCode = 5
	SSLWithUserAuthRequiredByServer FailureCode = 6
)

var failureNames = map[FailureCode]string{
	SSLRequiredByServer:             "SSL_REQUIRED_BY_SERVER",
	SSLNotAllowedByServer:           "SSL_NOT_ALLOWED_BY_SERVER",
	SSLCertNotOnServer:              "SSL_CERT_NOT_ON_SERVER",
	InconsistentFlags:               "INCONSISTENT_FLAGS",
	HybridRequiredByServer:          "HYBRID_REQUIRED_BY_SERVER",
	SSLWithUserAuthRequiredByServer: "SSL_WITH_USER_AUTH_REQUIRED_BY_SERVER",
}

// String names the code as the specification does.
func (c FailureCode) String() string {
	if name, ok := failureNames[c]; ok {
		return name
	}
	return fmt.Sprintf("0x%08X", uint32(c))
}

// Errors from reading and parsing. Each comes wrapped with details.
var (
	// ErrNoAnswer: the connection ended before the server sent anything.
	// Through a proxy this usually means the proxy could not reach the
	// target and gave up.
	ErrNoAnswer = errors.New("the connection closed before the target answered")
	// ErrNotRDP: the server answered, but not with RDP (for example an HTTP
	// error page from the wrong port).
	ErrNotRDP = errors.New("the target did not answer with RDP")
	// ErrTruncated: the answer stopped part-way through a packet.
	ErrTruncated = errors.New("the target's answer was cut short")
	// ErrMalformed: the answer was RDP-shaped but broken.
	ErrMalformed = errors.New("the target's answer is malformed")
)

// Packet layout.
const (
	tpktVersion = 3
	tpktHeader  = 4 // version, reserved, length (big endian)
	x224Header  = 7 // length indicator, code, DST-REF, SRC-REF, class
	minPacket   = tpktHeader + x224Header
	negDataLen  = 8 // type, flags, length (little endian), value (little endian)
	// The X.224 length indicator is one byte and 255 is reserved, so the
	// variable part after the fixed header is at most 254-6 bytes.
	maxVariable = 254 - (x224Header - 1)

	codeMask              = 0xF0
	codeConnectionRequest = 0xE0
	codeConnectionConfirm = 0xD0

	typeNegReq          = 0x01
	typeNegRsp          = 0x02
	typeNegFailure      = 0x03
	typeCorrelationInfo = 0x06

	flagCorrelationInfoPresent = 0x08
	correlationInfoLen         = 36
)

var cookiePrefix = []byte("Cookie: ")

// ConnectionRequest is the client's first packet.
type ConnectionRequest struct {
	// Cookie is the text of an optional "Cookie: ...\r\n" line, without the
	// "Cookie: " prefix and the line break: a routing token ("msts=...") or a
	// user hint ("mstshash=..."). The probe sends none.
	Cookie string
	// Negotiate adds an RDP Negotiation Request. A server that gets none
	// answers without negotiation and uses standard RDP security.
	Negotiate bool
	Flags     uint8
	Protocols Protocols
}

// Marshal encodes the request as a TPKT packet.
func (r ConnectionRequest) Marshal() ([]byte, error) {
	var variable []byte
	if r.Cookie != "" {
		if strings.ContainsAny(r.Cookie, "\r\n") {
			return nil, errors.New("the cookie contains a line break")
		}
		variable = append(variable, cookiePrefix...)
		variable = append(variable, r.Cookie...)
		variable = append(variable, '\r', '\n')
	}
	if r.Negotiate {
		variable = appendNegData(variable, typeNegReq, r.Flags, uint32(r.Protocols))
	}
	return packet(codeConnectionRequest, variable)
}

// ParseConnectionRequest decodes one TPKT packet holding a Connection
// Request. It is what an RDP server does with the client's first packet; the
// tests' fake server uses it.
func ParseConnectionRequest(pkt []byte) (ConnectionRequest, error) {
	rest, err := tpdu(pkt, codeConnectionRequest)
	if err != nil {
		return ConnectionRequest{}, err
	}
	var r ConnectionRequest
	if bytes.HasPrefix(rest, cookiePrefix) {
		end := bytes.Index(rest, []byte("\r\n"))
		if end < 0 {
			return ConnectionRequest{}, fmt.Errorf("%w: the cookie line has no end", ErrMalformed)
		}
		r.Cookie = string(rest[len(cookiePrefix):end])
		rest = rest[end+2:]
	}
	if len(rest) == 0 {
		return r, nil
	}
	typ, flags, value, err := negData(rest)
	if err != nil {
		return ConnectionRequest{}, err
	}
	if typ != typeNegReq {
		return ConnectionRequest{}, fmt.Errorf("%w: negotiation type 0x%02X in a request", ErrMalformed, typ)
	}
	r.Negotiate, r.Flags, r.Protocols = true, flags, Protocols(value)
	rest = rest[negDataLen:]
	if flags&flagCorrelationInfoPresent != 0 &&
		(len(rest) < correlationInfoLen || rest[0] != typeCorrelationInfo) {
		return ConnectionRequest{}, fmt.Errorf("%w: correlation info announced but missing", ErrMalformed)
	}
	return r, nil
}

// ConnectionConfirm is the server's answer.
type ConnectionConfirm struct {
	// Negotiated is true when the server took part in negotiation: it sent
	// either a response or a failure. A server that sends neither only
	// speaks standard RDP security.
	Negotiated bool
	Flags      uint8
	// Selected is the protocol the server chose. It means nothing when
	// Failure is set.
	Selected Protocols
	// Failure is why the server refused every protocol offered; zero means
	// it accepted one.
	Failure FailureCode
}

// Marshal encodes the confirm as a TPKT packet. A nonzero Failure is sent as
// a negotiation failure; otherwise Negotiated decides whether a negotiation
// response is included.
func (c ConnectionConfirm) Marshal() []byte {
	var variable []byte
	switch {
	case c.Failure != 0:
		variable = appendNegData(nil, typeNegFailure, 0, uint32(c.Failure))
	case c.Negotiated:
		variable = appendNegData(nil, typeNegRsp, c.Flags, uint32(c.Selected))
	}
	b, err := packet(codeConnectionConfirm, variable)
	if err != nil {
		panic(err) // eight bytes always fit
	}
	return b
}

// ParseConnectionConfirm decodes one TPKT packet holding a Connection
// Confirm.
func ParseConnectionConfirm(pkt []byte) (ConnectionConfirm, error) {
	rest, err := tpdu(pkt, codeConnectionConfirm)
	if err != nil {
		return ConnectionConfirm{}, err
	}
	if len(rest) == 0 {
		return ConnectionConfirm{}, nil
	}
	typ, flags, value, err := negData(rest)
	if err != nil {
		return ConnectionConfirm{}, err
	}
	switch typ {
	case typeNegRsp:
		return ConnectionConfirm{Negotiated: true, Flags: flags, Selected: Protocols(value)}, nil
	case typeNegFailure:
		if value == 0 {
			return ConnectionConfirm{}, fmt.Errorf("%w: a negotiation failure without a code", ErrMalformed)
		}
		return ConnectionConfirm{Negotiated: true, Flags: flags, Failure: FailureCode(value)}, nil
	default:
		return ConnectionConfirm{}, fmt.Errorf("%w: negotiation type 0x%02X in a confirm", ErrMalformed, typ)
	}
}

// ReadTPKT reads one whole TPKT packet, header included.
func ReadTPKT(r io.Reader) ([]byte, error) {
	var hdr [tpktHeader]byte
	n, err := io.ReadFull(r, hdr[:])
	switch {
	case n == 0 && err != nil:
		return nil, fmt.Errorf("%w (%w)", ErrNoAnswer, err)
	case hdr[0] != tpktVersion:
		// One byte is enough to tell; show what arrived.
		return nil, fmt.Errorf("%w: it began with %q", ErrNotRDP, hdr[:n])
	case err != nil:
		return nil, fmt.Errorf("%w: %d of %d header bytes", ErrTruncated, n, tpktHeader)
	}
	length := int(binary.BigEndian.Uint16(hdr[2:]))
	if length < minPacket {
		return nil, fmt.Errorf("%w: a %d-byte packet", ErrMalformed, length)
	}
	pkt := make([]byte, length)
	copy(pkt, hdr[:])
	if n, err := io.ReadFull(r, pkt[tpktHeader:]); err != nil {
		return nil, fmt.Errorf("%w: %d of %d bytes", ErrTruncated, tpktHeader+n, length)
	}
	return pkt, nil
}

// packet frames the fixed X.224 header with the given code, followed by the
// variable part, in a TPKT.
func packet(code byte, variable []byte) ([]byte, error) {
	if len(variable) > maxVariable {
		return nil, fmt.Errorf("the packet's variable part is %d bytes, more than %d", len(variable), maxVariable)
	}
	total := minPacket + len(variable)
	b := make([]byte, 0, total)
	b = append(b, tpktVersion, 0)
	b = binary.BigEndian.AppendUint16(b, uint16(total))
	// Length indicator: the header after this byte, variable part included.
	b = append(b, byte(x224Header-1+len(variable)), code, 0, 0, 0, 0, 0)
	return append(b, variable...), nil
}

// tpdu checks the TPKT and X.224 headers of pkt and returns the variable
// part after them.
func tpdu(pkt []byte, wantCode byte) ([]byte, error) {
	if len(pkt) < tpktHeader {
		return nil, fmt.Errorf("%w: %d bytes", ErrTruncated, len(pkt))
	}
	if pkt[0] != tpktVersion {
		return nil, fmt.Errorf("%w: it began with %q", ErrNotRDP, pkt[:tpktHeader])
	}
	length := int(binary.BigEndian.Uint16(pkt[2:]))
	switch {
	case length < minPacket:
		return nil, fmt.Errorf("%w: a %d-byte packet", ErrMalformed, length)
	case len(pkt) < length:
		return nil, fmt.Errorf("%w: %d of %d bytes", ErrTruncated, len(pkt), length)
	case len(pkt) > length:
		return nil, fmt.Errorf("%w: %d bytes after the packet", ErrMalformed, len(pkt)-length)
	}
	// The length indicator should cover the rest of the packet. Only an
	// impossible value is rejected; the TPKT length is what counts.
	if li := int(pkt[4]); li < x224Header-1 || li > length-tpktHeader-1 {
		return nil, fmt.Errorf("%w: length indicator %d in a %d-byte packet", ErrMalformed, li, length)
	}
	if code := pkt[5]; code&codeMask != wantCode {
		return nil, fmt.Errorf("%w: X.224 code 0x%02X, expected 0x%02X", ErrMalformed, code, wantCode)
	}
	return pkt[minPacket:], nil
}

func appendNegData(b []byte, typ, flags byte, value uint32) []byte {
	b = append(b, typ, flags)
	b = binary.LittleEndian.AppendUint16(b, negDataLen)
	return binary.LittleEndian.AppendUint32(b, value)
}

// negData decodes the 8-byte negotiation structure at the start of b.
func negData(b []byte) (typ, flags byte, value uint32, err error) {
	if len(b) < negDataLen {
		return 0, 0, 0, fmt.Errorf("%w: %d bytes of negotiation data, expected %d", ErrMalformed, len(b), negDataLen)
	}
	if n := binary.LittleEndian.Uint16(b[2:]); n != negDataLen {
		return 0, 0, 0, fmt.Errorf("%w: negotiation data claims %d bytes, expected %d", ErrMalformed, n, negDataLen)
	}
	return b[0], b[1], binary.LittleEndian.Uint32(b[4:]), nil
}
