package lwread

import (
	"cmp"
	"encoding/base64"
	"encoding/hex"
	"slices"
	"strconv"
	"unicode"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
)

// itemType is ct with its list or set modifier cleared: the type of one item.
func itemType(ct canonicaltypes.PrimitiveAstNodeI) canonicaltypes.PrimitiveAstNodeI {
	switch n := ct.(type) {
	case canonicaltypes.MachineNumericTypeAstNode:
		n.ScalarModifier = canonicaltypes.ScalarModifierNone
		return n
	case *canonicaltypes.MachineNumericTypeAstNode:
		out := *n
		out.ScalarModifier = canonicaltypes.ScalarModifierNone
		return out
	case canonicaltypes.StringAstNode:
		n.ScalarModifier = canonicaltypes.ScalarModifierNone
		return n
	case *canonicaltypes.StringAstNode:
		out := *n
		out.ScalarModifier = canonicaltypes.ScalarModifierNone
		return out
	case canonicaltypes.TemporalTypeAstNode:
		n.ScalarModifier = canonicaltypes.ScalarModifierNone
		return n
	case *canonicaltypes.TemporalTypeAstNode:
		out := *n
		out.ScalarModifier = canonicaltypes.ScalarModifierNone
		return out
	case canonicaltypes.NetworkTypeAstNode:
		n.ScalarModifier = canonicaltypes.ScalarModifierNone
		return n
	case *canonicaltypes.NetworkTypeAstNode:
		out := *n
		out.ScalarModifier = canonicaltypes.ScalarModifierNone
		return out
	}
	return ct
}

// isBytes reports a byte-string type.
func isBytes(ct canonicaltypes.PrimitiveAstNodeI) bool {
	switch n := ct.(type) {
	case canonicaltypes.StringAstNode:
		return n.BaseType == canonicaltypes.BaseTypeStringBytes
	case *canonicaltypes.StringAstNode:
		return n.BaseType == canonicaltypes.BaseTypeStringBytes
	}
	return false
}

// isNumeric reports a machine-numeric type.
func isNumeric(ct canonicaltypes.PrimitiveAstNodeI) bool {
	switch ct.(type) {
	case canonicaltypes.MachineNumericTypeAstNode, *canonicaltypes.MachineNumericTypeAstNode:
		return true
	}
	return false
}

// spell is the reading's spelling of the driver's text raw of type ct. The
// driver writes bytes as standard base64; they read as text when they are
// printable UTF-8 and as 0x-hex otherwise. Every other type reads as the
// driver wrote it.
func spell(raw string, ct canonicaltypes.PrimitiveAstNodeI) string {
	if !isBytes(ct) {
		return raw
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return raw
	}
	if printable(b) {
		return string(b)
	}
	return "0x" + hex.EncodeToString(b)
}

// printable reports valid UTF-8 without control characters but tab and
// line breaks.
func printable(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}

// sortItems puts a set's items in value order: numbers by magnitude,
// everything else by its spelling.
func sortItems(items []Item, ct canonicaltypes.PrimitiveAstNodeI) {
	if isNumeric(ct) {
		slices.SortStableFunc(items, func(a, b Item) int {
			x, ex := strconv.ParseFloat(a.Raw, 64)
			y, ey := strconv.ParseFloat(b.Raw, 64)
			if ex == nil && ey == nil {
				return cmp.Compare(x, y)
			}
			return cmp.Compare(a.Text, b.Text)
		})
		return
	}
	slices.SortStableFunc(items, func(a, b Item) int { return cmp.Compare(a.Text, b.Text) })
}

// cutUTF8 cuts s to at most n bytes without splitting a rune.
func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
