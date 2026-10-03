package sharelink

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/liubz102/RDP-over-proxy/internal/model"
)

// parseVMess reads both kinds of VMess links: v2rayN's, which is base64 of
// a JSON object, and the standard one, which looks like a VLESS link.
func parseVMess(rest string) (result, error) {
	body, _, _ := strings.Cut(rest, "?")
	if data, ok := decodeBase64(unescape(body)); ok && bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		return parseVMessJSON(data)
	}
	if strings.Contains(rest, "@") {
		return parseURI(model.KindVMess, rest)
	}
	return result{}, invalid("neither v2rayN's VMess format nor the standard one")
}

// vmessKeys are the members of v2rayN's VMess JSON that this app reads.
var vmessKeys = []string{"v", "ps", "add", "port", "id", "aid", "scy", "net", "type", "host", "path", "tls",
	"sni", "alpn", "fp", "pbk", "sid", "spx", "allowInsecure", "insecure"}

// parseVMessJSON reads v2rayN's VMess format. Its members are strings, but
// some generators write numbers, so every member is read as text. What
// "type", "host" and "path" mean depends on the network:
//
//	network     type          host           path
//	tcp         header type   HTTP hosts     HTTP paths
//	kcp         header type   -              seed
//	ws, httpupgrade, xhttp      (xhttp: mode)  host  path
//	grpc        mode          authority      service name
func parseVMessJSON(data []byte) (result, error) {
	var r result
	var m map[string]flexString
	if err := json.Unmarshal(data, &m); err != nil {
		return r, invalid("v2rayN's VMess JSON: " + err.Error())
	}
	get := func(k string) string { return strings.TrimSpace(string(m[k])) }
	port, ok := parsePort(get("port"))
	if !ok {
		return r, invalid("no valid port")
	}
	r.proxy = model.Proxy{Kind: model.KindVMess, Name: get("ps"), Server: get("add"), Port: port, Secret: get("id")}
	if aid := get("aid"); aid != "" && aid != "0" {
		r.note(NoteAlterID, map[string]string{"value": aid})
	}
	o := &r.proxy.Options
	o.Cipher = get("scy")
	// What "type", "host" and "path" mean depends on the network, so its
	// other names ("mkcp", "splithttp", …) are settled first.
	o.Network = model.CanonicalNetwork(get("net"))
	if err := checkNetwork(o.Network); err != nil {
		return r, err
	}
	host, path := get("host"), get("path")
	// Version 1 put the WebSocket path after the host: "host;path".
	if v := get("v"); (v == "" || v == "1") && path == "" && o.Network == model.NetworkWS {
		host, path, _ = strings.Cut(host, ";")
	}
	switch o.Network {
	case model.NetworkGRPC:
		o.Mode, o.Authority, o.ServiceName = get("type"), host, path
	case model.NetworkKCP:
		o.HeaderType, o.Seed = get("type"), path
	case model.NetworkXHTTP:
		o.Mode, o.Host, o.Path = get("type"), host, path
	default:
		o.HeaderType, o.Host, o.Path = get("type"), host, path
	}
	switch tls := strings.ToLower(get("tls")); tls {
	case "tls", "reality":
		o.Security = tls
	case "", "none", "0", "false":
		o.Security = model.SecurityNone
	default:
		return r, unsupported("tls=" + tls)
	}
	o.SNI, o.ALPN, o.Fingerprint = get("sni"), get("alpn"), get("fp")
	o.PublicKey, o.ShortID, o.SpiderX = get("pbk"), get("sid"), get("spx")
	for _, k := range []string{"allowInsecure", "insecure"} {
		switch strings.ToLower(get(k)) {
		case "1", "true":
			if o.Security == model.SecurityTLS {
				r.note(NoteInsecure, nil)
			}
		}
	}
	for k, v := range m {
		// Generators fill in many members of their own; only those that say
		// something are worth a note.
		if !slices.Contains(vmessKeys, k) && v != "" && v != "0" && v != "false" && v != "none" {
			r.ignored = append(r.ignored, k)
		}
	}
	return r, nil
}

// flexString reads a JSON string, number, boolean or null as text.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch v := v.(type) {
	case nil:
		*f = ""
	case string:
		*f = flexString(v)
	case float64:
		*f = flexString(strconv.FormatFloat(v, 'f', -1, 64))
	case bool:
		*f = flexString(strconv.FormatBool(v))
	default:
		*f = flexString(b)
	}
	return nil
}

// parsePort reads a port number from 1 to 65535.
func parsePort(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil && n >= 1 && n <= 65535 && !strings.HasPrefix(s, "+")
}

// vmessLink is v2rayN's VMess format, in the order v2rayN writes it.
type vmessLink struct {
	V    string `json:"v"`
	PS   string `json:"ps"`
	Add  string `json:"add"`
	Port string `json:"port"`
	ID   string `json:"id"`
	Aid  string `json:"aid"`
	Scy  string `json:"scy"`
	Net  string `json:"net"`
	Type string `json:"type"`
	Host string `json:"host"`
	Path string `json:"path"`
	TLS  string `json:"tls"`
	SNI  string `json:"sni"`
	ALPN string `json:"alpn"`
	FP   string `json:"fp"`
}

// formatVMess writes v2rayN's format, which other clients read too, unless
// the proxy uses something it cannot hold. named reports whether the link
// carries the name already: v2rayN's format has it inside ("ps"), and its
// readers decode everything after "vmess://" as base64, so a "#name" after
// it would spoil the link for them.
func formatVMess(p model.Proxy) (link string, named bool) {
	o := p.Options
	if o.Security == model.SecurityREALITY || o.Extra != "" || o.FinalMask != "" || o.PinnedCerts != "" ||
		o.VerifyNames != "" || o.ECH != "" || (o.Network == model.NetworkKCP && o.Host != "") {
		return formatURI(p), false
	}
	l := vmessLink{V: "2", PS: p.Name, Add: p.Server, Port: strconv.Itoa(p.Port), ID: p.Secret, Aid: "0",
		Scy: o.Cipher, Net: o.Network, SNI: o.SNI, ALPN: o.ALPN, FP: o.Fingerprint}
	switch o.Network {
	case model.NetworkGRPC:
		l.Type, l.Host, l.Path = o.Mode, o.Authority, o.ServiceName
	case model.NetworkKCP:
		l.Type, l.Path = o.HeaderType, o.Seed
	case model.NetworkXHTTP:
		l.Type, l.Host, l.Path = o.Mode, o.Host, o.Path
	default:
		l.Type, l.Host, l.Path = o.HeaderType, o.Host, o.Path
	}
	if o.Security == model.SecurityTLS {
		l.TLS = "tls"
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(l) // a struct of strings always encodes
	return "vmess://" + base64.StdEncoding.EncodeToString(bytes.TrimSpace(b.Bytes())), true
}
