package model

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// Validation problem codes. The UI shows a translated message for each one
// next to the field it belongs to.
const (
	CodeRequired    = "required"
	CodeInvalid     = "invalid"
	CodeOutOfRange  = "out_of_range"
	CodeTooLong     = "too_long"
	CodeUnsupported = "unsupported"
	CodeConflict    = "conflict"
)

// Length limits for free text. They only stop something absurd from being
// pasted in; real values are far shorter.
const (
	MaxNameLen     = 100      // runes: profile, group and proxy names
	MaxUsernameLen = 256      // runes: a Remote Desktop user name, with domain
	MaxOutboundLen = 64 << 10 // bytes of Xray outbound JSON
)

// FieldError is one problem with one field. Field is the field's JSON path,
// such as "target.host"; Code is one of the Code constants.
type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

// FieldErrors is the error Validate returns for a profile or a proxy. It
// lists every problem found, so a form can mark all the fields at once.
type FieldErrors []FieldError

func (e FieldErrors) Error() string {
	parts := make([]string, len(e))
	for i, f := range e {
		parts[i] = f.Field + " " + f.Code
	}
	return "invalid fields: " + strings.Join(parts, ", ")
}

// Has reports whether field has a problem with the given code.
func (e FieldErrors) Has(field, code string) bool {
	return slices.Contains(e, FieldError{Field: field, Code: code})
}

func (e *FieldErrors) add(field, code string) {
	*e = append(*e, FieldError{Field: field, Code: code})
}

// err returns e as an error, or nil when there are no problems. (A nil
// FieldErrors stored in an error interface would not compare equal to nil.)
func (e FieldErrors) err() error {
	if len(e) == 0 {
		return nil
	}
	return e
}

// text checks a free-text field that may be required and is limited to max
// runes.
func (e *FieldErrors) text(field, value string, required bool, max int) {
	switch {
	case value == "":
		if required {
			e.add(field, CodeRequired)
		}
	case utf8.RuneCountInString(value) > max:
		e.add(field, CodeTooLong)
	}
}

// host checks a host name or IP address field.
func (e *FieldErrors) host(field, value string, required bool) {
	switch {
	case value == "":
		if required {
			e.add(field, CodeRequired)
		}
	case !ValidHost(value):
		e.add(field, CodeInvalid)
	}
}

// port checks a TCP port field. Zero means "not set".
func (e *FieldErrors) port(field string, value int, required bool) {
	switch {
	case value == 0:
		if required {
			e.add(field, CodeRequired)
		}
	case value < 1 || value > 65535:
		e.add(field, CodeOutOfRange)
	}
}
