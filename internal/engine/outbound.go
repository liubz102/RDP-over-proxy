package engine

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/xtls/xray-core/common/utils"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/route"
)

// ErrConfig: Xray rejected a proxy's settings. The message says why, in
// Xray's words.
var ErrConfig = errcode.New("proxy.config", "Xray rejected the proxy's settings")

// quicKeepAlive is how often, in seconds, a Hysteria2 connection pings an
// idle server. QUIC closes a connection that stays silent for its idle
// timeout (30 seconds in Xray), which would cut a remote desktop that shows
// nothing new, and NAT routers forget idle UDP mappings even sooner. Nothing
// happens on an idle connection that could be waited for, so this is a
// keep-alive heartbeat, one of the exceptions to the event-driven rule
// registered in CLAUDE.md. Ten seconds is Hysteria's own default.
const quicKeepAlive = 10

// kcpHeaderMasks are Xray's masks for mKCP's disguises, which Xray moved
// from mKCP's settings into "finalmask".
var kcpHeaderMasks = map[string]string{
	"srtp":         "header-srtp",
	"utp":          "header-utp",
	"wechat-video": "header-wechat",
	"dtls":         "header-dtls",
	"wireguard":    "header-wireguard",
	"dns":          "header-dns",
}

// Outbound returns the Xray outbound object, as JSON, for proxy p. A proxy
// that does not pass validation never reaches Xray: some of what validation
// keeps out, Xray would accept and then crash on (see model/masks.go).
func Outbound(p model.Proxy) (string, error) {
	if !slices.Contains(model.Kinds, p.Kind) {
		return "", fmt.Errorf("%w: %s", route.ErrUnsupported, p.Kind)
	}
	p = p.Normalize()
	if err := p.Validate(); err != nil {
		return "", fmt.Errorf("%w: %w", ErrConfig, err)
	}
	o, err := outboundObject(p)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(o)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrConfig, err)
	}
	return string(b), nil
}

// Check reports whether Xray accepts proxy p, so that settings Xray rejects
// show when the proxy is saved rather than when a session first uses it. It
// builds the outbound and adds it to the instance under a tag of its own,
// for what Xray finds only then (a VLESS Encryption's padding, for one),
// and removes it again. Nothing connects.
func (e *Engine) Check(p model.Proxy) error {
	if p.Kind == model.KindDirect {
		return nil
	}
	ob, err := Outbound(p)
	if err != nil {
		return err
	}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return ErrClosed
	}
	e.seq++
	tag := fmt.Sprintf("check-%d", e.seq)
	e.mu.Unlock()
	if err := e.add(ob, tag); err != nil {
		return err
	}
	e.remove(tag)
	return nil
}

// build turns an outbound's JSON into Xray's form, under tag.
func build(outboundJSON, tag string) (built *core.OutboundHandlerConfig, err error) {
	defer recoverConfig(&err)
	var c conf.OutboundDetourConfig
	if err := json.Unmarshal([]byte(outboundJSON), &c); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfig, err)
	}
	c.Tag = tag
	if built, err = c.Build(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfig, err)
	}
	return built, nil
}

// recoverConfig turns a panic in Xray's handling of a configuration into
// ErrConfig. Xray crashes on some malformed settings instead of rejecting
// them (a VLESS Encryption cut short, for one). Validation keeps out what is
// known; this catches the rest of what goes wrong while the outbound is
// built and added. A crash later, in one of Xray's own goroutines while it
// connects, cannot be caught here, which is why validation refuses the
// settings known to cause one.
func recoverConfig(err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%w: Xray failed on these settings: %v", ErrConfig, r)
	}
}

func outboundObject(p model.Proxy) (map[string]any, error) {
	o := p.Options
	switch p.Kind {
	case model.KindSocks, model.KindHTTP:
		settings := map[string]any{"address": p.Server, "port": p.Port}
		if p.Username != "" {
			settings["user"] = p.Username
			settings["pass"] = p.Secret
		}
		return map[string]any{"protocol": p.Kind, "settings": settings}, nil
	case model.KindShadowsocks:
		return map[string]any{"protocol": "shadowsocks", "settings": map[string]any{
			"address": p.Server, "port": p.Port, "method": o.Cipher, "password": p.Secret,
		}}, nil
	case model.KindVMess:
		return map[string]any{"protocol": "vmess", "settings": map[string]any{
			"address": p.Server, "port": p.Port, "id": p.Secret, "security": o.Cipher,
		}, "streamSettings": stream(o)}, nil
	case model.KindVLESS:
		settings := map[string]any{"address": p.Server, "port": p.Port, "id": p.Secret, "encryption": o.Encryption}
		if o.Flow != "" {
			settings["flow"] = o.Flow
		}
		return map[string]any{"protocol": "vless", "settings": settings, "streamSettings": stream(o)}, nil
	case model.KindTrojan:
		return map[string]any{"protocol": "trojan", "settings": map[string]any{
			"address": p.Server, "port": p.Port, "password": p.Secret,
		}, "streamSettings": stream(o)}, nil
	case model.KindHysteria2:
		return map[string]any{"protocol": "hysteria", "settings": map[string]any{
			"version": 2, "address": p.Server, "port": p.Port,
		}, "streamSettings": hysteriaStream(p)}, nil
	case model.KindXray:
		return custom(p.Outbound)
	}
	return nil, fmt.Errorf("%w: %s", route.ErrUnsupported, p.Kind)
}

// custom reads an outbound the user wrote. Its tag is left out: the engine
// gives every outbound a tag of its own.
func custom(text string) (map[string]any, error) {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber() // numbers stay as written
	var ob map[string]any
	if err := dec.Decode(&ob); err != nil || ob == nil {
		return nil, fmt.Errorf("%w: the outbound is not a JSON object", ErrConfig)
	}
	delete(ob, "tag")
	return ob, nil
}

// stream returns the streamSettings of VMess, VLESS and Trojan.
func stream(o model.ProxyOptions) map[string]any {
	s := map[string]any{"network": o.Network, "security": o.Security}
	switch o.Network {
	case model.NetworkTCP:
		s["network"] = "raw"
		if o.HeaderType == "http" {
			s["rawSettings"] = map[string]any{"header": httpDisguise(o)}
		}
	case model.NetworkWS:
		s["wsSettings"] = map[string]any{"host": o.Host, "path": o.Path}
	case model.NetworkHTTPUpgrade:
		s["httpupgradeSettings"] = map[string]any{"host": o.Host, "path": o.Path}
	case model.NetworkGRPC:
		s["grpcSettings"] = map[string]any{"serviceName": o.ServiceName, "authority": o.Authority, "multiMode": o.Mode == "multi"}
	case model.NetworkXHTTP:
		x := map[string]any{"host": o.Host, "path": o.Path, "mode": o.Mode}
		if o.Extra != "" {
			x["extra"] = json.RawMessage(o.Extra)
		}
		s["xhttpSettings"] = x
	case model.NetworkKCP:
		s["kcpSettings"] = map[string]any{}
		s["finalmask"] = map[string]any{"udp": kcpMasks(o)}
	}
	if o.FinalMask != "" {
		s["finalmask"] = json.RawMessage(o.FinalMask)
	}
	switch o.Security {
	case model.SecurityTLS:
		s["tlsSettings"] = tlsSettings(o)
	case model.SecurityREALITY:
		r := map[string]any{"serverName": o.SNI, "fingerprint": o.Fingerprint, "publicKey": o.PublicKey,
			"shortId": o.ShortID, "spiderX": o.SpiderX}
		if o.MLDSA65Verify != "" {
			r["mldsa65Verify"] = o.MLDSA65Verify
		}
		s["realitySettings"] = r
	}
	return s
}

// httpDisguise makes TCP look like HTTP/1.1 requests to the given hosts and
// paths. Naming the headers replaces Xray's defaults, so the usual ones of a
// browser come along.
func httpDisguise(o model.ProxyOptions) map[string]any {
	request := map[string]any{}
	if paths := split(o.Path); len(paths) > 0 {
		request["path"] = paths
	}
	if hosts := split(o.Host); len(hosts) > 0 {
		request["headers"] = map[string]any{
			"Host":            hosts,
			"User-Agent":      []string{utils.ChromeUA},
			"Accept-Encoding": []string{"gzip, deflate"},
			"Connection":      []string{"keep-alive"},
			"Pragma":          []string{"no-cache"},
		}
	}
	return map[string]any{"type": "http", "request": request}
}

// kcpMasks are mKCP's disguise and encryption, in the order they go on the
// wire: the disguise's header first, then the packet, encrypted with the
// seed or, without one, only obfuscated, as mKCP always did.
func kcpMasks(o model.ProxyOptions) []any {
	var masks []any
	if t, ok := kcpHeaderMasks[o.HeaderType]; ok {
		m := map[string]any{"type": t}
		if o.HeaderType == "dns" && o.Host != "" {
			m["settings"] = map[string]any{"domain": o.Host}
		}
		masks = append(masks, m)
	}
	if o.Seed != "" {
		masks = append(masks, map[string]any{"type": "mkcp-aes128gcm", "settings": map[string]any{"password": o.Seed}})
	} else {
		masks = append(masks, map[string]any{"type": "mkcp-original"})
	}
	return masks
}

// tlsSettings are the TLS settings of TLS proper and of Hysteria2.
func tlsSettings(o model.ProxyOptions, defaultALPN ...string) map[string]any {
	t := map[string]any{}
	if o.SNI != "" {
		t["serverName"] = o.SNI
	}
	if alpn := split(o.ALPN); len(alpn) > 0 {
		t["alpn"] = alpn
	} else if len(defaultALPN) > 0 {
		t["alpn"] = defaultALPN
	}
	if o.Fingerprint != "" {
		t["fingerprint"] = o.Fingerprint
	}
	if o.PinnedCerts != "" {
		t["pinnedPeerCertSha256"] = o.PinnedCerts
	}
	if o.VerifyNames != "" {
		t["verifyPeerCertByName"] = o.VerifyNames
	}
	if o.ECH != "" {
		t["echConfigList"] = o.ECH
	}
	return t
}

// hysteriaStream is Hysteria2's transport: QUIC with TLS, whose ALPN
// Hysteria2 servers expect to be "h3", and Salamander when there is an
// obfuscation password.
func hysteriaStream(p model.Proxy) map[string]any {
	o := p.Options
	masks := map[string]any{"quicParams": map[string]any{"keepAlivePeriod": quicKeepAlive}}
	if o.ObfsPassword != "" {
		masks["udp"] = []any{map[string]any{"type": "salamander", "settings": map[string]any{"password": o.ObfsPassword}}}
	}
	return map[string]any{
		"network":          "hysteria",
		"hysteriaSettings": map[string]any{"version": 2, "auth": p.Secret},
		"security":         "tls",
		"tlsSettings":      tlsSettings(o, "h3"),
		"finalmask":        masks,
	}
}

// split splits a comma-separated list.
func split(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
