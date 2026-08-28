package example

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	cwruntime "github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime"
)

// The smoke test of ADR-0207 M1: a batch written through the generated dml
// builders, read back through the generated readaccess classes and encoded by
// the generated encoder, checked against the table-free canonical rules. It
// says the three generated APIs fit together and that the bytes the encoder
// produces are in the form VerifyCanonical accepts — not that they decode back,
// which is the decoder's own milestone.

const smokeEntities = 3

// TestEncodeTestTableSmoke drives the sample table: scalars, an `h`
// co-container pair, and a shared membership pack carrying a ref channel
// beside a mixed carrier channel.
func TestEncodeTestTableSmoke(t *testing.T) {
	dml := NewInEntityTestTable(memory.DefaultAllocator, 128)
	ts := []time.Time{
		time.UnixMilli(1_700_000_000_000).UTC(),
		time.UnixMilli(1_700_000_001_000).UTC(),
		time.UnixMilli(1_700_000_002_000).UTC(),
	}
	const lrBase uint64 = 0
	{ // dml write — the write block of readaccess/example/roundtrip_test.go
		secText := dml.GetSectionText()
		secGeo := dml.GetSectionGeo()
		for i := range smokeEntities {
			ent := dml.BeginEntity()
			ent.SetId(uint64(i))
			ent.SetTimestamp(ts[0], ts[1:3])
			secText.BeginAttribute(fmt.Sprintf("hello world! %d", i)).
				AddToCoContainers(uint32(len("hello")), "hello").
				AddToCoContainers(uint32(len("world")), "world").
				AddMembershipLowCardRef(lrBase+uint64(i)*5).
				AddMembershipLowCardRef(lrBase+uint64(i)*5+1).
				AddMembershipMixedLowCardVerbatim([]byte("verbatim1"), []byte("params1")).
				EndAttribute()
			if i%2 == 0 {
				secText.BeginAttribute(fmt.Sprintf("hallo welt! %d", i)).
					AddToCoContainers(uint32(len("hallo")), "hallo").
					AddToCoContainers(uint32(len("welt")), "welt").
					AddMembershipLowCardRef(lrBase+uint64(i)*5+2).
					AddMembershipLowCardRef(lrBase+uint64(i)*5+3).
					AddMembershipMixedLowCardVerbatim([]byte("wortwörtlich1"), []byte("parameter1")).
					EndAttribute()
			}
			secGeo.BeginAttribute(12.0, -3.5, 0x45494, 0x45454543).
				AddMembershipMixedLowCardVerbatim([]byte("verbatim2"), []byte("params2")).
				AddMembershipLowCardRef(lrBase + uint64(i)*5 + 4).
				EndAttribute()
			require.NoError(t, ent.CheckErrors())
			require.NoError(t, ent.CommitEntity())
		}
	}

	ra := NewReadAccessTestTable()
	var records []arrow.RecordBatch
	records, err := dml.TransferRecords(nil)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.EqualValues(t, smokeEntities, records[0].NumRows())
	require.NoError(t, ra.LoadFromRecord(records[0]))

	enc, err := NewCanonWireEncoderTestTable(ra, nil)
	require.NoError(t, err)

	var first bytes.Buffer
	require.NoError(t, enc.EncodeAll(&first))
	n, err := cwruntime.VerifyCanonicalSequence(first.Bytes())
	require.NoError(t, err)
	require.Equal(t, smokeEntities, n)

	// The same batch through the same encoder twice: the buffers are reused
	// across entities, so a leak from one entity into the next would show here.
	var second bytes.Buffer
	require.NoError(t, enc.EncodeAll(&second))
	require.Equal(t, first.Bytes(), second.Bytes())
}

// TestEncodePlaceSmoke drives the co-section table: geo and h3 are one slot,
// so an attribute of it carries two membership groups and the value columns of
// both sections.
func TestEncodePlaceSmoke(t *testing.T) {
	dml := NewInEntityPlace(memory.DefaultAllocator, 128)
	{
		secGeo := dml.GetSectionGeo()
		secH3 := dml.GetSectionH3()
		secTags := dml.GetSectionTags()
		for i := range smokeEntities {
			ent := dml.BeginEntity()
			ent.SetId(uint64(i))
			// One attribute per co-section member, in step: the slot's
			// attribute count is the same on both sides.
			secGeo.BeginAttribute(float32(i)+0.5, float32(i)-0.25).
				AddMembershipLowCardRef(uint64(i)).
				EndAttribute()
			secH3.BeginAttribute(uint64(0x8928308280fffff) + uint64(i)).
				AddMembershipLowCardRef(uint64(i) + 100).
				EndAttribute()
			if i%2 == 0 {
				// A second attribute of the co-section group, written to both
				// members again.
				secGeo.BeginAttribute(-float32(i), float32(i)*2).
					AddMembershipLowCardRef(uint64(i) + 7).
					EndAttribute()
				secH3.BeginAttribute(uint64(0x8928308280bffff) + uint64(i)).
					EndAttribute()
			}
			secTags.BeginAttribute().
				AddToCoContainers("alpha", 11).
				AddToCoContainers("beta", 11).
				AddMembershipLowCardVerbatim([]byte("kind")).
				EndAttribute()
			require.NoError(t, ent.CheckErrors())
			require.NoError(t, ent.CommitEntity())
		}
	}

	ra := NewReadAccessPlace()
	records, err := dml.TransferRecords(nil)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.EqualValues(t, smokeEntities, records[0].NumRows())
	require.NoError(t, ra.LoadFromRecord(records[0]))

	enc, err := NewCanonWireEncoderPlace(ra, nil)
	require.NoError(t, err)

	var first bytes.Buffer
	require.NoError(t, enc.EncodeAll(&first))
	n, err := cwruntime.VerifyCanonicalSequence(first.Bytes())
	require.NoError(t, err)
	require.Equal(t, smokeEntities, n)

	var second bytes.Buffer
	require.NoError(t, enc.EncodeAll(&second))
	require.Equal(t, first.Bytes(), second.Bytes())
}

// The built-in tagger is a valid tagger for a table with no ambiguous
// signature too: it is consulted for no attribute, so the bytes do not move.
func TestOrdinalTaggerLeavesUnambiguousBytesAlone(t *testing.T) {
	require.False(t, CanonWireSlotPlaceGeoH3.Ambiguous())
	require.False(t, CanonWireSlotPlaceTags.Ambiguous())
	require.Equal(t, CanonWireSignaturePlaceGeoH3, CanonWireSlotPlaceGeoH3.Signature())
	require.Equal(t, "geo+h3", CanonWireSlotPlaceGeoH3.String())
	require.Zero(t, CanonWireOrdinalTaggerPlace{}.Tag(CanonWireSlotPlaceGeoH3, 0, 0))

	// The JSON table is the other half of the story: its two ambiguity sets
	// are exactly the slots the tagger is consulted for.
	require.True(t, CanonWireSlotJsonNull.Ambiguous())
	require.True(t, CanonWireSlotJsonSymbol.Ambiguous())
	require.False(t, CanonWireSlotJsonBool.Ambiguous())
	require.Equal(t, uint64(1), CanonWireOrdinalTaggerJson{}.Tag(CanonWireSlotJsonNull, 0, 0))
	require.Equal(t, uint64(1), CanonWireOrdinalTaggerJson{}.Tag(CanonWireSlotJsonSymbol, 0, 0))
	require.Equal(t, uint64(0), CanonWireOrdinalTaggerJson{}.Tag(CanonWireSlotJsonBool, 0, 0))
}

// TestEncodeJsonSmoke drives the ambiguity story: four value-less sections
// share the empty signature and two share `s`, so one wire slot carries the
// attributes of several sections and every attribute of it needs the SD5
// discriminator to be told apart again. The built-in ordinal tagger supplies
// it.
func TestEncodeJsonSmoke(t *testing.T) {
	dml := NewInEntityJson(memory.DefaultAllocator, 128)
	{
		secNull := dml.GetSectionNull()
		secUndefined := dml.GetSectionUndefined()
		secString := dml.GetSectionString()
		secSymbol := dml.GetSectionSymbol()
		secBool := dml.GetSectionBool()
		for i := range smokeEntities {
			ent := dml.BeginEntity()
			ent.SetId([]byte{byte(i), 0x01, 0x02})
			// The two ambiguity sets, both populated: the value-less sections
			// have no attribute class at all, so their attribute count comes
			// off the membership pack's accelerator.
			secNull.BeginAttribute().
				AddMembershipMixedLowCardVerbatim([]byte("/a"), nil).
				EndAttribute()
			secUndefined.BeginAttribute().
				AddMembershipMixedLowCardVerbatim([]byte("/b"), nil).
				EndAttribute()
			secString.BeginAttribute(fmt.Sprintf("text %d", i)).
				AddMembershipMixedLowCardVerbatim([]byte("/c"), nil).
				EndAttribute()
			secSymbol.BeginAttribute(fmt.Sprintf("sym %d", i)).
				AddMembershipMixedLowCardVerbatim([]byte("/d"), nil).
				EndAttribute()
			secBool.BeginAttribute(i%2 == 0).
				AddMembershipMixedLowCardVerbatim([]byte("/e"), nil).
				EndAttribute()
			require.NoError(t, ent.CheckErrors())
			require.NoError(t, ent.CommitEntity())
		}
	}

	ra := NewReadAccessJson()
	records, err := dml.TransferRecords(nil)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.NoError(t, ra.LoadFromRecord(records[0]))

	// With the ordinal tagger every attribute of an ambiguous slot carries a
	// discriminator; without one, none does. Both are canonical, and the two
	// differ in bytes, which is what the discriminator being content means.
	tagged, err := NewCanonWireEncoderJson(ra, CanonWireOrdinalTaggerJson{})
	require.NoError(t, err)
	var withTagger bytes.Buffer
	require.NoError(t, tagged.EncodeAll(&withTagger))
	n, err := cwruntime.VerifyCanonicalSequence(withTagger.Bytes())
	require.NoError(t, err)
	require.Equal(t, smokeEntities, n)

	untagged, err := NewCanonWireEncoderJson(ra, nil)
	require.NoError(t, err)
	var withoutTagger bytes.Buffer
	require.NoError(t, untagged.EncodeAll(&withoutTagger))
	n, err = cwruntime.VerifyCanonicalSequence(withoutTagger.Bytes())
	require.NoError(t, err)
	require.Equal(t, smokeEntities, n)
	require.NotEqual(t, withTagger.Bytes(), withoutTagger.Bytes())

	var again bytes.Buffer
	require.NoError(t, tagged.EncodeAll(&again))
	require.Equal(t, withTagger.Bytes(), again.Bytes())
}
