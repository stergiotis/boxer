//go:build llm_generated_opus47

package example

import (
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
	"github.com/stretchr/testify/require"
)

// loadSingleEntity is a small helper: writes one entity with the given Text
// section attributes, transfers, and loads into a fresh RA.
func loadSingleEntity(t *testing.T, ts time.Time, writeText func(sec *InEntityTestTableSectionText)) *ReadAccessTestTable {
	t.Helper()
	dml := NewInEntityTestTable(memory.DefaultAllocator, 1)
	ent := dml.BeginEntity()
	ent.SetId(0)
	ent.SetTimestamp(ts, []time.Time{ts}) // plain Proc array, cardinality 1
	writeText(ent.GetSectionText())
	require.NoError(t, ent.CheckErrors())
	require.NoError(t, ent.CommitEntity())

	records, err := dml.TransferRecords(nil)
	require.NoError(t, err)
	require.Len(t, records, 1)

	ra := NewReadAccessTestTable()
	require.NoError(t, ra.LoadFromRecord(records[0]))
	return ra
}

// Happy path: tagged section's single-attribute populated via
// BeginAttributeSingle reads back via GetAttrValueSingle in one call,
// returning scalar + co-arrays atomically.
func TestGetAttrValueSingleTaggedHappyPath(t *testing.T) {
	ts := time.UnixMilli(1700000000000).UTC()
	ra := loadSingleEntity(t, ts, func(sec *InEntityTestTableSectionText) {
		sec.BeginAttributeSingle("hello", 2, "hi").
			AddMembershipLowCardRef(0).
			EndAttribute()
	})

	const eIdx = runtime.EntityIdx(0)
	const aIdx = runtime.AttributeIdx(0)

	text, wordLength, words, err := ra.Text.Attributes.GetAttrValueSingle(eIdx, aIdx)
	require.NoError(t, err)
	require.Equal(t, "hello", text)
	require.EqualValues(t, 2, wordLength)
	require.Equal(t, "hi", words)

	// Sanity-check vs the iter.Seq variant.
	require.EqualValues(t, []uint32{wordLength},
		slices.Collect(ra.Text.Attributes.GetAttrValueWordLength(eIdx, aIdx)))
	require.EqualValues(t, []string{words},
		slices.Collect(ra.Text.Attributes.GetAttrValueWords(eIdx, aIdx)))
}

// Cardinality > 1 returns an error and leaves return values at zero.
func TestGetAttrValueSingleTaggedRejectsMultipleElements(t *testing.T) {
	ts := time.UnixMilli(1700000000000).UTC()
	ra := loadSingleEntity(t, ts, func(sec *InEntityTestTableSectionText) {
		sec.BeginAttribute("hello").
			AddToCoContainers(5, "hello").
			AddToCoContainers(5, "world").
			AddMembershipLowCardRef(0).
			EndAttribute()
	})

	const eIdx = runtime.EntityIdx(0)
	const aIdx = runtime.AttributeIdx(0)

	_, _, _, err := ra.Text.Attributes.GetAttrValueSingle(eIdx, aIdx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected exactly one element")
	// Sanity: iter.Seq variant still works.
	require.EqualValues(t, []string{"hello", "world"},
		slices.Collect(ra.Text.Attributes.GetAttrValueWords(eIdx, aIdx)))
}

// Plain attribute class: scalar ts + cardinality-1 Proc returns both in one
// call.
func TestGetAttrValueSinglePlainHappyPath(t *testing.T) {
	ts := time.UnixMilli(1700000000000).UTC()
	ra := loadSingleEntity(t, ts, func(sec *InEntityTestTableSectionText) {
		sec.BeginAttributeSingle("noop", 0, "").EndAttribute()
	})

	const eIdx = runtime.EntityIdx(0)
	tsOut, proc, err := ra.EntityTimestamp.GetAttrValueSingle(eIdx)
	require.NoError(t, err)
	require.EqualValues(t, ts, tsOut)
	require.EqualValues(t, ts, proc)
}

// Cardinality-0 Proc → error from plain GetAttrValueSingle.
func TestGetAttrValueSinglePlainRejectsZeroElements(t *testing.T) {
	ts := time.UnixMilli(1700000000000).UTC()

	dml := NewInEntityTestTable(memory.DefaultAllocator, 1)
	ent := dml.BeginEntity()
	ent.SetId(0)
	ent.SetTimestamp(ts, nil) // plain Proc: zero elements
	ent.GetSectionText().BeginAttributeSingle("x", 1, "x").EndAttribute()
	require.NoError(t, ent.CheckErrors())
	require.NoError(t, ent.CommitEntity())

	var records []arrow.RecordBatch
	records, err := dml.TransferRecords(nil)
	require.NoError(t, err)

	ra := NewReadAccessTestTable()
	require.NoError(t, ra.LoadFromRecord(records[0]))

	const eIdx = runtime.EntityIdx(0)
	_, _, err = ra.EntityTimestamp.GetAttrValueSingle(eIdx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expected exactly one element")
}

// GetAttrValueSingleOrDefault: on cardinality-1, returns the same values
// as GetAttrValueSingle (just without err).
func TestGetAttrValueSingleOrDefaultTaggedHappyPath(t *testing.T) {
	ts := time.UnixMilli(1700000000000).UTC()
	ra := loadSingleEntity(t, ts, func(sec *InEntityTestTableSectionText) {
		sec.BeginAttributeSingle("hello", 2, "hi").EndAttribute()
	})

	const eIdx = runtime.EntityIdx(0)
	const aIdx = runtime.AttributeIdx(0)

	text, wordLength, words := ra.Text.Attributes.GetAttrValueSingleOrDefault(eIdx, aIdx)
	require.Equal(t, "hello", text)
	require.EqualValues(t, 2, wordLength)
	require.Equal(t, "hi", words)
}

// GetAttrValueSingleOrDefault: on cardinality != 1, returns zero values
// for every column (all-or-nothing) without surfacing an error.
func TestGetAttrValueSingleOrDefaultTaggedDefaultsOnCardinalityMismatch(t *testing.T) {
	ts := time.UnixMilli(1700000000000).UTC()
	ra := loadSingleEntity(t, ts, func(sec *InEntityTestTableSectionText) {
		sec.BeginAttribute("hello").
			AddToCoContainers(5, "hello").
			AddToCoContainers(5, "world").
			EndAttribute()
	})

	const eIdx = runtime.EntityIdx(0)
	const aIdx = runtime.AttributeIdx(0)

	text, wordLength, words := ra.Text.Attributes.GetAttrValueSingleOrDefault(eIdx, aIdx)
	require.Equal(t, "", text)
	require.EqualValues(t, 0, wordLength)
	require.Equal(t, "", words)
}

// Multi-entity, multi-attribute-per-entity stress for the HomogenousArray
// accel-backed GetAttrValueSingle path. Each entity contributes >1 attribute,
// each attribute carries a distinct WordLength/Words pair, and we verify each
// (entity, attr) reads back its own values. The bug being guarded against:
// the AccelHomogenousArray's per-entity LookupForwardRange returns offsets
// relative to the entity's HA window, but the generated reader used them as
// global element indices — so every entity > 0 silently returned entity 0's
// values.
func TestGetAttrValueSingleMultiEntityMultiAttribute(t *testing.T) {
	ts := time.UnixMilli(1700000000000).UTC()

	dml := NewInEntityTestTable(memory.DefaultAllocator, 3)
	type attr struct {
		text       string
		wordLength uint32
		words      string
	}
	entities := [][]attr{
		{{"alpha", 100, "a"}, {"beta", 101, "b"}},
		{{"gamma", 200, "c"}, {"delta", 201, "d"}, {"epsilon", 202, "e"}},
		{{"zeta", 300, "f"}},
	}
	for i, ents := range entities {
		ent := dml.BeginEntity()
		ent.SetId(uint64(i))
		ent.SetTimestamp(ts, []time.Time{ts})
		sec := ent.GetSectionText()
		for _, a := range ents {
			sec.BeginAttributeSingle(a.text, a.wordLength, a.words).EndAttribute()
		}
		require.NoError(t, ent.CheckErrors())
		require.NoError(t, ent.CommitEntity())
	}

	records, err := dml.TransferRecords(nil)
	require.NoError(t, err)
	require.Len(t, records, 1)

	ra := NewReadAccessTestTable()
	require.NoError(t, ra.LoadFromRecord(records[0]))

	for i, ents := range entities {
		eIdx := runtime.EntityIdx(i)
		require.EqualValues(t, len(ents), ra.Text.Attributes.GetNumberOfAttributes(eIdx),
			"entity %d attribute count", i)
		for j, want := range ents {
			aIdx := runtime.AttributeIdx(j)
			text, wordLength, words, gerr := ra.Text.Attributes.GetAttrValueSingle(eIdx, aIdx)
			require.NoError(t, gerr, "entity %d attr %d", i, j)
			require.Equal(t, want.text, text, "entity %d attr %d text", i, j)
			require.EqualValues(t, want.wordLength, wordLength, "entity %d attr %d wordLength", i, j)
			require.Equal(t, want.words, words, "entity %d attr %d words", i, j)

			// iter.Seq variants must agree.
			require.EqualValues(t, []uint32{want.wordLength},
				slices.Collect(ra.Text.Attributes.GetAttrValueWordLength(eIdx, aIdx)),
				"entity %d attr %d wordLength seq", i, j)
			require.EqualValues(t, []string{want.words},
				slices.Collect(ra.Text.Attributes.GetAttrValueWords(eIdx, aIdx)),
				"entity %d attr %d words seq", i, j)
		}
	}
}

// Plain attribute class: OrDefault returns values on cardinality-1 and
// zero values on mismatch, mirroring the tagged side.
func TestGetAttrValueSingleOrDefaultPlain(t *testing.T) {
	ts := time.UnixMilli(1700000000000).UTC()

	// Cardinality-1 Proc: happy path.
	{
		ra := loadSingleEntity(t, ts, func(sec *InEntityTestTableSectionText) {
			sec.BeginAttributeSingle("x", 1, "x").EndAttribute()
		})
		tsOut, proc := ra.EntityTimestamp.GetAttrValueSingleOrDefault(runtime.EntityIdx(0))
		require.EqualValues(t, ts, tsOut)
		require.EqualValues(t, ts, proc)
	}
	// Cardinality-0 Proc: ts populated (it's the entity's plain ts scalar
	// — wait, actually under all-or-nothing, both default to zero).
	{
		dml := NewInEntityTestTable(memory.DefaultAllocator, 1)
		ent := dml.BeginEntity()
		ent.SetId(0)
		ent.SetTimestamp(ts, nil) // Proc: zero elements
		ent.GetSectionText().BeginAttributeSingle("x", 1, "x").EndAttribute()
		require.NoError(t, ent.CheckErrors())
		require.NoError(t, ent.CommitEntity())
		records, err := dml.TransferRecords(nil)
		require.NoError(t, err)
		ra := NewReadAccessTestTable()
		require.NoError(t, ra.LoadFromRecord(records[0]))

		tsOut, proc := ra.EntityTimestamp.GetAttrValueSingleOrDefault(runtime.EntityIdx(0))
		require.True(t, tsOut.IsZero(), "ts defaulted under all-or-nothing")
		require.True(t, proc.IsZero(), "proc defaulted on cardinality 0")
	}
}
