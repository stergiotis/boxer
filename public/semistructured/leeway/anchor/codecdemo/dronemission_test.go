package codecdemo

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
)

// TestDroneMissionRoundTrip exercises the codecdemo end-to-end against
// anchor's InEntityTestTable + ReadAccessTestTable*:
//
//   DroneMission slice
//     → DroneMissionBuildEntities(*anchor.InEntityTestTable, ...)
//     → TransferRecords → []arrow.RecordBatch
//     → ReadAccess loaders
//     → DroneMissionFillFromArrow(symbolReaders…, u64ArrayReaders…)
//     → DroneMission slice
//
// Both BuildEntities and FillFromArrow are the schema-agnostic
// generic helpers; the test passes anchor's concrete types directly,
// Go's type inference binds every type parameter at the call site.
func TestDroneMissionRoundTrip(t *testing.T) {
	original := []DroneMission{
		{ID: 1001, Tracking: []byte("TRK-A"), Status: "IN_TRANSIT", Battery: 8500},
		{ID: 1002, Tracking: []byte("TRK-B"), Status: "DELIVERED", Battery: 7200},
		{ID: 1003, Tracking: []byte("TRK-C"), Status: "DELIVERED", Battery: 6100},
	}
	cols := &DroneMissionColumns{}
	for _, row := range original {
		cols.Append(row)
	}

	// --- Write: drive anchor's DML via the schema-agnostic helper. ---
	allocator := memory.NewGoAllocator()
	table := anchor.NewInEntityTestTable(allocator, cols.Len())
	err := DroneMissionBuildEntities(table, cols)
	require.NoError(t, err, "DroneMissionBuildEntities should succeed")

	var recs []arrow.RecordBatch
	recs, err = table.TransferRecords(nil)
	require.NoError(t, err, "TransferRecords should succeed")
	require.NotEmpty(t, recs, "expected at least one record")
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()
	var totalRows int64
	for _, r := range recs {
		totalRows += r.NumRows()
	}
	require.Equal(t, int64(3), totalRows)

	// --- Read: load anchor's RA helpers from the resulting record. ---
	// arrow.RecordBatch directly satisfies runtime.RecordI, no wrapper needed.
	rec := recs[0]

	idReader := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idReader.SetColumnIndices(idReader.GetColumnIndices())
	err = idReader.LoadFromRecord(rec)
	require.NoError(t, err)
	defer idReader.Release()

	symbolReader := anchor.NewReadAccessTestTableTaggedSymbol()
	symbolReader.SetColumnIndices(symbolReader.GetColumnIndices())
	err = symbolReader.LoadFromRecord(rec)
	require.NoError(t, err)
	defer symbolReader.Release()

	u64ArrayReader := anchor.NewReadAccessTestTableTaggedU64Array()
	u64ArrayReader.SetColumnIndices(u64ArrayReader.GetColumnIndices())
	err = u64ArrayReader.LoadFromRecord(rec)
	require.NoError(t, err)
	defer u64ArrayReader.Release()

	// --- Drive FillFromArrow into a fresh Columns. ---
	got := &DroneMissionColumns{}
	err = DroneMissionFillFromArrow(
		got,
		idReader.Len(),
		idReader.ValueId,
		idReader.ValueNaturalKey,
		symbolReader.GetAttributes(),
		symbolReader.GetMemberships(),
		u64ArrayReader.GetAttributes(),
		u64ArrayReader.GetMemberships(),
	)
	require.NoError(t, err)
	require.Equal(t, len(original), got.Len(), "row count survives round-trip")

	for i, want := range original {
		round := got.Row(i)
		require.Equal(t, want.ID, round.ID, "row %d ID", i)
		require.Equal(t, want.Tracking, round.Tracking, "row %d Tracking", i)
		require.Equal(t, want.Status, round.Status, "row %d Status", i)
		require.Equal(t, want.Battery, round.Battery, "row %d Battery", i)
	}
}
