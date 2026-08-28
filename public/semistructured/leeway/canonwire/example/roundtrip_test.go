package example

import (
	"bytes"
	"fmt"
	"iter"
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	cwruntime "github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime"
	rartime "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
)

// The round-trip suite of ADR-0210 M2: dml → batch → Encoder → bytes₁ →
// Decoder → batch → Encoder → bytes₂, with bytes₁ == bytes₂ and the two
// batches agreeing under the generated read accessors.
//
// The comparison of the two batches is deliberately **order-insensitive**
// within a section: the form sorts a slot's attributes into canonical order
// (ADR-0210 SD3), and it sorts an attribute's memberships too, so a decoded
// batch holds the same attributes as the written one but not in the order they
// were written. Each attribute is rendered as one line, the lines of one
// section of one entity are sorted, and the sorted lists are compared — a
// multiset comparison spelled as a sort. Attribute *counts* and plain values
// are compared directly, since neither is reordered.
//
// bytes₁ == bytes₂ is the stronger claim and is what says nothing was lost:
// the second pass starts from a batch the decoder built, so any column the
// decoder dropped, widened or reordered would move the bytes.

const roundTripEntities = 4

// seqLines renders an iterator's elements as sorted lines. Container order *is*
// content for an `h` column, so this is only used where the source is a set of
// memberships, never for a value container.
func seqLines[T any](seq iter.Seq[T]) (lines []string) {
	lines = make([]string, 0, 4)
	for v := range seq {
		lines = append(lines, fmt.Sprintf("%v", v))
	}
	slices.Sort(lines)
	return
}

func seq2Lines[A any, B any](seq iter.Seq2[A, B]) (lines []string) {
	lines = make([]string, 0, 4)
	for a, b := range seq {
		lines = append(lines, fmt.Sprintf("%v/%v", a, b))
	}
	slices.Sort(lines)
	return
}

// seqOrdered renders an iterator's elements keeping their order — an `h`
// column's stored order is content and must survive the round trip.
func seqOrdered[T any](seq iter.Seq[T]) (line string) {
	parts := make([]string, 0, 4)
	for v := range seq {
		parts = append(parts, fmt.Sprintf("%v", v))
	}
	return fmt.Sprintf("%v", parts)
}

// seqMultiset renders an `m` column: sorted, duplicates kept. The form sorts a
// set's elements bytewise and keeps its length (ADR-0210 SD3), so what survives
// the round trip is the multiset, not the stored order — and not the pairing
// with the co-container `h` column beside it either.
func seqMultiset[T any](seq iter.Seq[T]) (line string) {
	parts := make([]string, 0, 4)
	for v := range seq {
		parts = append(parts, fmt.Sprintf("%v", v))
	}
	slices.Sort(parts)
	return fmt.Sprintf("%v", parts)
}

// transfer drains a builder into one record batch and loads it into ra.
func transfer(t *testing.T, records func([]arrow.RecordBatch) ([]arrow.RecordBatch, error), load func(rartime.RecordI) error) {
	t.Helper()
	recs, err := records(nil)
	require.NoError(t, err)
	require.Len(t, recs, 1)
	require.NoError(t, load(recs[0]))
}

// ---------------------------------------------------------------- test_table

// testTableLines renders one batch of the sample table: the plain values, then
// the attributes of each section as sorted lines.
func testTableLines(ra *ReadAccessTestTable) (perEntity []string) {
	n := ra.GetNumberOfEntities()
	perEntity = make([]string, 0, n)
	for e := range n {
		idx := rartime.EntityIdx(e)
		var sb bytes.Buffer
		fmt.Fprintf(&sb, "id=%d ts=%s proc=%s\n", ra.EntityId.GetAttrValueId(idx),
			ra.EntityTimestamp.GetAttrValueTs(idx).UTC().Format(time.RFC3339Nano),
			seqOrdered(ra.EntityTimestamp.GetAttrValueProc(idx)))
		geo := make([]string, 0, 4)
		for a := range int(ra.Geo.Attributes.GetNumberOfAttributes(idx)) {
			ai := rartime.AttributeIdx(a)
			geo = append(geo, fmt.Sprintf("lat=%v lng=%v h1=%d h2=%d lr=%v lmv=%v",
				ra.Geo.Attributes.GetAttrValueLat(idx, ai), ra.Geo.Attributes.GetAttrValueLng(idx, ai),
				ra.Geo.Attributes.GetAttrValueH3Res1(idx, ai), ra.Geo.Attributes.GetAttrValueH3Res2(idx, ai),
				seqLines(ra.Geo.Memberships.GetMembValueLowCardRef(idx, ai)),
				seq2Lines(ra.Geo.Memberships.GetMembValueLowCardVerbatimHighCardParams(idx, ai))))
		}
		slices.Sort(geo)
		text := make([]string, 0, 4)
		for a := range int(ra.Text.Attributes.GetNumberOfAttributes(idx)) {
			ai := rartime.AttributeIdx(a)
			text = append(text, fmt.Sprintf("text=%q words=%s wl=%s lr=%v lmv=%v",
				ra.Text.Attributes.GetAttrValueText(idx, ai),
				seqOrdered(ra.Text.Attributes.GetAttrValueWords(idx, ai)),
				seqOrdered(ra.Text.Attributes.GetAttrValueWordLength(idx, ai)),
				seqLines(ra.Text.Memberships.GetMembValueLowCardRef(idx, ai)),
				seq2Lines(ra.Text.Memberships.GetMembValueLowCardVerbatimHighCardParams(idx, ai))))
		}
		slices.Sort(text)
		fmt.Fprintf(&sb, "geo=%v\ntext=%v", geo, text)
		perEntity = append(perEntity, sb.String())
	}
	return
}

// writeTestTable drives the sample table's dml: both membership channels of
// both sections, co-containers of length 0, 1 and 3, and an entity with no
// attribute at all in one of the sections — the slot the encoder then omits.
func writeTestTable(t *testing.T, dml *InEntityTestTable) {
	t.Helper()
	base := time.UnixMilli(1_700_000_000_123).UTC()
	secGeo := dml.GetSectionGeo()
	secText := dml.GetSectionText()
	for i := range roundTripEntities {
		procs := []time.Time{base, base.Add(time.Second), base.Add(2 * time.Second)}
		ent := dml.BeginEntity()
		ent.SetId(uint64(i)*7 + 1)
		ent.SetTimestamp(base.Add(time.Duration(i)*time.Millisecond), procs[:i%4])

		// Three co-container elements, both channels, two refs (aliasing).
		secText.BeginAttribute(fmt.Sprintf("three %d", i)).
			AddToCoContainers(5, "alpha").
			AddToCoContainers(4, "beta").
			AddToCoContainers(5, "gamma").
			AddMembershipLowCardRef(uint64(i)*10+1).
			AddMembershipLowCardRef(uint64(i)*10+2).
			AddMembershipMixedLowCardVerbatim([]byte("kind"), []byte("params")).
			EndAttribute()
		// Empty containers, one carrier membership with empty params.
		secText.BeginAttribute(fmt.Sprintf("empty %d", i)).
			AddMembershipMixedLowCardVerbatim([]byte("kind"), nil).
			EndAttribute()
		if i%2 == 0 {
			// One element, one ref.
			secText.BeginAttribute(fmt.Sprintf("one %d", i)).
				AddToCoContainers(3, "one").
				AddMembershipLowCardRef(uint64(i)*10 + 3).
				EndAttribute()
		}
		if i != 1 {
			// Entity 1 carries no geo attribute at all, so the geo slot is
			// omitted from its entity item.
			secGeo.BeginAttribute(float32(i)+0.5, -float32(i)-0.25, 0x45494+uint64(i), 0x45454543).
				AddMembershipLowCardRef(uint64(i) + 100).
				EndAttribute()
			secGeo.BeginAttribute(-0.0, 12.5, 1, 2).
				AddMembershipMixedLowCardVerbatim([]byte("geo-kind"), []byte("geo-params")).
				EndAttribute()
		}
		require.NoError(t, ent.CheckErrors())
		require.NoError(t, ent.CommitEntity())
	}
}

func TestRoundTripTestTable(t *testing.T) {
	src := NewInEntityTestTable(memory.DefaultAllocator, 128)
	writeTestTable(t, src)

	raFirst := NewReadAccessTestTable()
	transfer(t, src.TransferRecords, raFirst.LoadFromRecord)
	require.Equal(t, roundTripEntities, raFirst.GetNumberOfEntities())

	enc, err := NewCanonWireEncoderTestTable(raFirst, nil)
	require.NoError(t, err)
	var first bytes.Buffer
	require.NoError(t, enc.EncodeAll(&first))
	n, err := cwruntime.VerifyCanonicalSequence(first.Bytes())
	require.NoError(t, err)
	require.Equal(t, roundTripEntities, n)

	dst := NewInEntityTestTable(memory.DefaultAllocator, 128)
	dec, err := NewCanonWireDecoderTestTable(dst, nil)
	require.NoError(t, err)
	decoded, err := dec.DecodeAll(first.Bytes())
	require.NoError(t, err)
	require.Equal(t, roundTripEntities, decoded)

	raSecond := NewReadAccessTestTable()
	transfer(t, dst.TransferRecords, raSecond.LoadFromRecord)
	require.Equal(t, raFirst.GetNumberOfEntities(), raSecond.GetNumberOfEntities())

	enc2, err := NewCanonWireEncoderTestTable(raSecond, nil)
	require.NoError(t, err)
	var second bytes.Buffer
	require.NoError(t, enc2.EncodeAll(&second))
	require.Equal(t, first.Bytes(), second.Bytes())
	n, err = cwruntime.VerifyCanonicalSequence(second.Bytes())
	require.NoError(t, err)
	require.Equal(t, roundTripEntities, n)

	require.Equal(t, testTableLines(raFirst), testTableLines(raSecond))
}

// ----------------------------------------------------------------- net_table

func netTableLines(ra *ReadAccessNetTable) (perEntity []string) {
	n := ra.GetNumberOfEntities()
	perEntity = make([]string, 0, n)
	for e := range n {
		idx := rartime.EntityIdx(e)
		var sb bytes.Buffer
		fmt.Fprintf(&sb, "id=%d ts=%s\n", ra.EntityId.GetAttrValueId(idx),
			ra.EntityTimestamp.GetAttrValueTs(idx).UTC().Format(time.RFC3339Nano))
		net := make([]string, 0, 4)
		for a := range int(ra.Net.Attributes.GetNumberOfAttributes(idx)) {
			ai := rartime.AttributeIdx(a)
			net = append(net, fmt.Sprintf("v=%08x w=%x vc=%x wc=%x lr=%v lmv=%v",
				ra.Net.Attributes.GetAttrValueIpv4(idx, ai), ra.Net.Attributes.GetAttrValueIpv6(idx, ai),
				ra.Net.Attributes.GetAttrValueIpv4Cidr(idx, ai), ra.Net.Attributes.GetAttrValueIpv6Cidr(idx, ai),
				seqLines(ra.Net.Memberships.GetMembValueLowCardRef(idx, ai)),
				seq2Lines(ra.Net.Memberships.GetMembValueLowCardVerbatimHighCardParams(idx, ai))))
		}
		slices.Sort(net)
		fmt.Fprintf(&sb, "net=%v", net)
		perEntity = append(perEntity, sb.String())
	}
	return
}

// writeNetTable drives the four network lanes. The prefixes are written
// **masked**: ADR-0210 SD3 keeps a prefix's host bits out of the wire (RFC
// 9164), so an unmasked prefix would come back masked and the two batches
// would differ where the bytes do not.
func writeNetTable(t *testing.T, dml *InEntityNetTable) {
	t.Helper()
	base := time.UnixMilli(1_700_000_000_500).UTC()
	sec := dml.GetSectionNet()
	v6 := [16]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	v6mapped := [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 10, 0, 0, 1}
	v6pfx := [17]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 32}
	zeroPfx := [17]byte{}
	for i := range roundTripEntities {
		ent := dml.BeginEntity()
		ent.SetId(uint64(i) + 1)
		ent.SetTimestamp(base.Add(time.Duration(i) * time.Second))
		sec.BeginAttribute(0x0a000001+uint32(i), v6, [5]byte{10, 0, 0, 0, 8}, v6pfx).
			AddMembershipLowCardRef(uint64(i)+1).
			AddMembershipMixedLowCardVerbatim([]byte("net-kind"), []byte("net-params")).
			EndAttribute()
		// An IPv4-mapped IPv6 stays IPv6 on the wire (no reduction), and a
		// zero-length prefix is the degenerate RFC 9164 encoding.
		sec.BeginAttribute(0, v6mapped, [5]byte{192, 168, 1, 0, 24}, zeroPfx).
			AddMembershipMixedLowCardVerbatim([]byte("mapped"), nil).
			EndAttribute()
		require.NoError(t, ent.CheckErrors())
		require.NoError(t, ent.CommitEntity())
	}
}

func TestRoundTripNetTable(t *testing.T) {
	src := NewInEntityNetTable(memory.DefaultAllocator, 128)
	writeNetTable(t, src)

	raFirst := NewReadAccessNetTable()
	transfer(t, src.TransferRecords, raFirst.LoadFromRecord)

	enc, err := NewCanonWireEncoderNetTable(raFirst, nil)
	require.NoError(t, err)
	var first bytes.Buffer
	require.NoError(t, enc.EncodeAll(&first))
	n, err := cwruntime.VerifyCanonicalSequence(first.Bytes())
	require.NoError(t, err)
	require.Equal(t, roundTripEntities, n)

	dst := NewInEntityNetTable(memory.DefaultAllocator, 128)
	dec, err := NewCanonWireDecoderNetTable(dst, nil)
	require.NoError(t, err)
	decoded, err := dec.DecodeAll(first.Bytes())
	require.NoError(t, err)
	require.Equal(t, roundTripEntities, decoded)

	raSecond := NewReadAccessNetTable()
	transfer(t, dst.TransferRecords, raSecond.LoadFromRecord)
	require.Equal(t, raFirst.GetNumberOfEntities(), raSecond.GetNumberOfEntities())

	enc2, err := NewCanonWireEncoderNetTable(raSecond, nil)
	require.NoError(t, err)
	var second bytes.Buffer
	require.NoError(t, enc2.EncodeAll(&second))
	require.Equal(t, first.Bytes(), second.Bytes())
	require.Equal(t, netTableLines(raFirst), netTableLines(raSecond))
}

// --------------------------------------------------------------------- place

func placeLines(ra *ReadAccessPlace) (perEntity []string) {
	n := ra.GetNumberOfEntities()
	perEntity = make([]string, 0, n)
	for e := range n {
		idx := rartime.EntityIdx(e)
		var sb bytes.Buffer
		fmt.Fprintf(&sb, "id=%d\n", ra.EntityId.GetAttrValueId(idx))
		// The co-section group is one wire slot, so its two members are
		// rendered as one line per attribute index — the pairing is what the
		// slot preserves and what would break if the decoder split them.
		place := make([]string, 0, 4)
		for a := range int(ra.Geo.Attributes.GetNumberOfAttributes(idx)) {
			ai := rartime.AttributeIdx(a)
			place = append(place, fmt.Sprintf("lat=%v lng=%v cell=%d geoLr=%v h3Lr=%v",
				ra.Geo.Attributes.GetAttrValueLat(idx, ai), ra.Geo.Attributes.GetAttrValueLng(idx, ai),
				ra.H3.Attributes.GetAttrValueCell(idx, ai),
				seqLines(ra.Geo.Memberships.GetMembValueLowCardRef(idx, ai)),
				seqLines(ra.H3.Memberships.GetMembValueLowCardRef(idx, ai))))
		}
		slices.Sort(place)
		tags := make([]string, 0, 4)
		for a := range int(ra.Tags.Attributes.GetNumberOfAttributes(idx)) {
			ai := rartime.AttributeIdx(a)
			tags = append(tags, fmt.Sprintf("tag=%s tagId=%s lv=%v",
				seqOrdered(ra.Tags.Attributes.GetAttrValueTag(idx, ai)),
				seqMultiset(ra.Tags.Attributes.GetAttrValueTagId(idx, ai)),
				seqLines(ra.Tags.Memberships.GetMembValueLowCardVerbatim(idx, ai))))
		}
		slices.Sort(tags)
		fmt.Fprintf(&sb, "place=%v\ntags=%v\nh3n=%d", place, tags, ra.H3.Attributes.GetNumberOfAttributes(idx))
		perEntity = append(perEntity, sb.String())
	}
	return
}

// writePlace drives the co-section table: geo and h3 in step, so their
// attribute counts agree, plus a standalone section whose two co-containers —
// one `h`, one `m` — are written at length 0, 1 and 3, the set carrying a
// duplicate at length 3 so the form's "sorted, duplicates kept" rule is what
// keeps the two columns the same length.
func writePlace(t *testing.T, dml *InEntityPlace) {
	t.Helper()
	secGeo := dml.GetSectionGeo()
	secH3 := dml.GetSectionH3()
	secTags := dml.GetSectionTags()
	for i := range roundTripEntities {
		ent := dml.BeginEntity()
		ent.SetId(uint64(i) * 3)
		secGeo.BeginAttribute(float32(i)+0.5, float32(i)-0.25).
			AddMembershipLowCardRef(uint64(i)).
			EndAttribute()
		secH3.BeginAttribute(uint64(0x8928308280fffff) + uint64(i)).
			AddMembershipLowCardRef(uint64(i) + 100).
			EndAttribute()
		if i%2 == 0 {
			// A second attribute of the co-section group, and a member with no
			// membership at all on one side of it.
			secGeo.BeginAttribute(-float32(i), float32(i)*2).
				AddMembershipLowCardRef(uint64(i) + 7).
				EndAttribute()
			secH3.BeginAttribute(uint64(0x8928308280bffff) + uint64(i)).
				EndAttribute()
		}
		switch i % 3 {
		case 0:
			// The set carries 7 twice: a deduplicating set rule would write
			// two elements where the `h` column beside it has three, and the
			// decoder could not rebuild the attribute at all.
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
}

func TestRoundTripPlace(t *testing.T) {
	src := NewInEntityPlace(memory.DefaultAllocator, 128)
	writePlace(t, src)

	raFirst := NewReadAccessPlace()
	transfer(t, src.TransferRecords, raFirst.LoadFromRecord)

	enc, err := NewCanonWireEncoderPlace(raFirst, nil)
	require.NoError(t, err)
	var first bytes.Buffer
	require.NoError(t, enc.EncodeAll(&first))
	n, err := cwruntime.VerifyCanonicalSequence(first.Bytes())
	require.NoError(t, err)
	require.Equal(t, roundTripEntities, n)

	dst := NewInEntityPlace(memory.DefaultAllocator, 128)
	dec, err := NewCanonWireDecoderPlace(dst, nil)
	require.NoError(t, err)
	decoded, err := dec.DecodeAll(first.Bytes())
	require.NoError(t, err)
	require.Equal(t, roundTripEntities, decoded)

	raSecond := NewReadAccessPlace()
	transfer(t, dst.TransferRecords, raSecond.LoadFromRecord)
	require.Equal(t, raFirst.GetNumberOfEntities(), raSecond.GetNumberOfEntities())

	enc2, err := NewCanonWireEncoderPlace(raSecond, nil)
	require.NoError(t, err)
	var second bytes.Buffer
	require.NoError(t, enc2.EncodeAll(&second))
	require.Equal(t, first.Bytes(), second.Bytes())
	require.Equal(t, placeLines(raFirst), placeLines(raSecond))
}

// ---------------------------------------------------------------------- json

func jsonLines(ra *ReadAccessJson) (perEntity []string) {
	n := ra.GetNumberOfEntities()
	perEntity = make([]string, 0, n)
	valueless := func(p *MembershipPackJsonShared1, idx rartime.EntityIdx) []string {
		lines := make([]string, 0, 4)
		for a := range int(p.AccelMixedLowCardVerbatim.GetEntityAttributeCount(int(idx))) {
			lines = append(lines, fmt.Sprintf("lmv=%v",
				seq2Lines(p.GetMembValueLowCardVerbatimHighCardParams(idx, rartime.AttributeIdx(a)))))
		}
		slices.Sort(lines)
		return lines
	}
	for e := range n {
		idx := rartime.EntityIdx(e)
		var sb bytes.Buffer
		fmt.Fprintf(&sb, "id=%x\n", ra.EntityId.GetAttrValueBlake3hash(idx))
		str := make([]string, 0, 4)
		for a := range int(ra.String.Attributes.GetNumberOfAttributes(idx)) {
			ai := rartime.AttributeIdx(a)
			str = append(str, fmt.Sprintf("v=%q lmv=%v", ra.String.Attributes.GetAttrValueValue(idx, ai),
				seq2Lines(ra.String.Memberships.GetMembValueLowCardVerbatimHighCardParams(idx, ai))))
		}
		slices.Sort(str)
		sym := make([]string, 0, 4)
		for a := range int(ra.Symbol.Attributes.GetNumberOfAttributes(idx)) {
			ai := rartime.AttributeIdx(a)
			sym = append(sym, fmt.Sprintf("v=%q lmv=%v", ra.Symbol.Attributes.GetAttrValueValue(idx, ai),
				seq2Lines(ra.Symbol.Memberships.GetMembValueLowCardVerbatimHighCardParams(idx, ai))))
		}
		slices.Sort(sym)
		bl := make([]string, 0, 4)
		for a := range int(ra.Bool.Attributes.GetNumberOfAttributes(idx)) {
			ai := rartime.AttributeIdx(a)
			bl = append(bl, fmt.Sprintf("v=%v lmv=%v", ra.Bool.Attributes.GetAttrValueValue(idx, ai),
				seq2Lines(ra.Bool.Memberships.GetMembValueLowCardVerbatimHighCardParams(idx, ai))))
		}
		slices.Sort(bl)
		fmt.Fprintf(&sb, "string=%v\nsymbol=%v\nbool=%v\nnull=%v\nundefined=%v\nemptyObject=%v\nemptyArray=%v",
			str, sym, bl,
			valueless(ra.Null.Memberships, idx), valueless(ra.Undefined.Memberships, idx),
			valueless(ra.EmptyObject.Memberships, idx), valueless(ra.EmptyArray.Memberships, idx))
		perEntity = append(perEntity, sb.String())
	}
	return
}

// writeJson populates both ambiguity sets of the JSON mapping — the four
// value-less sections that share the empty signature and the two that share
// `s` — plus one section outside them, so the decoder has to take the
// dispatched path and the direct one in the same entity.
func writeJson(t *testing.T, dml *InEntityJson) {
	t.Helper()
	secNull := dml.GetSectionNull()
	secUndefined := dml.GetSectionUndefined()
	secEmptyObject := dml.GetSectionEmptyObject()
	secEmptyArray := dml.GetSectionEmptyArray()
	secString := dml.GetSectionString()
	secSymbol := dml.GetSectionSymbol()
	secBool := dml.GetSectionBool()
	for i := range roundTripEntities {
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
			// A second attribute in each half of the `s` ambiguity set, so the
			// dispatch is exercised with more than one attribute per slot.
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
}

func TestRoundTripJson(t *testing.T) {
	src := NewInEntityJson(memory.DefaultAllocator, 128)
	writeJson(t, src)

	raFirst := NewReadAccessJson()
	transfer(t, src.TransferRecords, raFirst.LoadFromRecord)

	// The ordinal pair is the only one that can round-trip null vs undefined:
	// they have the same signature *and* the same memberships, so nothing on
	// the wire but the discriminator tells them apart (ADR-0210 SD5).
	enc, err := NewCanonWireEncoderJson(raFirst, CanonWireOrdinalTaggerJson{})
	require.NoError(t, err)
	var first bytes.Buffer
	require.NoError(t, enc.EncodeAll(&first))
	n, err := cwruntime.VerifyCanonicalSequence(first.Bytes())
	require.NoError(t, err)
	require.Equal(t, roundTripEntities, n)

	dst := NewInEntityJson(memory.DefaultAllocator, 128)
	dec, err := NewCanonWireDecoderJson(dst, CanonWireOrdinalDispatcherJson{})
	require.NoError(t, err)
	decoded, err := dec.DecodeAll(first.Bytes())
	require.NoError(t, err)
	require.Equal(t, roundTripEntities, decoded)

	raSecond := NewReadAccessJson()
	transfer(t, dst.TransferRecords, raSecond.LoadFromRecord)
	require.Equal(t, raFirst.GetNumberOfEntities(), raSecond.GetNumberOfEntities())

	enc2, err := NewCanonWireEncoderJson(raSecond, CanonWireOrdinalTaggerJson{})
	require.NoError(t, err)
	var second bytes.Buffer
	require.NoError(t, enc2.EncodeAll(&second))
	require.Equal(t, first.Bytes(), second.Bytes())
	require.Equal(t, jsonLines(raFirst), jsonLines(raSecond))
}
