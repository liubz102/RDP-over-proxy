package engine

import (
	"strings"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
)

// Codes for the reasons an outbound gives up on a connection.
const (
	// CodeProxyAuth: the proxy rejected the user name and password, or
	// wanted some and got none.
	CodeProxyAuth = "proxy.auth"
	// CodeProxyUnreachable: the proxy server itself could not be reached
	// (refused, timed out, name not found).
	CodeProxyUnreachable = "proxy.unreachable"
	// CodeProxyTargetFailed: the proxy was reached and said it could not, or
	// would not, connect to the target.
	CodeProxyTargetFailed = "proxy.targetFailed"
	// CodeProxyDropped: the proxy accepted the request, then closed the
	// connection before the target answered. Xray-based servers (such as
	// v2rayN's local port) do this when they cannot reach the target.
	CodeProxyDropped = "proxy.dropped"
	// CodeProxyTLS: the proxy server's certificate did not pass: it is not
	// the pinned one, it is not valid for the name, or a REALITY server did
	// not prove it has the key.
	CodeProxyTLS = "proxy.tls"
	// CodeProxyFailed: any other reason Xray gave.
	CodeProxyFailed = "proxy.failed"
)

func init() {
	errcode.Declare(CodeProxyAuth, CodeProxyUnreachable, CodeProxyTargetFailed, CodeProxyDropped, CodeProxyTLS,
		CodeProxyFailed)
}

// classify labels the error Xray reported for a connection.
//
// Xray's errors are a chain of messages, and its retry helper turns the
// errors it collected into text, so the dial error inside is no longer a
// value that errors.As could find. The labels therefore come from Xray's
// message texts, which the engine tests check against real proxies.
func classify(err error) error {
	msg := err.Error()
	has := func(s string) bool { return strings.Contains(msg, s) }
	switch {
	// SOCKS5 user name/password sign-in refused (RFC 1929), HTTP 407, or a
	// Hysteria2 server that turned the password down.
	case has("server rejects account"), has("non 200 code: 407"), has("hysteria: auth failed"):
		return errcode.Wrap(CodeProxyAuth, err)
	// The SOCKS5 CONNECT reply was an error, or an HTTP CONNECT got another
	// status: the proxy answered but did not reach the target.
	case has("server rejects request"), has("non 200 code"):
		return errcode.Wrap(CodeProxyTargetFailed, err)
	// The certificate is not the pinned one, or does not verify; a REALITY
	// server without the key shows its target's certificate instead.
	case has("peer cert is"), has("x509:"), has("tls: failed to verify"), has("REALITY: processed invalid connection"):
		return errcode.Wrap(CodeProxyTLS, err)
	// Every attempt to connect to the proxy server failed.
	case has("failed to find an available destination"):
		return errcode.Wrap(CodeProxyUnreachable, err)
	// The proxy took the request and then ended the connection.
	case has("connection ends"):
		return errcode.Wrap(CodeProxyDropped, err)
	}
	return errcode.Wrap(CodeProxyFailed, err)
}
