package codecdemo

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
)

// TestLabeledTextDocRoundTrip exercises the dynamic-membership tuple
// codec (ADR-0103) end-to-end against anchor's InEntityTestTable +
// ReadAccessTestTable*: N = 3 / 1 / 0 attributes per entity with
// distinct memberships, per-element container lengths 2 / 0 / 1.
//
//	LabeledTextDoc slice
//	  → LabeledTextDocBuildEntities(*anchor.InEntityTestTable, ...)
//	  → TransferRecords → []arrow.RecordBatch
//	  → ReadAccess loaders
//	  → LabeledTextDocFillFromArrow(textReaders…)
//	  → LabeledTextDoc slice (elements in wire order, memberships intact)
func TestLabeledTextDocRoundTrip(t *testing.T) {
	original := []LabeledTextDoc{
		{ID: 4001, Tracking: []byte("TPL-A"), Texts: []LabeledText{
			{Label: "title", Text: "hello world", WordLength: []uint32{5, 5}, WordBag: []string{"hello", "world"}},
			{Label: "summary", Text: "empty containers", WordLength: nil, WordBag: nil},
			{Label: "note", Text: "one", WordLength: []uint32{3}, WordBag: []string{"one"}},
		}},
		{ID: 4002, Tracking: []byte("TPL-B"), Texts: []LabeledText{
			{Label: "title", Text: "solo", WordLength: []uint32{4}, WordBag: []string{"solo"}},
		}},
		{ID: 4003, Tracking: []byte("TPL-C")}, // zero elements → zero attributes
	}
	cols := &LabeledTextDocColumns{}
	for _, row := range original {
		cols.Append(row)
	}

	// --- Write: drive anchor's DML via the schema-agnostic helper. ---
	allocator := memory.NewGoAllocator()
	table := anchor.NewInEntityTestTable(allocator, cols.Len())
	err := LabeledTextDocBuildEntities(table, cols)
	require.NoError(t, err, "LabeledTextDocBuildEntities should succeed")

	var recs []arrow.RecordBatch
	recs, err = table.TransferRecords(nil)
	require.NoError(t, err, "TransferRecords should succeed")
	require.NotEmpty(t, recs, "expected at least one record")
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()

	// --- Read: load anchor's RA helpers from the resulting record. ---
	rec := recs[0]

	idReader := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idReader.SetColumnIndices(idReader.GetColumnIndices())
	err = idReader.LoadFromRecord(rec)
	require.NoError(t, err)
	defer idReader.Release()

	textReader := anchor.NewReadAccessTestTableTaggedText()
	textReader.SetColumnIndices(textReader.GetColumnIndices())
	err = textReader.LoadFromRecord(rec)
	require.NoError(t, err)
	defer textReader.Release()

	// --- Drive FillFromArrow into a fresh Columns. ---
	got := &LabeledTextDocColumns{}
	err = LabeledTextDocFillFromArrow(
		got,
		idReader.Len(),
		idReader.ValueId,
		idReader.ValueNaturalKey,
		textReader.GetAttributes(),
		textReader.GetMemberships(),
	)
	require.NoError(t, err)
	require.Equal(t, len(original), got.Len(), "row count survives round-trip")

	for i, want := range original {
		require.Equal(t, want, got.Row(i), "row %d", i)
	}

	// The zip-length guard is a per-element runtime error, not a panic.
	bad := &LabeledTextDocColumns{}
	bad.Append(LabeledTextDoc{ID: 1, Tracking: []byte("BAD"), Texts: []LabeledText{
		{Label: "bad", Text: "broken", WordLength: []uint32{1, 2}, WordBag: []string{"a"}},
	}})
	badTable := anchor.NewInEntityTestTable(allocator, bad.Len())
	err = LabeledTextDocBuildEntities(badTable, bad)
	require.Error(t, err)
	require.ErrorContains(t, err, "co-container slices have different lengths")
}
