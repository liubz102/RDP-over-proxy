package engine

import (
	"fmt"

	xlog "github.com/xtls/xray-core/common/log"
)

// registerLogBridge makes fn receive Xray's log lines. Xray's logger is
// process-wide, and creating any Xray instance replaces it.
func registerLogBridge(fn func(level, msg string), verbose bool) {
	xlog.RegisterHandler(bridge{fn: fn, verbose: verbose})
}

type bridge struct {
	fn      func(level, msg string)
	verbose bool
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
	if !b.verbose && (level == "info" || level == "debug") {
		return
	}
	b.fn(level, fmt.Sprint(m.Content))
}
