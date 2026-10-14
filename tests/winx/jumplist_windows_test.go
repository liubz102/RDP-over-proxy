package winx_test

import (
	. "github.com/liubz102/RDP-over-proxy/internal/winx"

	"testing"
)

// Reading Remote Desktop Connection's jump list works, and a prune that
// picks nothing changes nothing. The list is the user's: this only reads it.
// It may be empty (a build machine), which still reads the list.
func TestPruneJumpListReads(t *testing.T) {
	seen := 0
	removed, err := PruneJumpList("Microsoft.Windows.RemoteDesktop", func(l JumpLink) bool {
		seen++
		if l.Path == "" {
			t.Errorf("a shortcut without a program: %d characters of arguments", len(l.Args))
		}
		return false
	})
	if err != nil || removed != 0 {
		t.Fatalf("PruneJumpList = %d, %v", removed, err)
	}
	t.Logf("%d shortcuts", seen)
}
