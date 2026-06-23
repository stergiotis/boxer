package play

import (
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/thestack/utfsafe"
)

// truncateRunes returns s clamped to at most maxRunes runes, appending an
// ellipsis when it had to cut. It first runs utfsafe.EnsureUTF8 so the result
// is always valid UTF-8: a plain byte slice (s[:n]) can split a multi-byte
// rune and ship invalid UTF-8 to the Rust FFI wire (read_plain_s does
// String::from_utf8), which breaks the frame mid-render. Modelled on
// configview.truncate — the iteration over `range s` yields byte indices that
// always sit on a rune boundary. maxRunes <= 0 returns the sanitised string
// untouched.
func truncateRunes(s string, maxRunes int) string {
	s = utfsafe.EnsureUTF8(s)
	if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	i := 0
	for byteIdx := range s {
		if i == maxRunes {
			return s[:byteIdx] + "…"
		}
		i++
	}
	return s
}
