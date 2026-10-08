// Package sysproxy follows Windows' proxy settings for one connection: the
// proxy server the current user's Internet settings use to reach a
// computer, or none. It is what the built-in "follow the system proxy"
// entry (model.KindSystem) does.
//
// Decide, Pick and Bypassed make no system calls; Decide asks Windows
// through the Windows interface (System is the real one).
package sysproxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"unicode"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/model"
)

// Errors.
var (
	// ErrUnreadable: Windows' proxy settings could not be read. A
	// connection that follows them does not go directly instead: that
	// could be what the user set a proxy to avoid.
	ErrUnreadable = errcode.New("sysproxy.unreadable", "Windows' proxy settings could not be read")
	// ErrUnusable: the manual proxy setting names no server the app can go
	// through (only an FTP proxy, say).
	ErrUnusable = errcode.New("sysproxy.unusable", "Windows' proxy setting names no proxy server this app can use")
	// ErrNothingDetected: automatic detection found no configuration on the
	// network. Windows' own programs then go by the manual setting, and so
	// does Decide, without remark: it is what most networks answer.
	ErrNothingDetected = errors.New("no automatic proxy configuration was found on the network")
	// ErrNoUsableProxy: the automatic configuration names only proxies this
	// app cannot go through (HTTPS proxies, which are spoken to over TLS).
	ErrNoUsableProxy = errcode.New("sysproxy.noUsableProxy", "the automatic configuration names no proxy server this app can use")
)

// Codes of why the automatic configuration gave no answer
// (Decision.ConfigCode). Windows labels its errors with them (System);
// CodeConfigFailed is for any other.
const (
	CodeScriptUnavailable = "sysproxy.scriptUnavailable"
	CodeScriptFailed      = "sysproxy.scriptFailed"
	CodeConfigService     = "sysproxy.configService"
	CodeNotDetected       = "sysproxy.notDetected"
	CodeConfigFailed      = "sysproxy.configFailed"
)

func init() {
	errcode.Declare(CodeScriptUnavailable, CodeScriptFailed, CodeConfigService, CodeNotDetected, CodeConfigFailed)
}

// Settings are the current user's Internet proxy settings.
type Settings struct {
	// AutoDetect: "Automatically detect settings" (WPAD).
	AutoDetect bool `json:"autoDetect"`
	// Script is the setup script's address ("Use setup script"), or "".
	Script string `json:"script"`
	// Proxy is the manual proxy server while it is on, as Windows keeps it:
	// "host:port" for every protocol, or "http=host:port;https=…;socks=…";
	// else "".
	Proxy string `json:"proxy"`
	// Bypass lists the addresses the manual proxy is not used for:
	// "localhost;127.*;*.example.com;<local>".
	Bypass string `json:"bypass"`
}

// Server is a proxy server: model.KindHTTP, which tunnels with CONNECT, or
// model.KindSocks.
type Server struct {
	Kind string `json:"kind"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

// Address is the server's host:port.
func (s Server) Address() string { return net.JoinHostPort(s.Host, strconv.Itoa(s.Port)) }

// Entry is one of the ways the automatic configuration names for an
// address, as Windows reads it.
type Entry struct {
	// Direct: connect directly.
	Direct bool
	// Scheme is how the proxy is spoken to: "http" (PROXY), "https" (over
	// TLS) or "socks"; anything else Windows does not name.
	Scheme string
	Host   string
	Port   int
}

// Windows is what Decide asks Windows.
type Windows interface {
	// Settings reads the current user's Internet proxy settings.
	Settings() (Settings, error)
	// Configure runs the automatic configuration for url: detection when
	// autoDetect, then the setup script at script when it is set. It
	// returns the ways the configuration names, in its order;
	// ErrNothingDetected when detection alone was asked and found nothing.
	// It takes as long as the network does; cancelling ctx ends it.
	Configure(ctx context.Context, url string, autoDetect bool, script string) ([]Entry, error)
}

// How Decide decided.
const (
	// ByConfig: the automatic configuration (detection or setup script).
	ByConfig = "config"
	// ByManual: the manual proxy server.
	ByManual = "manual"
	// ByBypass: the target is one of the manual proxy's exceptions.
	ByBypass = "bypass"
	// ByNone: no proxy is set.
	ByNone = "none"
)

// Decision is how a connection to a target goes.
type Decision struct {
	// Server is the proxy to go through; nil to connect directly.
	Server *Server `json:"server"`
	By     string  `json:"by"`
	// ConfigError is why the automatic configuration gave no answer, when
	// it was asked and then the manual setting decided, as it does for
	// Windows' own programs; ConfigCode is its errcode code. Nothing found
	// by detection is no error.
	ConfigError string `json:"configError,omitempty"`
	ConfigCode  string `json:"configCode,omitempty"`
}

// Decide works out how Windows' own programs reach target ("host:port"):
// the first way the automatic configuration names for it (detection first,
// then the setup script) that the app can take, when one is on and
// answers; else through the manual proxy server, unless target is one of
// its exceptions; else directly. Running the automatic configuration takes
// as long as the network does; cancelling ctx ends it, and Decide then
// returns ctx's error.
func Decide(ctx context.Context, w Windows, target string) (Decision, error) {
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		return Decision{}, err
	}
	s, err := w.Settings()
	if err != nil {
		return Decision{}, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	var d Decision
	if s.AutoDetect || s.Script != "" {
		// A tunnel is what HTTPS asks a proxy for (CONNECT), so the
		// configuration is asked as for an https address.
		entries, err := w.Configure(ctx, "https://"+target+"/", s.AutoDetect, s.Script)
		if ctx.Err() != nil {
			return Decision{}, ctx.Err()
		}
		if err == nil {
			if c, ok := configured(entries); ok {
				return c, nil
			}
			err = ErrNoUsableProxy
		}
		if !errors.Is(err, ErrNothingDetected) {
			d.ConfigError, d.ConfigCode = err.Error(), errcode.Of(err)
			if d.ConfigCode == errcode.Unknown {
				d.ConfigCode = CodeConfigFailed
			}
		}
	}
	switch {
	case s.Proxy == "":
		d.By = ByNone
	case Bypassed(s.Bypass, host):
		d.By = ByBypass
	default:
		srv, ok := Pick(s.Proxy)
		if !ok {
			// Without the setting: it names servers, and this text is
			// logged.
			return Decision{}, ErrUnusable
		}
		d.Server, d.By = &srv, ByManual
	}
	return d, nil
}

// configured is the first of the ways the automatic configuration named
// that the app can take; none of them when ok is false. Naming nothing
// means connecting directly.
func configured(entries []Entry) (d Decision, ok bool) {
	if len(entries) == 0 {
		return Decision{By: ByConfig}, true
	}
	for _, e := range entries {
		if e.Direct {
			return Decision{By: ByConfig}, true
		}
		if srv, ok := configServer(e); ok {
			return Decision{Server: &srv, By: ByConfig}, true
		}
	}
	return Decision{}, false
}

// configServer reads a proxy the automatic configuration named: a PROXY
// (HTTP) or SOCKS server. An HTTPS proxy is spoken to over TLS, which the
// app's HTTP proxies are not; it and anything else are skipped.
func configServer(e Entry) (Server, bool) {
	host := strings.Trim(e.Host, "[]")
	var kind string
	switch e.Scheme {
	case "http":
		kind = model.KindHTTP
	case "socks":
		kind = model.KindSocks
		// Setup scripts written for browsers name "SOCKS5 host:port", which
		// Windows 10 reads as a SOCKS server called "5 host". Windows Server
		// 2025 leaves such an entry out, so there is nothing to read back
		// there; a script that also names "SOCKS host:port" still gets
		// through.
		host = strings.TrimPrefix(host, "5 ")
	default:
		return Server{}, false
	}
	if host == "" || strings.ContainsFunc(host, unicode.IsSpace) || e.Port <= 0 || e.Port > 65535 {
		return Server{}, false
	}
	return Server{Kind: kind, Host: host, Port: e.Port}, true
}

// Pick reads a proxy list the way Windows writes it, in the manual setting
// ("host:port", or "http=host:port;https=host:port;socks=host:port") or as
// the automatic configuration answers ("host:port;host:port"), and picks
// the server a tunnel goes through: the one for https, which tunnels with
// CONNECT; else the one for every protocol; else the one for http; else a
// SOCKS server, spoken to in SOCKS5 (Windows itself speaks SOCKS4 to it,
// which SOCKS5 servers commonly speak too). Within each, the first counts.
func Pick(list string) (Server, bool) {
	order := map[string]int{"https": 0, "": 1, "http": 2, "socks": 3}
	var best Server
	rank := len(order)
	for _, e := range strings.FieldsFunc(list, func(r rune) bool { return r == ';' || unicode.IsSpace(r) }) {
		scheme, value, ok := strings.Cut(e, "=")
		if !ok {
			scheme, value = "", e
		}
		scheme = strings.ToLower(scheme)
		r, ok := order[scheme]
		if !ok || r >= rank {
			continue
		}
		kind := model.KindHTTP
		if scheme == "socks" {
			kind = model.KindSocks
		}
		srv, ok := server(kind, value)
		if !ok {
			continue
		}
		best, rank = srv, r
	}
	return best, rank < len(order)
}

// server reads "host:port", or "scheme://host:port" as some programs write
// it, where a socks scheme makes it a SOCKS server. An https scheme means a
// proxy spoken to over TLS, which the app's HTTP proxies are not.
func server(kind, value string) (Server, bool) {
	if scheme, rest, ok := strings.Cut(value, "://"); ok {
		switch scheme = strings.ToLower(scheme); {
		case strings.HasPrefix(scheme, "socks"):
			kind = model.KindSocks
		case scheme != "http":
			return Server{}, false
		}
		value = rest
	}
	host, portText, err := net.SplitHostPort(strings.TrimSuffix(value, "/"))
	if err != nil || host == "" {
		return Server{}, false
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return Server{}, false
	}
	return Server{Kind: kind, Host: host, Port: int(port)}, true
}

// Bypassed reports whether host is one of the exceptions in bypass, the way
// Windows matches them: entries separated by semicolons, compared without
// regard to case, "*" standing for any characters, and "<local>" for names
// without a dot. The address as written is compared; nothing is looked up.
func Bypassed(bypass, host string) bool {
	host = strings.ToLower(strings.Trim(host, "[]"))
	for _, e := range strings.FieldsFunc(bypass, func(r rune) bool { return r == ';' || unicode.IsSpace(r) }) {
		e = strings.ToLower(e)
		if e == "<local>" {
			if !strings.ContainsAny(host, ".:") {
				return true
			}
			continue
		}
		if _, rest, ok := strings.Cut(e, "://"); ok {
			e = rest
		}
		switch {
		case strings.HasPrefix(e, "["):
			// [IPv6] with or without a port.
			if end := strings.Index(e, "]"); end > 0 {
				e = e[1:end]
			}
		case strings.Count(e, ":") == 1:
			e, _, _ = strings.Cut(e, ":") // a port, which a tunnel's address has no say in
		}
		if wildcard(e, host) {
			return true
		}
	}
	return false
}

// wildcard reports whether s matches pattern, where "*" stands for any run
// of characters.
func wildcard(pattern, s string) bool {
	// The classic two-pointer match: on a mismatch, go back to the last
	// star and let it take one more character.
	p, i, star, mark := 0, 0, -1, 0
	for i < len(s) {
		switch {
		case p < len(pattern) && pattern[p] == '*':
			star, mark = p, i
			p++
		case p < len(pattern) && pattern[p] == s[i]:
			p++
			i++
		case star >= 0:
			p = star + 1
			mark++
			i = mark
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}
