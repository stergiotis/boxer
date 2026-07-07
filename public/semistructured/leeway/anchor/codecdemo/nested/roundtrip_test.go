package nested

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/lw"
)

func releaseAll(recs []arrow.RecordBatch) {
	for _, r := range recs {
		r.Release()
	}
}

// loadIdReader loads anchor's plain id/naturalKey read-access helper.
func loadIdReader(t *testing.T, rec arrow.RecordBatch) *anchor.ReadAccessTestTablePlainEntityIdAttributes {
	t.Helper()
	r := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	r.SetColumnIndices(r.GetColumnIndices())
	require.NoError(t, r.LoadFromRecord(rec))
	return r
}

// TestTextDocNested_RoundTrip drives the One-cardinality nested `text` section
// (scalar + co-containers, static membership "prose") through the GENERATED
// codec: BuildEntities → anchor RA → FillFromArrow, back to the original rows.
func TestTextDocNested_RoundTrip(t *testing.T) {
	original := []TextDocNested{
		{ID: 1, Tracking: []byte("A"), Body: proseAttrs{Text: "hello world", WordLength: []uint32{5, 5}, WordBag: []string{"hello", "world"}}},
		{ID: 2, Tracking: []byte("B"), Body: proseAttrs{Text: "solo", WordLength: []uint32{4}, WordBag: []string{"solo"}}},
	}
	cols := &TextDocNestedColumns{}
	for _, r := range original {
		cols.Append(r)
	}
	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), cols.Len())
	require.NoError(t, TextDocNestedBuildEntities(table, cols))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recs)
	rec := recs[0]

	idReader := loadIdReader(t, rec)
	defer idReader.Release()
	textReader := anchor.NewReadAccessTestTableTaggedText()
	textReader.SetColumnIndices(textReader.GetColumnIndices())
	require.NoError(t, textReader.LoadFromRecord(rec))
	defer textReader.Release()

	got := &TextDocNestedColumns{}
	require.NoError(t, TextDocNestedFillFromArrow(got, idReader.Len(), idReader.ValueId, idReader.ValueNaturalKey, textReader.GetAttributes(), textReader.GetMemberships()))
	require.Equal(t, len(original), got.Len())
	for i, want := range original {
		require.Equal(t, want, got.Row(i), "row %d", i)
	}
}

// TestManyTagsDoc_RoundTrip drives the static-membership Many nested `symbol`
// section (`[]S`, one attribute per element) — including a zero-element row that
// reads back as a nil slice.
func TestManyTagsDoc_RoundTrip(t *testing.T) {
	original := []ManyTagsDoc{
		{ID: 10, Tracking: []byte("M1"), Blocks: []symBlock{{Val: "a"}, {Val: "b"}, {Val: "c"}}},
		{ID: 11, Tracking: []byte("M2"), Blocks: []symBlock{{Val: "x"}}},
		{ID: 12, Tracking: []byte("M3")}, // zero elements → nil slice
	}
	cols := &ManyTagsDocColumns{}
	for _, r := range original {
		cols.Append(r)
	}
	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), cols.Len())
	require.NoError(t, ManyTagsDocBuildEntities(table, cols))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recs)
	rec := recs[0]

	idReader := loadIdReader(t, rec)
	defer idReader.Release()
	symReader := anchor.NewReadAccessTestTableTaggedSymbol()
	symReader.SetColumnIndices(symReader.GetColumnIndices())
	require.NoError(t, symReader.LoadFromRecord(rec))
	defer symReader.Release()

	got := &ManyTagsDocColumns{}
	require.NoError(t, ManyTagsDocFillFromArrow(got, idReader.Len(), idReader.ValueId, idReader.ValueNaturalKey, symReader.GetAttributes(), symReader.GetMemberships()))
	require.Equal(t, len(original), got.Len())
	for i, want := range original {
		require.Equal(t, want, got.Row(i), "row %d", i)
	}
	require.Nil(t, got.Row(2).Blocks, "zero elements → nil slice")
}

// TestOptNoteDoc_RoundTrip drives the Optional nested `symbol` section
// (option.Option[S]) — present and absent rows.
func TestOptNoteDoc_RoundTrip(t *testing.T) {
	original := []OptNoteDoc{
		{ID: 20, Tracking: []byte("O1"), Note: option.Option[noteAttr]{Has: true, Val: noteAttr{Val: "present"}}},
		{ID: 21, Tracking: []byte("O2")}, // absent (Has=false)
		{ID: 22, Tracking: []byte("O3"), Note: option.Option[noteAttr]{Has: true, Val: noteAttr{Val: "here"}}},
	}
	cols := &OptNoteDocColumns{}
	for _, r := range original {
		cols.Append(r)
	}
	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), cols.Len())
	require.NoError(t, OptNoteDocBuildEntities(table, cols))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recs)
	rec := recs[0]

	idReader := loadIdReader(t, rec)
	defer idReader.Release()
	symReader := anchor.NewReadAccessTestTableTaggedSymbol()
	symReader.SetColumnIndices(symReader.GetColumnIndices())
	require.NoError(t, symReader.LoadFromRecord(rec))
	defer symReader.Release()

	got := &OptNoteDocColumns{}
	require.NoError(t, OptNoteDocFillFromArrow(got, idReader.Len(), idReader.ValueId, idReader.ValueNaturalKey, symReader.GetAttributes(), symReader.GetMemberships()))
	require.Equal(t, len(original), got.Len())
	for i, want := range original {
		require.Equal(t, want, got.Row(i), "row %d", i)
	}
	require.False(t, got.Row(1).Note.Has, "absent row reads back as Has=false")
}

// --- Step 2a marker round-trips: verify the decode newtype bridging. ---

func TestLabeledTextNested_RoundTrip(t *testing.T) {
	original := []LabeledTextNested{
		{ID: 1, Tracking: []byte("A"), Texts: []labeledTextAttr{
			{Label: "title", Text: "hello world", WordLength: []uint32{5, 5}, WordBag: []string{"hello", "world"}},
			{Label: "body", Text: "hi", WordLength: []uint32{2}, WordBag: []string{"hi"}},
		}},
		{ID: 2, Tracking: []byte("B")},
	}
	cols := &LabeledTextNestedColumns{}
	for _, r := range original {
		cols.Append(r)
	}
	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), cols.Len())
	require.NoError(t, LabeledTextNestedBuildEntities(table, cols))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recs)
	rec := recs[0]

	idReader := loadIdReader(t, rec)
	defer idReader.Release()
	textReader := anchor.NewReadAccessTestTableTaggedText()
	textReader.SetColumnIndices(textReader.GetColumnIndices())
	require.NoError(t, textReader.LoadFromRecord(rec))
	defer textReader.Release()

	got := &LabeledTextNestedColumns{}
	require.NoError(t, LabeledTextNestedFillFromArrow(got, idReader.Len(), idReader.ValueId, idReader.ValueNaturalKey, textReader.GetAttributes(), textReader.GetMemberships()))
	require.Equal(t, len(original), got.Len())
	for i, want := range original {
		require.Equal(t, want, got.Row(i), "row %d", i)
	}
}

func TestNamedTextNested_RoundTrip(t *testing.T) {
	original := []NamedTextNested{
		{ID: 5, Tracking: []byte("N1"), Notes: []namedTextAttr{
			{Name: "author", Kind: 7, Text: "ann", WordLength: []uint32{3}, WordBag: []string{"ann"}},
			{Name: "editor", Kind: 9, Text: "bob", WordLength: []uint32{3}, WordBag: []string{"bob"}},
		}},
	}
	cols := &NamedTextNestedColumns{}
	for _, r := range original {
		cols.Append(r)
	}
	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), cols.Len())
	require.NoError(t, NamedTextNestedBuildEntities(table, cols))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recs)
	rec := recs[0]

	idReader := loadIdReader(t, rec)
	defer idReader.Release()
	textReader := anchor.NewReadAccessTestTableTaggedText()
	textReader.SetColumnIndices(textReader.GetColumnIndices())
	require.NoError(t, textReader.LoadFromRecord(rec))
	defer textReader.Release()

	got := &NamedTextNestedColumns{}
	require.NoError(t, NamedTextNestedFillFromArrow(got, idReader.Len(), idReader.ValueId, idReader.ValueNaturalKey, textReader.GetAttributes(), textReader.GetMemberships()))
	for i, want := range original {
		require.Equal(t, want, got.Row(i), "row %d", i)
	}
}

func TestLineageNested_RoundTrip(t *testing.T) {
	original := []LineageNested{
		{ID: 9, Tracking: []byte("L1"), Types: []lineageAttr{
			{Ancestors: []lw.Ref{10, 20, 30}, Kind: "person"},
			{Ancestors: []lw.Ref{40}, Kind: "thing"},
		}},
	}
	cols := &LineageNestedColumns{}
	for _, r := range original {
		cols.Append(r)
	}
	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), cols.Len())
	require.NoError(t, LineageNestedBuildEntities(table, cols))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	defer releaseAll(recs)
	rec := recs[0]

	idReader := loadIdReader(t, rec)
	defer idReader.Release()
	symReader := anchor.NewReadAccessTestTableTaggedSymbol()
	symReader.SetColumnIndices(symReader.GetColumnIndices())
	require.NoError(t, symReader.LoadFromRecord(rec))
	defer symReader.Release()

	got := &LineageNestedColumns{}
	require.NoError(t, LineageNestedFillFromArrow(got, idReader.Len(), idReader.ValueId, idReader.ValueNaturalKey, symReader.GetAttributes(), symReader.GetMemberships()))
	for i, want := range original {
		require.Equal(t, want, got.Row(i), "row %d", i)
	}
}
