package route

import (
	"errors"
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/model"
)

func TestDirectOnly(t *testing.T) {
	d, release, err := DirectOnly{}.Acquire(model.DirectProxy())
	if err != nil || d == nil || release == nil {
		t.Fatalf("Acquire(direct) = %v, %v", d, err)
	}
	release()

	socks := model.Proxy{ID: "a1", Name: "x", Kind: model.KindSocks, Server: "192.0.2.1", Port: 1080}
	if _, _, err := (DirectOnly{}).Acquire(socks); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Acquire(socks) error = %v, want ErrUnsupported", err)
	}
}
