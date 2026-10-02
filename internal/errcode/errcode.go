// Package errcode gives errors stable codes that the UI translates. A code is
// a dotted key such as "proxy.auth"; the frontend shows the message for
// "errors.<code>" and keeps the error's own text as the details.
//
// Packages create their sentinel errors with New or Weak, and an adapter that
// learns what went wrong from someone else's error (the Xray engine) labels
// it with Wrap. errors.Is keeps working on all of them.
//
// Of picks the most useful code in an error tree, in this order: the first
// strong code; a code recognised from an operating-system error; the first
// weak code; "unknown". A weak code belongs to a generic wrapper, such as "the
// connection closed before the target answered", that any more specific cause
// in the same tree should override.
package errcode

import (
	"context"
	"errors"
	"maps"
	"net"
	"slices"
	"sync"
)

// Codes that no single package owns.
const (
	Unknown   = "unknown"
	Cancelled = "cancelled"

	// Network failures recognised from the operating system's error.
	NetRefused         = "net.refused"
	NetTimeout         = "net.timeout"
	NetUnreachable     = "net.unreachable"
	NetReset           = "net.reset"
	NetDNS             = "net.dns"
	NetAddressInUse    = "net.addressInUse"
	NetAccessDenied    = "net.accessDenied"
	NetAddressNotAvail = "net.addressNotAvailable"
)

var (
	registryMu sync.Mutex
	registry   = map[string]bool{}
)

func init() {
	Declare(Unknown, Cancelled, NetRefused, NetTimeout, NetUnreachable, NetReset, NetDNS,
		NetAddressInUse, NetAccessDenied, NetAddressNotAvail)
}

// Declare records codes that errors may carry, so tests can check that the
// UI translates every one. New and Weak declare theirs; a package that
// labels errors with Wrap declares its codes itself.
func Declare(codes ...string) {
	registryMu.Lock()
	defer registryMu.Unlock()
	for _, c := range codes {
		registry[c] = true
	}
}

// All returns every declared code, sorted.
func All() []string {
	registryMu.Lock()
	defer registryMu.Unlock()
	return slices.Sorted(maps.Keys(registry))
}

// Error is an error with a code.
type Error struct {
	code string
	weak bool
	text string // the message when err is nil
	err  error
}

// New returns a sentinel error with a strong code.
func New(code, text string) error {
	Declare(code)
	return &Error{code: code, text: text}
}

// Weak returns a sentinel error whose code gives way to any more specific
// code in the same error tree.
func Weak(code, text string) error {
	Declare(code)
	return &Error{code: code, weak: true, text: text}
}

// Wrap labels err with a strong code. The message stays err's own, and
// errors.Is and errors.As still reach err. Wrap(code, nil) is nil.
func Wrap(code string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{code: code, err: err}
}

func (e *Error) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return e.text
}

// Unwrap returns the labelled error, if there is one.
func (e *Error) Unwrap() error { return e.err }

// Code is the error's own code.
func (e *Error) Code() string { return e.code }

// argsError attaches arguments for the translated message to an error.
type argsError struct {
	err  error
	args map[string]string
}

func (e *argsError) Error() string { return e.err.Error() }
func (e *argsError) Unwrap() error { return e.err }

// WithArgs attaches arguments that fill in the translated message of err's
// code, such as the name of an RD Gateway. Codes and errors.Is see through
// it. WithArgs(nil, …) is nil.
func WithArgs(err error, args map[string]string) error {
	if err == nil {
		return nil
	}
	return &argsError{err: err, args: args}
}

// Args returns the arguments attached to err with WithArgs, or nil.
func Args(err error) map[string]string {
	var a *argsError
	if errors.As(err, &a) {
		return a.args
	}
	return nil
}

// Of returns the code that best describes err, or "" when err is nil.
func Of(err error) string {
	if err == nil {
		return ""
	}
	var weak, system string
	var walk func(error) string // returns the first strong code
	walk = func(err error) string {
		if e, ok := err.(*Error); ok {
			if !e.weak {
				return e.code
			}
			if weak == "" {
				weak = e.code
			}
		} else if system == "" {
			system = fromSystem(err)
		}
		switch u := err.(type) {
		case interface{ Unwrap() error }:
			if inner := u.Unwrap(); inner != nil {
				return walk(inner)
			}
		case interface{ Unwrap() []error }:
			for _, inner := range u.Unwrap() {
				if code := walk(inner); code != "" {
					return code
				}
			}
		}
		return ""
	}
	if code := walk(err); code != "" {
		return code
	}
	switch {
	case system != "":
		return system
	case weak != "":
		return weak
	}
	return Unknown
}

// fromSystem recognises a single error value, not the ones it wraps, from
// the standard library or the operating system.
func fromSystem(err error) string {
	if e, ok := err.(*net.DNSError); ok {
		if e.IsTimeout {
			return NetTimeout
		}
		return NetDNS
	}
	if errors.Is(err, context.Canceled) {
		return Cancelled
	}
	return fromErrno(err)
}
