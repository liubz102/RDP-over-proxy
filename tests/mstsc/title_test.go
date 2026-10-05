package mstsc_test

import (
	"testing"

	"github.com/liubz102/RDP-over-proxy/internal/mstsc"
)

func TestTitle(t *testing.T) {
	const own = "127.1.2.3:13389 - Remote Desktop Connection"
	cases := []struct{ name, current, want string }{
		{"Office", own, "Office - " + own},
		{"办公室", "127.1.2.3:13389 - 远程桌面连接", "办公室 - 127.1.2.3:13389 - 远程桌面连接"},
		// The whole name, dots and all: mstsc itself would cut an .rdp file's
		// name at the first dot.
		{"pc.example.com", own, "pc.example.com - " + own},
		// The name is in front already.
		{"Office", "Office - " + own, "Office - " + own},
		{"127.1.2.3:13389", own, own},
		// Only the whole name counts.
		{"Off", "Office - " + own, "Off - Office - " + own},
		// mstsc has not titled the window yet; its title will come.
		{"Office", "", ""},
		{"", own, own},
	}
	for _, c := range cases {
		if got := mstsc.Title(c.name, c.current); got != c.want {
			t.Errorf("Title(%q, %q) = %q, want %q", c.name, c.current, got, c.want)
		}
	}
}
