package probe

import (
	"context"
	"net/http"
	"time"
)

// Latency measures one HTTP GET of url through d: connecting through the
// route (and TLS for https) up to the response headers. Any HTTP response
// counts, whatever its status, because it proves the route carried the
// request there and back; redirects are not followed.
//
// There is no timeout; cancel ctx to stop it, and Latency returns ctx's
// error.
func Latency(ctx context.Context, d ContextDialer, url string) (time.Duration, error) {
	tr := &http.Transport{
		Proxy:             nil, // the route is the proxy; never the system's
		DialContext:       d.DialContext,
		DisableKeepAlives: true,
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{
		Transport:     tr,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, canceled(ctx, err)
	}
	elapsed := time.Since(start)
	resp.Body.Close()
	return elapsed, nil
}
