// Package ansi strips terminal control sequences so captured output can be
// rendered inside a plain-text view.
package ansi

import "strings"

// Strip removes ANSI escape sequences and control characters from raw terminal
// output. Newlines and tabs are preserved.
func Strip(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	runes := []rune(raw)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == 0x1b:
			i = skipEscape(runes, i)
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r == '\r':
			// Carriage returns rewrite the line; drop them so the captured
			// tail reads as plain text.
		case r < 0x20 || r == 0x7f:
			// drop other control characters
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// skipEscape consumes one ANSI escape sequence starting at index i (which
// points at ESC) and returns the index of its last byte.
func skipEscape(runes []rune, i int) int {
	if i+1 >= len(runes) {
		return i
	}
	switch runes[i+1] {
	case '[': // CSI ... final byte in @-~
		j := i + 2
		for j < len(runes) {
			if runes[j] >= '@' && runes[j] <= '~' {
				return j
			}
			j++
		}
		return len(runes) - 1
	case ']': // OSC ... terminated by BEL or ST
		j := i + 2
		for j < len(runes) {
			if runes[j] == 0x07 {
				return j
			}
			if runes[j] == 0x1b && j+1 < len(runes) && runes[j+1] == '\\' {
				return j + 1
			}
			j++
		}
		return len(runes) - 1
	default:
		return i + 1
	}
}
