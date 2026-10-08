package diag_test

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/cbor/diag"
)

// AnnotateItem is handed each annotated item's encoded bytes beside its
// path: a scalar's, a tag's content, a container's whole encoding — at its
// closer on one line, at its opening bracket when it spans lines.
func TestAnnotateItemSeesTheItemBytes(t *testing.T) {
	// [1, h'6869', 1001({1: 5})]
	b, err := hex.DecodeString("83" + "01" + "426869" + "d903e9" + "a10105")
	require.NoError(t, err)
	var seen []string
	hook := func(path []diag.PathElem, item []byte) string {
		seen = append(seen, fmt.Sprintf("%d:%x", len(path), item))
		return ""
	}
	_, err = diag.String(b, diag.Options{Compact: true, AnnotateItem: hook})
	require.NoError(t, err)
	assert.Equal(t, []string{"1:01", "1:426869", "3:05", "2:a10105", "0:" + hex.EncodeToString(b)}, seen,
		"elements, the tag's content and the map value under it, then the root at its closer; a map key is not annotated")

	seen = nil
	out, err := diag.String(b, diag.Options{Width: 4, AnnotateItem: func(path []diag.PathElem, item []byte) string {
		if len(path) == 0 {
			return fmt.Sprintf("%d bytes", len(item))
		}
		return ""
	}})
	require.NoError(t, err)
	assert.Contains(t, out, "[ / 11 bytes /", "a container over lines is labelled at its opening bracket, with its whole encoding")

	plain, err := diag.String(b, diag.Options{Compact: true, Annotate: func(path []diag.PathElem) string { return "" }, AnnotateItem: func([]diag.PathElem, []byte) string { return "item" }})
	require.NoError(t, err)
	assert.Contains(t, plain, "/ item /", "AnnotateItem is asked instead of Annotate")
}

// A comment is a label, not content: one earlier on a line does not push a
// container later on that line over the width and onto lines of its own.
func TestAnnotationDoesNotBreakTheRestOfItsLine(t *testing.T) {
	// ["a", [1]]
	b, err := hex.DecodeString("82" + "6161" + "8101")
	require.NoError(t, err)
	out, err := diag.String(b, diag.Options{Width: 20, AnnotateItem: func(path []diag.PathElem, item []byte) string {
		if len(path) == 1 && path[0].Index == 0 {
			return "a label far longer than the width"
		}
		return ""
	}})
	require.NoError(t, err)
	assert.Equal(t, `["a" / a label far longer than the width /, [1]]`, out)
}
