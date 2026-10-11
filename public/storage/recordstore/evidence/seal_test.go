package evidence_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// A seal's item is order-free over its rows, and its digest chains from
// the seal before it.
func TestSealItemAndDigest(t *testing.T) {
	scheme := evidence.NewScheme("seal-test/v1")
	rows := []evidence.SealedRow{{Id: 9, Digest: "b"}, {Id: 3, Digest: "a"}, {Id: 9, Digest: "b"}}
	assert.Equal(t, "3 a\n9 b\n", string(evidence.SealItem(rows)))
	assert.Equal(t, evidence.SealItem(rows), evidence.SealItem(rows[1:]))
	first := scheme.SealDigest("", rows)
	assert.NotEqual(t, first, scheme.SealDigest(first, rows), "the predecessor is in the digest")
	assert.Equal(t, scheme.ChainDigest("", evidence.SealItem(rows)), first)
}

// Law L34 over a stream: it holds when every seal chains and every row is
// named once; the verifier names the first seal that breaks it and why,
// and the rows no seal names.
func TestVerifySeals(t *testing.T) {
	scheme := evidence.NewScheme("seal-test/v1")
	present := map[uint64]string{1: "d1", 2: "d2", 4: "d4", 5: "d5"}
	seal := func(id, prev uint64, prevDigest string, rows ...evidence.SealedRow) evidence.Seal {
		return evidence.Seal{Id: id, Prev: prev, Digest: scheme.SealDigest(prevDigest, rows), Rows: rows}
	}
	s1 := seal(3, 0, "", evidence.SealedRow{Id: 1, Digest: "d1"}, evidence.SealedRow{Id: 2, Digest: "d2"})
	s2 := seal(6, 3, s1.Digest, evidence.SealedRow{Id: 4, Digest: "d4"}, evidence.SealedRow{Id: 5, Digest: "d5"})
	broken, unnamed := evidence.VerifySeals(scheme, "", []evidence.Seal{s2, s1}, present)
	assert.Equal(t, evidence.SealBreak{}, broken, "the seals are walked by Prev, in whatever order they come")
	assert.Empty(t, unnamed)

	// A row that landed without a seal is a gap, not a break.
	present[7] = "d7"
	broken, unnamed = evidence.VerifySeals(scheme, "", []evidence.Seal{s1, s2}, present)
	assert.Equal(t, evidence.SealBreak{}, broken)
	assert.Equal(t, []uint64{7}, unnamed)
	delete(present, 7)

	cases := map[string]struct {
		seals   []evidence.Seal
		present map[uint64]string
		want    evidence.SealBreak
	}{
		"fork":   {[]evidence.Seal{s1, s2, seal(8, 3, s1.Digest)}, present, evidence.SealBreak{Seal: 8, Why: evidence.SealBreakOrder}},
		"orphan": {[]evidence.Seal{s2, s1, seal(9, 4, s1.Digest)}, present, evidence.SealBreak{Seal: 9, Why: evidence.SealBreakPrev}},
		"cycle":  {[]evidence.Seal{s1, seal(6, 6, s1.Digest)}, present, evidence.SealBreak{Seal: 6, Why: evidence.SealBreakPrev}},
		"digest": {[]evidence.Seal{s1, seal(6, 3, "other")}, present, evidence.SealBreak{Seal: 6, Why: evidence.SealBreakDigest}},
		"row missing": {[]evidence.Seal{s1, s2}, map[uint64]string{1: "d1", 2: "d2", 4: "d4"},
			evidence.SealBreak{Seal: 6, Why: evidence.SealBreakRowMissing, Row: 5}},
		"row digest": {[]evidence.Seal{s1, s2}, map[uint64]string{1: "d1", 2: "x", 4: "d4", 5: "d5"},
			evidence.SealBreak{Seal: 3, Why: evidence.SealBreakRowDigest, Row: 2}},
		"row twice": {[]evidence.Seal{s1, seal(6, 3, s1.Digest, evidence.SealedRow{Id: 2, Digest: "d2"})}, present,
			evidence.SealBreak{Seal: 6, Why: evidence.SealBreakRowTwice, Row: 2}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			broken, _ := evidence.VerifySeals(scheme, "", c.seals, c.present)
			require.Equal(t, c.want, broken)
			assert.NotEqual(t, "none", broken.Why.String())
		})
	}
	ordered, ob := evidence.OrderSeals([]evidence.Seal{s2, s1})
	require.Equal(t, evidence.SealBreak{}, ob)
	assert.Equal(t, []uint64{3, 6}, []uint64{ordered[0].Id, ordered[1].Id})
	assert.Equal(t, "row-twice", evidence.SealBreakRowTwice.String())
	assert.Equal(t, "unknown", evidence.SealBreakE(99).String())
}

// Law L32 for a sealed store: an anchored ordinal names the n-th seal and
// must hold its digest; the earliest receipt counts; an ordinal past the
// stream breaks.
func TestVerifySealAnchors(t *testing.T) {
	ordered := []evidence.Seal{{Id: 3, Digest: "s1"}, {Id: 6, Prev: 3, Digest: "s2"}}
	at := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	anchored := []evidence.AnchoredHead{
		{Ordinal: 1, Digest: "s1", ReceiptAt: at, ReceiptId: "a"},
		{Ordinal: 2, Digest: "s2", ReceiptAt: at.Add(time.Second), ReceiptId: "b"},
	}
	assert.Empty(t, evidence.VerifySealAnchors(ordered, anchored))
	late := append([]evidence.AnchoredHead(nil), anchored...)
	late = append(late, evidence.AnchoredHead{Ordinal: 2, Digest: "forged", ReceiptAt: at.Add(time.Hour), ReceiptId: "c"})
	assert.Equal(t, []uint64{2}, evidence.VerifySealAnchors(ordered, late), "a later conflicting receipt breaks the ordinal")
	beyond := append([]evidence.AnchoredHead(nil), anchored...)
	beyond = append(beyond, evidence.AnchoredHead{Ordinal: 3, Digest: "s3", ReceiptAt: at, ReceiptId: "d"})
	assert.Equal(t, []uint64{3}, evidence.VerifySealAnchors(ordered, beyond), "an anchored seal the stream no longer has")
	assert.Equal(t, []uint64{1}, evidence.VerifySealAnchors(ordered[1:], anchored[:1]), "the stream reordered under an anchor")
}
