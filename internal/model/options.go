package model

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
)

// ProxyOptions are a V2Ray-family proxy's settings besides the server, the
// port and the secret: the protocol's own options, the transport and its
// security layer. Most are named after the share-link parameters of the same
// meaning (XTLS/Xray-core discussion #716).
//
// Normalize keeps only the options that apply to the proxy's kind, network
// and security, and fills in the defaults, so what is stored is exactly what
// is used.
type ProxyOptions struct {
	// Cipher is Shadowsocks' method (ShadowsocksCiphers), or VMess' own
	// encryption, which Xray calls "security" (VMessCiphers).
	Cipher string `json:"cipher"`
	// Flow is VLESS' flow control (VLESSFlows); empty for none.
	Flow string `json:"flow"`
	// Encryption is VLESS' encryption: "none", or the client setting of VLESS
	// Encryption ("mlkem768x25519plus.…").
	Encryption string `json:"encryption"`
	// ObfsPassword turns on Hysteria2's Salamander obfuscation.
	ObfsPassword string `json:"obfsPassword"`

	// Network is how VMess, VLESS and Trojan carry the connection (Networks).
	Network string `json:"network"`
	// HeaderType disguises TCP as HTTP (TCPHeaders), or mKCP packets as
	// another protocol (KCPHeaders).
	HeaderType string `json:"headerType"`
	// Host and Path are the HTTP host and path of WebSocket, HTTPUpgrade and
	// XHTTP, and of TCP's HTTP disguise, where both are comma-separated
	// lists. For mKCP's DNS disguise, Host is the domain.
	Host string `json:"host"`
	Path string `json:"path"`
	// ServiceName and Authority are gRPC's.
	ServiceName string `json:"serviceName"`
	Authority   string `json:"authority"`
	// Mode is gRPC's (GRPCModes) or XHTTP's (XHTTPModes).
	Mode string `json:"mode"`
	// Seed encrypts mKCP packets with AES-128-GCM; without it they are only
	// obfuscated.
	Seed string `json:"seed"`
	// Extra is XHTTP's further settings, a JSON object.
	Extra string `json:"extra"`
	// FinalMask is Xray's "finalmask" settings (masks for the packets on the
	// wire, QUIC parameters), a JSON object; share links call it "fm".
	FinalMask string `json:"finalMask"`

	// Security is the layer around the transport: SecurityNone, SecurityTLS
	// or SecurityREALITY.
	Security string `json:"security"`
	// SNI is the server name that TLS and REALITY present. Empty means the
	// server's address (TLS only).
	SNI string `json:"sni"`
	// ALPN is TLS' application protocols, comma-separated.
	ALPN string `json:"alpn"`
	// Fingerprint is the TLS client that TLS and REALITY imitate, such as
	// "chrome" (Fingerprints); empty is Xray's default, Chrome.
	Fingerprint string `json:"fingerprint"`
	// PinnedCerts are the SHA-256 hashes, in hex, of the certificates to
	// trust, comma-separated. They are the way to trust a self-signed
	// certificate: Xray no longer skips verification ("allowInsecure").
	PinnedCerts string `json:"pinnedCerts"`
	// VerifyNames are the names to verify the certificate against instead of
	// the SNI, comma-separated.
	VerifyNames string `json:"verifyNames"`
	// ECH is the config list of TLS Encrypted Client Hello.
	ECH string `json:"ech"`
	// PublicKey, ShortID, SpiderX and MLDSA65Verify are REALITY's.
	PublicKey     string `json:"publicKey"`
	ShortID       string `json:"shortId"`
	SpiderX       string `json:"spiderX"`
	MLDSA65Verify string `json:"mldsa65Verify"`
}

// Networks of VMess, VLESS and Trojan. Xray calls TCP "RAW"; share links
// keep "tcp". Xray no longer has HTTP/2 ("h2", "http") or QUIC transports.
const (
	NetworkTCP         = "tcp"
	NetworkWS          = "ws"
	NetworkHTTPUpgrade = "httpupgrade"
	NetworkGRPC        = "grpc"
	NetworkXHTTP       = "xhttp"
	NetworkKCP         = "kcp"
)

// Networks lists the networks in the order the UI offers them.
var Networks = []string{NetworkTCP, NetworkWS, NetworkGRPC, NetworkXHTTP, NetworkHTTPUpgrade, NetworkKCP}

// Security layers.
const (
	SecurityNone    = "none"
	SecurityTLS     = "tls"
	SecurityREALITY = "reality"
)

// Securities lists the security layers.
var Securities = []string{SecurityNone, SecurityTLS, SecurityREALITY}

// HeaderNone is the HeaderType without a disguise.
const HeaderNone = "none"

var (
	// VMessCiphers are VMess' encryptions; "auto" lets Xray choose.
	VMessCiphers = []string{"auto", "aes-128-gcm", "chacha20-poly1305", "none", "zero"}
	// ShadowsocksCiphers are the Shadowsocks methods Xray has.
	ShadowsocksCiphers = []string{
		"2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305",
		"aes-128-gcm", "aes-256-gcm", "chacha20-poly1305", "xchacha20-poly1305", "none",
	}
	// VLESSFlows are VLESS' flows; empty is none.
	VLESSFlows = []string{"", "xtls-rprx-vision", "xtls-rprx-vision-udp443"}
	// TCPHeaders disguise TCP.
	TCPHeaders = []string{HeaderNone, "http"}
	// KCPHeaders disguise mKCP packets as another protocol.
	KCPHeaders = []string{HeaderNone, "srtp", "utp", "wechat-video", "dtls", "wireguard", "dns"}
	// GRPCModes: "gun" is one stream per connection, "multi" several.
	GRPCModes = []string{"gun", "multi"}
	// XHTTPModes are XHTTP's ways of sending.
	XHTTPModes = []string{"auto", "packet-up", "stream-up", "stream-one"}
	// Fingerprints are the TLS clients the UI offers to imitate. Xray knows
	// more, and checks the name when the proxy is saved.
	Fingerprints = []string{"chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized"}
)

// ss2022KeyLen is the key length, in bytes, of each Shadowsocks 2022
// method. Its password is the key in base64, or several keys separated by
// ':' (a server's key, then the user's).
var ss2022KeyLen = map[string]int{
	"2022-blake3-aes-128-gcm":       16,
	"2022-blake3-aes-256-gcm":       32,
	"2022-blake3-chacha20-poly1305": 32,
}

// cipherAliases are other names of the Shadowsocks methods, as some links
// and older clients write them.
var cipherAliases = map[string]string{
	"chacha20-ietf-poly1305":  "chacha20-poly1305",
	"xchacha20-ietf-poly1305": "xchacha20-poly1305",
	"aead_aes_128_gcm":        "aes-128-gcm",
	"aead_aes_256_gcm":        "aes-256-gcm",
	"aead_chacha20_poly1305":  "chacha20-poly1305",
	"aead_xchacha20_poly1305": "xchacha20-poly1305",
	"plain":                   "none",
}

// networkAliases are other names of the networks.
var networkAliases = map[string]string{
	"raw":       NetworkTCP,
	"websocket": NetworkWS,
	"splithttp": NetworkXHTTP,
	"mkcp":      NetworkKCP,
}

// Limits of the options' text.
const (
	maxOptionLen = 1024
	// maxKeyLen fits a VLESS Encryption client setting, whose ML-KEM key
	// alone is about 1600 characters in base64.
	maxKeyLen = 8 << 10
)

// v2rayKinds are the kinds whose settings are ProxyOptions.
var v2rayKinds = []string{KindShadowsocks, KindVMess, KindVLESS, KindTrojan, KindHysteria2}

// IsV2Ray reports whether proxies of this kind keep their settings in
// ProxyOptions.
func IsV2Ray(kind string) bool { return slices.Contains(v2rayKinds, kind) }

// HasTransport reports whether proxies of this kind choose a network and a
// security layer.
func HasTransport(kind string) bool {
	return kind == KindVMess || kind == KindVLESS || kind == KindTrojan
}

// Transport describes the proxy's transport for lists: its network and
// security layer. Either is empty when the kind has none (Shadowsocks has
// neither, Hysteria2 is always QUIC with TLS) or a custom outbound does not
// say.
func (p Proxy) Transport() (network, security string) {
	switch {
	case HasTransport(p.Kind):
		return p.Options.Network, p.Options.Security
	case p.Kind == KindHysteria2:
		return "", SecurityTLS
	case p.Kind == KindXray:
		var ob struct {
			StreamSettings struct {
				Network  string `json:"network"`
				Security string `json:"security"`
			} `json:"streamSettings"`
		}
		if json.Unmarshal([]byte(p.Outbound), &ob) != nil {
			return "", ""
		}
		network, security = lower(ob.StreamSettings.Network), lower(ob.StreamSettings.Security)
		if a, ok := networkAliases[network]; ok {
			network = a
		}
		if network == "" {
			network = NetworkTCP // Xray's default
		}
		if security == "" {
			security = SecurityNone
		}
		return network, security
	}
	return "", ""
}

// CanonicalNetwork returns a network's usual name, in lower case: "raw" is
// "tcp", "splithttp" is "xhttp", and so on.
func CanonicalNetwork(network string) string {
	network = lower(network)
	if a, ok := networkAliases[network]; ok {
		return a
	}
	return network
}

// normalize keeps the options that apply to the kind, network and security,
// and fills in the defaults.
func (o ProxyOptions) normalize(kind string) ProxyOptions {
	var n ProxyOptions
	switch kind {
	case KindShadowsocks:
		n.Cipher = lower(o.Cipher)
		if c, ok := cipherAliases[n.Cipher]; ok {
			n.Cipher = c
		}
		return n
	case KindHysteria2:
		// Always TLS over QUIC; Xray has no fingerprints for QUIC.
		n.ObfsPassword = o.ObfsPassword
		n.SNI = normalizeHost(o.SNI)
		n.ALPN = list(o.ALPN)
		n.PinnedCerts = pins(o.PinnedCerts)
		n.VerifyNames = list(o.VerifyNames)
		return n
	case KindVMess:
		n.Cipher = lower(o.Cipher)
		if n.Cipher == "" {
			n.Cipher = "auto"
		}
	case KindVLESS:
		n.Flow = lower(o.Flow)
		if n.Flow == "none" {
			n.Flow = ""
		}
		n.Encryption = strings.TrimSpace(o.Encryption)
		if n.Encryption == "" {
			n.Encryption = "none"
		}
	}

	n.Network = CanonicalNetwork(o.Network)
	if n.Network == "" {
		n.Network = NetworkTCP
	}
	switch n.Network {
	case NetworkTCP:
		n.HeaderType = orNone(lower(o.HeaderType))
		if n.HeaderType != HeaderNone {
			n.Host, n.Path = list(o.Host), list(o.Path)
		}
	case NetworkWS, NetworkHTTPUpgrade:
		n.Host, n.Path = strings.TrimSpace(o.Host), strings.TrimSpace(o.Path)
	case NetworkGRPC:
		n.ServiceName, n.Authority = strings.TrimSpace(o.ServiceName), strings.TrimSpace(o.Authority)
		n.Mode = lower(o.Mode)
		if n.Mode == "" {
			n.Mode = "gun"
		}
	case NetworkXHTTP:
		n.Host, n.Path = strings.TrimSpace(o.Host), strings.TrimSpace(o.Path)
		n.Mode = lower(o.Mode)
		if n.Mode == "" {
			n.Mode = "auto"
		}
		n.Extra = strings.TrimSpace(o.Extra)
	case NetworkKCP:
		n.HeaderType = orNone(lower(o.HeaderType))
		if n.HeaderType == "wechat" {
			n.HeaderType = "wechat-video"
		}
		if n.HeaderType == "dns" {
			n.Host = strings.TrimSpace(o.Host)
		}
		n.Seed = o.Seed
	}
	n.FinalMask = strings.TrimSpace(o.FinalMask)

	n.Security = lower(o.Security)
	if n.Security == "" {
		// Trojan runs over TLS unless told otherwise; the others start bare.
		n.Security = SecurityNone
		if kind == KindTrojan {
			n.Security = SecurityTLS
		}
	}
	switch n.Security {
	case SecurityTLS:
		n.SNI = normalizeHost(o.SNI)
		n.ALPN = list(o.ALPN)
		n.Fingerprint = lower(o.Fingerprint)
		n.PinnedCerts = pins(o.PinnedCerts)
		n.VerifyNames = list(o.VerifyNames)
		n.ECH = strings.TrimSpace(o.ECH)
	case SecurityREALITY:
		n.SNI = normalizeHost(o.SNI)
		n.Fingerprint = lower(o.Fingerprint)
		n.PublicKey = rawURLBase64(o.PublicKey)
		n.ShortID = lower(o.ShortID)
		n.SpiderX = strings.TrimSpace(o.SpiderX)
		if n.SpiderX == "/" {
			n.SpiderX = "" // Xray's default
		}
		n.MLDSA65Verify = rawURLBase64(o.MLDSA65Verify)
	}
	return n
}

// validate adds the problems of options that normalize has already seen.
func (o ProxyOptions) validate(kind string, e *FieldErrors) {
	switch kind {
	case KindShadowsocks:
		e.oneOf("options.cipher", o.Cipher, ShadowsocksCiphers)
		return
	case KindHysteria2:
		if o.ObfsPassword != "" && len(o.ObfsPassword) < salamanderMinLen {
			e.add("options.obfsPassword", CodeInvalid)
		} else {
			e.plain("options.obfsPassword", o.ObfsPassword, maxOptionLen)
		}
		o.validateTLS(e)
		return
	case KindVMess:
		e.oneOf("options.cipher", o.Cipher, VMessCiphers)
	case KindVLESS:
		e.oneOf("options.flow", o.Flow, VLESSFlows)
		switch {
		case o.Encryption == "none":
		case !strings.HasPrefix(o.Encryption, "mlkem768x25519plus."):
			e.add("options.encryption", CodeUnsupported)
		case len(o.Encryption) > maxKeyLen:
			e.add("options.encryption", CodeTooLong)
		case !validVLESSEncryption(o.Encryption):
			e.add("options.encryption", CodeInvalid)
		}
		// Vision needs to see TLS directly (not inside TCP's HTTP disguise),
		// or VLESS Encryption.
		direct := o.Network == NetworkTCP && o.HeaderType == HeaderNone && o.Security != SecurityNone
		if o.Flow != "" && !direct && o.Encryption == "none" {
			e.add("options.flow", CodeConflict)
		}
	}

	e.oneOf("options.network", o.Network, Networks)
	switch o.Network {
	case NetworkTCP:
		e.oneOf("options.headerType", o.HeaderType, TCPHeaders)
		e.plain("options.host", o.Host, maxOptionLen)
		if len(o.Path) > maxOptionLen {
			e.add("options.path", CodeTooLong)
		} else if !all(splitList(o.Path), validPath) {
			e.add("options.path", CodeInvalid)
		}
	case NetworkWS, NetworkHTTPUpgrade, NetworkXHTTP:
		e.plain("options.host", o.Host, maxOptionLen)
		e.path("options.path", o.Path)
		if o.Network == NetworkXHTTP {
			e.oneOf("options.mode", o.Mode, XHTTPModes)
			e.extra("options.extra", o.Extra)
		}
	case NetworkGRPC:
		e.plain("options.serviceName", o.ServiceName, maxOptionLen)
		e.plain("options.authority", o.Authority, maxOptionLen)
		e.oneOf("options.mode", o.Mode, GRPCModes)
	case NetworkKCP:
		e.oneOf("options.headerType", o.HeaderType, KCPHeaders)
		if o.Host != "" && !ValidHost(o.Host) {
			e.add("options.host", CodeInvalid)
		}
		e.plain("options.seed", o.Seed, maxOptionLen)
		// The mask settings replace the ones the disguise and seed make.
		if o.FinalMask != "" && (o.HeaderType != HeaderNone || o.Seed != "") {
			e.add("options.finalMask", CodeConflict)
		}
	}
	e.finalMask("options.finalMask", o.FinalMask)

	e.oneOf("options.security", o.Security, Securities)
	switch o.Security {
	case SecurityTLS:
		o.validateTLS(e)
		e.token("options.fingerprint", o.Fingerprint)
		e.plain("options.ech", o.ECH, maxKeyLen)
	case SecurityREALITY:
		// Xray's REALITY works over RAW, XHTTP and gRPC only.
		if o.Network != NetworkTCP && o.Network != NetworkXHTTP && o.Network != NetworkGRPC {
			e.add("options.security", CodeConflict)
		}
		if o.SNI != "" && !ValidHost(o.SNI) {
			e.add("options.sni", CodeInvalid)
		}
		e.token("options.fingerprint", o.Fingerprint)
		switch {
		case o.PublicKey == "":
			e.add("options.publicKey", CodeRequired)
		case !base64Len(o.PublicKey, 32):
			e.add("options.publicKey", CodeInvalid)
		}
		if b, err := hex.DecodeString(o.ShortID); err != nil || len(b) > 8 {
			e.add("options.shortId", CodeInvalid)
		}
		if o.SpiderX != "" {
			e.path("options.spiderX", o.SpiderX)
		}
		if o.MLDSA65Verify != "" && !base64Len(o.MLDSA65Verify, 1952) {
			e.add("options.mldsa65Verify", CodeInvalid)
		}
	}
}

// validateTLS checks the certificate options that TLS and Hysteria2 share.
func (o ProxyOptions) validateTLS(e *FieldErrors) {
	if o.SNI != "" && !ValidHost(o.SNI) {
		e.add("options.sni", CodeInvalid)
	}
	switch {
	case len(o.ALPN) > maxOptionLen:
		e.add("options.alpn", CodeTooLong)
	case !all(splitList(o.ALPN), func(a string) bool {
		return len(a) <= 255 && !strings.ContainsFunc(a, func(r rune) bool { return r <= ' ' || r == 0x7f })
	}):
		e.add("options.alpn", CodeInvalid)
	}
	if !all(splitList(o.PinnedCerts), func(p string) bool {
		b, err := hex.DecodeString(p)
		return err == nil && len(b) == 32
	}) {
		e.add("options.pinnedCerts", CodeInvalid)
	}
	if !all(splitList(o.VerifyNames), ValidHost) {
		e.add("options.verifyNames", CodeInvalid)
	}
}

// all reports whether ok holds for every item.
func all(items []string, ok func(string) bool) bool {
	return !slices.ContainsFunc(items, func(s string) bool { return !ok(s) })
}

// validSecret checks the secret of a V2Ray-family proxy.
func validSecret(kind, cipher, secret string, e *FieldErrors) {
	switch {
	case secret == "":
		e.add("secret", CodeRequired)
		return
	case len(secret) > maxOptionLen:
		e.add("secret", CodeTooLong)
		return
	}
	switch kind {
	case KindVMess, KindVLESS:
		if !ValidUserID(secret) {
			e.add("secret", CodeInvalid)
		}
	case KindShadowsocks:
		n, ok := ss2022KeyLen[cipher]
		if !ok {
			return
		}
		for _, key := range strings.Split(secret, ":") {
			if b, err := base64.StdEncoding.DecodeString(key); err != nil || len(b) != n {
				e.add("secret", CodeInvalid)
				return
			}
		}
	}
}

// validVLESSEncryption checks a VLESS Encryption client setting:
// mlkem768x25519plus.<mode>.<rtt>, then optional padding settings (short
// parts), then the server's keys (X25519: 32 bytes, ML-KEM-768: 1184 bytes,
// in unpadded URL-safe base64). Xray reads it in that order, and reading a
// malformed one crashes it rather than failing.
func validVLESSEncryption(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) < 4 || parts[0] != "mlkem768x25519plus" {
		return false
	}
	if !slices.Contains([]string{"native", "xorpub", "random"}, parts[1]) || (parts[2] != "1rtt" && parts[2] != "0rtt") {
		return false
	}
	keys := 0
	var padding []string
	for _, p := range parts[3:] {
		if len(p) < 20 {
			if keys > 0 {
				return false // padding goes before the keys
			}
			padding = append(padding, p)
			continue
		}
		if !base64Len(p, 32) && !base64Len(p, 1184) {
			return false
		}
		keys++
	}
	return keys > 0 && validPadding(padding)
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{12}$`)

// ValidUserID reports whether Xray takes s as a VMess or VLESS user ID: a
// UUID, or any other text of 1 to 30 bytes, which Xray maps to a UUID.
func ValidUserID(s string) bool {
	switch n := len(s); {
	case n == 0:
		return false
	case n <= 30:
		return true
	default:
		return uuidPattern.MatchString(s)
	}
}

// oneOf checks a field that takes one of a few values.
func (e *FieldErrors) oneOf(field, value string, allowed []string) {
	switch {
	case slices.Contains(allowed, value):
	case value == "":
		e.add(field, CodeRequired)
	default:
		e.add(field, CodeUnsupported)
	}
}

// plain checks text that goes into the protocol as it is: at most max bytes
// and no control characters (a line break would end an HTTP header).
func (e *FieldErrors) plain(field, value string, max int) {
	if len(value) > max {
		e.add(field, CodeTooLong)
		return
	}
	if strings.ContainsFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		e.add(field, CodeInvalid)
	}
}

// path checks an HTTP path: empty, or starting with "/".
func (e *FieldErrors) path(field, value string) {
	if len(value) > maxOptionLen {
		e.add(field, CodeTooLong)
	} else if value != "" && !validPath(value) {
		e.add(field, CodeInvalid)
	}
}

func validPath(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.ContainsFunc(p, func(r rune) bool { return r <= ' ' || r == 0x7f })
}

var tokenPattern = regexp.MustCompile(`^[a-z0-9_]{0,64}$`)

// token checks a name such as a fingerprint.
func (e *FieldErrors) token(field, value string) {
	if !tokenPattern.MatchString(value) {
		e.add(field, CodeInvalid)
	}
}

// jsonObject checks a field that holds a JSON object, when it is not empty.
func (e *FieldErrors) jsonObject(field, value string) {
	if value == "" {
		return
	}
	if len(value) > MaxOutboundLen {
		e.add(field, CodeTooLong)
		return
	}
	var v map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &v); err != nil || v == nil {
		e.add(field, CodeInvalid)
	}
}

func lower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func orNone(s string) string {
	if s == "" {
		return HeaderNone
	}
	return s
}

// splitList splits a comma-separated list, leaving out empty items.
func splitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// list tidies a comma-separated list.
func list(s string) string { return strings.Join(splitList(s), ",") }

// pins tidies certificate hashes: lowercase hex, without the colons that
// OpenSSL puts between the bytes.
func pins(s string) string {
	items := splitList(s)
	for i, p := range items {
		items[i] = strings.ToLower(strings.ReplaceAll(p, ":", ""))
	}
	return strings.Join(items, ",")
}

// rawURLBase64 turns a key into the unpadded URL-safe base64 Xray reads,
// whatever base64 alphabet and padding it came in.
func rawURLBase64(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, "=")
	return strings.NewReplacer("+", "-", "/", "_").Replace(s)
}

// base64Len reports whether s is unpadded URL-safe base64 of n bytes.
func base64Len(s string, n int) bool {
	b, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil && len(b) == n
}
