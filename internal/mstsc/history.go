package mstsc

import (
	"encoding/binary"
	"slices"
	"strings"
	"unicode/utf16"
)

// HistoryKey is where mstsc keeps its list of recent computers, under
// HKEY_CURRENT_USER: the values MRU0 (the latest) to MRU9, which the Computer
// box of Remote Desktop Connection lists. mstsc adds the address it connects
// to, tunnel entrances included, as it connects.
const HistoryKey = `Software\Microsoft\Terminal Server Client\Default`

// HistorySize is how many computers mstsc's list holds. A new one goes
// first and, when the list is full, the last one goes.
const HistorySize = 10

// Prune works out mstsc's list of recent computers without the entries drop
// matches (dropped) and without repeats: mstsc never lists a computer twice,
// but its writes and ours meeting can leave one. Entries of known (the list
// as last pruned) that are missing now then come back at the end, in known's
// order: one for each entry dropped, while the list has room. Those are what
// the dropped entries pushed out. restored counts them.
func Prune(list, known []string, drop func(entry string) bool) (kept, dropped []string, restored int) {
	seen := map[string]bool{}
	for _, e := range list {
		k := strings.ToLower(e)
		switch {
		case drop(e):
			dropped = append(dropped, e)
		case seen[k]:
		default:
			seen[k] = true
			kept = append(kept, e)
		}
	}
	room := max(HistorySize, len(list)) - len(kept)
	for _, e := range known {
		if restored == min(len(dropped), room) {
			break
		}
		k := strings.ToLower(e)
		if seen[k] || drop(e) {
			continue
		}
		seen[k] = true
		kept = append(kept, e)
		restored++
	}
	return kept, dropped, restored
}

// addressProps are the .rdp properties that name the computer a file
// connects to, in lower case.
var addressProps = []string{"full address", "alternate full address"}

// ClearAddress empties the address an .rdp file opens with ("full address",
// and "alternate full address") where drop matches it, and reports whether it
// did. Every other byte stays as it was. It reads only what mstsc writes
// Default.rdp as, UTF-16LE with a byte-order mark; anything else comes back
// unchanged.
func ClearAddress(data []byte, drop func(entry string) bool) ([]byte, bool) {
	if len(data) < 2 || len(data)%2 != 0 || data[0] != 0xFF || data[1] != 0xFE {
		return data, false
	}
	units := make([]uint16, len(data)/2-1)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(data[2+2*i:])
	}
	out := make([]uint16, 0, len(units))
	changed := false
	for len(units) > 0 {
		line := units
		if end := slices.Index(units, '\n'); end >= 0 {
			line = units[:end+1]
		}
		units = units[len(line):]
		body := line
		for len(body) > 0 && (body[len(body)-1] == '\n' || body[len(body)-1] == '\r') {
			body = body[:len(body)-1]
		}
		if keep, ok := clearedLine(string(utf16.Decode(body)), drop); ok {
			out = append(out, utf16.Encode([]rune(keep))...)
			out = append(out, line[len(body):]...) // the line ending
			changed = true
			continue
		}
		out = append(out, line...)
	}
	if !changed {
		return data, false
	}
	result := make([]byte, 2+2*len(out))
	result[0], result[1] = 0xFF, 0xFE
	for i, u := range out {
		binary.LittleEndian.PutUint16(result[2+2*i:], u)
	}
	return result, true
}

// clearedLine returns line up to its value, "full address:s:", when it sets
// an address drop matches. Names and types are read as rdpfile reads them.
func clearedLine(line string, drop func(entry string) bool) (string, bool) {
	name, rest, ok := strings.Cut(line, ":")
	if !ok || !slices.Contains(addressProps, strings.ToLower(strings.TrimSpace(name))) {
		return "", false
	}
	typ, value, ok := strings.Cut(rest, ":")
	value = strings.TrimSpace(value)
	if !ok || !strings.EqualFold(typ, "s") || value == "" || !drop(value) {
		return "", false
	}
	return line[:len(line)-len(rest)+len(typ)+1], true
}
