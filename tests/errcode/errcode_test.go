package errcode_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/errcode"

	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
)

var (
	errStrong = New("test.strong", "something specific went wrong")
	errWeak   = Weak("test.weak", "something went wrong")
)

func TestOf(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connectex", refusedErrno)}
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"plain", errors.New("boom"), Unknown},
		{"strong sentinel", errStrong, "test.strong"},
		{"wrapped strong", fmt.Errorf("check: %w", errStrong), "test.strong"},
		{"weak alone", fmt.Errorf("%w (%w)", errWeak, io.EOF), "test.weak"},
		{"strong cause beats weak wrapper", fmt.Errorf("%w (%w)", errWeak, errStrong), "test.strong"},
		{"label beats what it labels", Wrap("test.label", fmt.Errorf("x: %w", errStrong)), "test.label"},
		{"system error beats weak wrapper", fmt.Errorf("%w (%w)", errWeak, refused), NetRefused},
		{"strong beats system error", Wrap("test.label", refused), "test.label"},
		{"system error", fmt.Errorf("connect: %w", refused), NetRefused},
		{"dns", fmt.Errorf("x: %w", &net.DNSError{Err: "no such host", Name: "host.example.com", IsNotFound: true}), NetDNS},
		{"cancelled", fmt.Errorf("x: %w", context.Canceled), Cancelled},
		{"wrap nil", Wrap("test.label", nil), ""},
	}
	for _, c := range cases {
		if got := Of(c.err); got != c.want {
			t.Errorf("%s: Of(%v) = %q, want %q", c.name, c.err, got, c.want)
		}
	}
}

func TestErrorsIsStillWorks(t *testing.T) {
	wrapped := Wrap("test.label", fmt.Errorf("x: %w", io.ErrUnexpectedEOF))
	if !errors.Is(wrapped, io.ErrUnexpectedEOF) {
		t.Fatal("errors.Is does not see through Wrap")
	}
	if wrapped.Error() != "x: unexpected EOF" {
		t.Fatalf("Wrap changed the message: %q", wrapped.Error())
	}
	if !errors.Is(fmt.Errorf("y: %w", errWeak), errWeak) {
		t.Fatal("errors.Is does not find a sentinel")
	}
}

func TestWithArgs(t *testing.T) {
	sentinel := New("test.withArgs", "with args")
	err := fmt.Errorf("preflight: %w", WithArgs(sentinel, map[string]string{"server": "gw.example.com"}))
	if Of(err) != "test.withArgs" || !errors.Is(err, sentinel) {
		t.Fatalf("Of = %q, Is = %v; want the arguments to be transparent", Of(err), errors.Is(err, sentinel))
	}
	if Args(err)["server"] != "gw.example.com" {
		t.Fatalf("Args = %v", Args(err))
	}
	if Args(sentinel) != nil || WithArgs(nil, map[string]string{"a": "b"}) != nil {
		t.Fatal("no arguments expected")
	}
}
