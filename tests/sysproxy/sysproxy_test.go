package sysproxy_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/sysproxy"

	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/model"
)

func TestPick(t *testing.T) {
	http := func(host string, port int) Server { return Server{Kind: model.KindHTTP, Host: host, Port: port} }
	socks := func(host string, port int) Server { return Server{Kind: model.KindSocks, Host: host, Port: port} }
	for _, tc := range []struct {
		list string
		want Server
		ok   bool
	}{
		{"127.0.0.1:10809", http("127.0.0.1", 10809), true},
		{"proxy.example.com:8080", http("proxy.example.com", 8080), true},
		// A tunnel is what HTTPS asks for, so its entry comes first.
		{"http=127.0.0.1:8080;https=127.0.0.1:8443;socks=127.0.0.1:1080", http("127.0.0.1", 8443), true},
		{"HTTP=127.0.0.1:8080; ftp=127.0.0.1:21", http("127.0.0.1", 8080), true},
		{"socks=127.0.0.1:1080", socks("127.0.0.1", 1080), true},
		{"ftp=127.0.0.1:21", Server{}, false},
		// The automatic configuration's answer: the first one counts.
		{"192.0.2.1:3128;192.0.2.2:3128", http("192.0.2.1", 3128), true},
		{"[::1]:7890", http("::1", 7890), true},
		// As some programs write it.
		{"http://127.0.0.1:7890/", http("127.0.0.1", 7890), true},
		{"socks5://127.0.0.1:7891", socks("127.0.0.1", 7891), true},
		// An HTTPS proxy is spoken to over TLS, which the app's are not.
		{"https://192.0.2.1:3129", Server{}, false},
		{"https=https://192.0.2.1:3129;http=192.0.2.1:3128", http("192.0.2.1", 3128), true},
		{"127.0.0.1", Server{}, false},
		{"127.0.0.1:0", Server{}, false},
		{"127.0.0.1:70000", Server{}, false},
		{":8080", Server{}, false},
		{"", Server{}, false},
	} {
		got, ok := Pick(tc.list)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Pick(%q) = %+v, %v; want %+v, %v", tc.list, got, ok, tc.want, tc.ok)
		}
	}
	if a := http("::1", 7890).Address(); a != "[::1]:7890" {
		t.Errorf("Address = %q", a)
	}
}

func TestBypassed(t *testing.T) {
	// Like what v2rayN and Clash write, with the tests' own addresses.
	const list = "localhost;127.*;192.0.2.*;*.local;*.corp.example.com; <local>"
	for host, want := range map[string]bool{
		"localhost":               true,
		"LocalHost":               true,
		"127.0.0.1":               true,
		"192.0.2.10":              true,
		"192.0.20.1":              false,
		"pc.local":                true,
		"office.corp.example.com": true,
		"corp.example.com":        false,
		"officepc":                true, // <local>: a name without a dot
		"pc.example.com":          false,
		"2001:db8::5":             false, // no dot, but an address rather than a name
	} {
		if got := Bypassed(list, host); got != want {
			t.Errorf("Bypassed(%q) = %v, want %v", host, got, want)
		}
	}
	for _, tc := range []struct {
		bypass, host string
		want         bool
	}{
		{"http://*.example.com", "pc.example.com", true}, // a scheme is ignored
		{"*.example.com:8080", "pc.example.com", true},   // and a port
		{"[2001:db8::5]", "2001:db8::5", true},
		{"[2001:db8::5]:3389", "[2001:db8::5]", true},
		{"2001:db8::*", "2001:db8::5", true},
		{"pc*", "pc.example.com", true},
		{"*pc*", "my-pc.example.com", true},
		{"p*c.example.com", "pc.example.com", true},
		{"pc.example.co", "pc.example.com", false},
		{"", "pc.example.com", false},
	} {
		if got := Bypassed(tc.bypass, tc.host); got != tc.want {
			t.Errorf("Bypassed(%q, %q) = %v, want %v", tc.bypass, tc.host, got, tc.want)
		}
	}
}

// fakeWindows stands in for Windows' settings and automatic configuration.
type fakeWindows struct {
	settings    Settings
	settingsErr error
	// configure answers the automatic configuration.
	configure func(ctx context.Context, url string, autoDetect bool, script string) ([]Entry, error)
	asked     []string // the URLs the configuration was asked about
}

func (w *fakeWindows) Settings() (Settings, error) { return w.settings, w.settingsErr }

func (w *fakeWindows) Configure(ctx context.Context, url string, autoDetect bool, script string) ([]Entry, error) {
	w.asked = append(w.asked, url)
	return w.configure(ctx, url, autoDetect, script)
}

// answers is a configuration that names entries.
func answers(entries ...Entry) func(context.Context, string, bool, string) ([]Entry, error) {
	return func(context.Context, string, bool, string) ([]Entry, error) { return entries, nil }
}

// fails is a configuration that gives no answer.
func fails(err error) func(context.Context, string, bool, string) ([]Entry, error) {
	return func(context.Context, string, bool, string) ([]Entry, error) { return nil, err }
}

func TestDecide(t *testing.T) {
	manual := &Server{Kind: model.KindHTTP, Host: "127.0.0.1", Port: 10809}
	pac := &Server{Kind: model.KindHTTP, Host: "192.0.2.1", Port: 3128}
	pacSocks := &Server{Kind: model.KindSocks, Host: "192.0.2.2", Port: 1080}
	direct := Entry{Direct: true}
	proxy := func(scheme, host string, port int) Entry { return Entry{Scheme: scheme, Host: host, Port: port} }
	const script = "http://127.0.0.1:10810/pac"
	unavailable := errcode.Wrap(CodeScriptUnavailable, errors.New("the setup script could not be downloaded (WinHTTP error 12167)"))
	for _, tc := range []struct {
		name      string
		settings  Settings
		configure func(context.Context, string, bool, string) ([]Entry, error)
		target    string
		want      Decision
	}{
		{"nothing set", Settings{}, nil, "pc.example.com:3389", Decision{By: ByNone}},
		{"manual", Settings{Proxy: "127.0.0.1:10809", Bypass: "localhost;192.0.2.*"}, nil, "pc.example.com:3389",
			Decision{Server: manual, By: ByManual}},
		{"an exception of the manual proxy", Settings{Proxy: "127.0.0.1:10809", Bypass: "localhost;192.0.2.*"}, nil,
			"192.0.2.10:3389", Decision{By: ByBypass}},
		{"a name without a dot", Settings{Proxy: "127.0.0.1:10809", Bypass: "<local>"}, nil, "officepc:3389",
			Decision{By: ByBypass}},
		// Detection is on by default and usually finds nothing: the manual
		// setting decides, without remark.
		{"detection finds nothing", Settings{AutoDetect: true, Proxy: "127.0.0.1:10809"}, fails(ErrNothingDetected),
			"pc.example.com:3389", Decision{Server: manual, By: ByManual}},
		{"detection finds nothing, no manual proxy", Settings{AutoDetect: true}, fails(ErrNothingDetected),
			"pc.example.com:3389", Decision{By: ByNone}},
		{"the script names a proxy", Settings{Script: script, Proxy: "127.0.0.1:10809"},
			func(_ context.Context, url string, autoDetect bool, s string) ([]Entry, error) {
				if url != "https://pc.example.com:3389/" || autoDetect || s != script {
					t.Errorf("Configure(%q, %v, %q)", url, autoDetect, s)
				}
				return []Entry{proxy("http", "192.0.2.1", 3128), proxy("http", "192.0.2.2", 3128)}, nil
			}, "pc.example.com:3389", Decision{Server: pac, By: ByConfig}},
		{"the script names a SOCKS proxy", Settings{Script: script}, answers(proxy("socks", "192.0.2.2", 1080), direct),
			"pc.example.com:3389", Decision{Server: pacSocks, By: ByConfig}},
		// Windows reads SOCKS5, which browsers know, as a server "5 host".
		{"the script names a SOCKS5 proxy", Settings{Script: script}, answers(proxy("socks", "5 192.0.2.2", 1080), direct),
			"pc.example.com:3389", Decision{Server: pacSocks, By: ByConfig}},
		// The first way the app can take counts.
		{"an HTTPS proxy first", Settings{Script: script},
			answers(proxy("https", "192.0.2.3", 3129), proxy("http", "192.0.2.1", 3128)),
			"pc.example.com:3389", Decision{Server: pac, By: ByConfig}},
		{"a name Windows could not read first", Settings{Script: script},
			answers(proxy("socks", "4 192.0.2.2", 1080), proxy("http", "", 3128), direct),
			"pc.example.com:3389", Decision{By: ByConfig}},
		// The script's word counts over the manual setting's exceptions.
		{"the script says direct", Settings{Script: script, Proxy: "127.0.0.1:10809"}, answers(direct),
			"pc.example.com:3389", Decision{By: ByConfig}},
		{"the script names nothing", Settings{Script: script, Proxy: "127.0.0.1:10809"}, answers(),
			"pc.example.com:3389", Decision{By: ByConfig}},
		{"the script fails", Settings{Script: script, Proxy: "127.0.0.1:10809"}, fails(unavailable),
			"pc.example.com:3389", Decision{Server: manual, By: ByManual,
				ConfigError: unavailable.Error(), ConfigCode: CodeScriptUnavailable}},
		{"the script fails in a way without a code", Settings{Script: script}, fails(errors.New("winapi error #12345")),
			"pc.example.com:3389", Decision{By: ByNone, ConfigError: "winapi error #12345", ConfigCode: CodeConfigFailed}},
		{"the script names nothing usable", Settings{Script: script}, answers(proxy("https", "192.0.2.3", 3129)),
			"pc.example.com:3389", Decision{By: ByNone, ConfigError: ErrNoUsableProxy.Error(), ConfigCode: "sysproxy.noUsableProxy"}},
		{"an IPv6 target", Settings{Script: script},
			func(_ context.Context, url string, _ bool, _ string) ([]Entry, error) {
				if url != "https://[2001:db8::5]:3389/" {
					t.Errorf("Configure(%q)", url)
				}
				return []Entry{direct}, nil
			}, "[2001:db8::5]:3389", Decision{By: ByConfig}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &fakeWindows{settings: tc.settings, configure: tc.configure}
			got, err := Decide(context.Background(), w, tc.target)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Decide = %+v, %v; want %+v", got, err, tc.want)
			}
			if tc.configure == nil && len(w.asked) > 0 {
				t.Fatalf("the automatic configuration was asked though none is set: %q", w.asked)
			}
		})
	}
}

func TestDecideFailures(t *testing.T) {
	ctx := context.Background()
	// Unreadable settings do not mean "no proxy": going direct could be what
	// the user set a proxy to avoid.
	w := &fakeWindows{settingsErr: errors.New("access denied")}
	if _, err := Decide(ctx, w, "pc.example.com:3389"); !errors.Is(err, ErrUnreadable) || errcode.Of(err) != "sysproxy.unreadable" {
		t.Fatalf("Decide with unreadable settings = %v", err)
	}
	// The setting names servers and the error is logged, so it is left out.
	w = &fakeWindows{settings: Settings{Proxy: "ftp=ftp.example.com:21"}}
	if _, err := Decide(ctx, w, "pc.example.com:3389"); !errors.Is(err, ErrUnusable) || strings.Contains(err.Error(), "example") {
		t.Fatalf("Decide with only an FTP proxy = %v", err)
	}
	if _, err := Decide(ctx, &fakeWindows{}, "no port"); err == nil {
		t.Fatal("Decide of a target without a port")
	}
}

// Running the configuration may wait on the network; cancelling ends it,
// rather than falling back on the manual setting.
func TestDecideCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	asked := make(chan struct{})
	w := &fakeWindows{settings: Settings{AutoDetect: true, Proxy: "127.0.0.1:10809"},
		configure: func(ctx context.Context, _ string, _ bool, _ string) ([]Entry, error) {
			close(asked)
			<-ctx.Done()
			return nil, errors.New("the operation was cancelled (WinHTTP error 12017)")
		}}
	go func() {
		<-asked
		cancel()
	}()
	if d, err := Decide(ctx, w, "pc.example.com:3389"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Decide = %+v, %v; want context.Canceled", d, err)
	}
}
