package example

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

var ledgerScheme = evidence.NewScheme("example-ledger/v1")

func newLedger(t *testing.T) *LedgerStore {
	t.Helper()
	st := NewLedgerStore(nil, nil, LedgerStoreConfig{Scheme: ledgerScheme})
	t.Cleanup(st.Close)
	return st
}

func note(scope uint64, label string) *LedgerEntity {
	ent := &LedgerEntity{ID: scope}
	ent.LedgerMeta = option.Some(LedgerMeta{Label: label})
	return ent
}

// append chains ents one after another from seed and returns their rows as
// the laws read them, with the head after the last.
func appendAll(t *testing.T, st *LedgerStore, ctx context.Context, seed string, ents ...*LedgerEntity) (rows []evidence.RowI, head evidence.Head) {
	t.Helper()
	head = evidence.Head{Digest: seed}
	for _, ent := range ents {
		digest, err := st.AppendChained(ctx, head, ent)
		require.NoError(t, err)
		head = evidence.Head{Ordinal: ent.Ord, Digest: digest}
		rows = append(rows, st.ChainRow(ent))
	}
	return
}

// AppendChained (gen.Evidence.Chain) assigns the ordinal after the head's,
// sets Prev, Digest and Scheme on the chain component, and digests the
// row's canonical item with Prev and Digest at zero against the head's
// digest — so the rows verify under law L31 from the seed, and a changed
// row breaks at itself (ADR-0327 (proposed) §SD2).
func TestAppendChainedRowsVerifyFromTheSeed(t *testing.T) {
	st := newLedger(t)
	ctx := context.Background()
	a, b, c := note(7, "a"), note(7, "b"), note(7, "c")
	rows, head := appendAll(t, st, ctx, "seed", a, b, c)
	require.Equal(t, uint64(3), head.Ordinal)
	require.Equal(t, []uint64{1, 2, 3}, []uint64{a.Ord, b.Ord, c.Ord})
	require.Equal(t, uint64(1), b.EvidenceChain.Val.Prev)
	require.Equal(t, ledgerScheme.Name(), b.EvidenceChain.Val.Scheme)
	require.Equal(t, evidence.MarkNone.String(), b.EvidenceChain.Val.Mark)
	require.Equal(t, head.Digest, c.EvidenceChain.Val.Digest)

	broken, why, err := evidence.Verify(ledgerScheme, "seed", rows)
	require.NoError(t, err)
	require.Zero(t, broken, why.String())

	b.LedgerMeta = option.Some(LedgerMeta{Label: "B"})
	broken, why, _ = evidence.Verify(ledgerScheme, "seed", rows)
	require.Equal(t, uint64(2), broken)
	require.Equal(t, evidence.BreakDigest, why)
	require.Equal(t, 3, st.Buffered(), "the rows are buffered as Commit buffers them")
}

// The mark is the caller's and is inside the digest; a deleted mark at the
// end explains nothing here (no vault), and a row after it breaks.
func TestTheMarkIsTheCallersAndNothingFollowsDeleted(t *testing.T) {
	st := newLedger(t)
	ctx := context.Background()
	a := note(7, "a")
	deleted := &LedgerEntity{ID: 7}
	deleted.EvidenceChain = option.Some(EvidenceChain{Mark: evidence.MarkDeleted.String()})
	late := note(7, "late")
	rows, _ := appendAll(t, st, ctx, "seed", a, deleted, late)
	require.Equal(t, evidence.MarkDeleted, rows[1].Mark().Mark)
	broken, why, _ := evidence.Verify(ledgerScheme, "seed", rows)
	require.Equal(t, uint64(3), broken)
	require.Equal(t, evidence.BreakAfterDeleted, why)
	broken, why, _ = evidence.Verify(ledgerScheme, "seed", rows[:2])
	require.Zero(t, broken, why.String())
}

// A keep-marked row recording a head answers it as evidence.HeadRowI, and
// law L32 holds against a store that received that head.
func TestAKeepMarkedHeadRowAnchors(t *testing.T) {
	st := newLedger(t)
	ctx := context.Background()
	rows, head := appendAll(t, st, ctx, "seed", note(7, "a"), note(7, "b"))
	h := &LedgerEntity{ID: 7}
	h.EvidenceChain = option.Some(EvidenceChain{Mark: evidence.MarkKeep.String(), MarkTarget: head.Ordinal, HeadDigest: option.Some(head.Digest)})
	digest, err := st.AppendChained(ctx, head, h)
	require.NoError(t, err)
	rows = append(rows, st.ChainRow(h))
	covered, d, ok := rows[2].(evidence.HeadRowI).Head()
	require.True(t, ok)
	require.Equal(t, uint64(2), covered)
	require.Equal(t, head.Digest, d)
	require.NotEqual(t, head.Digest, digest)
	anchored := []evidence.AnchoredHead{{Ordinal: 2, Digest: head.Digest, ReceiptId: "r1"}}
	require.Empty(t, evidence.VerifyHeads(ledgerScheme, "seed", rows, anchored))
	require.Equal(t, []uint64{2}, evidence.VerifyHeads(ledgerScheme, "seed", rows[:2], anchored), "a dropped head row breaks its ordinal")
}

// The written-by component (gen.Evidence.WrittenBy) is the context's: the
// call identity's origin and claims, the batch, and the attestation; a
// value the caller put on the entity is replaced; and it is inside the
// chain digest, so a changed identity breaks the chain (law L35).
func TestTheWrittenByComponentIsTheContexts(t *testing.T) {
	st := newLedger(t)
	ctx := callident.WithCallIdentity(context.Background(), callident.CallIdentity{
		Origin: callident.Origin{Run: "run-1", App: "app-1", Instance: 3},
		Claims: callident.Claims{Principal: "p-1", Purpose: "audit", Correlation: "c-1"},
	})
	ctx = recordstore.WithBatchId(ctx, "batch-1")
	ctx = evidence.WithAttestation(ctx, evidence.Attestation{OriginBy: evidence.OriginByEnvelope, PrincipalBy: evidence.PrincipalByEnv})
	a := note(7, "a")
	a.EvidenceWrittenBy = option.Some(EvidenceWrittenBy{Run: option.Some("forged")})
	rows, _ := appendAll(t, st, ctx, "seed", a)
	w := a.EvidenceWrittenBy.Val
	require.Equal(t, EvidenceWrittenByKind, w.Kind)
	require.Equal(t, option.Some("run-1"), w.Run)
	require.Equal(t, option.Some("app-1"), w.App)
	require.Equal(t, uint64(3), w.Instance)
	require.Equal(t, "envelope", w.OriginBy)
	require.Equal(t, option.Some("p-1"), w.Principal)
	require.Equal(t, evidence.PrincipalByEnv, w.PrincipalBy)
	require.Equal(t, option.Some("audit"), w.Purpose)
	require.Equal(t, option.Some("c-1"), w.Correlation)
	require.Equal(t, option.Some("batch-1"), w.Batch)
	broken, why, _ := evidence.Verify(ledgerScheme, "seed", rows)
	require.Zero(t, broken, why.String())
	w.Run = option.Some("other")
	a.EvidenceWrittenBy = option.Some(w)
	broken, why, _ = evidence.Verify(ledgerScheme, "seed", rows)
	require.Equal(t, uint64(1), broken)
	require.Equal(t, evidence.BreakDigest, why)

	// Without an identity the component still rides, as self with no
	// principal; Begin on its own adds it too.
	bare := note(8, "x")
	_, err := st.AppendChained(context.Background(), evidence.Head{Digest: "seed"}, bare)
	require.NoError(t, err)
	require.Equal(t, "self", bare.EvidenceWrittenBy.Val.OriginBy)
	require.Equal(t, evidence.PrincipalByNone, bare.EvidenceWrittenBy.Val.PrincipalBy)
	require.False(t, bare.EvidenceWrittenBy.Val.Run.Has)
}

// A store without a scheme refuses to chain, and a nil entity is refused.
func TestAppendChainedRefusals(t *testing.T) {
	st := NewLedgerStore(nil, nil, LedgerStoreConfig{})
	defer st.Close()
	_, err := st.AppendChained(context.Background(), evidence.Head{}, note(1, "a"))
	require.ErrorContains(t, err, "no scheme")
	st2 := newLedger(t)
	_, err = st2.AppendChained(context.Background(), evidence.Head{}, nil)
	require.ErrorContains(t, err, "nil entity")
}
