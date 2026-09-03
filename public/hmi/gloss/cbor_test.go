package gloss

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cborInst(t *testing.T, name string) InstanceI {
	t.Helper()
	d, declared := Default().ParseColumn(name)
	require.True(t, declared)
	require.Empty(t, d.Reason)
	return d.Instance
}

func unhexCbor(t *testing.T, s string) string {
	t.Helper()
	b, err := hex.DecodeString(s)
	require.NoError(t, err)
	return string(b)
}

// The inline face is the compact notation: one line, no tag comments, no
// fold — what a table cell can hold.
func TestCborInlineFace(t *testing.T) {
	inst := cborInst(t, "item@application/cbor")
	// [1, [2, 3], {"a": 4}]
	raw := unhexCbor(t, "8301820203a1616104")
	face := inst.Inline(TextCell{S: raw, K: ValueKindBytes})
	assert.Equal(t, `[1, [2, 3], {"a": 4}]`, face.Text)
	assert.Equal(t, ToneNeutral, face.Tone)

	assert.Equal(t, Inline{}, inst.Inline(TextCell{S: "", K: ValueKindBytes}))
}

// A truncated item is shown, not hidden: what parsed, then the failure, in
// the error tone — the row a person is looking for.
func TestCborInlineMalformed(t *testing.T) {
	inst := cborInst(t, "item@application/cbor")
	face := inst.Inline(TextCell{S: unhexCbor(t, "830102"), K: ValueKindBytes})
	assert.Equal(t, ToneError, face.Tone)
	assert.Contains(t, face.Text, "/ error: ")
}

// Past the bound the face is a descriptor, as an image's is: the inline face
// runs per visible cell per frame and has no cache behind it.
func TestCborInlineOversizeIsDescriptor(t *testing.T) {
	inst := cborInst(t, "item@application/cbor")
	// A definite-length byte string of cborInlineMaxBytes payload.
	n := cborInlineMaxBytes
	raw := "\x5a" + string([]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}) + strings.Repeat("\x00", n)
	face := inst.Inline(TextCell{S: raw, K: ValueKindBytes})
	assert.Equal(t, "[application/cbor · 1.0 KiB]", face.Text)
	assert.Equal(t, ToneNeutral, face.Tone)
}

// sequence is read once, in Bind, and reaches both faces through Options.
func TestCborSequenceParam(t *testing.T) {
	seq := unhexCbor(t, "010203")

	one := cborInst(t, "item@application/cbor")
	face := one.Inline(TextCell{S: seq, K: ValueKindBytes})
	assert.Equal(t, ToneError, face.Tone, "without ;sequence, bytes after the first item are a truncation")

	many := cborInst(t, "item@application/cbor;sequence=1")
	face = many.Inline(TextCell{S: seq, K: ValueKindBytes})
	assert.Equal(t, "1, 2, 3", face.Text)
	assert.Equal(t, ToneNeutral, face.Tone)

	opts := many.(*cborInstance).Options(false)
	assert.True(t, opts.Sequence)
	assert.False(t, opts.Compact)
	assert.True(t, opts.TagComments, "the block face names the tags it knows")
	assert.Positive(t, opts.BytesFold, "the block face folds a long byte string across rows")
}

// The parameter contract: a closed value set, and the reserved encoding.
func TestCborParams(t *testing.T) {
	d, declared := Default().ParseColumn("item@application/cbor;sequence=yes")
	require.True(t, declared)
	assert.NotEmpty(t, d.Reason, "sequence takes 0 or 1")

	d, declared = Default().ParseColumn("item@application/cbor;encoding=base64")
	require.True(t, declared)
	assert.Contains(t, d.Reason, "reserved")

	inst := cborInst(t, "item@application/cbor")
	ok, _ := inst.Accepts(ValueKindBytes)
	assert.True(t, ok)
	ok, _ = inst.Accepts(ValueKindText)
	assert.True(t, ok, "a ClickHouse String holding CBOR arrives as text")
	ok, reason := inst.Accepts(ValueKindNumeric)
	assert.False(t, ok)
	assert.Contains(t, reason, "application/cbor expects text or bytes, got numeric")

	assert.Nil(t, (&cborGloss{}).Affinities(), "no aspect says these bytes are CBOR")
}
