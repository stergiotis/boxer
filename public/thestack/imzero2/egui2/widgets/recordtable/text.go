package recordtable

import (
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
)

// FitRunes cuts s to at most n runes plus an ellipsis: the text a fit-frame
// cell ([Cell.Untruncated]) hands egui_table to measure, so a paragraph cannot
// size its column past the grid's ceiling.
func FitRunes(s string, n int) (out string) {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos] + "…"
		}
		i++
	}
	return s
}

// StringLike reports whether values of this Arrow type render as free text:
// plain and large UTF-8 strings, and dictionary-encoded strings (ClickHouse
// LowCardinality(String) arrives as a dictionary). A grid left-aligns such
// columns so their text lines up under the left-aligned header, where numeric
// columns read fine centred.
func StringLike(dt arrow.DataType) (ok bool) {
	switch dt.ID() {
	case arrow.STRING, arrow.LARGE_STRING:
		return true
	case arrow.DICTIONARY:
		if d, isDict := dt.(*arrow.DictionaryType); isDict {
			return StringLike(d.ValueType)
		}
	}
	return false
}
