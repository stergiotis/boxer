package codecdemo

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
)

// TestLineageDocRoundTrip exercises the multi-membership + ref-channel tuple
// codec (ADR-0109) end-to-end through the generated BuildEntities / FillFromArrow
// pair across all three shapes: a slice of ref memberships (Types), two fixed ref
// memberships (Edges), and a heterogeneous verbatim+ref pair over a mixed-shape
// section (Notes). Elements round-trip in wire order; N = 0 ref memberships and
// empty co-containers restore as nil.
func TestLineageDocRoundTrip(t *testing.T) {
	original := []LineageDoc{
		{ID: 1001, Tracking: []byte("LIN-A"),
			Types: []LineageTag{
				{Ancestors: []uint64{1, 2, 3}, Kind: "Person"},
				{Ancestors: []uint64{2}, Kind: "Company"},
				{Kind: "Thing"}, // N = 0 ref memberships
			},
			Edges: []EdgeTag{
				{Predicate: 100, Generic: 200, Target: 900},
				{Predicate: 101, Generic: 200, Target: 901},
			},
			Notes: []NamedText{
				{Name: "name", Kind: 50, Text: "Ivan", WordLength: []uint32{4}, WordBag: []string{"Ivan"}},
			},
		},
		{ID: 1002, Tracking: []byte("LIN-B"),
			Types: []LineageTag{
				{Ancestors: []uint64{7, 8}, Kind: "Asset"},
			},
			Notes: []NamedText{
				{Name: "alias", Kind: 51, Text: "empty", WordLength: nil, WordBag: nil},
			},
		},
		{ID: 1003, Tracking: []byte("LIN-C")}, // everything empty
	}

	cols := &LineageDocColumns{}
	for _, row := range original {
		cols.Append(row)
	}

	allocator := memory.NewGoAllocator()
	table := anchor.NewInEntityTestTable(allocator, cols.Len())
	require.NoError(t, LineageDocBuildEntities(table, cols))

	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()

	got := &LineageDocColumns{}
	require.NoError(t, fillLineageDoc(t, got, recs[0]))
	require.Equal(t, len(original), got.Len())
	for i, want := range original {
		require.Equal(t, want, got.Row(i), "row %d", i)
	}
}

// TestLineageDocFillFromArrow_CountMismatch pins the generated decode's guard:
// an EdgeTag element declares two fixed ref memberships, so a foreignKey
// attribute carrying only one on the wire is a membership count mismatch, an
// error rather than an out-of-range panic.
func TestLineageDocFillFromArrow_CountMismatch(t *testing.T) {
	allocator := memory.NewGoAllocator()
	table := anchor.NewInEntityTestTable(allocator, 1)
	table.BeginEntity()
	table.SetId(1, []byte("MM"))
	table.GetSectionSymbol().EndSection()
	fk := table.GetSectionForeignKey()
	attr := fk.BeginAttribute(900)
	attr.AddMembershipLowCardRefP(100) // only one — EdgeTag expects two
	attr.EndAttributeP()
	fk.EndSection()
	table.GetSectionText().EndSection()
	require.NoError(t, table.CommitEntity())

	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()

	got := &LineageDocColumns{}
	err = fillLineageDoc(t, got, recs[0])
	require.Error(t, err)
	require.ErrorContains(t, err, "membership count mismatch on read")
}

// fillLineageDoc loads the four readers a LineageDoc record needs and drives the
// generated FillFromArrow into out.
func fillLineageDoc(t *testing.T, out *LineageDocColumns, rec arrow.RecordBatch) error {
	t.Helper()
	idReader := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idReader.SetColumnIndices(idReader.GetColumnIndices())
	require.NoError(t, idReader.LoadFromRecord(rec))
	defer idReader.Release()

	symReader := anchor.NewReadAccessTestTableTaggedSymbol()
	symReader.SetColumnIndices(symReader.GetColumnIndices())
	require.NoError(t, symReader.LoadFromRecord(rec))
	defer symReader.Release()

	fkReader := anchor.NewReadAccessTestTableTaggedForeignKey()
	fkReader.SetColumnIndices(fkReader.GetColumnIndices())
	require.NoError(t, fkReader.LoadFromRecord(rec))
	defer fkReader.Release()

	textReader := anchor.NewReadAccessTestTableTaggedText()
	textReader.SetColumnIndices(textReader.GetColumnIndices())
	require.NoError(t, textReader.LoadFromRecord(rec))
	defer textReader.Release()

	return LineageDocFillFromArrow(
		out,
		idReader.Len(),
		idReader.ValueId,
		idReader.ValueNaturalKey,
		symReader.GetAttributes(), symReader.GetMemberships(),
		fkReader.GetAttributes(), fkReader.GetMemberships(),
		textReader.GetAttributes(), textReader.GetMemberships(),
	)
}
