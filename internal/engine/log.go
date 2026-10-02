package engine

import (
	"fmt"
	"strings"

	xlog "github.com/xtls/xray-core/common/log"
)

// registerLogBridge makes fn receive Xray's log lines. Xray's logger is
// process-wide, and creating any Xray instance replaces it.
func registerLogBridge(fn func(level, msg string), verbose func() bool) {
	if verbose == nil {
		verbose = func() bool { return false }
	}
	xlog.RegisterHandler(bridge{fn: fn, verbose: verbose})
}

type bridge struct {
	fn      func(level, msg string)
	verbose func() bool
}

// Handle implements log.Handler. Access lines (one per connection, naming
// the target) are left out; general lines are passed on by severity.
func (b bridge) Handle(msg xlog.Message) {
	m, ok := msg.(*xlog.GeneralMessage)
	if !ok {
		return
	}
	var level string
	switch m.Severity {
	case xlog.Severity_Error:
		level = "error"
	case xlog.Severity_Warning:
		level = "warn"
	case xlog.Severity_Info:
		level = "info"
	case xlog.Severity_Debug:
		level = "debug"
	default:
		return
	}
	if (level == "info" || level == "debug") && !b.verbose() {
		return
	}
	text := fmt.Sprint(m.Content)
	// Xray announces its start, with its version, as a warning so that it
	// shows at Xray's default log level. It is information, not a warning.
	if level == "warn" && strings.HasPrefix(text, "core: Xray ") && strings.HasSuffix(text, " started") {
		level = "info"
	}
	b.fn(level, text)
}
