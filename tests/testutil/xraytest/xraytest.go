// Package xraytest runs Xray-core proxy servers inside the test process, so
// tests can put a real proxy between the engine and a target: SOCKS5, HTTP,
// and the V2Ray family over each transport, with TLS or REALITY. It is a
// package of its own so that only the tests that need Xray link it.
package xraytest

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"

	// The server side: inbounds, transports, and the outbound that connects
	// to targets.
	_ "github.com/xtls/xray-core/app/dispatcher"
	_ "github.com/xtls/xray-core/app/proxyman/inbound"
	_ "github.com/xtls/xray-core/app/proxyman/outbound"
	_ "github.com/xtls/xray-core/proxy/freedom"
	_ "github.com/xtls/xray-core/proxy/http"
	_ "github.com/xtls/xray-core/proxy/hysteria"
	_ "github.com/xtls/xray-core/proxy/shadowsocks"
	_ "github.com/xtls/xray-core/proxy/shadowsocks_2022"
	_ "github.com/xtls/xray-core/proxy/socks"
	_ "github.com/xtls/xray-core/proxy/trojan"
	_ "github.com/xtls/xray-core/proxy/vless/inbound"
	_ "github.com/xtls/xray-core/proxy/vmess/inbound"
	_ "github.com/xtls/xray-core/transport/internet/grpc"
	_ "github.com/xtls/xray-core/transport/internet/httpupgrade"
	_ "github.com/xtls/xray-core/transport/internet/hysteria"
	_ "github.com/xtls/xray-core/transport/internet/kcp"
	_ "github.com/xtls/xray-core/transport/internet/reality"
	_ "github.com/xtls/xray-core/transport/internet/splithttp"
	_ "github.com/xtls/xray-core/transport/internet/tcp"
	_ "github.com/xtls/xray-core/transport/internet/tls"
	_ "github.com/xtls/xray-core/transport/internet/websocket"

	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/tests/testutil"
)

// ServerName is the name in the TLS servers' certificates, and the name the
// REALITY servers accept.
const ServerName = "proxy.example.com"

// Options configure a proxy server.
type Options struct {
	// Protocol is the kind of proxy: model.KindSocks, model.KindHTTP or one
	// of the V2Ray family (model.IsV2Ray).
	Protocol string
	// User and Pass, when User is set, are the only account a SOCKS5 or
	// HTTP server accepts.
	User, Pass string
	// Secret is the only user ID or password a V2Ray-family server accepts.
	Secret string
	// Options are the clients' settings; the server listens to match them.
	// Model fills in what the server makes up: its certificate's hash and
	// its REALITY key.
	Options model.ProxyOptions
}

// Proxy is a running proxy server on 127.0.0.1. It connects to targets
// directly, and stops when the test ends.
type Proxy struct {
	Host string
	Port int
	// CertHash is the SHA-256 hash of the TLS certificate, in hex.
	CertHash string
	// PublicKey is the REALITY public key.
	PublicKey string
}

// Start starts a proxy server for the test.
//
// Note that creating an Xray instance makes its logger the process-wide one,
// replacing the engine's log bridge. Tests that look at the engine's log
// lines must start their proxy servers before the engine.
func Start(t testing.TB, o Options) *Proxy {
	t.Helper()
	opts := model.Proxy{Kind: o.Protocol, Options: o.Options}.Normalize().Options
	px := &Proxy{Host: "127.0.0.1"}
	udp := o.Protocol == model.KindHysteria2 || (model.HasTransport(o.Protocol) && opts.Network == model.NetworkKCP)
	if udp {
		px.Port = testutil.FreeUDPPort(t)
	} else {
		px.Port = testutil.FreePort(t)
	}

	protocol := o.Protocol
	var settings map[string]any
	switch o.Protocol {
	case model.KindSocks:
		settings = map[string]any{"auth": "noauth"}
		if o.User != "" {
			settings = map[string]any{"auth": "password", "accounts": []any{map[string]string{"user": o.User, "pass": o.Pass}}}
		}
	case model.KindHTTP:
		settings = map[string]any{}
		if o.User != "" {
			settings["accounts"] = []any{map[string]string{"user": o.User, "pass": o.Pass}}
		}
	case model.KindShadowsocks:
		settings = map[string]any{"method": opts.Cipher, "password": o.Secret, "network": "tcp"}
	case model.KindVMess:
		settings = map[string]any{"clients": []any{map[string]any{"id": o.Secret}}}
	case model.KindVLESS:
		settings = map[string]any{"clients": []any{map[string]any{"id": o.Secret, "flow": opts.Flow}}, "decryption": "none"}
	case model.KindTrojan:
		settings = map[string]any{"clients": []any{map[string]any{"password": o.Secret}}}
	case model.KindHysteria2:
		protocol = "hysteria"
		settings = map[string]any{"version": 2, "clients": []any{map[string]any{"auth": o.Secret}}}
	default:
		t.Fatalf("xraytest: unknown protocol %q", o.Protocol)
	}
	inbound := map[string]any{"listen": "127.0.0.1", "port": px.Port, "protocol": protocol, "settings": settings}
	switch {
	case model.HasTransport(o.Protocol):
		inbound["streamSettings"] = px.stream(t, opts)
	case o.Protocol == model.KindHysteria2:
		s := map[string]any{
			"network":          "hysteria",
			"hysteriaSettings": map[string]any{"version": 2},
			"security":         "tls",
			"tlsSettings":      px.tls(t, "h3"),
		}
		if opts.ObfsPassword != "" {
			s["finalmask"] = map[string]any{"udp": []any{
				map[string]any{"type": "salamander", "settings": map[string]any{"password": opts.ObfsPassword}},
			}}
		}
		inbound["streamSettings"] = s
	}
	config := map[string]any{
		"log": map[string]any{"loglevel": "none"},
		// A VMess server holds on to a client it does not know until the
		// handshake time is up, by default 60 seconds; tests that try a wrong
		// user ID wait for that. Four seconds, Xray's earlier default, leaves
		// a handshake on this computer ample time.
		"policy":    map[string]any{"levels": map[string]any{"0": map[string]any{"handshake": 4}}},
		"inbounds":  []any{inbound},
		"outbounds": []any{map[string]any{"protocol": "freedom"}},
	}
	run(t, config)
	return px
}

// stream is the server's side of the transport and security of VMess,
// VLESS and Trojan, written the way a server's configuration says it.
func (px *Proxy) stream(t testing.TB, o model.ProxyOptions) map[string]any {
	s := map[string]any{"network": o.Network, "security": o.Security}
	switch o.Network {
	case model.NetworkTCP:
		s["network"] = "raw"
		if o.HeaderType == "http" {
			// The server turns away requests for paths it was not told of.
			header := map[string]any{"type": "http"}
			if o.Path != "" {
				header["request"] = map[string]any{"path": strings.Split(o.Path, ",")}
			}
			s["rawSettings"] = map[string]any{"header": header}
		}
	case model.NetworkWS:
		s["wsSettings"] = map[string]any{"path": o.Path}
	case model.NetworkHTTPUpgrade:
		s["httpupgradeSettings"] = map[string]any{"path": o.Path}
	case model.NetworkGRPC:
		s["grpcSettings"] = map[string]any{"serviceName": o.ServiceName}
	case model.NetworkXHTTP:
		s["xhttpSettings"] = map[string]any{"path": o.Path, "mode": o.Mode}
	case model.NetworkKCP:
		var masks []any
		header := map[string]string{"srtp": "header-srtp", "utp": "header-utp", "wechat-video": "header-wechat",
			"dtls": "header-dtls", "wireguard": "header-wireguard", "dns": "header-dns"}[o.HeaderType]
		if header != "" {
			masks = append(masks, map[string]any{"type": header})
		}
		if o.Seed != "" {
			masks = append(masks, map[string]any{"type": "mkcp-aes128gcm", "settings": map[string]any{"password": o.Seed}})
		} else {
			masks = append(masks, map[string]any{"type": "mkcp-original"})
		}
		s["kcpSettings"] = map[string]any{}
		s["finalmask"] = map[string]any{"udp": masks}
	}
	switch o.Security {
	case model.SecurityTLS:
		s["tlsSettings"] = px.tls(t)
	case model.SecurityREALITY:
		s["realitySettings"] = px.reality(t, o)
	}
	return s
}

// certificate makes a self-signed certificate for ServerName.
func certificate(t testing.TB) (der []byte, key *ecdsa.PrivateKey) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: ServerName},
		DNSNames:     []string{ServerName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err = x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der, key
}

// tls makes the server a certificate of its own for ServerName.
func (px *Proxy) tls(t testing.TB, alpn ...string) map[string]any {
	der, key := certificate(t)
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	px.CertHash = hex.EncodeToString(sum[:])
	lines := func(block *pem.Block) []string {
		return strings.Split(strings.TrimSpace(string(pem.EncodeToMemory(block))), "\n")
	}
	settings := map[string]any{"certificates": []any{map[string]any{
		"certificate": lines(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		"key":         lines(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	}}}
	if len(alpn) > 0 {
		settings["alpn"] = alpn
	}
	return settings
}

// reality makes the server a REALITY key, and a TLS 1.3 site for it to
// borrow handshakes from: REALITY passes anyone without the key on to it.
func (px *Proxy) reality(t testing.TB, o model.ProxyOptions) map[string]any {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	px.PublicKey = base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
	return map[string]any{
		"target":      site(t),
		"serverNames": []string{ServerName},
		"privateKey":  base64.RawURLEncoding.EncodeToString(priv.Bytes()),
		"shortIds":    []string{o.ShortID},
	}
}

// site runs a TLS 1.3 server that ends each connection once the handshake
// is done, and returns its address. A REALITY server first reads what its
// target sends after a handshake until the target closes, or for at most 5
// seconds; a site that keeps connections open makes every test wait that
// long.
func site(t testing.TB) string {
	der, key := certificate(t)
	config := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_ = tls.Server(c, config).Handshake()
			}()
		}
	}()
	return ln.Addr().String()
}

func run(t testing.TB, config map[string]any) {
	t.Helper()
	text, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := serial.LoadJSONConfig(strings.NewReader(string(text)))
	if err != nil {
		t.Fatalf("xraytest: config: %v\n%s", err, text)
	}
	instance, err := core.New(pb)
	if err != nil {
		t.Fatalf("xraytest: %v\n%s", err, text)
	}
	// Start returns once the inbound is listening.
	if err := instance.Start(); err != nil {
		t.Fatalf("xraytest: start: %v", err)
	}
	t.Cleanup(func() { instance.Close() })
}

// Model returns a proxy entry for this server: the server's account or
// secret, the clients' settings, and what the server made up (the hash of
// its certificate, its REALITY key).
func (px *Proxy) Model(id string, o Options) model.Proxy {
	p := model.Proxy{
		Schema:   model.ProxySchema,
		ID:       id,
		Name:     "Test " + o.Protocol,
		Kind:     o.Protocol,
		Server:   px.Host,
		Port:     px.Port,
		Username: o.User,
		Secret:   o.Pass,
	}
	if model.IsV2Ray(o.Protocol) {
		p.Secret = o.Secret
		p.Options = o.Options
		if px.CertHash != "" {
			p.Options.PinnedCerts = px.CertHash
		}
		if px.PublicKey != "" {
			p.Options.PublicKey = px.PublicKey
		}
		if (px.CertHash != "" || px.PublicKey != "") && p.Options.SNI == "" {
			p.Options.SNI = ServerName
		}
	}
	return p.Normalize()
}
