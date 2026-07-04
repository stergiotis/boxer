package marshallreflect_test

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// reflectOctets selects the u8 homogenous-array lane for a []uint8 field
// via `,ct=u8h` (ADR-0101 OQ2): []uint8 is []byte, so without the
// override the field classifies as a scalar blob.
type reflectOctets struct {
	_ struct{} `kind:"octets"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`

	Data []uint8 `lw:"octets,u8Array,ct=u8h"`
}

// TestPlanFor_U8LaneSelection pins the []uint8 ≡ []byte lane rules on the
// reflect front-end: blob by default, u8 array via `,ct=u8h`, reshapes
// still rejected.
func TestPlanFor_U8LaneSelection(t *testing.T) {
	t.Run("ct=u8h selects the array lane", func(t *testing.T) {
		plan, err := marshallreflect.PlanFor[reflectOctets]()
		require.NoError(t, err)
		require.Len(t, plan.Fields, 1)
		require.True(t, plan.Fields[0].IsSlice(), "ct=u8h must relabel to the homogenous-array lane")
		require.Equal(t, "uint8", plan.Fields[0].GoType())
	})
	t.Run("bare []uint8 stays a blob", func(t *testing.T) {
		type blobDoc struct {
			_    struct{} `kind:"blobDoc"`
			ID   uint64   `lw:",id"`
			Blob []uint8  `lw:"b,blob"`
		}
		plan, err := marshallreflect.PlanFor[blobDoc]()
		require.NoError(t, err)
		require.Len(t, plan.Fields, 1)
		require.False(t, plan.Fields[0].IsSlice())
		require.Equal(t, "[]byte", plan.Fields[0].GoType())
	})
	t.Run("reshape rejected", func(t *testing.T) {
		type bad struct {
			_    struct{} `kind:"bad"`
			ID   uint64   `lw:",id"`
			Data []uint32 `lw:"m,u8Array,ct=u8h"`
		}
		_, err := marshallreflect.PlanFor[bad]()
		require.Error(t, err)
		require.ErrorContains(t, err, "may only relabel, not reshape")
	})
}

// TestRoundTrip_U8ArrayViaCt drives the relabelled []uint8 field through
// anchor's u8Array section end-to-end: container write path (splice on
// empty), RA read, Unmarshal.
func TestRoundTrip_U8ArrayViaCt(t *testing.T) {
	original := []reflectOctets{
		{ID: 4001, Tracking: []byte("OCT-A"), Data: []uint8{1, 2, 3}},
		{ID: 4002, Tracking: []byte("OCT-B"), Data: []uint8{7}},
		{ID: 4003, Tracking: []byte("OCT-C"), Data: nil}, // lone container: spliced
	}
	lookup := marshallreflect.MapLookup{"octets": 1}
	table := anchor.NewInEntityTestTable(memory.NewGoAllocator(), len(original))
	require.NoError(t, marshallreflect.Validate[reflectOctets](table))
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

	idReader := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	idReader.SetColumnIndices(idReader.GetColumnIndices())
	require.NoError(t, idReader.LoadFromRecord(rec))
	defer idReader.Release()

	u8Reader := anchor.NewReadAccessTestTableTaggedU8Array()
	u8Reader.SetColumnIndices(u8Reader.GetColumnIndices())
	require.NoError(t, u8Reader.LoadFromRecord(rec))
	defer u8Reader.Release()

	args := marshallreflect.NewSectionReaders(idReader.Len()).
		PlainColumn("id", idReader.ValueId).
		PlainColumn("naturalKey", idReader.ValueNaturalKey).
		Section("u8Array", u8Reader.GetAttributes(), u8Reader.GetMemberships())
	var got []reflectOctets
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Equal(t, len(original), len(got))
	// A spliced lone container reads back as an empty (non-nil) slice —
	// the pre-existing accumulator convention for single-sub-column
	// container sections (contrast mixed sections, where an absent
	// container is nil).
	want := append([]reflectOctets(nil), original...)
	want[2].Data = []uint8{}
	for i := range want {
		require.Equal(t, want[i], got[i], "row %d", i)
	}
}
