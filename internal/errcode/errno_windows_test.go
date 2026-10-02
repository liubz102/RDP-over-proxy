//go:build windows

package errcode

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

var refusedErrno = windows.WSAECONNREFUSED

func TestSystemMessageIsWindowsOwnText(t *testing.T) {
	msgs := SystemMessage(NetRefused)
	if len(msgs) == 0 {
		t.Fatal("no message for net.refused")
	}
	found := false
	for _, m := range msgs {
		if strings.TrimSpace(m) != "" && m == windows.WSAECONNREFUSED.Error() {
			found = true
		}
	}
	if !found {
		t.Fatalf("SystemMessage(net.refused) = %q, does not include Windows' text for WSAECONNREFUSED", msgs)
	}
}
