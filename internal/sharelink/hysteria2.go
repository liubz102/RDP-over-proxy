package sharelink

import (
	"strconv"
	"strings"

	"github.com/liubz102/RDP-over-proxy/internal/model"
)

// parseHysteria2 reads Hysteria2's URI scheme:
// hysteria2://<auth>@host[:port][/]?sni=…&obfs=salamander&obfs-password=…&pinSHA256=…
// The port defaults to 443, and may be a list ("443,20000-30000") for port
// hopping, which this app does without: it uses the first port.
func parseHysteria2(rest string) (result, error) {
	var r result
	a := splitAuthority(rest)
	host, ports := a.hostPort, ""
	if strings.HasPrefix(host, "[") {
		if end := strings.IndexByte(host, ']'); end >= 0 {
			host, ports = host[1:end], strings.TrimPrefix(host[end+1:], ":")
		}
	} else if i := strings.LastIndexByte(host, ':'); i >= 0 && strings.Count(host, ":") == 1 {
		host, ports = host[:i], host[i+1:]
	}
	if !model.ValidHost(host) {
		return r, invalid("no valid server address")
	}
	port := 443
	if ports != "" {
		first, _, _ := strings.Cut(ports, ",")
		first, _, _ = strings.Cut(first, "-")
		n, ok := parsePort(first)
		if !ok {
			return r, invalid("no valid port")
		}
		port = n
		if first != ports {
			r.note(NotePorts, map[string]string{"ports": ports, "port": strconv.Itoa(port)})
		}
	}
	q := parseParams(a.query)
	r.proxy = model.Proxy{Kind: model.KindHysteria2, Server: host, Port: port, Secret: unescape(a.user)}
	o := &r.proxy.Options
	o.SNI = q.get("sni")
	o.ALPN = q.get("alpn")
	o.PinnedCerts = q.get("pinSHA256")
	o.VerifyNames = q.get("vcn") // as in the other links
	switch obfs := strings.ToLower(q.get("obfs")); obfs {
	case "", "none":
	case "salamander":
		o.ObfsPassword = q.get("obfs-password")
	default:
		return r, unsupported("obfs=" + obfs)
	}
	if q.flag("insecure") {
		r.note(NoteInsecure, nil)
	}
	if mport := q.get("mport"); mport != "" {
		r.note(NotePorts, map[string]string{"ports": mport, "port": strconv.Itoa(port)})
	}
	r.ignored = q.unused()
	return r, nil
}

func formatHysteria2(p model.Proxy) string {
	o := p.Options
	var q query
	q.add("sni", o.SNI)
	q.add("alpn", o.ALPN)
	if o.ObfsPassword != "" {
		q.add("obfs", "salamander")
		q.add("obfs-password", o.ObfsPassword)
	}
	q.add("pinSHA256", o.PinnedCerts)
	q.add("vcn", o.VerifyNames)
	return "hysteria2://" + escape(p.Secret) + "@" + joinHostPort(p.Server, p.Port) + "/" + q.String()
}
