package mstsc

import "strings"

// titleSeparator separates the parts of mstsc's window title.
const titleSeparator = " - "

// Title is the title for mstsc's remote session window: the connection's
// name in front of mstsc's own title, the way mstsc itself shows the name of
// an .rdp file ("Office - 127.1.2.3:13389 - Remote Desktop Connection").
// Started with /v:, mstsc names the window after the address only, the
// tunnel entrance, which says nothing about the computer behind it. A title
// that already starts with the name stays as it is, and so does one mstsc
// has not set yet.
func Title(name, current string) string {
	if name == "" || current == "" || strings.HasPrefix(current, name+titleSeparator) {
		return current
	}
	return name + titleSeparator + current
}
