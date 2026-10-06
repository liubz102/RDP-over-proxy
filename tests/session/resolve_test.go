package session_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/session"

	"context"
	"errors"
	"sync"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/model"
	"github.com/liubz102/RDP-over-proxy/internal/route"
	"github.com/liubz102/RDP-over-proxy/tests/testutil"
)

// askedRoutes reaches every target directly, like routes, and keeps the
// proxies it was asked for.
type askedRoutes struct {
	mu    sync.Mutex
	asked []model.Proxy
}

func (r *askedRoutes) Acquire(p model.Proxy) (route.Dialer, func(), error) {
	r.mu.Lock()
	r.asked = append(r.asked, p)
	r.mu.Unlock()
	return route.Direct(), func() {}, nil
}

// The entry that follows the system is turned into the proxy to use in the
// route step, and the route is taken for that one.
func TestResolveHook(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	resolved := model.Proxy{Schema: model.ProxySchema, ID: model.SystemProxyID, Name: "system",
		Kind: model.KindHTTP, Server: "127.0.0.1", Port: 10809}
	failed := errors.New("Windows' proxy settings could not be read")

	for _, tc := range []struct {
		name    string
		resolve func(context.Context, Request) (model.Proxy, error)
		// fails is the error the session ends with in the route step; nil
		// when it gets as far as starting mstsc.
		fails error
	}{
		{"to a proxy server", func(_ context.Context, r Request) (model.Proxy, error) {
			if r.Proxy.Kind != model.KindSystem {
				t.Errorf("Resolve got %+v", r.Proxy)
			}
			return resolved, nil
		}, nil},
		// The target is this computer (the fake server listens on loopback):
		// direct would connect the tunnel to itself.
		{"to direct", func(context.Context, Request) (model.Proxy, error) { return model.DirectProxy(), nil }, ErrLoopbackDirect},
		{"failing", func(context.Context, Request) (model.Proxy, error) { return model.Proxy{}, failed }, failed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := &askedRoutes{}
			launcher := &launcher{started: make(chan *fakeProcess, 1)}
			rec := newRecorder()
			m := NewManager(Options{Routes: routes, Launch: launcher.launch, Resolve: tc.resolve, Changed: rec.state})
			t.Cleanup(m.Quit)
			req := request(t, "sys", srv)
			req.Proxy, req.Profile.ProxyID = model.SystemProxy(), model.SystemProxyID
			if _, err := m.Connect(req); err != nil {
				t.Fatal(err)
			}
			if tc.fails != nil {
				end := rec.ended("sys")
				if end.Failure == nil || end.Failure.Step != StepRoute || !errors.Is(end.Failure.Err, tc.fails) {
					t.Fatalf("final state %+v, failure %+v; want %v in the route step", end, end.Failure, tc.fails)
				}
				if len(routes.asked) != 0 {
					t.Fatalf("a route was taken: %+v", routes.asked)
				}
				return
			}
			p := <-launcher.started
			if len(routes.asked) != 1 || routes.asked[0] != resolved {
				t.Fatalf("routes asked for %+v, want %+v", routes.asked, resolved)
			}
			p.exitWith(0)
			if end := rec.ended("sys"); end.Outcome != OutcomeClosed {
				t.Fatalf("final state %+v", end)
			}
		})
	}
}

// Following the system may wait on Windows' automatic configuration, which
// takes as long as the network does: neither stopping the session nor
// quitting waits for it.
func TestStopWhileResolving(t *testing.T) {
	srv := testutil.NewRDPServer(t, testutil.RDPOptions{Answer: testutil.AnswerConfirm})
	for _, quit := range []bool{false, true} {
		asked := make(chan struct{})
		resolve := func(ctx context.Context, _ Request) (model.Proxy, error) {
			close(asked)
			<-ctx.Done()
			return model.Proxy{}, ctx.Err()
		}
		routes := &askedRoutes{}
		rec := newRecorder()
		launcher := &launcher{started: make(chan *fakeProcess, 1)}
		m := NewManager(Options{Routes: routes, Launch: launcher.launch, Resolve: resolve, Changed: rec.state})
		t.Cleanup(m.Quit)
		req := request(t, "sys", srv)
		req.Proxy, req.Profile.ProxyID = model.SystemProxy(), model.SystemProxyID
		if _, err := m.Connect(req); err != nil {
			t.Fatal(err)
		}
		<-asked
		if quit {
			m.Quit()
		} else if !m.Stop("sys", false) {
			t.Fatal("Stop found no session")
		}
		end := rec.ended("sys")
		if end.Outcome != OutcomeCancelled || end.Failure != nil {
			t.Fatalf("quit %v: final state %+v, failure %+v; want cancelled", quit, end, end.Failure)
		}
		if len(routes.asked) != 0 {
			t.Fatalf("quit %v: a route was taken: %+v", quit, routes.asked)
		}
	}
}
