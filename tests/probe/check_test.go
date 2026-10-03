package probe_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/probe"
	"github.com/liubz102/RDP-over-proxy/tests/testutil"
)

func TestCheckConfirm(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm, Selected: probe.ProtocolHybridEx})
	res, err := probe.Check(t.Context(), &net.Dialer{}, srv.Addr)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if want := (probe.ConnectionConfirm{Negotiated: true, Selected: probe.ProtocolHybridEx}); res.Confirm != want {
		t.Errorf("Confirm = %+v, want %+v", res.Confirm, want)
	}
	// A loopback round trip can be shorter than a tick of the Windows clock,
	// so zero is a legitimate reading here.
	if res.Elapsed < 0 {
		t.Errorf("Elapsed = %v, want a duration", res.Elapsed)
	}
	// The probe offers what mstsc offers and sends no user name.
	req := <-srv.Requests()
	if want := (probe.ConnectionRequest{Negotiate: true, Protocols: probe.Requested}); req != want {
		t.Errorf("the server received %+v, want %+v", req, want)
	}
}

func TestCheckCountsANegotiationFailureAsReachable(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerFailure, Failure: probe.SSLNotAllowedByServer})
	res, err := probe.Check(t.Context(), &net.Dialer{}, srv.Addr)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Confirm.Failure != probe.SSLNotAllowedByServer {
		t.Errorf("Confirm = %+v, want the failure code", res.Confirm)
	}
}

func TestCheckLegacyServer(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerLegacy})
	res, err := probe.Check(t.Context(), &net.Dialer{}, srv.Addr)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Confirm.Negotiated {
		t.Errorf("Confirm = %+v, want no negotiation", res.Confirm)
	}
}

func TestCheckErrors(t *testing.T) {
	cases := []struct {
		answer testutil.Answer
		want   error
	}{
		{testutil.AnswerNotRDP, probe.ErrNotRDP},
		{testutil.AnswerTruncated, probe.ErrTruncated},
		{testutil.AnswerClose, probe.ErrNoAnswer},
	}
	for _, c := range cases {
		srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: c.answer, Selected: probe.ProtocolSSL})
		_, err := probe.Check(t.Context(), &net.Dialer{}, srv.Addr)
		if !errors.Is(err, c.want) {
			t.Errorf("answer %d: err = %v, want %v", c.answer, err, c.want)
		}
	}
}

type failingDialer struct{ err error }

func (d failingDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, d.err
}

func TestCheckReportsDialErrors(t *testing.T) {
	cause := errors.New("proxy refused")
	_, err := probe.Check(t.Context(), failingDialer{cause}, "rdp.example.com:3389")
	if !errors.Is(err, cause) || !strings.HasPrefix(err.Error(), "connect: ") {
		t.Fatalf("err = %v, want the dial error", err)
	}
}

func TestCheckCanBeCancelled(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerNothing})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := probe.Check(ctx, &net.Dialer{}, srv.Addr)
		done <- err
	}()
	// Cancel once the request has reached the server, i.e. while Check is
	// waiting for an answer that never comes.
	<-srv.Requests()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestCheckCancelledBeforeDialing(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := probe.Check(ctx, &net.Dialer{}, "192.0.2.1:3389")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
