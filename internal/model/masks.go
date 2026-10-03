package model

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// Checks of the JSON settings that go to Xray as they are: its "finalmask"
// (masks for the packets on the wire, QUIC parameters), XHTTP's "extra", and
// the transport of a custom outbound.
//
// Xray takes some values it should refuse and then crashes on them while
// connecting, in a goroutine of its own where nothing can recover: a
// negative length slices a packet backwards (the fragment mask), a negative
// size makes a negative buffer (the noise mask, XHTTP's padding and
// chunks). A share link can carry any of them. So:
//   - no negative number may appear in these settings, written as a number
//     or as a number or range in a string ("-5", "1--5");
//   - masks from share links and the editor are limited to the ones a
//     server needs to be reached by (disguise headers, mKCP's obfuscation,
//     Salamander), none of which has a size to get wrong. A custom outbound
//     may use any mask; it is the user's own.

// safeMasks are the UDP masks share links and the editor may set.
var safeMasks = map[string]bool{
	"header-dns":       true,
	"header-dtls":      true,
	"header-srtp":      true,
	"header-utp":       true,
	"header-wechat":    true,
	"header-wireguard": true,
	"mkcp-original":    true,
	"mkcp-aes128gcm":   true,
	"salamander":       true,
}

// negativeOK are the keys whose negative values mean "off" rather than a
// size: XHTTP's HTTP/2 keep-alive.
var negativeOK = map[string]bool{"hKeepAlivePeriod": true}

// salamanderMinLen is the shortest Salamander password Xray takes; a shorter
// one fails only when connecting.
const salamanderMinLen = 4

// negativeNumber matches a number or range in a string with a negative part.
var negativeNumber = regexp.MustCompile(`^(-\d+(-+\d+)?|\d+--\d+)$`)

// decodeJSON reads one JSON value, keeping numbers as written.
func decodeJSON(s string) (any, bool) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil || dec.More() {
		return nil, false
	}
	return v, true
}

// nonNegative reports whether v holds no negative number, apart from the
// keys in negativeOK.
func nonNegative(v any) bool {
	switch v := v.(type) {
	case map[string]any:
		for k, inner := range v {
			if !negativeOK[k] && !nonNegative(inner) {
				return false
			}
		}
	case []any:
		for _, inner := range v {
			if !nonNegative(inner) {
				return false
			}
		}
	case json.Number:
		return !strings.HasPrefix(string(v), "-")
	case string:
		return !negativeNumber.MatchString(strings.TrimSpace(v))
	}
	return true
}

// nested collects the values of key anywhere in v.
func nested(v any, key string) []any {
	var out []any
	switch v := v.(type) {
	case map[string]any:
		for k, inner := range v {
			if k == key {
				out = append(out, inner)
			}
			out = append(out, nested(inner, key)...)
		}
	case []any:
		for _, inner := range v {
			out = append(out, nested(inner, key)...)
		}
	}
	return out
}

// finalMaskProblem checks finalmask settings from a share link or the
// editor: UDP masks from safeMasks, no TCP masks, QUIC parameters (Xray
// checks their values itself). It returns the code of the problem, or "".
func finalMaskProblem(v any) string {
	obj, ok := v.(map[string]any)
	if !ok || !nonNegative(obj) {
		return CodeInvalid
	}
	for k, inner := range obj {
		switch k {
		case "udp":
			masks, ok := inner.([]any)
			if !ok {
				return CodeInvalid
			}
			for _, m := range masks {
				if code := maskProblem(m); code != "" {
					return code
				}
			}
		case "tcp":
			masks, ok := inner.([]any)
			if !ok {
				return CodeInvalid
			}
			if len(masks) > 0 {
				return CodeUnsupported
			}
		case "quicParams":
			if _, ok := inner.(map[string]any); !ok {
				return CodeInvalid
			}
		default:
			return CodeUnsupported
		}
	}
	return ""
}

func maskProblem(m any) string {
	mask, ok := m.(map[string]any)
	if !ok {
		return CodeInvalid
	}
	typ, _ := mask["type"].(string)
	if !safeMasks[typ] {
		return CodeUnsupported
	}
	if typ == "salamander" {
		settings, _ := mask["settings"].(map[string]any)
		if password, _ := settings["password"].(string); len(password) < salamanderMinLen {
			return CodeInvalid
		}
	}
	return ""
}

// finalMask checks a finalmask field.
func (e *FieldErrors) finalMask(field, value string) {
	switch {
	case value == "":
	case len(value) > MaxOutboundLen:
		e.add(field, CodeTooLong)
	default:
		v, ok := decodeJSON(value)
		if !ok {
			e.add(field, CodeInvalid)
		} else if code := finalMaskProblem(v); code != "" {
			e.add(field, code)
		}
	}
}

// extra checks XHTTP's extra settings: a JSON object without negative
// numbers, whose download path, a transport of its own, follows the rules
// of finalmask too.
func (e *FieldErrors) extra(field, value string) {
	switch {
	case value == "":
	case len(value) > MaxOutboundLen:
		e.add(field, CodeTooLong)
	default:
		v, ok := decodeJSON(value)
		if _, isObject := v.(map[string]any); !ok || !isObject || !nonNegative(v) {
			e.add(field, CodeInvalid)
			return
		}
		for _, fm := range nested(v, "finalmask") {
			if code := finalMaskProblem(fm); code != "" {
				e.add(field, code)
				return
			}
		}
	}
}

// customTransportSafe reports whether a custom outbound's masks and XHTTP
// settings are free of negative numbers.
func customTransportSafe(ob map[string]any) bool {
	stream, _ := ob["streamSettings"].(map[string]any)
	for _, key := range []string{"finalmask", "xhttpSettings", "splithttpSettings"} {
		if inner, ok := stream[key]; ok && !nonNegative(inner) {
			return false
		}
	}
	return true
}

// validPadding checks the padding settings of VLESS Encryption as Xray
// reads them (proxy/vless/encryption ParsePadding): lengths and gaps
// "a-b-c" by turns, the first length at least 100-35-35, and the lengths
// adding up to at most 65553 bytes. Xray finds a malformed one only when it
// builds the outbound.
func validPadding(parts []string) bool {
	total := 0
	for i, p := range parts {
		x := strings.Split(p, "-")
		if len(x) != 3 {
			return false
		}
		var y [3]int
		for j, s := range x {
			n, err := strconv.Atoi(s)
			if err != nil || n < 0 {
				return false
			}
			y[j] = n
		}
		if i == 0 && (y[0] < 100 || y[1] < 35 || y[2] < 35) {
			return false
		}
		if i%2 == 0 {
			total += max(y[1], y[2])
		}
	}
	return total <= 18+65535
}
