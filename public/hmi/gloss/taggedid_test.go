package gloss

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The reference ids below are composed with identifier's own AddTag, so a
// change to the encoding breaks these goldens rather than quietly changing
// what a column reads as:
//
//	tag value 12 (code width 6)   body 58   → 12393906174523605050
//	tag value 1  (code width 2)   body 1    → 13835058055282163713  (> 2^63)
//	tag value 4294967295 (w 47)   body 4711 → 2632363476778357351
const (
	tid12x58 = uint64(12393906174523605050)
	tid1x1   = uint64(13835058055282163713)
	tidMaxTv = uint64(2632363476778357351)
	// tag value 12 over the reserved body 0 — well-formed, never minted.
	tid12x0 = uint64(12393906174523604992)
)

func TestSplitTaggedId(t *testing.T) {
	p, ok := SplitTaggedId(tid12x58)
	require.True(t, ok)
	assert.Equal(t, uint32(12), p.TagValue)
	assert.Equal(t, 6, p.TagWidth)
	assert.Equal(t, uint64(58), p.Body)
	assert.Equal(t, uint64(288230376151711743), p.MaxBody, "6 tag bits leave 58 body bits")
	assert.Equal(t, "c:3a", p.Inline())
	assert.Equal(t, "0xac0000000000003a", p.Hex())

	p, ok = SplitTaggedId(tid1x1)
	require.True(t, ok, "the smallest tag sets the top two bits, so most ids are above 2^63")
	assert.Equal(t, uint32(1), p.TagValue)
	assert.Equal(t, 2, p.TagWidth)
	assert.Equal(t, uint64(1), p.Body)
	assert.Equal(t, "1:1", p.Inline())

	p, ok = SplitTaggedId(tidMaxTv)
	require.True(t, ok)
	assert.Equal(t, uint32(4294967295), p.TagValue)
	assert.Equal(t, 47, p.TagWidth)
	assert.Equal(t, "ffffffff:1267", p.Inline())

	// The reserved body still splits: a real tag over a body never minted.
	p, ok = SplitTaggedId(tid12x0)
	require.True(t, ok)
	assert.Equal(t, uint32(12), p.TagValue)
	assert.Zero(t, p.Body)
}

func TestSplitTaggedIdRefusals(t *testing.T) {
	for _, v := range []uint64{
		0,                  // the reserved zero word
		1,                  // no comma anywhere
		4294967296,         // a lone bit: no adjacent pair
		0x5555555555555555, // alternating bits: no adjacent pair
		0xAAAAAAAAAAAAAAAA, // the other alternation
	} {
		_, ok := SplitTaggedId(v)
		assert.False(t, ok, "%d carries no fibonacci comma and is not a tagged id", v)
	}
}

// The face: hex tag value, colon, hex body — and the plain value in the
// warning tone whenever the split cannot be shown.
func TestTaggedIdFace(t *testing.T) {
	g := instFor(t, "id@gloss/taggedid")

	assert.Equal(t, Inline{Text: "c:3a"}, g.Inline(num("12393906174523605050")))
	assert.Equal(t, Inline{Text: "1:1"}, g.Inline(num("13835058055282163713")))
	assert.Equal(t, Inline{Text: "ffffffff:1267"}, g.Inline(num("2632363476778357351")))

	// A toString(id) column is still a column of ids, and so is one a user
	// typed as hex.
	assert.Equal(t, Inline{Text: "c:3a"}, g.Inline(txt("12393906174523605050")))
	assert.Equal(t, Inline{Text: "c:3a"}, g.Inline(txt("0xac0000000000003a")))
	assert.Equal(t, Inline{Text: "c:3a"}, g.Inline(txt("  12393906174523605050 ")), "surrounding space is not part of the value")

	// Not an id: the plain value, warning tone.
	assert.Equal(t, Inline{Text: "4294967296", Tone: ToneWarning}, g.Inline(num("4294967296")))
	assert.Equal(t, Inline{Text: "not-a-number", Tone: ToneWarning}, g.Inline(txt("not-a-number")))
	// A tag over the reserved body 0: shown, and flagged.
	assert.Equal(t, Inline{Text: "c:0", Tone: ToneWarning}, g.Inline(num("12393906174523604992")))
}

func TestTaggedIdFaceOverArrow(t *testing.T) {
	mem := memory.NewGoAllocator()
	b := array.NewUint64Builder(mem)
	defer b.Release()
	b.Append(tid12x58)
	b.Append(tid1x1)
	b.AppendNull()
	arr := b.NewArray()
	defer arr.Release()

	g := instFor(t, "id@gloss/taggedid")
	assert.Equal(t, Inline{Text: "c:3a"}, g.Inline(ArrowCell{Arr: arr, Row: 0}))
	assert.Equal(t, Inline{Text: "1:1"}, g.Inline(ArrowCell{Arr: arr, Row: 1}),
		"an id above 2^63 must not fall back to Text or round through a float")
	assert.Equal(t, Inline{}, g.Inline(ArrowCell{Arr: arr, Row: 2}), "a null is empty and untoned")
}

func TestTaggedIdAcceptsAndParams(t *testing.T) {
	g := instFor(t, "id@gloss/taggedid")
	ok, _ := g.Accepts(ValueKindNumeric)
	assert.True(t, ok)
	ok, _ = g.Accepts(ValueKindText)
	assert.True(t, ok)
	ok, reason := g.Accepts(ValueKindBytes)
	assert.False(t, ok)
	assert.Contains(t, reason, MediaTypeTaggedId)

	// No parameters: one is as loud as an unknown media type.
	d, declared := Default().ParseColumn("id@gloss/taggedid;base=10")
	require.True(t, declared)
	assert.Contains(t, d.Reason, "takes no parameters")
}

// ArrowCell.Uint64 is what the face rides on: the full unsigned range, and a
// refusal rather than a wrap for anything that is not a non-negative integer.
func TestArrowCellUint64(t *testing.T) {
	mem := memory.NewGoAllocator()

	u64 := array.NewUint64Builder(mem)
	defer u64.Release()
	u64.Append(tid1x1)
	u64.AppendNull()
	au := u64.NewArray()
	defer au.Release()
	v, ok := ArrowCell{Arr: au, Row: 0}.Uint64()
	require.True(t, ok)
	assert.Equal(t, tid1x1, v)
	_, ok = ArrowCell{Arr: au, Row: 1}.Uint64()
	assert.False(t, ok, "a null has no value")
	_, ok = ArrowCell{Arr: au, Row: 0}.Int64()
	assert.False(t, ok, "the same value has no Int64 reading — which is why Uint64 exists")

	i64 := array.NewInt64Builder(mem)
	defer i64.Release()
	i64.Append(42)
	i64.Append(-1)
	ai := i64.NewArray()
	defer ai.Release()
	v, ok = ArrowCell{Arr: ai, Row: 0}.Uint64()
	require.True(t, ok)
	assert.Equal(t, uint64(42), v)
	_, ok = ArrowCell{Arr: ai, Row: 1}.Uint64()
	assert.False(t, ok, "a negative value is not a uint64 and must not wrap")

	i32 := array.NewInt32Builder(mem)
	defer i32.Release()
	i32.Append(7)
	a32 := i32.NewArray()
	defer a32.Release()
	v, ok = ArrowCell{Arr: a32, Row: 0}.Uint64()
	require.True(t, ok)
	assert.Equal(t, uint64(7), v)

	f64 := array.NewFloat64Builder(mem)
	defer f64.Release()
	f64.Append(1.5)
	af := f64.NewArray()
	defer af.Release()
	_, ok = ArrowCell{Arr: af, Row: 0}.Uint64()
	assert.False(t, ok, "Uint64 is the integer reading; a float goes through Float64")
}

func TestTextCellUint64(t *testing.T) {
	v, ok := TextCell{S: "13835058055282163713", K: ValueKindNumeric}.Uint64()
	require.True(t, ok)
	assert.Equal(t, tid1x1, v)
	_, ok = TextCell{S: "0xac0000000000003a", K: ValueKindText}.Uint64()
	assert.False(t, ok, "base 10, like Int64 and Float64: the text is a marshalled value, not a literal")
	_, ok = TextCell{S: "-1", K: ValueKindNumeric}.Uint64()
	assert.False(t, ok)
}
