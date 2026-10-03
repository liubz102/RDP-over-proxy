package sharelink

import (
	"encoding/base64"
	"strings"
	"unicode/utf8"

	"github.com/liubz102/RDP-over-proxy/internal/model"
)

// parseShadowsocks reads SIP002 links, ss://<user info>@host:port, whose
// user info is "method:password" in base64 or, for the 2022 methods,
// percent-encoded; and the older form, where all of
// "method:password@host:port" is base64.
func parseShadowsocks(rest string) (result, error) {
	var r result
	body, query, _ := strings.Cut(rest, "?")
	body = strings.TrimSuffix(body, "/")
	var method, password, server string
	if i := strings.LastIndex(body, "@"); i >= 0 {
		user := body[:i]
		server = body[i+1:]
		// Method names have no ':', so the first one, escaped or not,
		// ends the method.
		if m, pw, ok := decodedPair(user); ok {
			method, password = m, pw
		} else if m, pw, ok := strings.Cut(unescape(user), ":"); ok {
			method, password = m, pw
		} else {
			return r, invalid("no method and password")
		}
	} else {
		data, ok := decodeBase64(unescape(body))
		if !ok || !utf8.Valid(data) {
			return r, invalid("neither SIP002 nor base64")
		}
		s := string(data)
		i := strings.LastIndex(s, "@")
		if i < 0 {
			return r, invalid("no server")
		}
		var hasPassword bool
		method, password, hasPassword = strings.Cut(s[:i], ":")
		if !hasPassword {
			return r, invalid("no method and password")
		}
		server = s[i+1:]
	}
	host, port, err := hostPort(server, 0)
	if err != nil {
		return r, err
	}
	q := parseParams(query)
	if plugin := q.get("plugin"); plugin != "" {
		// SIP003 plugins (obfs-local, v2ray-plugin) run as programs of their
		// own, which Xray cannot do.
		name, _, _ := strings.Cut(plugin, ";")
		return r, unsupported("plugin=" + name)
	}
	r.proxy = model.Proxy{Kind: model.KindShadowsocks, Server: host, Port: port, Secret: password,
		Options: model.ProxyOptions{Cipher: method}}
	r.ignored = q.unused()
	return r, nil
}

// decodedPair reads "a:b" in base64, as v2rayN and SIP002 write user info.
func decodedPair(s string) (a, b string, ok bool) {
	data, ok := decodeBase64(unescape(s))
	if !ok || !utf8.Valid(data) {
		return "", "", false
	}
	return strings.Cut(string(data), ":")
}

// formatShadowsocks writes a SIP002 link. The 2022 methods' user info is
// percent-encoded, as SIP022 asks; the others' is base64.
func formatShadowsocks(p model.Proxy) string {
	method := p.Options.Cipher
	var user string
	if strings.HasPrefix(method, "2022-") {
		user = escape(method) + ":" + escape(p.Secret)
	} else {
		user = base64.RawURLEncoding.EncodeToString([]byte(method + ":" + p.Secret))
	}
	return "ss://" + user + "@" + joinHostPort(p.Server, p.Port)
}
