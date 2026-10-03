// Package sharelink reads and writes proxy share links: the vmess://,
// vless://, trojan://, ss://, hysteria2://, socks:// and http:// links that
// clients such as v2rayN export and import.
//
// Links are read here rather than by a library: libXray rejects v2rayN's
// VMess links and is tied to Xray's pre-releases. The parameters follow the
// share-link format of XTLS/Xray-core discussion #716, v2rayN's VMess JSON,
// SIP002 for Shadowsocks and Hysteria2's own URI scheme.
package sharelink

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/model"
)

// Note is something about a link the user should know: a parameter the
// proxy does without, or one that changes what it does. The UI translates
// "linkNotes.<code>" with Args filled in.
type Note struct {
	Code string            `json:"code"`
	Args map[string]string `json:"args,omitempty"`
}

// Note codes.
const (
	// NoteInsecure: the link asks to skip verifying the server's
	// certificate, which Xray no longer does. The certificate is verified;
	// a self-signed one needs its hash (ProxyOptions.PinnedCerts).
	NoteInsecure = "insecure"
	// NoteAlterID: a VMess alterId other than 0 ("value"). Xray speaks only
	// VMess AEAD, which servers accept unless they still require the old
	// authentication.
	NoteAlterID = "alterId"
	// NoteIgnored: parameters the proxy does without ("params").
	NoteIgnored = "ignored"
	// NotePorts: the link gives several ports (Hysteria2 port hopping); the
	// proxy uses the first one ("ports", "port").
	NotePorts = "ports"
)

// Notes lists every note code, for checking that the UI translates them.
var Notes = []string{NoteInsecure, NoteAlterID, NoteIgnored, NotePorts}

// Errors. ErrUnsupported and ErrTransportRemoved carry args ("what",
// "network") that the UI fills into their messages.
var (
	ErrInvalid = errcode.New("link.invalid", "this is not a valid share link")
	// ErrUnsupported: a scheme, plugin or setting the app cannot use.
	ErrUnsupported = errcode.New("link.unsupported", "the share link uses something this app does not support")
	// ErrTransportRemoved: HTTP/2 or QUIC, which Xray no longer has.
	ErrTransportRemoved = errcode.New("link.transportRemoved", "Xray no longer has this transport")
	// ErrNotShareable: a custom Xray outbound has no share link.
	ErrNotShareable = errcode.New("link.notShareable", "this proxy cannot be written as a share link")
)

// maxLinkLen is far more than any link needs; a VLESS Encryption key or
// XHTTP's extra settings make the longest ones a few kilobytes.
const maxLinkLen = 128 << 10

func unsupported(what string) error {
	return errcode.WithArgs(fmt.Errorf("%w: %s", ErrUnsupported, what), map[string]string{"what": what})
}

func invalid(why string) error { return fmt.Errorf("%w: %s", ErrInvalid, why) }

// Parse reads one share link. The proxy has no ID, and its name is the
// link's name, or else its server. It is normalized but not validated:
// what the link leaves out or gets wrong shows when the proxy is saved.
func Parse(link string) (model.Proxy, []Note, error) {
	link = strings.TrimSpace(link)
	if len(link) > maxLinkLen {
		return model.Proxy{}, nil, invalid("too long")
	}
	scheme, rest, ok := strings.Cut(link, "://")
	if !ok || scheme == "" {
		return model.Proxy{}, nil, invalid("no scheme")
	}
	// The name is the fragment. It is cut off first: names are often not
	// escaped, and may hold anything.
	rest, fragment, _ := strings.Cut(rest, "#")
	name := unescape(fragment)

	var r result
	var err error
	switch strings.ToLower(scheme) {
	case "vmess":
		r, err = parseVMess(rest)
	case "vless":
		r, err = parseURI(model.KindVLESS, rest)
	case "trojan":
		r, err = parseURI(model.KindTrojan, rest)
	case "ss":
		r, err = parseShadowsocks(rest)
	case "hysteria2", "hy2":
		r, err = parseHysteria2(rest)
	case "socks", "socks5":
		r, err = parseAccount(model.KindSocks, rest)
	case "http":
		r, err = parseAccount(model.KindHTTP, rest)
	default:
		return model.Proxy{}, nil, unsupported(strings.ToLower(scheme) + "://")
	}
	if err != nil {
		return model.Proxy{}, nil, err
	}
	p := r.proxy
	if name != "" {
		p.Name = name
	} else if p.Name == "" {
		p.Name = p.Server
	}
	if len(r.ignored) > 0 {
		slices.Sort(r.ignored)
		r.notes = append(r.notes, Note{Code: NoteIgnored, Args: map[string]string{"params": strings.Join(slices.Compact(r.ignored), ", ")}})
	}
	return p.Normalize(), r.notes, nil
}

// result is what a scheme's parser found.
type result struct {
	proxy   model.Proxy
	notes   []Note
	ignored []string // parameter names
}

func (r *result) note(code string, args map[string]string) {
	r.notes = append(r.notes, Note{Code: code, Args: args})
}

// Format writes p as a share link, credentials included.
func Format(p model.Proxy) (string, error) {
	var link string
	named := false
	switch p.Kind {
	case model.KindVMess:
		link, named = formatVMess(p)
	case model.KindVLESS, model.KindTrojan:
		link = formatURI(p)
	case model.KindShadowsocks:
		link = formatShadowsocks(p)
	case model.KindHysteria2:
		link = formatHysteria2(p)
	case model.KindSocks, model.KindHTTP:
		link = formatAccount(p)
	default:
		return "", ErrNotShareable
	}
	if p.Name != "" && !named {
		link += "#" + escape(p.Name)
	}
	return link, nil
}

// authority splits "userinfo@host:port/path?query" into its parts. The
// user info ends at the last "@": passwords are not always escaped.
type authority struct {
	user     string // still escaped
	hostPort string
	query    string
}

func splitAuthority(rest string) authority {
	var a authority
	rest, a.query, _ = strings.Cut(rest, "?")
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		a.user, rest = rest[:i], rest[i+1:]
	}
	// A path after the host ("host:port/") says nothing in share links.
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	a.hostPort = rest
	return a
}

// hostPort reads "host:port", "[IPv6]:port" or, when defaultPort is above
// zero, a host alone.
func hostPort(s string, defaultPort int) (string, int, error) {
	t, err := model.ParseTarget(s, defaultPort)
	if err != nil || t.Port == 0 {
		return "", 0, invalid("no server address and port")
	}
	return t.Host, t.Port, nil
}

// params are a link's query parameters in their order, unescaped. A "+"
// stays a "+": the values are often base64, and links escape spaces as %20.
type params struct {
	keys   []string
	values map[string]string
	used   map[string]bool
}

func parseParams(query string) *params {
	p := &params{values: map[string]string{}, used: map[string]bool{}}
	for _, pair := range strings.Split(query, "&") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		k = unescape(k)
		if _, seen := p.values[k]; !seen {
			p.keys = append(p.keys, k)
		}
		p.values[k] = unescape(v)
	}
	return p
}

// get returns a parameter and marks it as used.
func (p *params) get(key string) string {
	p.used[key] = true
	return p.values[key]
}

// flag reports whether a parameter is set to a true value.
func (p *params) flag(key string) bool {
	switch strings.ToLower(p.get(key)) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// unused lists the parameters that nothing asked for.
func (p *params) unused() []string {
	var out []string
	for _, k := range p.keys {
		if !p.used[k] {
			out = append(out, k)
		}
	}
	return out
}

// query builds a link's parameters in a fixed order, leaving out empty ones.
type query []string

func (q *query) add(key, value string) {
	if value != "" {
		*q = append(*q, key+"="+escape(value))
	}
}

func (q query) String() string {
	if len(q) == 0 {
		return ""
	}
	return "?" + strings.Join(q, "&")
}

// escape escapes like JavaScript's encodeURIComponent, as the share-link
// format asks.
func escape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// unescape undoes percent-encoding, and leaves malformed text as it is.
func unescape(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

// decodeBase64 reads base64 in either alphabet, with or without padding and
// line breaks, as links carry it.
func decodeBase64(s string) ([]byte, bool) {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', ' ', '\t':
			return -1
		case '-':
			return '+'
		case '_':
			return '/'
		}
		return r
	}, s)
	s = strings.TrimRight(s, "=")
	b, err := base64.RawStdEncoding.DecodeString(s)
	return b, err == nil
}

// joinHostPort writes a server for a link, with brackets around IPv6.
func joinHostPort(host string, port int) string {
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return host + ":" + strconv.Itoa(port)
}
