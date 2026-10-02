package logging

import (
	"context"
	"log/slog"
	"strings"
)

// boundMethodError is how the Wails runtime reports an error that a service
// method returned. Those errors are the frontend's to show (the call is
// rejected with them), so the runtime's report of one is left out.
const boundMethodError = "Bound method returned an error"

// payloadKeys are the attributes in which the Wails runtime puts what the
// frontend sent or received, such as a service call's arguments, which can
// be a password. Their values never reach the log.
var payloadKeys = map[string]bool{"args": true, "result": true, "message": true, "data": true}

// SlogHandler passes the Wails runtime's warnings and errors to l as lines
// from source. Its debug and info records are left out at every log level:
// they are the runtime's own diagnostics, it logs every asset it serves at
// info level, and some of them carry the arguments of service calls, which
// include passwords.
func SlogHandler(l *Logger, source string) slog.Handler {
	return &slogHandler{l: l, source: source}
}

type slogHandler struct {
	l      *Logger
	source string
	attrs  []slog.Attr
}

func (h *slogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

func (h *slogHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level < slog.LevelWarn || strings.Contains(r.Message, boundMethodError) {
		return nil
	}
	level := LevelWarn
	if r.Level >= slog.LevelError {
		level = LevelError
	}
	args := map[string]any{}
	add := func(a slog.Attr) {
		if payloadKeys[a.Key] {
			args[a.Key] = "<omitted>"
			return
		}
		args[a.Key] = a.Value.String()
	}
	for _, a := range h.attrs {
		add(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		add(a)
		return true
	})
	if len(args) == 0 {
		args = nil
	}
	h.l.Log(Line{Time: r.Time, Level: level, Source: h.source, Msg: r.Message, Args: args})
	return nil
}

func (h *slogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &slogHandler{l: h.l, source: h.source, attrs: append(append([]slog.Attr(nil), h.attrs...), attrs...)}
}

// WithGroup is not needed by Wails; attributes keep their own names.
func (h *slogHandler) WithGroup(string) slog.Handler { return h }
