package sharelink

import (
	"encoding/base64"
	"strings"

	"github.com/liubz102/RDP-over-proxy/internal/model"
)

// parseAccount reads socks:// and http:// links. The account is either
// "user:password", percent-encoded, or that in base64, as v2rayN writes it.
func parseAccount(kind, rest string) (result, error) {
	var r result
	a := splitAuthority(rest)
	host, port, err := hostPort(a.hostPort, 0)
	if err != nil {
		return r, err
	}
	r.proxy = model.Proxy{Kind: kind, Server: host, Port: port}
	if a.user != "" {
		if user, pass, ok := strings.Cut(a.user, ":"); ok {
			r.proxy.Username, r.proxy.Secret = unescape(user), unescape(pass)
		} else if user, pass, ok := decodedPair(a.user); ok {
			r.proxy.Username, r.proxy.Secret = user, pass
		} else {
			r.proxy.Username = unescape(a.user)
		}
	}
	r.ignored = parseParams(a.query).unused()
	return r, nil
}

// formatAccount writes the account in base64, as v2rayN does.
func formatAccount(p model.Proxy) string {
	scheme := "socks"
	if p.Kind == model.KindHTTP {
		scheme = "http"
	}
	user := ""
	if p.Username != "" {
		user = base64.RawURLEncoding.EncodeToString([]byte(p.Username+":"+p.Secret)) + "@"
	}
	return scheme + "://" + user + joinHostPort(p.Server, p.Port)
}
