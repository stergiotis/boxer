package example

import (
	"bytes"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	cwruntime "github.com/stergiotis/boxer/public/semistructured/leeway/canonwire/runtime"
	rartime "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"
)

// The cross-table suite of ADR-0207: the requirement the whole form exists for.
// test_table_renamed declares the same canonical types as test_table in the
// same co-topology and under the same membership specs, and nothing else in
// common — every section and column is renamed, the sections are declared in
// the opposite order, the columns are permuted inside each section, the two
// plain timestamp columns are swapped, and the encoding hints, value aspects
// and section use-aspects all differ. None of that is on the wire (SD2), so an
// entity written through one decodes into the other.
//
// What the suite proves:
//
//   - the slot key carries no name, no declaration order, no aspect and no
//     hint: bytes₁ from the source table and bytes₂ re-encoded from the target
//     are identical, so the decode lost nothing and added nothing;
//   - the generated decoder bakes the permutation from the wire's key order
//     into the target DML's argument order, which differs between the two
//     tables for both the scalars (coords takes coarse, north, fine, east) and
//     the containers (phrases takes tokens, token_lengths where text takes
//     word_length, words);
//   - a section whose declared MembershipSpecE cannot store the carriage the
//     wire carries is refused, not silently narrowed (TestCrossTableNarrowed).
//
// What it does *not* prove. Co-section topology is part of the key (SD2), so
// a co-grouped source does not decode into a target that declares the same
// sections standalone — the signatures differ and the decode fails with
// ErrUnknownSlot; the renamed table therefore keeps test_table's topology, and
// the limit is stated in the ADR rather than tested away. Section names being
// off the wire also means two sections of one signature are told apart only by
// the SD5 dispatch, which the json table's tests cover and this one avoids by
// construction: test_table has no ambiguous signature.

// renamedLines renders a batch of test_table_renamed in exactly the format
// testTableLines renders a batch of test_table, so the two are comparable. The
// column mapping is spelled out here and nowhere else — it follows from the
// key order (the canonical types stable-sorted), not from the names.
func renamedLines(ra *ReadAccessTestTableRenamed) (perEntity []string) {
	n := ra.GetNumberOfEntities()
	perEntity = make([]string, 0, n)
	for e := range n {
		idx := rartime.EntityIdx(e)
		var sb bytes.Buffer
		fmt.Fprintf(&sb, "id=%d ts=%s proc=%s\n", ra.EntityId.GetAttrValueIdent(idx),
			ra.EntityTimestamp.GetAttrValueSeenAt(idx).UTC().Format(time.RFC3339Nano),
			seqOrdered(ra.EntityTimestamp.GetAttrValueStampedAt(idx)))
		geo := make([]string, 0, 4)
		for a := range int(ra.Coords.Attributes.GetNumberOfAttributes(idx)) {
			ai := rartime.AttributeIdx(a)
			geo = append(geo, fmt.Sprintf("lat=%v lng=%v h1=%d h2=%d lr=%v lmv=%v",
				ra.Coords.Attributes.GetAttrValueNorth(idx, ai), ra.Coords.Attributes.GetAttrValueEast(idx, ai),
				ra.Coords.Attributes.GetAttrValueCoarse(idx, ai), ra.Coords.Attributes.GetAttrValueFine(idx, ai),
				seqLines(ra.Coords.Memberships.GetMembValueLowCardRef(idx, ai)),
				seq2Lines(ra.Coords.Memberships.GetMembValueLowCardVerbatimHighCardParams(idx, ai))))
		}
		slices.Sort(geo)
		text := make([]string, 0, 4)
		for a := range int(ra.Phrases.Attributes.GetNumberOfAttributes(idx)) {
			ai := rartime.AttributeIdx(a)
			text = append(text, fmt.Sprintf("text=%q words=%s wl=%s lr=%v lmv=%v",
				ra.Phrases.Attributes.GetAttrValuePhrase(idx, ai),
				seqOrdered(ra.Phrases.Attributes.GetAttrValueTokens(idx, ai)),
				seqOrdered(ra.Phrases.Attributes.GetAttrValueTokenLengths(idx, ai)),
				seqLines(ra.Phrases.Memberships.GetMembValueLowCardRef(idx, ai)),
				seq2Lines(ra.Phrases.Memberships.GetMembValueLowCardVerbatimHighCardParams(idx, ai))))
		}
		slices.Sort(text)
		fmt.Fprintf(&sb, "geo=%v\ntext=%v", geo, text)
		perEntity = append(perEntity, sb.String())
	}
	return
}

// writeRenamed drives the renamed table's dml with content of its own, so the
// reverse direction starts from a batch this package built rather than from
// one the forward direction decoded. The argument orders are the renamed
// table's: coords takes its two u64 columns interleaved with its two f32 ones,
// and phrases takes its containers the other way round from text.
func writeRenamed(t *testing.T, dml *InEntityTestTableRenamed) {
	t.Helper()
	base := time.UnixMilli(1_700_000_555_001).UTC()
	secCoords := dml.GetSectionCoords()
	secPhrases := dml.GetSectionPhrases()
	for i := range roundTripEntities {
		stamps := []time.Time{base, base.Add(3 * time.Second)}
		ent := dml.BeginEntity()
		ent.SetId(uint64(i)*11 + 5)
		ent.SetTimestamp(base.Add(time.Duration(i)*time.Millisecond), stamps[:i%3])

		secPhrases.BeginAttribute(fmt.Sprintf("phrase %d", i)).
			AddToCoContainers("alpha", 5).
			AddToCoContainers("beta", 4).
			AddMembershipLowCardRef(uint64(i)*10+1).
			AddMembershipMixedLowCardVerbatim([]byte("kind"), []byte("params")).
			EndAttribute()
		secPhrases.BeginAttribute("").
			AddMembershipLowCardRef(uint64(i) * 10).
			EndAttribute()
		if i != 2 {
			secCoords.BeginAttribute(0x45494+uint64(i), float32(i)+0.5, 0x45454543, -float32(i)-0.25).
				AddMembershipLowCardRef(uint64(i) + 100).
				EndAttribute()
		}
		require.NoError(t, ent.CheckErrors())
		require.NoError(t, ent.CommitEntity())
	}
}

// encodeTestTable drains a test_table builder into a batch and encodes it.
func encodeTestTable(t *testing.T, dml *InEntityTestTable) (ra *ReadAccessTestTable, raw []byte) {
	t.Helper()
	ra = NewReadAccessTestTable()
	transfer(t, dml.TransferRecords, ra.LoadFromRecord)
	enc, err := NewCanonWireEncoderTestTable(ra, nil)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, enc.EncodeAll(&buf))
	_, err = cwruntime.VerifyCanonicalSequence(buf.Bytes())
	require.NoError(t, err)
	return ra, buf.Bytes()
}

// encodeRenamed is encodeTestTable for the renamed table.
func encodeRenamed(t *testing.T, dml *InEntityTestTableRenamed) (ra *ReadAccessTestTableRenamed, raw []byte) {
	t.Helper()
	ra = NewReadAccessTestTableRenamed()
	transfer(t, dml.TransferRecords, ra.LoadFromRecord)
	enc, err := NewCanonWireEncoderTestTableRenamed(ra, nil)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, enc.EncodeAll(&buf))
	_, err = cwruntime.VerifyCanonicalSequence(buf.Bytes())
	require.NoError(t, err)
	return ra, buf.Bytes()
}

// TestCrossTableTestTableToRenamed is the forward direction: bytes written from
// test_table decode into test_table_renamed, and re-encoding that batch yields
// the same bytes.
func TestCrossTableTestTableToRenamed(t *testing.T) {
	src := NewInEntityTestTable(memory.DefaultAllocator, 128)
	writeTestTable(t, src)
	raSrc, first := encodeTestTable(t, src)

	dst := NewInEntityTestTableRenamed(memory.DefaultAllocator, 128)
	dec, err := NewCanonWireDecoderTestTableRenamed(dst, nil)
	require.NoError(t, err)
	decoded, err := dec.DecodeAll(first)
	require.NoError(t, err)
	require.Equal(t, roundTripEntities, decoded)

	raDst, second := encodeRenamed(t, dst)
	require.Equal(t, first, second)
	require.Equal(t, testTableLines(raSrc), renamedLines(raDst))
}

// TestCrossTableRenamedToTestTable is the same claim the other way round, from
// a batch written through the renamed table's own dml — whose argument orders
// differ from test_table's for both the scalars and the containers.
func TestCrossTableRenamedToTestTable(t *testing.T) {
	src := NewInEntityTestTableRenamed(memory.DefaultAllocator, 128)
	writeRenamed(t, src)
	raSrc, first := encodeRenamed(t, src)

	dst := NewInEntityTestTable(memory.DefaultAllocator, 128)
	dec, err := NewCanonWireDecoderTestTable(dst, nil)
	require.NoError(t, err)
	decoded, err := dec.DecodeAll(first)
	require.NoError(t, err)
	require.Equal(t, roundTripEntities, decoded)

	raDst, second := encodeTestTable(t, dst)
	require.Equal(t, first, second)
	require.Equal(t, renamedLines(raSrc), testTableLines(raDst))
}

// TestCrossTableNarrowed is the negative cross-table case: test_table_narrow
// declares the same signatures but its `text` section accepts LowCardRef only.
// The slot key still matches — the types did not change — so the refusal comes
// from the narrowing step of ADR-0207 SD5 and nowhere else, and it is the
// carriage that the target cannot store.
func TestCrossTableNarrowed(t *testing.T) {
	src := NewInEntityTestTable(memory.DefaultAllocator, 128)
	writeTestTable(t, src)
	_, first := encodeTestTable(t, src)

	dst := NewInEntityTestTableNarrow(memory.DefaultAllocator, 128)
	dec, err := NewCanonWireDecoderTestTableNarrow(dst, nil)
	require.NoError(t, err)
	_, err = dec.DecodeAll(first)
	require.ErrorIs(t, err, cwruntime.ErrChannelNotAccepted)

	// The same table takes an entity whose text attributes carry only the
	// channel it does declare, so the refusal is about the carriage and not
	// about the table being unreachable in general.
	plain := NewInEntityTestTable(memory.DefaultAllocator, 128)
	{
		secText := plain.GetSectionText()
		ent := plain.BeginEntity()
		ent.SetId(1)
		ent.SetTimestamp(time.UnixMilli(1_700_000_000_000).UTC(), nil)
		secText.BeginAttribute("only a ref").
			AddToCoContainers(3, "one").
			AddMembershipLowCardRef(9).
			EndAttribute()
		require.NoError(t, ent.CheckErrors())
		require.NoError(t, ent.CommitEntity())
	}
	_, acceptable := encodeTestTable(t, plain)
	n, err := dec.DecodeAll(acceptable)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}
