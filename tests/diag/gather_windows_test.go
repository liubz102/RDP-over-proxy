package diag_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/diag"

	"strings"
	"testing"
)

// Gather reads this computer; what it finds varies, but the parts every
// Windows has must be there, and nothing may name the Windows account.
func TestGather(t *testing.T) {
	f := Gather(Options{App: "test", Xray: "test", Logs: t.TempDir()})
	if f.Windows.Major < 10 || f.Windows.Build == 0 {
		t.Errorf("Windows = %+v", f.Windows)
	}
	if f.Mstsc == "" {
		t.Error("mstsc.exe has no version")
	}
	if f.Home == "" {
		t.Error("no profile folder")
	}
	if !f.LogsLocal {
		t.Error("the log folder, a temporary folder, is not on this PC")
	}
	for _, it := range Build(f) {
		for _, s := range []string{it.Text, it.Detail, it.Args["error"]} {
			if strings.Contains(strings.ToLower(s), strings.ToLower(f.Home)) {
				t.Errorf("%s names the profile folder: %q", it.Key, s)
			}
		}
	}
}
