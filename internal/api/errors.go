package api

import (
	"encoding/json"
	"errors"

	"github.com/liubz102/RDP-over-proxy/internal/errcode"
	"github.com/liubz102/RDP-over-proxy/internal/model"
)

// CodeValidation is the code of an error that lists field problems
// (model.FieldErrors).
const CodeValidation = "validation"

// ErrorView is how an error reaches the UI: a code to translate
// ("errors.<code>"), the error's own text as details, and, for validation
// errors, the fields to mark.
type ErrorView struct {
	Code    string             `json:"code"`
	Message string             `json:"message"`
	Fields  []model.FieldError `json:"fields,omitempty"`
	// Args fill in the translated message, such as the names of the
	// connections that still use a proxy.
	Args map[string]string `json:"args,omitempty"`
}

// errorView describes err for the UI; nil for a nil error.
func errorView(err error) *ErrorView {
	if err == nil {
		return nil
	}
	v := &ErrorView{Code: errcode.Of(err), Message: err.Error()}
	var fields model.FieldErrors
	if errors.As(err, &fields) {
		v.Code, v.Fields = CodeValidation, fields
	}
	var a *argsError
	if errors.As(err, &a) {
		v.Args = a.args
	}
	return v
}

// MarshalError turns the errors service methods return into ErrorView JSON
// (application.ServiceOptions.MarshalError). The frontend finds it in the
// rejected call's "cause".
func MarshalError(err error) []byte {
	b, jerr := json.Marshal(errorView(err))
	if jerr != nil {
		return nil // Wails falls back to its own encoding
	}
	return b
}

// argsError attaches arguments for the UI's message to an error.
type argsError struct {
	err  error
	args map[string]string
}

func (e *argsError) Error() string { return e.err.Error() }
func (e *argsError) Unwrap() error { return e.err }

func withArgs(err error, args map[string]string) error {
	if err == nil {
		return nil
	}
	return &argsError{err: err, args: args}
}

// Errors of the services themselves.
var (
	// ErrSessionRunning: the profile cannot be deleted while it is
	// connected.
	ErrSessionRunning = errcode.New("session.running", "the connection is in use")
	// ErrProxyMissing: the profile's proxy no longer exists.
	ErrProxyMissing = errcode.New("profile.proxyMissing", "the connection's proxy no longer exists")
	// ErrNoSession: there is no session to stop or bring forward.
	ErrNoSession = errcode.New("session.none", "the connection is not running")
)
