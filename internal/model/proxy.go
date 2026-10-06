package model

import "strings"

// ProxySchema is the data-format number written into each proxy file.
const ProxySchema = 1

// Proxy kinds.
const (
	// KindDirect is no proxy at all: the tunnel connects to the target itself.
	// Only the built-in DirectProxyID entry has this kind.
	KindDirect = "direct"
	// KindSystem follows Windows' proxy settings: each connection goes
	// through the proxy they name for its target, or directly when they name
	// none (package sysproxy). Only the built-in SystemProxyID entry has this
	// kind, and it is turned into one of the others before a route is taken.
	KindSystem = "system"
	// KindSocks is a SOCKS5 proxy.
	KindSocks = "socks"
	// KindHTTP is an HTTP proxy that supports CONNECT.
	KindHTTP        = "http"
	KindShadowsocks = "shadowsocks"
	KindVMess       = "vmess"
	KindVLESS       = "vless"
	KindTrojan      = "trojan"
	KindHysteria2   = "hysteria2"
	// KindXray is a complete Xray outbound the user wrote by hand.
	KindXray = "xray"
)

// Kinds lists the kinds a user can create, in the order the UI offers them.
var Kinds = []string{KindSocks, KindHTTP, KindShadowsocks, KindVMess, KindVLESS, KindTrojan, KindHysteria2, KindXray}

// DirectProxyID is the built-in "no proxy" entry every profile can choose.
// It is never stored in a file.
const DirectProxyID = "direct"

// DirectProxy returns the built-in "no proxy" entry. Its name is left empty
// for the UI to show in the user's language.
func DirectProxy() Proxy {
	return Proxy{Schema: ProxySchema, ID: DirectProxyID, Kind: KindDirect}
}

// SystemProxyID is the built-in "follow the system proxy" entry every
// profile can choose. Like the direct entry, it is never stored.
const SystemProxyID = "system"

// SystemProxy returns the built-in "follow the system proxy" entry, with no
// name of its own either.
func SystemProxy() Proxy {
	return Proxy{Schema: ProxySchema, ID: SystemProxyID, Kind: KindSystem}
}

// BuiltInProxy returns the built-in entry with the given ID: direct, or
// following the system proxy.
func BuiltInProxy(id string) (Proxy, bool) {
	switch id {
	case DirectProxyID:
		return DirectProxy(), true
	case SystemProxyID:
		return SystemProxy(), true
	}
	return Proxy{}, false
}

// BuiltInKind reports whether only a built-in entry may have this kind.
func BuiltInKind(kind string) bool {
	return kind == KindDirect || kind == KindSystem
}

// Proxy is a way to reach targets (proxies\<id>.json). Many profiles can use
// the same proxy, so switching to another server is done in one place.
type Proxy struct {
	Schema int    `json:"schema"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	// Server and Port locate the proxy server. For KindXray they are only
	// shown in lists; the outbound decides where connections go.
	Server string `json:"server"`
	Port   int    `json:"port"`
	// Username signs in to a SOCKS5 or HTTP proxy, with Secret as the
	// password.
	Username string `json:"username"`
	// Secret is what signs in: the password of a SOCKS5, HTTP, Shadowsocks,
	// Trojan or Hysteria2 proxy, or the user ID of a VMess or VLESS one. The
	// store seals it with DPAPI before it writes the file.
	Secret string `json:"secret"`
	// Options are the settings of the V2Ray-family kinds (IsV2Ray) besides
	// the server, the port and the secret. They include obfuscation keys and
	// the paths that lead to the server, so the store seals them like Secret.
	Options ProxyOptions `json:"options"`
	// Outbound is the complete Xray outbound object, as JSON text, of a
	// KindXray proxy. It holds credentials, so the store seals it like
	// Secret.
	Outbound string `json:"outbound"`
}

// Normalize trims the fields where surrounding spaces are never meant, and
// clears the ones the kind does not use. Passwords are kept exactly as
// entered: they may contain spaces.
func (p Proxy) Normalize() Proxy {
	p.Schema = ProxySchema
	p.Name = strings.TrimSpace(p.Name)
	p.Server = normalizeHost(p.Server)
	p.Username = strings.TrimSpace(p.Username)
	p.Outbound = strings.TrimSpace(p.Outbound)
	switch {
	case p.Kind == KindSocks || p.Kind == KindHTTP:
		p.Options, p.Outbound = ProxyOptions{}, ""
	case IsV2Ray(p.Kind):
		p.Username, p.Outbound = "", ""
		p.Options = p.Options.normalize(p.Kind)
		if p.Kind == KindVMess || p.Kind == KindVLESS {
			p.Secret = strings.TrimSpace(p.Secret) // a user ID
		}
	case p.Kind == KindXray:
		// The outbound holds its own credentials.
		p.Username, p.Secret, p.Options = "", "", ProxyOptions{}
	}
	return p
}

// Validate reports every field that cannot be saved as is, as FieldErrors.
// It expects a normalized proxy: Normalize fills in the options' defaults.
func (p Proxy) Validate() error {
	var e FieldErrors
	if !ValidID(p.ID) {
		e.add("id", CodeInvalid)
	}
	switch p.Kind {
	case KindDirect, KindSystem:
		// Built in; nothing else to check.
	case KindSocks, KindHTTP:
		e.text("name", p.Name, true, MaxNameLen)
		e.host("server", p.Server, true)
		e.port("port", p.Port, true)
		// SOCKS5 user name/password authentication (RFC 1929) limits both
		// to 255 bytes; HTTP Basic has no limit, but 255 is plenty.
		if len(p.Username) > 255 {
			e.add("username", CodeTooLong)
		}
		if len(p.Secret) > 255 {
			e.add("secret", CodeTooLong)
		}
		if p.Secret != "" && p.Username == "" {
			e.add("username", CodeRequired)
		}
	case KindShadowsocks, KindVMess, KindVLESS, KindTrojan, KindHysteria2:
		e.text("name", p.Name, true, MaxNameLen)
		e.host("server", p.Server, true)
		e.port("port", p.Port, true)
		validSecret(p.Kind, p.Options.Cipher, p.Secret, &e)
		p.Options.validate(p.Kind, &e)
	case KindXray:
		e.text("name", p.Name, true, MaxNameLen)
		e.host("server", p.Server, false)
		e.port("port", p.Port, false)
		e.outbound(p.Outbound)
	case "":
		e.add("kind", CodeRequired)
	default:
		e.add("kind", CodeUnsupported)
	}
	return e.err()
}

// outbound checks that s is a JSON object with a "protocol", the minimum the
// Xray engine needs, and that its masks and XHTTP settings hold no negative
// number (see masks.go). The engine reports anything else Xray rejects.
func (e *FieldErrors) outbound(s string) {
	switch {
	case s == "":
		e.add("outbound", CodeRequired)
	case len(s) > MaxOutboundLen:
		e.add("outbound", CodeTooLong)
	default:
		v, ok := decodeJSON(s)
		ob, isObject := v.(map[string]any)
		protocol, _ := ob["protocol"].(string)
		if !ok || !isObject || protocol == "" || !customTransportSafe(ob) {
			e.add("outbound", CodeInvalid)
		}
	}
}
