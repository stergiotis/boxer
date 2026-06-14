package codecdemo

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/marshalltypes"
)

// TestSensorReadingRoundTrip is the carrier-path twin of
// TestDroneMissionRoundTrip: it compiles + runs the keelsoncodec
// --target=anchor Cut-2 emit (the AddMembershipMixedLowCardRefP write
// path + the GetMembValueLowCardRefHighCardParams carrier-decode read
// path) against anchor's real DML / RA. Those paths are otherwise only
// string-asserted in marshallgen's emit_test, never compiled — this
// golden + test is what locks them in.
//
// Membership identity is per-row carrier data (ReadingC.Id / .Params),
// so no lookup is consulted; the wire id comes straight from the carrier.
func TestSensorReadingRoundTrip(t *testing.T) {
	original := []SensorReading{
		{ID: 1, Tracking: []byte("M1"), Reading: "alpha", ReadingC: marshalltypes.MixedLowCardRef{Id: 7, Params: []byte("p-one")}},
		{ID: 2, Tracking: []byte("M2"), Reading: "beta", ReadingC: marshalltypes.MixedLowCardRef{Id: 9, Params: []byte("p-two")}},
		{ID: 3, Tracking: []byte("M3"), Reading: "", ReadingC: marshalltypes.MixedLowCardRef{Id: 0, Params: nil}},
	}
	cols := &SensorReadingColumns{}
	for _, row := range original {
		cols.Append(row)
	}

	// --- Write: drive anchor's DML via the schema-agnostic helper. ---
	allocator := memory.NewGoAllocator()
	table := anchor.NewInEntityTestTable(allocator, cols.Len())
	err := SensorReadingBuildEntities(table, cols)
	require.NoError(t, err, "SensorReadingBuildEntities should succeed")

	var recs []arrow.RecordBatch
	recs, err = table.TransferRecords(nil)
	require.NoError(t, err, "TransferRecords should succeed")
	require.NotEmpty(t, recs, "expected at least one record")
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()
	rec := recs[0]
	require.Equal(t, int64(len(original)), rec.NumRows())

	// --- Read: load anchor's RA helpers from the resulting record. ---
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

	// One scalar attribute per row carries the value + the mixed carrier.
	require.Equal(t, int64(1), symbolReader.GetAttributes().GetNumberOfAttributes(0))

	// --- Drive FillFromArrow into a fresh Columns. ---
	got := &SensorReadingColumns{}
	err = SensorReadingFillFromArrow(
		got,
		idReader.Len(),
		idReader.ValueId,
		idReader.ValueNaturalKey,
		symbolReader.GetAttributes(),
		symbolReader.GetMemberships(),
	)
	require.NoError(t, err)
	require.Equal(t, len(original), got.Len(), "row count survives round-trip")

	for i, want := range original {
		round := got.Row(i)
		require.Equal(t, want.ID, round.ID, "row %d ID", i)
		require.Equal(t, want.Tracking, round.Tracking, "row %d Tracking", i)
		require.Equal(t, want.Reading, round.Reading, "row %d Reading (value)", i)
		require.Equal(t, want.ReadingC.Id, round.ReadingC.Id, "row %d carrier id", i)
		require.Equal(t, want.ReadingC.Params, round.ReadingC.Params, "row %d carrier params", i)
	}
}
