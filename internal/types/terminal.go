// SPDX-License-Identifier: MIT

package types

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// TerminalSafe returns a string a device or a client sent (an object name, a description,
// a character string value, a message) for text and table output. The characters that
// would act on a terminal instead of being shown are written as escapes, the way
// strconv.Quote writes them: the C0 controls (\n, \r, \t, \x1b, …), DEL (\x7f), the C1
// controls (\u0080 to \u009f), and the bidirectional embeddings, overrides and isolates
// (\u202a to \u202e, \u2066 to \u2069). An octet that is not UTF-8 is written \xNN. Every
// other character is kept as it is, backslashes and quotes included. JSON output keeps
// strings as they are.
func TerminalSafe(s string) string {
	var b strings.Builder
	done := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		invalid := r == utf8.RuneError && size == 1
		if !invalid && !control(r) {
			i += size
			continue
		}
		if done == 0 {
			b.Grow(len(s) + 8)
		}
		b.WriteString(s[done:i])
		if invalid {
			b.WriteString(`\x`)
			b.WriteString(strconv.FormatUint(uint64(s[i]>>4), 16))
			b.WriteString(strconv.FormatUint(uint64(s[i]&0xF), 16))
		} else {
			q := strconv.QuoteRune(r)
			b.WriteString(q[1 : len(q)-1])
		}
		i += size
		done = i
	}
	if done == 0 {
		return s
	}
	b.WriteString(s[done:])
	return b.String()
}

// control reports whether TerminalSafe writes r as an escape.
func control(r rune) bool {
	switch {
	case r < 0x20, r == 0x7F, r >= 0x80 && r <= 0x9F:
		return true
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	default:
		return false
	}
}
