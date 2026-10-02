package probe_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/probe"
)

func TestLatencyCountsAnyResponse(t *testing.T) {
	statuses := map[string]int{"/generate_204": http.StatusNoContent, "/error": http.StatusInternalServerError, "/moved": http.StatusFound}
	var hits atomic.Int32
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/moved" {
			w.Header().Set("Location", "/generate_204")
		}
		w.WriteHeader(statuses[r.URL.Path])
	}))
	t.Cleanup(web.Close)
	for path := range statuses {
		hits.Store(0)
		if _, err := probe.Latency(t.Context(), &net.Dialer{}, web.URL+path); err != nil {
			t.Errorf("%s: Latency: %v", path, err)
		}
		if n := hits.Load(); n != 1 {
			t.Errorf("%s: %d requests; redirects must not be followed", path, n)
		}
	}
}

func TestLatencyFailures(t *testing.T) {
	if _, err := probe.Latency(t.Context(), &net.Dialer{}, "::not a url"); err == nil {
		t.Error("an invalid URL should fail")
	}
	cause := errors.New("proxy refused")
	if _, err := probe.Latency(t.Context(), failingDialer{cause}, "http://example.com/generate_204"); !errors.Is(err, cause) {
		t.Errorf("err = %v, want the dial error", err)
	}
}

func TestLatencyCanBeCancelled(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
	}))
	t.Cleanup(web.Close)
	t.Cleanup(func() { close(release) }) // runs first, so web.Close does not wait forever

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := probe.Latency(ctx, &net.Dialer{}, web.URL)
		done <- err
	}()
	<-entered // the request is at the server, which never answers
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
