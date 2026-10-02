package model

import (
	"encoding/json"
	"strings"
)

// ProxySchema is the data-format number written into each proxy file.
const ProxySchema = 1

// Proxy kinds.
const (
	// KindDirect is no proxy at all: the tunnel connects to the target itself.
	// Only the built-in DirectProxyID entry has this kind.
	KindDirect = "direct"
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
	// Username and Secret sign in to a SOCKS5 or HTTP proxy. The store seals
	// Secret with DPAPI before it writes the file.
	Username string `json:"username"`
	Secret   string `json:"secret"`
	// Outbound is the complete Xray outbound object, as JSON text, for the
	// V2Ray-family kinds and KindXray. It holds credentials, so the store
	// seals it like Secret.
	Outbound string `json:"outbound"`
}

// Normalize trims the fields where surrounding spaces are never meant.
// Secret is kept exactly as entered: a password may contain spaces.
func (p Proxy) Normalize() Proxy {
	p.Schema = ProxySchema
	p.Name = strings.TrimSpace(p.Name)
	p.Server = normalizeHost(p.Server)
	p.Username = strings.TrimSpace(p.Username)
	p.Outbound = strings.TrimSpace(p.Outbound)
	return p
}

// Validate reports every field that cannot be saved as is, as FieldErrors.
func (p Proxy) Validate() error {
	var e FieldErrors
	if !ValidID(p.ID) {
		e.add("id", CodeInvalid)
	}
	switch p.Kind {
	case KindDirect:
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
		e.outbound(p.Outbound)
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
// Xray engine needs. The engine reports anything else it rejects.
func (e *FieldErrors) outbound(s string) {
	switch {
	case s == "":
		e.add("outbound", CodeRequired)
	case len(s) > MaxOutboundLen:
		e.add("outbound", CodeTooLong)
	default:
		var v struct {
			Protocol string `json:"protocol"`
		}
		// Unmarshalling into a struct rejects arrays, strings and numbers.
		if err := json.Unmarshal([]byte(s), &v); err != nil || v.Protocol == "" {
			e.add("outbound", CodeInvalid)
		}
	}
}
