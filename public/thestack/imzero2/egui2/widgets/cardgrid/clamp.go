package cardgrid

import (
	"strings"
	"unicode/utf8"
)

// The rune budgets a host bounds a slot's text to before it enters the
// model. They are what the hover shows at most, and what crosses to the
// renderer per card per frame at most; what a card draws is cut further, to
// the slot's estimated room.
const (
	// MaxTitleRunes bounds a title.
	MaxTitleRunes = 240
	// MaxLineRunes bounds a one-line slot: overline, subtitle, footer, and a
	// fact's label.
	MaxLineRunes = 160
	// MaxBodyRunes bounds a body's text.
	MaxBodyRunes = 1200
	// MaxFactRunes bounds a fact's value.
	MaxFactRunes = 160
	// MaxTagRunes bounds one tag.
	MaxTagRunes = 40
)

// ellipsis marks a cut.
const ellipsis = "…"

// OneLine bounds s for a one-line slot: newlines and tabs fold to a space,
// runs of spaces collapse, other C0 controls and DEL are dropped, invalid
// UTF-8 becomes U+FFFD, and the result is cut to maxRunes with an ellipsis.
// It stops reading s once the budget is spent, so a megabyte cell costs what
// its first few hundred bytes cost.
func OneLine(s string, maxRunes int) string {
	return clamp(s, maxRunes, 1)
}

// Lines is OneLine for a multi-line slot: newlines are kept, up to maxLines
// lines (a CRLF counts once), and everything after is cut.
func Lines(s string, maxRunes int, maxLines int) string {
	return clamp(s, maxRunes, max(1, maxLines))
}

func clamp(s string, maxRunes int, maxLines int) string {
	out, _ := clampCut(s, maxRunes, maxLines)
	return out
}

// clampCut is clamp reporting whether it cut — dropped text, as opposed to
// only folding whitespace or controls — which is when a hover is worth
// showing.
func clampCut(s string, maxRunes int, maxLines int) (out string, cut bool) {
	if maxRunes <= 0 || s == "" {
		return "", s != ""
	}
	if clean(s, maxRunes, maxLines) {
		return s, false
	}
	var b strings.Builder
	b.Grow(min(len(s), 4*maxRunes) + len(ellipsis))
	runes, lines := 0, 1
	space := true // swallows leading spaces, and the second of a run
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == '\r':
			continue
		case r == '\n' && maxLines > 1:
			if lines == maxLines {
				cut = strings.TrimSpace(s[i:]) != ""
				i = len(s)
				continue
			}
			lines++
			b.WriteByte('\n')
			runes++
			space = true
			continue
		case r == '\n' || r == '\t' || r == ' ':
			if space {
				continue
			}
			space = true
			r = ' '
		case r < 0x20 || r == 0x7f:
			continue
		default:
			space = false
		}
		if runes == maxRunes {
			cut = true
			break
		}
		b.WriteRune(r) // utf8.RuneError for an invalid byte, which is the point
		runes++
	}
	out = strings.TrimRight(b.String(), " \n")
	if cut {
		out += ellipsis
	}
	return out, cut
}

// clean reports whether s can be returned as it is: valid, within the rune
// and line budgets, and free of anything clamp would fold or drop. The
// common case — a short name — then allocates nothing.
func clean(s string, maxRunes int, maxLines int) bool {
	if len(s) > 4*maxRunes {
		return false
	}
	runes, lines := 0, 1
	prevSpace := true
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size <= 1 {
			return false
		}
		i += size
		switch {
		case r == '\n' && maxLines > 1:
			lines++
			if lines > maxLines || i == len(s) {
				return false
			}
			prevSpace = true
			runes++
			continue
		case r == ' ':
			if prevSpace {
				return false
			}
			prevSpace = true
		case r < 0x20 || r == 0x7f:
			return false
		default:
			prevSpace = false
		}
		runes++
		if runes > maxRunes {
			return false
		}
	}
	return !prevSpace || s == ""
}

// displayCut cuts an already-bounded string to the runes a slot is estimated
// to have room for. It reports whether it cut, which is when the hover is
// worth showing.
func displayCut(s string, maxRunes int) (out string, cut bool) {
	if len(s) <= maxRunes { // bytes ≥ runes, so this is the cheap sure case
		return s, false
	}
	n := 0
	for i := range s {
		if n == maxRunes {
			return strings.TrimRight(s[:i], " \n") + ellipsis, true
		}
		n++
	}
	return s, false
}
