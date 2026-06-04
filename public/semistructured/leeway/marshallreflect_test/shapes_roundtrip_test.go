package marshallreflect_test

import (
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshallreflect"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshalltypes"
)

// These round-trips cover the marshallreflect codec shapes the existing
// anchor_roundtrip_test.go leaves at 0%: multi-sub-column sections
// (timeRange beginIncl/endExcl), 1×N container emits (roaring bitmap and
// plain slice into u32Array), and the `,explode` flag (one attribute per
// element). marshallgen emits + asserts these shapes in its emit_test.go;
// before this file the reflect runtime codec had never been exercised on
// them, so a marshal/unmarshal wire-parity bug would have gone unseen.
//
// Each test drives marshallreflect.Marshal against anchor's
// InEntityTestTable, transfers to an Arrow record, reads back via anchor's
// RA helpers + marshallreflect.Unmarshal, and asserts row-in == row-out.
// Where the wire shape is the point (explode vs container), the per-row
// attribute count is asserted too, so a test passing by coincidence
// (reconstructing the same slice through the wrong wire shape) still fails.

// marshalToRecord marshals rows into a fresh InEntityTestTable and returns
// the first transferred Arrow record plus a release closure. The DTOs here
// all declare id + naturalKey, matching anchor's two-arg SetId.
func marshalToRecord[T any](t *testing.T, rows []T, lookup marshallreflect.LookupI) (arrow.RecordBatch, func()) {
	t.Helper()
	allocator := memory.NewGoAllocator()
	table := anchor.NewInEntityTestTable(allocator, len(rows))
	require.NoError(t, marshallreflect.Marshal(table, rows, lookup))

	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	require.Equal(t, int64(len(rows)), recs[0].NumRows())
	return recs[0], func() {
		for _, r := range recs {
			r.Release()
		}
	}
}

// loadIdReader loads the plain entity-id reader (id + naturalKey) every
// DTO in this file declares.
func loadIdReader(t *testing.T, rec arrow.RecordBatch) (*anchor.ReadAccessTestTablePlainEntityIdAttributes, func()) {
	t.Helper()
	r := anchor.NewReadAccessTestTablePlainEntityIdAttributes()
	r.SetColumnIndices(r.GetColumnIndices())
	require.NoError(t, r.LoadFromRecord(rec))
	return r, r.Release
}

// plainColFn returns the standard id/naturalKey PlainCol resolver.
func plainColFn(idReader *anchor.ReadAccessTestTablePlainEntityIdAttributes) func(string) any {
	return func(name string) any {
		switch name {
		case "id":
			return idReader.ValueId
		case "naturalKey":
			return idReader.ValueNaturalKey
		}
		return nil
	}
}

// --- Multi-sub-column: timeRange (beginIncl + endExcl). ---

// rangeDrone shares one membership ("window") across two time.Time fields
// targeting distinct sub-columns of the timeRange section — the
// marshalMultiSubColumn / unmarshalMultiSubColumn path. beginIncl is
// declared before endExcl so the section's sub-column order matches the
// DML's BeginAttribute(beginIncl, endExcl) parameter order.
type rangeDrone struct {
	_        struct{}  `kind:"rangeDrone"`
	ID       uint64    `lw:",id"`
	Tracking []byte    `lw:",naturalKey"`
	Begin    time.Time `lw:"window,timeRange:beginIncl"`
	End      time.Time `lw:"window,timeRange:endExcl"`
}

func TestRoundTrip_MultiSubColumn_TimeRange(t *testing.T) {
	// Whole-second, year-2020 instants: robust to whichever physical
	// precision the timeRange column stores (compared via Unix seconds).
	original := []rangeDrone{
		{ID: 1, Tracking: []byte("R1"), Begin: time.Unix(1_600_000_000, 0).UTC(), End: time.Unix(1_600_003_600, 0).UTC()},
		{ID: 2, Tracking: []byte("R2"), Begin: time.Unix(1_600_100_000, 0).UTC(), End: time.Unix(1_600_200_000, 0).UTC()},
	}
	lookup := marshallreflect.MapLookup{"window": 1}

	rec, release := marshalToRecord(t, original, lookup)
	defer release()
	idReader, relID := loadIdReader(t, rec)
	defer relID()

	trReader := anchor.NewReadAccessTestTableTaggedTimeRange()
	trReader.SetColumnIndices(trReader.GetColumnIndices())
	require.NoError(t, trReader.LoadFromRecord(rec))
	defer trReader.Release()

	// One tuple attribute per row.
	require.Equal(t, int64(1), trReader.GetAttributes().GetNumberOfAttributes(0))

	args := marshallreflect.UnmarshalArgs{
		NumRows:  idReader.Len(),
		PlainCol: plainColFn(idReader),
		SectionAttrs: func(name string) any {
			if name == "timeRange" {
				return trReader.GetAttributes()
			}
			return nil
		},
		SectionMembs: func(name string) any {
			if name == "timeRange" {
				return trReader.GetMemberships()
			}
			return nil
		},
	}
	var got []rangeDrone
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Equal(t, len(original), len(got))
	for i := range original {
		require.Equal(t, original[i].ID, got[i].ID, "row %d ID", i)
		require.Equal(t, original[i].Begin.Unix(), got[i].Begin.Unix(), "row %d Begin", i)
		require.Equal(t, original[i].End.Unix(), got[i].End.Unix(), "row %d End", i)
		// Begin and End must not collapse — guards against reading both
		// sub-columns from the same source, or swapping them.
		require.NotEqual(t, got[i].Begin.Unix(), got[i].End.Unix(), "row %d begin/end distinct", i)
	}
}

// --- 1×N container: roaring bitmap into u32Array. ---

type roaringDrone struct {
	_        struct{}        `kind:"roaringDrone"`
	ID       uint64          `lw:",id"`
	Tracking []byte          `lw:",naturalKey"`
	Sensors  *roaring.Bitmap `lw:"sensors,u32Array"`
}

func TestRoundTrip_Container_RoaringBitmap(t *testing.T) {
	bm := func(vals ...uint32) *roaring.Bitmap {
		b := roaring.New()
		b.AddMany(vals)
		return b
	}
	original := []roaringDrone{
		{ID: 1, Tracking: []byte("B1"), Sensors: bm(5, 100, 4000)},
		{ID: 2, Tracking: []byte("B2"), Sensors: bm(7)},
	}
	lookup := marshallreflect.MapLookup{"sensors": 1}

	rec, release := marshalToRecord(t, original, lookup)
	defer release()
	idReader, relID := loadIdReader(t, rec)
	defer relID()

	u32Reader := anchor.NewReadAccessTestTableTaggedU32Array()
	u32Reader.SetColumnIndices(u32Reader.GetColumnIndices())
	require.NoError(t, u32Reader.LoadFromRecord(rec))
	defer u32Reader.Release()

	// Container shape: exactly one attribute per row carries the whole set.
	require.Equal(t, int64(1), u32Reader.GetAttributes().GetNumberOfAttributes(0))

	args := marshallreflect.UnmarshalArgs{
		NumRows:  idReader.Len(),
		PlainCol: plainColFn(idReader),
		SectionAttrs: func(name string) any {
			if name == "u32Array" {
				return u32Reader.GetAttributes()
			}
			return nil
		},
		SectionMembs: func(name string) any {
			if name == "u32Array" {
				return u32Reader.GetMemberships()
			}
			return nil
		},
	}
	var got []roaringDrone
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Equal(t, len(original), len(got))
	for i := range original {
		require.NotNil(t, got[i].Sensors, "row %d bitmap present", i)
		require.True(t, original[i].Sensors.Equals(got[i].Sensors), "row %d bitmap members: want %v got %v",
			i, original[i].Sensors.ToArray(), got[i].Sensors.ToArray())
	}
}

// --- 1×N container: plain slice into u32Array (read-back of the slice
// container path the in-package stack_test only ever writes). ---

type sliceDrone struct {
	_        struct{} `kind:"sliceDrone"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Codes    []uint32 `lw:"codes,u32Array"`
}

func TestRoundTrip_Container_Slice(t *testing.T) {
	// Non-sorted, with a duplicate, to prove the homogeneous-array section
	// preserves element order and multiplicity (unlike a set/bitmap).
	original := []sliceDrone{
		{ID: 1, Tracking: []byte("S1"), Codes: []uint32{30, 10, 20, 10}},
		{ID: 2, Tracking: []byte("S2"), Codes: []uint32{99}},
	}
	lookup := marshallreflect.MapLookup{"codes": 1}

	rec, release := marshalToRecord(t, original, lookup)
	defer release()
	idReader, relID := loadIdReader(t, rec)
	defer relID()

	u32Reader := anchor.NewReadAccessTestTableTaggedU32Array()
	u32Reader.SetColumnIndices(u32Reader.GetColumnIndices())
	require.NoError(t, u32Reader.LoadFromRecord(rec))
	defer u32Reader.Release()

	// Container shape: one attribute per row holds the whole slice.
	require.Equal(t, int64(1), u32Reader.GetAttributes().GetNumberOfAttributes(0))

	args := marshallreflect.UnmarshalArgs{
		NumRows:  idReader.Len(),
		PlainCol: plainColFn(idReader),
		SectionAttrs: func(name string) any {
			if name == "u32Array" {
				return u32Reader.GetAttributes()
			}
			return nil
		},
		SectionMembs: func(name string) any {
			if name == "u32Array" {
				return u32Reader.GetMemberships()
			}
			return nil
		},
	}
	var got []sliceDrone
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Equal(t, len(original), len(got))
	for i := range original {
		require.Equal(t, original[i].Codes, got[i].Codes, "row %d codes (order + multiplicity preserved)", i)
	}
}

// --- Explode: one attribute per element, into u32Array (BeginAttributeSingle). ---

// explodeDrone differs from sliceDrone only in the `,explode,unit` flags:
// the same []uint32 emits N single-value attributes instead of one
// container attribute. The reconstructed slice must be identical to the
// container case; the per-row attribute count proves the wire shape
// genuinely differs.
type explodeDrone struct {
	_        struct{} `kind:"explodeDrone"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Codes    []uint32 `lw:"codes,u32Array,explode,unit"`
}

func TestRoundTrip_Explode(t *testing.T) {
	original := []explodeDrone{
		{ID: 1, Tracking: []byte("E1"), Codes: []uint32{30, 10, 20}},
		{ID: 2, Tracking: []byte("E2"), Codes: []uint32{99}},
	}
	lookup := marshallreflect.MapLookup{"codes": 1}

	rec, release := marshalToRecord(t, original, lookup)
	defer release()
	idReader, relID := loadIdReader(t, rec)
	defer relID()

	u32Reader := anchor.NewReadAccessTestTableTaggedU32Array()
	u32Reader.SetColumnIndices(u32Reader.GetColumnIndices())
	require.NoError(t, u32Reader.LoadFromRecord(rec))
	defer u32Reader.Release()

	// Explode shape: one attribute per element (3 for row 0, 1 for row 1) —
	// in contrast to the container test's single attribute per row.
	require.Equal(t, int64(3), u32Reader.GetAttributes().GetNumberOfAttributes(0))
	require.Equal(t, int64(1), u32Reader.GetAttributes().GetNumberOfAttributes(1))

	args := marshallreflect.UnmarshalArgs{
		NumRows:  idReader.Len(),
		PlainCol: plainColFn(idReader),
		SectionAttrs: func(name string) any {
			if name == "u32Array" {
				return u32Reader.GetAttributes()
			}
			return nil
		},
		SectionMembs: func(name string) any {
			if name == "u32Array" {
				return u32Reader.GetMemberships()
			}
			return nil
		},
	}
	var got []explodeDrone
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Equal(t, len(original), len(got))
	for i := range original {
		require.Equal(t, original[i].Codes, got[i].Codes, "row %d codes (explode round-trip, order preserved)", i)
	}
}

// --- Explode of a roaring bitmap: one attribute per set bit. ---

// roaringExplodeDrone exercises the marshalExplode roaring branch
// (iterate the bitmap, emit one BeginAttributeSingle per bit) — distinct
// from roaringDrone's single container attribute. Read-back collapses the
// per-bit attributes back into one bitmap.
type roaringExplodeDrone struct {
	_        struct{}        `kind:"roaringExplodeDrone"`
	ID       uint64          `lw:",id"`
	Tracking []byte          `lw:",naturalKey"`
	Sensors  *roaring.Bitmap `lw:"sensors,u32Array,explode,unit"`
}

func TestRoundTrip_Explode_Roaring(t *testing.T) {
	bm := func(vals ...uint32) *roaring.Bitmap {
		b := roaring.New()
		b.AddMany(vals)
		return b
	}
	original := []roaringExplodeDrone{
		{ID: 1, Tracking: []byte("RE1"), Sensors: bm(5, 100, 4000)},
		{ID: 2, Tracking: []byte("RE2"), Sensors: bm(7)},
	}
	lookup := marshallreflect.MapLookup{"sensors": 1}

	rec, release := marshalToRecord(t, original, lookup)
	defer release()
	idReader, relID := loadIdReader(t, rec)
	defer relID()

	u32Reader := anchor.NewReadAccessTestTableTaggedU32Array()
	u32Reader.SetColumnIndices(u32Reader.GetColumnIndices())
	require.NoError(t, u32Reader.LoadFromRecord(rec))
	defer u32Reader.Release()

	// Explode shape: one attribute per set bit (3 for row 0, 1 for row 1) —
	// in contrast to TestRoundTrip_Container_RoaringBitmap's single attribute.
	require.Equal(t, int64(3), u32Reader.GetAttributes().GetNumberOfAttributes(0))
	require.Equal(t, int64(1), u32Reader.GetAttributes().GetNumberOfAttributes(1))

	args := marshallreflect.UnmarshalArgs{
		NumRows:  idReader.Len(),
		PlainCol: plainColFn(idReader),
		SectionAttrs: func(name string) any {
			if name == "u32Array" {
				return u32Reader.GetAttributes()
			}
			return nil
		},
		SectionMembs: func(name string) any {
			if name == "u32Array" {
				return u32Reader.GetMemberships()
			}
			return nil
		},
	}
	var got []roaringExplodeDrone
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Equal(t, len(original), len(got))
	for i := range original {
		require.NotNil(t, got[i].Sensors, "row %d bitmap present", i)
		require.True(t, original[i].Sensors.Equals(got[i].Sensors), "row %d bitmap members: want %v got %v",
			i, original[i].Sensors.ToArray(), got[i].Sensors.ToArray())
	}
}

// --- Option[T] scalar read-back (Has=true and Has=false rows). ---

// optDrone routes an option.Option[string] into the scalar symbol section.
// A None row emits zero attributes (splice); the read side must leave the
// reconstructed Option at Has=false rather than materialising a zero value.
type optDrone struct {
	_        struct{}              `kind:"optDrone"`
	ID       uint64                `lw:",id"`
	Tracking []byte                `lw:",naturalKey"`
	Note     option.Option[string] `lw:"note,symbol"`
}

func TestRoundTrip_OptionScalar(t *testing.T) {
	original := []optDrone{
		{ID: 1, Tracking: []byte("O1"), Note: option.Some("hello")},
		{ID: 2, Tracking: []byte("O2"), Note: option.None[string]()},
		{ID: 3, Tracking: []byte("O3"), Note: option.Some("world")},
	}
	lookup := marshallreflect.MapLookup{"note": 1}

	rec, release := marshalToRecord(t, original, lookup)
	defer release()
	idReader, relID := loadIdReader(t, rec)
	defer relID()

	symReader := anchor.NewReadAccessTestTableTaggedSymbol()
	symReader.SetColumnIndices(symReader.GetColumnIndices())
	require.NoError(t, symReader.LoadFromRecord(rec))
	defer symReader.Release()

	// Present rows carry one attribute; the None row carries none.
	require.Equal(t, int64(1), symReader.GetAttributes().GetNumberOfAttributes(0))
	require.Equal(t, int64(0), symReader.GetAttributes().GetNumberOfAttributes(1))

	args := marshallreflect.UnmarshalArgs{
		NumRows:  idReader.Len(),
		PlainCol: plainColFn(idReader),
		SectionAttrs: func(name string) any {
			if name == "symbol" {
				return symReader.GetAttributes()
			}
			return nil
		},
		SectionMembs: func(name string) any {
			if name == "symbol" {
				return symReader.GetMemberships()
			}
			return nil
		},
	}
	var got []optDrone
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Equal(t, len(original), len(got))
	require.True(t, got[0].Note.Has, "row 0 present")
	require.Equal(t, "hello", got[0].Note.Val)
	require.False(t, got[1].Note.Has, "row 1 absent — Has stays false")
	require.Equal(t, "", got[1].Note.Val, "absent Option leaves the zero value")
	require.True(t, got[2].Note.Has, "row 2 present")
	require.Equal(t, "world", got[2].Note.Val)
}

// --- Fixed [N]byte round-trip (carried as a resliced blob). ---

// fixedDrone routes a [16]byte into blobArray with `,unit`: the write side
// reslices the array to []byte (reslicedIfFixedByte), the read side copies
// the blob back into the fixed array (consumeValue's fixed-byte branch).
type fixedDrone struct {
	_        struct{} `kind:"fixedDrone"`
	ID       uint64   `lw:",id"`
	Tracking []byte   `lw:",naturalKey"`
	Sig      [16]byte `lw:"sig,blobArray,unit"`
}

func TestRoundTrip_FixedByteArray(t *testing.T) {
	mk := func(s string) [16]byte {
		var a [16]byte
		copy(a[:], s)
		return a
	}
	original := []fixedDrone{
		{ID: 1, Tracking: []byte("F1"), Sig: mk("sixteen-byte-sig")},
		{ID: 2, Tracking: []byte("F2"), Sig: mk("another-16-byte!")},
	}
	lookup := marshallreflect.MapLookup{"sig": 1}

	rec, release := marshalToRecord(t, original, lookup)
	defer release()
	idReader, relID := loadIdReader(t, rec)
	defer relID()

	blobReader := anchor.NewReadAccessTestTableTaggedBlobArray()
	blobReader.SetColumnIndices(blobReader.GetColumnIndices())
	require.NoError(t, blobReader.LoadFromRecord(rec))
	defer blobReader.Release()

	args := marshallreflect.UnmarshalArgs{
		NumRows:  idReader.Len(),
		PlainCol: plainColFn(idReader),
		SectionAttrs: func(name string) any {
			if name == "blobArray" {
				return blobReader.GetAttributes()
			}
			return nil
		},
		SectionMembs: func(name string) any {
			if name == "blobArray" {
				return blobReader.GetMemberships()
			}
			return nil
		},
	}
	var got []fixedDrone
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Equal(t, len(original), len(got))
	for i := range original {
		require.Equal(t, original[i].Sig, got[i].Sig, "row %d Sig", i)
	}
}

// --- Option[[]byte] scalar-blob lane (Has=true and Has=false). ---

// optBlobDrone exercises the Option[[]byte] scalar-blob lane into blobArray:
// the write reads Option.Val as []byte, the read copies the blob out and
// sets Has via projectAccumulator's Option branch.
type optBlobDrone struct {
	_        struct{}              `kind:"optBlobDrone"`
	ID       uint64                `lw:",id"`
	Tracking []byte                `lw:",naturalKey"`
	Payload  option.Option[[]byte] `lw:"payload,blobArray,unit"`
}

func TestRoundTrip_OptionBlob(t *testing.T) {
	original := []optBlobDrone{
		{ID: 1, Tracking: []byte("P1"), Payload: option.Some([]byte("payload-bytes"))},
		{ID: 2, Tracking: []byte("P2"), Payload: option.None[[]byte]()},
	}
	lookup := marshallreflect.MapLookup{"payload": 1}

	rec, release := marshalToRecord(t, original, lookup)
	defer release()
	idReader, relID := loadIdReader(t, rec)
	defer relID()

	blobReader := anchor.NewReadAccessTestTableTaggedBlobArray()
	blobReader.SetColumnIndices(blobReader.GetColumnIndices())
	require.NoError(t, blobReader.LoadFromRecord(rec))
	defer blobReader.Release()

	require.Equal(t, int64(1), blobReader.GetAttributes().GetNumberOfAttributes(0))
	require.Equal(t, int64(0), blobReader.GetAttributes().GetNumberOfAttributes(1))

	args := marshallreflect.UnmarshalArgs{
		NumRows:  idReader.Len(),
		PlainCol: plainColFn(idReader),
		SectionAttrs: func(name string) any {
			if name == "blobArray" {
				return blobReader.GetAttributes()
			}
			return nil
		},
		SectionMembs: func(name string) any {
			if name == "blobArray" {
				return blobReader.GetMemberships()
			}
			return nil
		},
	}
	var got []optBlobDrone
	require.NoError(t, marshallreflect.Unmarshal(args, &got, lookup))

	require.Equal(t, len(original), len(got))
	require.True(t, got[0].Payload.Has, "row 0 present")
	require.Equal(t, []byte("payload-bytes"), got[0].Payload.Val)
	require.False(t, got[1].Payload.Has, "row 1 absent — Has stays false")
	require.Nil(t, got[1].Payload.Val, "absent blob leaves the zero value")
}

// --- Cut-2: mixedLowCardRef (value + per-row carrier id/params). ---

// mixedDrone pairs a value field with a marshalltypes.MixedLowCardRef
// carrier on one (membership, section, channel) triple. The membership
// identity is per-row carrier data — no lookup — so the codec emits
// AddMembershipMixedLowCardRefP(carrier.Id, carrier.Params) and reads both
// back via the symbol section's Seq2 combined accessor.
type mixedDrone struct {
	_        struct{}                      `kind:"mixedDrone"`
	ID       uint64                        `lw:",id"`
	Tracking []byte                        `lw:",naturalKey"`
	Reading  string                        `lw:"sensor,symbol,mixedLowCardRef"`
	ReadingC marshalltypes.MixedLowCardRef `lw:"sensor,symbol,mixedLowCardRef"`
}

func TestRoundTrip_MixedLowCardRef(t *testing.T) {
	original := []mixedDrone{
		{ID: 1, Tracking: []byte("M1"), Reading: "alpha", ReadingC: marshalltypes.MixedLowCardRef{Id: 7, Params: []byte("p-one")}},
		{ID: 2, Tracking: []byte("M2"), Reading: "beta", ReadingC: marshalltypes.MixedLowCardRef{Id: 9, Params: []byte("p-two")}},
	}
	// Per-row membership identity (carrier Id+Params) — no lookup consulted.
	rec, release := marshalToRecord(t, original, marshallreflect.NoLookup{})
	defer release()
	idReader, relID := loadIdReader(t, rec)
	defer relID()

	symReader := anchor.NewReadAccessTestTableTaggedSymbol()
	symReader.SetColumnIndices(symReader.GetColumnIndices())
	require.NoError(t, symReader.LoadFromRecord(rec))
	defer symReader.Release()

	// One scalar attribute per row in the symbol section.
	require.Equal(t, int64(1), symReader.GetAttributes().GetNumberOfAttributes(0))

	args := marshallreflect.UnmarshalArgs{
		NumRows:  idReader.Len(),
		PlainCol: plainColFn(idReader),
		SectionAttrs: func(name string) any {
			if name == "symbol" {
				return symReader.GetAttributes()
			}
			return nil
		},
		SectionMembs: func(name string) any {
			if name == "symbol" {
				return symReader.GetMemberships()
			}
			return nil
		},
	}
	var got []mixedDrone
	require.NoError(t, marshallreflect.Unmarshal(args, &got, marshallreflect.NoLookup{}))

	require.Equal(t, len(original), len(got))
	for i := range original {
		require.Equal(t, original[i].Reading, got[i].Reading, "row %d value", i)
		require.Equal(t, original[i].ReadingC.Id, got[i].ReadingC.Id, "row %d carrier id", i)
		require.Equal(t, original[i].ReadingC.Params, got[i].ReadingC.Params, "row %d carrier params", i)
	}
}
