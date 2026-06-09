package marshallreflect_test

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
	"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
)

// droneRow is the flat leeway drone DTO behind the ADR-0075 light version. The
// three components the typed widgets render map onto sections via lw: tags —
// Identity{Status}→symbol, Battery{Charge}→u64Array, Tasked{Window}→timeRange.
// The multi-sub-column timeRange uses the flat "section:subColumn" selector;
// marshallreflect has no nested-struct support, which is why this DTO is flat
// rather than reusing ecsdemo's nested component structs (full unification is
// deferred behind a codec feature — see ADR-0075).
type droneRow struct {
	_ struct{} `kind:"droneMission"`

	ID          uint64    `lw:",id"`
	Tracking    []byte    `lw:",naturalKey"`
	Status      string    `lw:"droneStatus,symbol"`         // Identity
	Battery     uint64    `lw:"battery,u64Array,unit"`      // Battery
	WindowBegin time.Time `lw:"window,timeRange:beginIncl"` // Tasked.Window
	WindowEnd   time.Time `lw:"window,timeRange:endExcl"`
}

// TestComponentDetectAndDecode_Light proves the ADR-0075 detect+decode core for
// the light-version components: per entity, each backing section is detected as
// present via the RA population count (the approximate leeway Presence), an
// unpopulated section (geoPoint, i.e. the deferred Located component) reads as
// absent, and the typed unmarshal recovers the component values.
func TestComponentDetectAndDecode_Light(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	original := []droneRow{
		{ID: 1, Tracking: []byte("TRK-1"), Status: "IN_TRANSIT", Battery: 8500, WindowBegin: base, WindowEnd: base.Add(time.Hour)},
		{ID: 2, Tracking: []byte("TRK-2"), Status: "DELIVERED", Battery: 7200, WindowBegin: base, WindowEnd: base.Add(2 * time.Hour)},
	}
	lookup := marshallreflect.MapLookup{"droneStatus": 1, "battery": 2, "window": 3}

	allocator := memory.NewGoAllocator()
	table := anchor.NewInEntityTestTable(allocator, len(original))
	require.NoError(t, marshallreflect.Marshal(table, original, lookup))

	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()
	rec := recs[0]
	require.Equal(t, int64(len(original)), rec.NumRows())

	idReader := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idReader.SetColumnIndices(idReader.GetColumnIndices())
	require.NoError(t, idReader.LoadFromRecord(rec))
	defer idReader.Release()

	symbolReader := anchor.NewReadAccessTestTableTaggedSymbol()
	symbolReader.SetColumnIndices(symbolReader.GetColumnIndices())
	require.NoError(t, symbolReader.LoadFromRecord(rec))
	defer symbolReader.Release()

	u64Reader := anchor.NewReadAccessTestTableTaggedU64Array()
	u64Reader.SetColumnIndices(u64Reader.GetColumnIndices())
	require.NoError(t, u64Reader.LoadFromRecord(rec))
	defer u64Reader.Release()

	timeRangeReader := anchor.NewReadAccessTestTableTaggedTimeRange()
	timeRangeReader.SetColumnIndices(timeRangeReader.GetColumnIndices())
	require.NoError(t, timeRangeReader.LoadFromRecord(rec))
	defer timeRangeReader.Release()

	geoPointReader := anchor.NewReadAccessTestTableTaggedGeoPoint()
	geoPointReader.SetColumnIndices(geoPointReader.GetColumnIndices())
	require.NoError(t, geoPointReader.LoadFromRecord(rec))
	defer geoPointReader.Release()

	// DETECT: approximate leeway Presence == the section is populated for the entity.
	for i := 0; i < idReader.Len(); i++ {
		ei := runtime.EntityIdx(i)
		require.Positive(t, symbolReader.GetAttributes().GetNumberOfAttributes(ei), "entity %d Identity present", i)
		require.Positive(t, u64Reader.GetAttributes().GetNumberOfAttributes(ei), "entity %d Battery present", i)
		require.Positive(t, timeRangeReader.GetAttributes().GetNumberOfAttributes(ei), "entity %d Tasked present", i)
		require.Zero(t, geoPointReader.GetAttributes().GetNumberOfAttributes(ei), "entity %d Located absent (deferred)", i)
	}

	// DECODE: typed unmarshal recovers the component values.
	args := marshallreflect.UnmarshalArgs{
		NumRows: idReader.Len(),
		PlainCol: func(name string) any {
			switch name {
			case "id":
				return idReader.ValueId
			case "naturalKey":
				return idReader.ValueNaturalKey
			}
			return nil
		},
		SectionAttrs: func(name string) any {
			switch name {
			case "symbol":
				return symbolReader.GetAttributes()
			case "u64Array":
				return u64Reader.GetAttributes()
			case "timeRange":
				return timeRangeReader.GetAttributes()
			}
			return nil
		},
		SectionMembs: func(name string) any {
			switch name {
			case "symbol":
				return symbolReader.GetMemberships()
			case "u64Array":
				return u64Reader.GetMemberships()
			case "timeRange":
				return timeRangeReader.GetMemberships()
			}
			return nil
		},
	}
	var got []droneRow
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Equal(t, len(original), len(got))
	for i := range original {
		require.Equal(t, original[i].Status, got[i].Status, "row %d Status", i)
		require.Equal(t, original[i].Battery, got[i].Battery, "row %d Battery", i)
		require.True(t, original[i].WindowBegin.Equal(got[i].WindowBegin), "row %d WindowBegin", i)
		require.True(t, original[i].WindowEnd.Equal(got[i].WindowEnd), "row %d WindowEnd", i)
	}
}
