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
