package attention

import (
	"strconv"
	"strings"
)

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func itoa(v uint64) string { return strconv.FormatUint(v, 10) }

func cutPrefix(s string, prefix string) (rest string, ok bool) { return strings.CutPrefix(s, prefix) }
