package winx_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/winx"

	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/liubz102/RDP-over-proxy/internal/model"
)

// The setting depends on the computer; reading it must work.
func TestIEProxyConfig(t *testing.T) {
	c, err := IEProxyConfig()
	if err != nil {
		t.Fatalf("IEProxyConfig: %v", err)
	}
	t.Logf("detect: %v, script: %v, manual proxy: %v", c.AutoDetect, c.Script != "", c.Proxy != "")
}

// pacServer serves a setup script on this computer. Windows keeps scripts
// it fetched for a while, so each test asks for one of its own name.
func pacServer(t *testing.T, script string) (pac string, srv *httptest.Server) {
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ".pac") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
		w.Write([]byte(script))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/" + model.NewID() + ".pac", srv
}

// Windows runs a setup script the test serves on this computer; nothing is
// detected on the network and no setting is changed.
func TestProxyForURL(t *testing.T) {
	pac, srv := pacServer(t, `function FindProxyForURL(url, host) {
		if (host == "pc.example.com") return "PROXY 192.0.2.1:3128; DIRECT";
		if (host == "socks.example.com") return "SOCKS5 192.0.2.2:1080; SOCKS 192.0.2.2:1081; DIRECT";
		if (host == "tls.example.com") return "HTTPS 192.0.2.3:3129; PROXY 192.0.2.3:3128";
		return "DIRECT";
	}`)
	ctx := context.Background()
	ask := func(host string) []ProxyEntry {
		t.Helper()
		entries, err := ProxyForURL(ctx, "https://"+host+":3389/", false, pac)
		if err != nil {
			t.Fatalf("ProxyForURL(%s): %v", host, err)
		}
		t.Logf("%s: %+v", host, entries)
		return entries
	}

	// The checks do not depend on each other, so a mismatch is told and the
	// next one still runs: where another Windows differs from the one this
	// was written on, one run shows every difference.

	// Every entry, in the script's order, with how each proxy is spoken to.
	want := []ProxyEntry{{Scheme: "http", Host: "192.0.2.1", Port: 3128}, {Direct: true}}
	if got := ask("pc.example.com"); !slices.Equal(got, want) {
		t.Errorf("pc.example.com = %+v, want %+v", got, want)
	}
	// "SOCKS5 host:port" is how browsers name a SOCKS5 server; Windows' own
	// word is SOCKS. What it makes of the other word depends on its version:
	// Windows 10 reads a SOCKS server called "5 host" (sysproxy reads it
	// back), Windows Server 2025 leaves the entry out. What follows it
	// arrives either way.
	got := ask("socks.example.com")
	if len(got) > 0 && got[0] == (ProxyEntry{Scheme: "socks", Host: "5 192.0.2.2", Port: 1080}) {
		got = got[1:]
	}
	want = []ProxyEntry{{Scheme: "socks", Host: "192.0.2.2", Port: 1081}, {Direct: true}}
	if !slices.Equal(got, want) {
		t.Errorf("socks.example.com, without its SOCKS5 entry = %+v, want %+v", got, want)
	}
	want = []ProxyEntry{{Scheme: "https", Host: "192.0.2.3", Port: 3129}, {Scheme: "http", Host: "192.0.2.3", Port: 3128}}
	if got := ask("tls.example.com"); !slices.Equal(got, want) {
		t.Errorf("tls.example.com = %+v, want %+v", got, want)
	}
	if got := ask("other.example.com"); !slices.Equal(got, []ProxyEntry{{Direct: true}}) {
		t.Errorf("other.example.com = %+v, want direct", got)
	}

	_, err := ProxyForURL(ctx, "https://pc.example.com:3389/", false, srv.URL+"/missing")
	var errno windows.Errno
	if !errors.As(err, &errno) || errno != 12167 {
		t.Errorf("a script that cannot be had: %v; want WinHTTP error 12167", err)
	}
	t.Logf("a script that cannot be had: %v", err)
	if _, err := ProxyForURL(ctx, "https://pc.example.com:3389/", false, ""); err == nil {
		t.Fatal("ProxyForURL without any configuration gave an answer")
	}
}

// A script that does not come stops nothing: cancelling ends the wait.
func TestProxyForURLCancelled(t *testing.T) {
	asked := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case asked <- struct{}{}:
		default:
		}
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) }) // before Close, which waits for the handler

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-asked:
			cancel()
		case <-ctx.Done():
		}
	}()
	if _, err := ProxyForURL(ctx, "https://pc.example.com:3389/", false, srv.URL+"/"+model.NewID()+".pac"); !errors.Is(err, context.Canceled) {
		t.Fatalf("ProxyForURL = %v, want context.Canceled", err)
	}
}

// A key of the test's own, below the one the tests may use.
func TestWatchKey(t *testing.T) {
	parent := `Software\RDP-over-proxy-test`
	path := parent + `\watch-` + model.NewID()
	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		k.Close()
		_ = registry.DeleteKey(registry.CURRENT_USER, path)
		_ = registry.DeleteKey(registry.CURRENT_USER, parent) // fails while other tests use it
	})

	changed := make(chan struct{}, 16)
	stop, err := WatchKey(registry.CURRENT_USER, path, func() { changed <- struct{}{} })
	if err != nil {
		t.Fatalf("WatchKey: %v", err)
	}
	for i := range 3 {
		if err := k.SetDWordValue("Value", uint32(i)); err != nil {
			t.Fatal(err)
		}
		<-changed // each change is told, armed again in between
	}
	stop()
	if err := k.SetDWordValue("Value", 9); err != nil {
		t.Fatal(err)
	}
	// stop has returned, so changed is not running and will not again.
	if n := len(changed); n != 0 {
		t.Fatalf("changed called %d more times after stop", n)
	}
}
