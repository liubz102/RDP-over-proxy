package tunnel_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/probe"
	"github.com/liubz102/RDP-over-proxy/internal/route"
	"github.com/liubz102/RDP-over-proxy/internal/tunnel"
	"github.com/liubz102/RDP-over-proxy/tests/testutil"
)

// entrance is a loopback address of the per-profile kind, with port 0 so the
// tests never collide with each other or with a running copy of the app.
var entrance = netip.MustParseAddrPort("127.201.202.203:0")

// recorder is a Reporter that keeps every report in order.
type recorder struct {
	mu      sync.Mutex
	reports []string
	errs    []error
	changed chan struct{}
}

func newRecorder() *recorder { return &recorder{changed: make(chan struct{}, 1)} }

func (r *recorder) add(kind string, err error) {
	r.mu.Lock()
	r.reports = append(r.reports, kind)
	if err != nil {
		r.errs = append(r.errs, err)
	}
	r.mu.Unlock()
	select {
	case r.changed <- struct{}{}:
	default:
	}
}

func (r *recorder) ConnOpened()              { r.add("opened", nil) }
func (r *recorder) ConnClosed()              { r.add("closed", nil) }
func (r *recorder) UpstreamAnswered()        { r.add("answered", nil) }
func (r *recorder) UpstreamFailed(err error) { r.add("failed", err) }
func (r *recorder) ListenerFailed(err error) { r.add("listener", err) }
func (r *recorder) snapshot() ([]string, []error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.reports), slices.Clone(r.errs)
}

// waitCount waits until kind has been reported n times and returns all
// reports so far.
func (r *recorder) waitCount(kind string, n int) ([]string, []error) {
	for {
		reports, errs := r.snapshot()
		count := 0
		for _, k := range reports {
			if k == kind {
				count++
			}
		}
		if count >= n {
			return reports, errs
		}
		<-r.changed
	}
}

func open(t *testing.T, target string, d route.Dialer) (*tunnel.Tunnel, *recorder) {
	t.Helper()
	rec := newRecorder()
	tun, err := tunnel.Listen(entrance, target, d, rec)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(tun.Close)
	return tun, rec
}

func dial(t *testing.T, tun *tunnel.Tunnel) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", tun.Addr().String())
	if err != nil {
		t.Fatalf("dial the tunnel: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestDataFlowsBothWays(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm, Selected: probe.ProtocolHybrid})
	tun, rec := open(t, srv.Addr, route.Direct())
	if tun.Addr().Addr() != entrance.Addr() || tun.Addr().Port() == 0 {
		t.Fatalf("Addr = %s, want the entrance address with the port the system chose", tun.Addr())
	}

	c := dial(t, tun)
	req, _ := probe.ConnectionRequest{Negotiate: true, Protocols: probe.Requested}.Marshal()
	if _, err := c.Write(req); err != nil {
		t.Fatal(err)
	}
	pkt, err := probe.ReadTPKT(c)
	if err != nil {
		t.Fatalf("read the confirm through the tunnel: %v", err)
	}
	if cc, err := probe.ParseConnectionConfirm(pkt); err != nil || cc.Selected != probe.ProtocolHybrid {
		t.Fatalf("confirm = %+v, %v", cc, err)
	}
	if got := <-srv.Requests(); got.Protocols != probe.Requested {
		t.Fatalf("the server received %+v", got)
	}

	// After the handshake the fake server echoes.
	payload := bytes.Repeat([]byte("rdp"), 50000) // larger than one copy buffer
	go func() { _, _ = c.Write(payload) }()
	echo := make([]byte, len(payload))
	if _, err := io.ReadFull(c, echo); err != nil || !bytes.Equal(echo, payload) {
		t.Fatalf("echo through the tunnel: %v", err)
	}

	c.Close()
	reports, errs := rec.waitCount("closed", 1)
	if want := []string{"opened", "answered", "closed"}; !slices.Equal(reports, want) {
		t.Fatalf("reports = %q (%v), want %q", reports, errs, want)
	}
	sent, received := tun.Bytes()
	if sent != int64(len(req)+len(payload)) || received != int64(len(pkt)+len(payload)) {
		t.Fatalf("Bytes = %d sent, %d received; want %d and %d", sent, received, len(req)+len(payload), len(pkt)+len(payload))
	}
}

type failingDialer struct{ err error }

func (d failingDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, d.err
}

func TestDialFailureIsReportedAndClosesTheConnection(t *testing.T) {
	cause := errors.New("proxy refused")
	tun, rec := open(t, "rdp.example.com:3389", failingDialer{cause})
	c := dial(t, tun)
	if n, err := c.Read(make([]byte, 1)); err == nil {
		t.Fatalf("read %d bytes from a connection whose target could not be reached", n)
	}
	reports, errs := rec.waitCount("closed", 1)
	if !slices.Equal(reports, []string{"opened", "failed", "closed"}) || !errors.Is(errs[0], cause) {
		t.Fatalf("reports = %q, errors %v", reports, errs)
	}
}

func TestRouteClosingBeforeTheAnswerIsAFailure(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerClose})
	tun, rec := open(t, srv.Addr, route.Direct())
	c := dial(t, tun)
	req, _ := probe.ConnectionRequest{Negotiate: true, Protocols: probe.Requested}.Marshal()
	if _, err := c.Write(req); err != nil {
		t.Fatal(err)
	}
	if _, err := probe.ReadTPKT(c); !errors.Is(err, probe.ErrNoAnswer) {
		t.Fatalf("mstsc's side: err = %v, want the connection closed without an answer", err)
	}
	reports, errs := rec.waitCount("closed", 1)
	if !slices.Equal(reports, []string{"opened", "failed", "closed"}) || !errors.Is(errs[0], tunnel.ErrNoAnswer) {
		t.Fatalf("reports = %q, errors %v", reports, errs)
	}
}

func TestMstscClosingFirstIsNotAFailure(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerNothing})
	tun, rec := open(t, srv.Addr, route.Direct())
	c := dial(t, tun)
	req, _ := probe.ConnectionRequest{Negotiate: true, Protocols: probe.Requested}.Marshal()
	if _, err := c.Write(req); err != nil {
		t.Fatal(err)
	}
	<-srv.Requests() // the request went all the way through
	c.Close()
	reports, _ := rec.waitCount("closed", 1)
	if !slices.Equal(reports, []string{"opened", "closed"}) {
		t.Fatalf("reports = %q, want no failure", reports)
	}
}

func TestCloseEndsOpenConnections(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerNothing})
	tun, rec := open(t, srv.Addr, route.Direct())
	c := dial(t, tun)
	req, _ := probe.ConnectionRequest{Negotiate: true, Protocols: probe.Requested}.Marshal()
	if _, err := c.Write(req); err != nil {
		t.Fatal(err)
	}
	<-srv.Requests()

	tun.Close()
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Fatal("the connection is still open after Close")
	}
	reports, _ := rec.snapshot()
	if !slices.Equal(reports, []string{"opened", "closed"}) {
		t.Fatalf("reports = %q; closing the tunnel is neither an upstream nor a listener failure", reports)
	}
	if _, err := net.Dial("tcp", tun.Addr().String()); err == nil {
		t.Fatal("the entrance still accepts connections after Close")
	}
	tun.Close() // a second Close is harmless
}

// blockingDialer waits until the dial is cancelled.
type blockingDialer struct{ started chan struct{} }

func (d blockingDialer) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	close(d.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestCloseAbortsADialInFlight(t *testing.T) {
	d := blockingDialer{started: make(chan struct{})}
	tun, rec := open(t, "rdp.example.com:3389", d)
	dial(t, tun)
	<-d.started
	tun.Close() // must not wait for a dial that would never finish by itself
	reports, _ := rec.snapshot()
	if !slices.Equal(reports, []string{"opened", "closed"}) {
		t.Fatalf("reports = %q, want no failure for a dial the tunnel cancelled", reports)
	}
}

func TestListenOnAnAddressInUse(t *testing.T) {
	first, _ := open(t, "rdp.example.com:3389", failingDialer{errors.New("unused")})
	_, err := tunnel.Listen(first.Addr(), "rdp.example.com:3389", failingDialer{}, newRecorder())
	if err == nil {
		t.Fatal("a second tunnel on the same address and port should fail to listen")
	}
	if !strings.Contains(err.Error(), first.Addr().String()) {
		t.Errorf("error %q does not name the address", err)
	}
}
