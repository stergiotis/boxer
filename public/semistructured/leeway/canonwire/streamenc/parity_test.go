package streamenc

// The parity suite of ADR-0219 SD3: over every table in canonwire/example, a
// batch written through the generated dml builders encodes to the same bytes
// through the generated encoder (nil tagger) and through this package's
// stream encoder driven by streamreadaccess. Byte equality is the whole
// claim; VerifyCanonical and the per-entity slices ride along, and a one-row
// slice of the batch must yield the entity's bytes unchanged — the play app
// drives single rows in one pane and whole batches in another and shows the
// two side by side.

import (
	"bytes"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/example"
	cwruntime "github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
)

const parityEntities = 4

// requireParity drives rec through a fresh stream encoder over tbl and
// requires the bytes the generated encoder produced.
func requireParity(t *testing.T, tbl common.TableDesc, rec arrow.RecordBatch, generated []byte) {
	t.Helper()
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tbl, clickhouse.NewTechnologySpecificCodeGenerator()))
	d, err := streamreadaccess.NewDriver(&tbl, ir, streamreadaccess.DefaultFormatters())
	require.NoError(t, err)
	enc, err := NewEncoder(&tbl, ir)
	require.NoError(t, err)

	require.NoError(t, d.DriveRecordBatch(enc, rec))
	require.NoError(t, enc.Err())
	require.Equal(t, generated, enc.Bytes(), "stream encoder bytes differ from the generated encoder's")
	require.Equal(t, int(rec.NumRows()), enc.NumEntities())
	n, err := cwruntime.VerifyCanonicalSequence(enc.Bytes())
	require.NoError(t, err)
	require.Equal(t, int(rec.NumRows()), n)

	// The per-entity views tile the sequence, and a one-row slice of the
	// batch encodes to the same entity item.
	var tiled bytes.Buffer
	whole := make([][]byte, enc.NumEntities())
	for i := range enc.NumEntities() {
		whole[i] = append([]byte(nil), enc.Entity(i)...)
		tiled.Write(whole[i])
	}
	require.Equal(t, generated, tiled.Bytes())
	for i := range whole {
		slice := rec.NewSlice(int64(i), int64(i)+1)
		require.NoError(t, d.DriveRecordBatch(enc, slice))
		slice.Release()
		require.NoError(t, enc.Err())
		require.Equal(t, 1, enc.NumEntities())
		require.Equal(t, whole[i], enc.Entity(0), "entity %d through a one-row slice", i)
	}

	// The same batch twice through the same encoder: the buffers are reused
	// across entities, so a leak from one drive into the next would show.
	require.NoError(t, d.DriveRecordBatch(enc, rec))
	require.Equal(t, generated, enc.Bytes())
}

func oneRecord(t *testing.T, records func([]arrow.RecordBatch) ([]arrow.RecordBatch, error)) arrow.RecordBatch {
	t.Helper()
	recs, err := records(nil)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	return recs[0]
}

func TestParityTestTable(t *testing.T) {
	tbl, err := sampleTableDesc()
	require.NoError(t, err)
	dml := example.NewInEntityTestTable(memory.DefaultAllocator, 128)
	base := time.UnixMilli(1_700_000_000_123).UTC()
	secGeo := dml.GetSectionGeo()
	secText := dml.GetSectionText()
	for i := range parityEntities {
		procs := []time.Time{base, base.Add(time.Second), base.Add(2 * time.Second)}
		ent := dml.BeginEntity()
		ent.SetId(uint64(i)*7 + 1)
		ent.SetTimestamp(base.Add(time.Duration(i)*time.Millisecond), procs[:i%4])
		// A zero ref: the wire keeps what the lane holds, and the driver
		// hands it over like any other value.
		secText.BeginAttribute(fmt.Sprintf("three %d", i)).
			AddToCoContainers(5, "alpha").
			AddToCoContainers(4, "beta").
			AddToCoContainers(5, "gamma").
			AddMembershipLowCardRef(uint64(i)*10).
			AddMembershipLowCardRef(uint64(i)*10+2).
			AddMembershipMixedLowCardVerbatim([]byte("kind"), []byte("params")).
			EndAttribute()
		secText.BeginAttribute(fmt.Sprintf("empty %d", i)).
			AddMembershipMixedLowCardVerbatim([]byte("kind"), nil).
			EndAttribute()
		if i%2 == 0 {
			secText.BeginAttribute(fmt.Sprintf("one %d", i)).
				AddToCoContainers(3, "one").
				AddMembershipLowCardRef(uint64(i)*10 + 3).
				EndAttribute()
		}
		if i != 1 {
			secGeo.BeginAttribute(float32(i)+0.5, -float32(i)-0.25, 0x45494+uint64(i), 0x45454543).
				AddMembershipLowCardRef(uint64(i) + 100).
				EndAttribute()
			secGeo.BeginAttribute(float32(math.Copysign(0, -1)), 12.5, 1, 2).
				AddMembershipMixedLowCardVerbatim([]byte("geo-kind"), []byte("geo-params")).
				EndAttribute()
		}
		require.NoError(t, ent.CheckErrors())
		require.NoError(t, ent.CommitEntity())
	}
	rec := oneRecord(t, dml.TransferRecords)
	defer rec.Release()
	ra := example.NewReadAccessTestTable()
	require.NoError(t, ra.LoadFromRecord(rec))
	enc, err := example.NewCanonWireEncoderTestTable(ra, nil)
	require.NoError(t, err)
	var generated bytes.Buffer
	require.NoError(t, enc.EncodeAll(&generated))
	requireParity(t, tbl, rec, generated.Bytes())
}

func TestParityNetTable(t *testing.T) {
	tbl, err := networkSampleTableDesc()
	require.NoError(t, err)
	dml := example.NewInEntityNetTable(memory.DefaultAllocator, 128)
	base := time.UnixMilli(1_700_000_000_500).UTC()
	sec := dml.GetSectionNet()
	v6 := [16]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	v6mapped := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 10, 0, 0, 1}
	v6pfx := [17]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 32}
	zeroPfx := [17]byte{}
	for i := range parityEntities {
		ent := dml.BeginEntity()
		ent.SetId(uint64(i) + 1)
		ent.SetTimestamp(base.Add(time.Duration(i) * time.Second))
		sec.BeginAttribute(0x0a000001+uint32(i), v6, [5]byte{10, 0, 0, 0, 8}, v6pfx).
			AddMembershipLowCardRef(uint64(i)+1).
			AddMembershipMixedLowCardVerbatim([]byte("net-kind"), []byte("net-params")).
			EndAttribute()
		// An IPv4-mapped IPv6 stays IPv6 (no reduction); an unmasked prefix
		// travels masked; a zero-length prefix is the degenerate encoding.
		sec.BeginAttribute(0, v6mapped, [5]byte{192, 168, 1, 77, 24}, zeroPfx).
			AddMembershipMixedLowCardVerbatim([]byte("mapped"), nil).
			EndAttribute()
		require.NoError(t, ent.CheckErrors())
		require.NoError(t, ent.CommitEntity())
	}
	rec := oneRecord(t, dml.TransferRecords)
	defer rec.Release()
	ra := example.NewReadAccessNetTable()
	require.NoError(t, ra.LoadFromRecord(rec))
	enc, err := example.NewCanonWireEncoderNetTable(ra, nil)
	require.NoError(t, err)
	var generated bytes.Buffer
	require.NoError(t, enc.EncodeAll(&generated))
	requireParity(t, tbl, rec, generated.Bytes())
}

func TestParityPlace(t *testing.T) {
	tbl, err := placeTableDesc()
	require.NoError(t, err)
	dml := example.NewInEntityPlace(memory.DefaultAllocator, 128)
	secGeo := dml.GetSectionGeo()
	secH3 := dml.GetSectionH3()
	secTags := dml.GetSectionTags()
	for i := range parityEntities {
		ent := dml.BeginEntity()
		ent.SetId(uint64(i) * 3)
		secGeo.BeginAttribute(float32(i)+0.5, float32(i)-0.25).
			AddMembershipLowCardRef(uint64(i)).
			EndAttribute()
		secH3.BeginAttribute(uint64(0x8928308280fffff) + uint64(i)).
			AddMembershipLowCardRef(uint64(i) + 100).
			EndAttribute()
		if i%2 == 0 {
			secGeo.BeginAttribute(-float32(i), float32(i)*2).
				AddMembershipLowCardRef(uint64(i) + 7).
				EndAttribute()
			secH3.BeginAttribute(uint64(0x8928308280bffff) + uint64(i)).
				EndAttribute()
		}
		switch i % 3 {
		case 0:
			secTags.BeginAttribute().
				AddToCoContainers("alpha", 7).
				AddToCoContainers("beta", 3).
				AddToCoContainers("gamma", 7).
				AddMembershipLowCardVerbatim([]byte("kind")).
				EndAttribute()
		case 1:
			secTags.BeginAttribute().
				AddToCoContainers("only", 1).
				AddMembershipLowCardVerbatim([]byte("kind")).
				EndAttribute()
		default:
			secTags.BeginAttribute().
				AddMembershipLowCardVerbatim([]byte("empty")).
				EndAttribute()
		}
		require.NoError(t, ent.CheckErrors())
		require.NoError(t, ent.CommitEntity())
	}
	rec := oneRecord(t, dml.TransferRecords)
	defer rec.Release()
	ra := example.NewReadAccessPlace()
	require.NoError(t, ra.LoadFromRecord(rec))
	enc, err := example.NewCanonWireEncoderPlace(ra, nil)
	require.NoError(t, err)
	var generated bytes.Buffer
	require.NoError(t, enc.EncodeAll(&generated))
	requireParity(t, tbl, rec, generated.Bytes())
}

func TestParityJson(t *testing.T) {
	tbl, err := jsonTableDesc()
	require.NoError(t, err)
	dml := example.NewInEntityJson(memory.DefaultAllocator, 128)
	secNull := dml.GetSectionNull()
	secUndefined := dml.GetSectionUndefined()
	secEmptyObject := dml.GetSectionEmptyObject()
	secEmptyArray := dml.GetSectionEmptyArray()
	secString := dml.GetSectionString()
	secSymbol := dml.GetSectionSymbol()
	secBool := dml.GetSectionBool()
	for i := range parityEntities {
		ent := dml.BeginEntity()
		ent.SetId([]byte{byte(i), 0x01, 0x02})
		secNull.BeginAttribute().
			AddMembershipMixedLowCardVerbatim([]byte("/a"), []byte("p")).
			EndAttribute()
		secUndefined.BeginAttribute().
			AddMembershipMixedLowCardVerbatim([]byte("/b"), nil).
			EndAttribute()
		secEmptyObject.BeginAttribute().
			AddMembershipMixedLowCardVerbatim([]byte("/c"), nil).
			EndAttribute()
		secEmptyArray.BeginAttribute().
			AddMembershipMixedLowCardVerbatim([]byte("/d"), nil).
			EndAttribute()
		secString.BeginAttribute(fmt.Sprintf("text %d", i)).
			AddMembershipMixedLowCardVerbatim([]byte("/e"), nil).
			EndAttribute()
		secSymbol.BeginAttribute(fmt.Sprintf("sym %d", i)).
			AddMembershipMixedLowCardVerbatim([]byte("/f"), nil).
			EndAttribute()
		secBool.BeginAttribute(i%2 == 0).
			AddMembershipMixedLowCardVerbatim([]byte("/g"), nil).
			EndAttribute()
		if i%2 == 0 {
			secString.BeginAttribute(fmt.Sprintf("more %d", i)).
				AddMembershipMixedLowCardVerbatim([]byte("/h"), nil).
				EndAttribute()
			secSymbol.BeginAttribute(fmt.Sprintf("more sym %d", i)).
				AddMembershipMixedLowCardVerbatim([]byte("/i"), nil).
				EndAttribute()
			secNull.BeginAttribute().
				AddMembershipMixedLowCardVerbatim([]byte("/j"), nil).
				EndAttribute()
		}
		require.NoError(t, ent.CheckErrors())
		require.NoError(t, ent.CommitEntity())
	}
	rec := oneRecord(t, dml.TransferRecords)
	defer rec.Release()
	ra := example.NewReadAccessJson()
	require.NoError(t, ra.LoadFromRecord(rec))
	// The stream encoder has no tagger, so the generated reference is the
	// nil-tagger encoding — the discriminator is absent on both sides.
	enc, err := example.NewCanonWireEncoderJson(ra, nil)
	require.NoError(t, err)
	var generated bytes.Buffer
	require.NoError(t, enc.EncodeAll(&generated))
	requireParity(t, tbl, rec, generated.Bytes())
}

func TestParityChannelTable(t *testing.T) {
	tbl, err := channelTableDesc()
	require.NoError(t, err)
	dml := example.NewInEntityChannelTable(memory.DefaultAllocator, 128)
	secMref := dml.GetSectionMref()
	secHref := dml.GetSectionHref()
	secHverb := dml.GetSectionHverb()
	secLparam := dml.GetSectionLparam()
	secHparam := dml.GetSectionHparam()
	for i := range parityEntities {
		ent := dml.BeginEntity()
		ent.SetId(uint64(i))
		secMref.BeginAttribute(uint64(i)*11).
			AddMembershipMixedLowCardRef(uint64(i)+1, []byte("p1")).
			AddMembershipMixedLowCardRef(uint64(i)+1, []byte("p0")).
			EndAttribute()
		secHref.BeginAttribute(-int64(i)).
			AddMembershipHighCardRef(9).
			AddMembershipHighCardRef(2).
			EndAttribute()
		secHverb.BeginAttribute(fmt.Sprintf("s%d", i)).
			AddMembershipHighCardVerbatim([]byte("zz")).
			AddMembershipHighCardVerbatim([]byte("aa")).
			EndAttribute()
		secLparam.BeginAttribute([]byte{0x01, byte(i)}).
			AddMembershipLowCardRefParametrized([]byte("k=10")).
			AddMembershipLowCardRefParametrized([]byte("k=2")).
			EndAttribute()
		if i%2 == 0 {
			secHparam.BeginAttribute(float64(i) + 0.5).
				AddMembershipHighCardRefParametrized([]byte("q")).
				EndAttribute()
		}
		require.NoError(t, ent.CheckErrors())
		require.NoError(t, ent.CommitEntity())
	}
	rec := oneRecord(t, dml.TransferRecords)
	defer rec.Release()
	ra := example.NewReadAccessChannelTable()
	require.NoError(t, ra.LoadFromRecord(rec))
	enc, err := example.NewCanonWireEncoderChannelTable(ra, nil)
	require.NoError(t, err)
	var generated bytes.Buffer
	require.NoError(t, enc.EncodeAll(&generated))
	requireParity(t, tbl, rec, generated.Bytes())
}

func TestParityFixedTable(t *testing.T) {
	tbl, err := fixedTableDesc()
	require.NoError(t, err)
	dml := example.NewInEntityFixedTable(memory.DefaultAllocator, 128)
	secCode := dml.GetSectionCode()
	secCodes := dml.GetSectionCodes()
	secHash := dml.GetSectionHash()
	for i := range parityEntities {
		ent := dml.BeginEntity()
		ent.SetId(uint64(i))
		secCode.BeginAttribute(fmt.Sprintf("c%03d", i)).
			AddMembershipLowCardRef(uint64(i) + 1).
			EndAttribute()
		secCodes.BeginAttribute().
			AddToContainer("abc").
			AddToContainer("xyz").
			AddToContainer("abc").
			AddMembershipLowCardRef(7).
			EndAttribute()
		secHash.BeginAttribute([4]byte{byte(i), 0xff, 0, 1}).
			AddMembershipLowCardRef(9).
			EndAttribute()
		require.NoError(t, ent.CheckErrors())
		require.NoError(t, ent.CommitEntity())
	}
	rec := oneRecord(t, dml.TransferRecords)
	defer rec.Release()
	ra := example.NewReadAccessFixedTable()
	require.NoError(t, ra.LoadFromRecord(rec))
	enc, err := example.NewCanonWireEncoderFixedTable(ra, nil)
	require.NoError(t, err)
	var generated bytes.Buffer
	require.NoError(t, enc.EncodeAll(&generated))
	requireParity(t, tbl, rec, generated.Bytes())
}
