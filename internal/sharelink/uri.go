package sharelink

import (
	"strings"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/model"
)

// parseURI reads the share-link format of XTLS/Xray-core discussion #716,
// which VLESS, Trojan and VMess in its standard form share:
// scheme://<user ID or password>@host:port?<parameters>#name.
func parseURI(kind, rest string) (result, error) {
	var r result
	a := splitAuthority(rest)
	defaultPort := 0
	if kind == model.KindTrojan {
		defaultPort = 443
	}
	host, port, err := hostPort(a.hostPort, defaultPort)
	if err != nil {
		return r, err
	}
	r.proxy = model.Proxy{Kind: kind, Server: host, Port: port, Secret: unescape(a.user)}
	q := parseParams(a.query)
	o := &r.proxy.Options
	switch kind {
	case model.KindVLESS:
		o.Encryption = q.get("encryption")
		o.Flow = q.get("flow")
	case model.KindVMess:
		o.Cipher = q.get("encryption")
	}
	if err := readStream(q, o, &r); err != nil {
		return r, err
	}
	// Trojan runs over TLS unless the link says otherwise.
	if kind == model.KindTrojan && q.values["security"] == "" {
		o.Security = model.SecurityTLS
	}
	r.ignored = append(r.ignored, q.unused()...)
	return r, nil
}

// readStream reads the transport and security parameters.
func readStream(q *params, o *model.ProxyOptions, r *result) error {
	o.Network = strings.ToLower(q.get("type"))
	if err := checkNetwork(o.Network); err != nil {
		return err
	}
	o.HeaderType = q.get("headerType")
	o.Host = q.get("host")
	o.Path = q.get("path")
	o.ServiceName = q.get("serviceName")
	o.Authority = q.get("authority")
	o.Mode = q.get("mode")
	o.Seed = q.get("seed")
	o.Extra = q.get("extra")
	o.FinalMask = q.get("fm")

	o.Security = strings.ToLower(q.get("security"))
	if err := checkSecurity(o.Security); err != nil {
		return err
	}
	o.SNI = q.get("sni")
	if peer := q.get("peer"); o.SNI == "" {
		o.SNI = peer // what older Trojan links call it
	}
	o.ALPN = q.get("alpn")
	o.Fingerprint = q.get("fp")
	o.PinnedCerts = q.get("pcs")
	o.VerifyNames = q.get("vcn")
	o.ECH = q.get("ech")
	o.PublicKey = q.get("pbk")
	o.ShortID = q.get("sid")
	o.SpiderX = q.get("spx")
	o.MLDSA65Verify = q.get("pqv")
	// Skipping verification means something to TLS only; REALITY has no
	// certificate to check.
	if (q.flag("allowInsecure") || q.flag("insecure")) && o.Security == model.SecurityTLS {
		r.note(NoteInsecure, nil)
	}
	return nil
}

// checkNetwork refuses the transports Xray no longer has.
func checkNetwork(network string) error {
	switch network {
	case "http", "h2", "h3", "quic":
		return errcode.WithArgs(ErrTransportRemoved, map[string]string{"network": network})
	}
	return nil
}

// checkSecurity refuses security layers Xray no longer has, such as the
// old XTLS.
func checkSecurity(security string) error {
	switch security {
	case "", model.SecurityNone, model.SecurityTLS, model.SecurityREALITY:
		return nil
	}
	return unsupported("security=" + security)
}

// formatURI writes VLESS and Trojan links, and VMess ones that v2rayN's
// format cannot hold.
func formatURI(p model.Proxy) string {
	o := p.Options
	var q query
	scheme := p.Kind
	switch p.Kind {
	case model.KindVLESS:
		q.add("encryption", o.Encryption)
		q.add("flow", o.Flow)
	case model.KindVMess:
		q.add("encryption", o.Cipher)
	}
	writeStream(&q, o)
	return scheme + "://" + escape(p.Secret) + "@" + joinHostPort(p.Server, p.Port) + q.String()
}

// writeStream adds the transport and security parameters.
func writeStream(q *query, o model.ProxyOptions) {
	q.add("type", o.Network)
	switch o.Network {
	case model.NetworkTCP:
		if o.HeaderType != model.HeaderNone {
			q.add("headerType", o.HeaderType)
		}
		q.add("host", o.Host)
		q.add("path", o.Path)
	case model.NetworkWS, model.NetworkHTTPUpgrade:
		q.add("host", o.Host)
		q.add("path", o.Path)
	case model.NetworkGRPC:
		q.add("serviceName", o.ServiceName)
		q.add("authority", o.Authority)
		q.add("mode", o.Mode)
	case model.NetworkXHTTP:
		q.add("host", o.Host)
		q.add("path", o.Path)
		q.add("mode", o.Mode)
		q.add("extra", o.Extra)
	case model.NetworkKCP:
		if o.HeaderType != model.HeaderNone {
			q.add("headerType", o.HeaderType)
		}
		q.add("host", o.Host)
		q.add("seed", o.Seed)
	}
	q.add("fm", o.FinalMask)
	q.add("security", o.Security)
	switch o.Security {
	case model.SecurityTLS:
		q.add("sni", o.SNI)
		q.add("alpn", o.ALPN)
		q.add("fp", o.Fingerprint)
		q.add("pcs", o.PinnedCerts)
		q.add("vcn", o.VerifyNames)
		q.add("ech", o.ECH)
	case model.SecurityREALITY:
		q.add("sni", o.SNI)
		q.add("fp", o.Fingerprint)
		q.add("pbk", o.PublicKey)
		q.add("sid", o.ShortID)
		q.add("spx", o.SpiderX)
		q.add("pqv", o.MLDSA65Verify)
	}
}
