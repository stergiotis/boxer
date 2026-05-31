//go:build llm_generated_opus47

package marshallreflect_test

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshallreflect"
)

// reflectDrone mirrors the codecdemo DroneMission DTO — same lw:
// tags, but lives inside this test file so marshallreflect doesn't
// take a dependency on codecdemo.
type reflectDrone struct {
	_ struct{} `kind:"droneMission"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`
	Status   string `lw:"droneStatus,symbol"`
	Battery  uint64 `lw:"battery,u64Array,unit"`
}

// TestRoundTrip_AnchorDroneMission walks a small batch through
// marshallreflect.Marshal against anchor's InEntityTestTable, then
// reads back via anchor's RA helpers + marshallreflect.Unmarshal,
// asserting row-in == row-out.
func TestRoundTrip_AnchorDroneMission(t *testing.T) {
	original := []reflectDrone{
		{ID: 1001, Tracking: []byte("TRK-A"), Status: "IN_TRANSIT", Battery: 8500},
		{ID: 1002, Tracking: []byte("TRK-B"), Status: "DELIVERED", Battery: 7200},
		{ID: 1003, Tracking: []byte("TRK-C"), Status: "DELIVERED", Battery: 6100},
	}

	// Membership-id assignment is opaque from the reflect codec's
	// perspective; the same MapLookup feeds Marshal and Unmarshal so
	// both sides agree on the wire ids.
	lookup := marshallreflect.MapLookup{
		"droneStatus": 1,
		"battery":     2,
	}

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

	u64ArrayReader := anchor.NewReadAccessTestTableTaggedU64Array()
	u64ArrayReader.SetColumnIndices(u64ArrayReader.GetColumnIndices())
	require.NoError(t, u64ArrayReader.LoadFromRecord(rec))
	defer u64ArrayReader.Release()

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
				return u64ArrayReader.GetAttributes()
			}
			return nil
		},
		SectionMembs: func(name string) any {
			switch name {
			case "symbol":
				return symbolReader.GetMemberships()
			case "u64Array":
				return u64ArrayReader.GetMemberships()
			}
			return nil
		},
	}
	var got []reflectDrone
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Equal(t, len(original), len(got))
	for i := range original {
		require.Equal(t, original[i].ID, got[i].ID, "row %d ID", i)
		require.Equal(t, original[i].Tracking, got[i].Tracking, "row %d Tracking", i)
		require.Equal(t, original[i].Status, got[i].Status, "row %d Status", i)
		require.Equal(t, original[i].Battery, got[i].Battery, "row %d Battery", i)
	}
}

// reflectDroneExt extends the round-trip surface with one `_` const
// per membership channel plus a regular verbatim field. Section
// uniformity: symbolArray carries only ref fields (regular + const),
// symbol carries only verbatim fields (regular + const).
type reflectDroneExt struct {
	_ struct{} `kind:"droneMissionExt"`
	// Ref-channel const into symbolArray (BeginAttributeSingle path).
	_ struct{} `lw:"defaultCategory,symbolArray,unit,const=baseline"`
	// Verbatim-channel const into symbol (AddMembershipLowCardVerbatimP).
	_ struct{} `lw:"appKind,symbol,verbatim,const=production"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	// Ref-channel regular: shares symbolArray with the ref const.
	Category string `lw:"flightMode,symbolArray,unit"`
	// Verbatim-channel regular: shares symbol with the verbatim const.
	Tag string `lw:"feature,symbol,verbatim"`
}

// TestRoundTrip_ConstAndVerbatim exercises the const and verbatim
// flag matrix end-to-end. Const fields are write-only — they emit a
// fixed attribute on every row but Unmarshal does not populate any
// Go-side slot (the `_` blank identifier has none). The reconstructed
// row therefore carries the non-const fields; const presence is
// verified indirectly by Marshal not panicking + Unmarshal recovering
// the other fields correctly.
func TestRoundTrip_ConstAndVerbatim(t *testing.T) {
	original := []reflectDroneExt{
		{ID: 2001, Tracking: []byte("EXT-A"), Category: "AUTOPILOT", Tag: "edge"},
		{ID: 2002, Tracking: []byte("EXT-B"), Category: "MANUAL", Tag: "stable"},
		{ID: 2003, Tracking: []byte("EXT-C"), Category: "AUTOPILOT", Tag: "edge"},
	}

	// Only ref-channel memberships need a lookup entry. Verbatim
	// memberships (appKind, feature) embed the literal name on the
	// wire; the lookup is never consulted for them.
	lookup := marshallreflect.MapLookup{
		"flightMode":      1,
		"defaultCategory": 2,
	}

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

	symbolArrayReader := anchor.NewReadAccessTestTableTaggedSymbolArray()
	symbolArrayReader.SetColumnIndices(symbolArrayReader.GetColumnIndices())
	require.NoError(t, symbolArrayReader.LoadFromRecord(rec))
	defer symbolArrayReader.Release()

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
			case "symbolArray":
				return symbolArrayReader.GetAttributes()
			}
			return nil
		},
		SectionMembs: func(name string) any {
			switch name {
			case "symbol":
				return symbolReader.GetMemberships()
			case "symbolArray":
				return symbolArrayReader.GetMemberships()
			}
			return nil
		},
	}
	var got []reflectDroneExt
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Equal(t, len(original), len(got))
	for i := range original {
		require.Equal(t, original[i].ID, got[i].ID, "row %d ID", i)
		require.Equal(t, original[i].Tracking, got[i].Tracking, "row %d Tracking", i)
		require.Equal(t, original[i].Category, got[i].Category, "row %d Category (ref)", i)
		require.Equal(t, original[i].Tag, got[i].Tag, "row %d Tag (verbatim)", i)
	}

	// Spot-check the constant emit: row 0 should carry the verbatim
	// "production" + ref-id-2 ("baseline") attributes alongside the
	// scalar Tag / Category. The symbol section's attributes are a
	// flat list (1 per row from Tag + 1 per row from the const), so
	// the per-row attribute count is 2 for symbol and 2 for symbolArray.
	require.Equal(t, int64(2), symbolReader.GetAttributes().GetNumberOfAttributes(0))
	require.Equal(t, int64(2), symbolArrayReader.GetAttributes().GetNumberOfAttributes(0))
}
