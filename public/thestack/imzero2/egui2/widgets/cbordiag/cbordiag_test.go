package cbordiag

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/semistructured/cbor/diag"
)

// TestKeyMovesWithEveryOption pins the memo key to the options that change
// the rendering: a toggle that did not move the key would show a stale
// notation.
func TestKeyMovesWithEveryOption(t *testing.T) {
	item := []byte{0x83, 0x01, 0x02, 0x03}
	base := keyOf(item, diag.Options{})
	variants := []diag.Options{
		{Compact: true},
		{FloatPrecision: true},
		{TagComments: true},
		{Sequence: true},
		{Indent: "\t"},
		{Width: 40},
		{BytesFold: 8},
	}
	seen := map[uint64]bool{base: true}
	for i, v := range variants {
		k := keyOf(item, v)
		assert.False(t, seen[k], "variant %d collides", i)
		seen[k] = true
	}
	assert.NotEqual(t, base, keyOf([]byte{0x83, 0x01, 0x02, 0x04}, diag.Options{}))
	assert.Equal(t, base, keyOf(item, diag.Options{}))
}

// TestStateMemo pins that a State rebuilds only when its key moves and that
// the toggle it owns takes part.
func TestStateMemo(t *testing.T) {
	var st State
	item := []byte{0x83, 0x01, 0x02, 0x03}
	st.prepare(item, diag.Options{})
	assert.Equal(t, "[1, 2, 3]", st.Text())
	assert.NoError(t, st.Err())
	k := st.key
	st.prepare(item, diag.Options{Compact: true}) // the state's toggle wins over the option
	assert.Equal(t, k, st.key)
	st.Compact = true
	st.prepare(item, diag.Options{})
	assert.NotEqual(t, k, st.key)
	st.prepare([]byte{0x83, 0x01}, diag.Options{})
	assert.Error(t, st.Err())
	assert.Contains(t, st.Text(), "/ error: ")
}
