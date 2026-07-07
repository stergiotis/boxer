package marshallreflect_test

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// Slice A, Step 1a — the nested-struct front-end's architecture prover.
//
// A NESTED, static-membership, cardinality-One section maps a struct value
// (whose fields are the section's sub-columns) onto a leeway section, emitting
// exactly one attribute per row. It must produce wire output byte-identical to
// the equivalent FLAT multi-sub-column DTO — proving the whole nested→plan→wire
// lowering (goplan.AddNestedSliceField), the AttrCardinalityOne enumeration, the
// static-membership resolution (H1: addMembership, not the raw dynamic path),
// and all four TupleSpec() dispatch sites — against the shipped flat rangeDrone
// as the byte reference.

// rangeWindow is the nested attribute struct for anchor's timeRange section:
// two time.Time scalar sub-columns. The sub-column column names (beginIncl /
// endExcl) differ from the Go field names, so they carry an explicit
// `lw:"<column>"` tag. Declaration order (Begin before End) matches the flat
// rangeDrone so the BeginAttribute(beginIncl, endExcl) argument order — hence
// the wire — is identical.
type rangeWindow struct {
	Begin time.Time `lw:"beginIncl"`
	End   time.Time `lw:"endExcl"`
}

// nestedRangeDrone is the nested spelling of rangeDrone (shapes_roundtrip_test.go):
// the two timeRange sub-columns live inside a struct value under one static
// membership ("window"), instead of two sibling fields sharing a tag.
type nestedRangeDrone struct {
	_        struct{}    `kind:"rangeDrone"`
	ID       uint64      `lw:",id"`
	Tracking []byte      `lw:",naturalKey"`
	Window   rangeWindow `lw:"window,timeRange"`
}

func TestNested_StaticOne_TimeRange_ByteIdenticalToFlat(t *testing.T) {
	lookup := marshallreflect.MapLookup{"window": 1}
	b1 := time.Unix(1_600_000_000, 0).UTC()
	e1 := time.Unix(1_600_003_600, 0).UTC()
	b2 := time.Unix(1_600_100_000, 0).UTC()
	e2 := time.Unix(1_600_200_000, 0).UTC()

	flat := []rangeDrone{
		{ID: 1, Tracking: []byte("R1"), Begin: b1, End: e1},
		{ID: 2, Tracking: []byte("R2"), Begin: b2, End: e2},
	}
	nested := []nestedRangeDrone{
		{ID: 1, Tracking: []byte("R1"), Window: rangeWindow{Begin: b1, End: e1}},
		{ID: 2, Tracking: []byte("R2"), Window: rangeWindow{Begin: b2, End: e2}},
	}

	// Preflight: the nested DTO satisfies the same DML write contract (exercises
	// the static-membership branch of checkSectionAttrContract — H7).
	require.NoError(t, marshallreflect.Validate[nestedRangeDrone](anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(nested))))

	flatRec, relF := marshalToRecord(t, flat, lookup)
	defer relF()
	nestedRec, relN := marshalToRecord(t, nested, lookup)
	defer relN()

	// The load-bearing invariant: the nested spelling is byte-identical to the
	// flat one — logically (RecordEqual) and on the wire (Arrow IPC bytes).
	require.True(t, array.RecordEqual(flatRec, nestedRec),
		"records differ:\nflat=%s\nnested=%s", flatRec, nestedRec)
	require.Equal(t, ipcBytes(t, flatRec), ipcBytes(t, nestedRec), "Arrow IPC bytes differ")

	// Round-trip: read the nested record back into the nested DTO.
	idReader, relID := loadIdReader(t, nestedRec)
	defer relID()
	trReader := anchor.NewReadAccessTestTableTaggedTimeRange()
	trReader.SetColumnIndices(trReader.GetColumnIndices())
	require.NoError(t, trReader.LoadFromRecord(nestedRec))
	defer trReader.Release()

	// One attribute per row (cardinality One).
	require.Equal(t, int64(1), trReader.GetAttributes().GetNumberOfAttributes(0))
	require.Equal(t, int64(1), trReader.GetAttributes().GetNumberOfAttributes(1))

	args := idReaders(idReader).
		Section("timeRange", trReader.GetAttributes(), trReader.GetMemberships())
	var got []nestedRangeDrone
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Equal(t, len(nested), len(got))
	for i := range nested {
		require.Equal(t, nested[i].ID, got[i].ID, "row %d ID", i)
		require.Equal(t, nested[i].Window.Begin.Unix(), got[i].Window.Begin.Unix(), "row %d Begin", i)
		require.Equal(t, nested[i].Window.End.Unix(), got[i].Window.End.Unix(), "row %d End", i)
		require.NotEqual(t, got[i].Window.Begin.Unix(), got[i].Window.End.Unix(), "row %d begin/end distinct", i)
	}
}

// TestNested_StaticOne_CrossDecodeFlat proves the two spellings are wire-compatible
// both ways: a record written by the FLAT DTO reads back into the NESTED DTO.
func TestNested_StaticOne_CrossDecodeFlat(t *testing.T) {
	lookup := marshallreflect.MapLookup{"window": 1}
	b := time.Unix(1_600_000_000, 0).UTC()
	e := time.Unix(1_600_003_600, 0).UTC()

	flat := []rangeDrone{{ID: 7, Tracking: []byte("X7"), Begin: b, End: e}}
	flatRec, rel := marshalToRecord(t, flat, lookup)
	defer rel()

	idReader, relID := loadIdReader(t, flatRec)
	defer relID()
	trReader := anchor.NewReadAccessTestTableTaggedTimeRange()
	trReader.SetColumnIndices(trReader.GetColumnIndices())
	require.NoError(t, trReader.LoadFromRecord(flatRec))
	defer trReader.Release()

	args := idReaders(idReader).
		Section("timeRange", trReader.GetAttributes(), trReader.GetMemberships())
	var got []nestedRangeDrone
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Len(t, got, 1)
	require.Equal(t, uint64(7), got[0].ID)
	require.Equal(t, b.Unix(), got[0].Window.Begin.Unix())
	require.Equal(t, e.Unix(), got[0].Window.End.Unix())
}
