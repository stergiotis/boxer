package example

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

func newVaulted(t *testing.T) *LedgerVaulted {
	t.Helper()
	v := NewLedgerVaulted(NewLedgerStore(nil, nil, LedgerStoreConfig{Scheme: ledgerScheme}), NewLedgerVaultStore(nil, nil, LedgerVaultStoreConfig{}))
	t.Cleanup(v.Close)
	return v
}

func content(text string) *LedgerVaultEntity {
	v := &LedgerVaultEntity{}
	v.LedgerNote = option.Some(LedgerNote{Text: text})
	return v
}

// appendVaulted chains ledger rows with their content from seed and returns
// the ledger entities, the content rows and the head.
func appendVaulted(t *testing.T, v *LedgerVaulted, seed string, texts ...string) (ledger []*LedgerEntity, vault []*LedgerVaultEntity, head evidence.Head) {
	t.Helper()
	head = evidence.Head{Digest: seed}
	for _, text := range texts {
		ent := &LedgerEntity{ID: 7}
		rows := []*LedgerVaultEntity{content(text)}
		digest, err := v.Append(context.Background(), head, ent, rows)
		require.NoError(t, err)
		head = evidence.Head{Ordinal: ent.Ord, Digest: digest}
		ledger = append(ledger, ent)
		vault = append(vault, rows...)
	}
	return
}

// Append (gen.Evidence.Vault) keeps content out of the ledger: the content
// row is keyed by the ledger row's scope and ordinal and named by its
// component, gains an opening, and the ledger row carries one commitment
// per component (ADR-0327 (proposed) §SD4). Joined, the rows verify; a
// changed content row breaks at its commitment.
func TestVaultedContentIsCommittedNotStored(t *testing.T) {
	v := newVaulted(t)
	ledger, vault, head := appendVaulted(t, v, "seed", "a", "b")
	require.Equal(t, uint64(2), head.Ordinal)
	a, va := ledger[0], vault[0]
	require.Equal(t, uint64(7), va.ID)
	require.Equal(t, uint64(1), va.Ord)
	require.Equal(t, "ledgerNote", va.LedgerVaultEnvelope.Component)
	require.True(t, va.EvidenceOpening.Has)
	require.Len(t, evidence.OpeningOf(va.EvidenceOpening.Val.Opening), evidence.OpeningSize)
	require.Equal(t, []string{"ledgerNote"}, a.EvidenceChain.Val.CommitComponents)
	require.Len(t, a.EvidenceChain.Val.Commitments, 1)
	require.Contains(t, LedgerVaultComponents, "ledgerNote")

	rows, byOrdinal := v.Join(ledger, vault)
	require.Len(t, byOrdinal[1], 1)
	broken, why, err := evidence.Verify(ledgerScheme, "seed", rows)
	require.NoError(t, err)
	require.Zero(t, broken, why.String())
	cs := rows[0].Commitments()
	require.Len(t, cs, 1)
	require.True(t, cs[0].Present)

	va.LedgerNote = option.Some(LedgerNote{Text: "tampered"})
	broken, why, _ = evidence.Verify(ledgerScheme, "seed", rows)
	require.Equal(t, uint64(1), broken)
	require.Equal(t, evidence.BreakCommitment, why)
}

// A content row that is gone leaves a standing commitment: unexplained in a
// live scope, explained by an erased mark naming its ordinal and component,
// and by a deleted mark at the end.
func TestAMissingContentRowNeedsAMark(t *testing.T) {
	v := newVaulted(t)
	ledger, vault, head := appendVaulted(t, v, "seed", "a", "b")
	rows, _ := v.Join(ledger, vault[1:])
	broken, why, _ := evidence.Verify(ledgerScheme, "seed", rows)
	require.Equal(t, uint64(1), broken)
	require.Equal(t, evidence.BreakUnexplained, why)

	erased := &LedgerEntity{ID: 7}
	erased.EvidenceChain = option.Some(EvidenceChain{Mark: evidence.MarkErased.String(), MarkTarget: 1, MarkComponent: option.Some("ledgerNote")})
	digest, err := v.Append(context.Background(), head, erased, nil)
	require.NoError(t, err)
	head = evidence.Head{Ordinal: erased.Ord, Digest: digest}
	rows, _ = v.Join(append(ledger, erased), vault[1:])
	broken, why, _ = evidence.Verify(ledgerScheme, "seed", rows)
	require.Zero(t, broken, why.String())

	deleted := &LedgerEntity{ID: 7}
	deleted.EvidenceChain = option.Some(EvidenceChain{Mark: evidence.MarkDeleted.String()})
	_, err = v.Append(context.Background(), head, deleted, nil)
	require.NoError(t, err)
	rows, _ = v.Join(append(ledger, erased, deleted), nil)
	broken, why, _ = evidence.Verify(ledgerScheme, "seed", rows)
	require.Zero(t, broken, why.String())
}

// Split refuses a content row that holds no, or more than one, vaulted
// component, and a ledger without a scheme; erasure needs an executor.
func TestVaultedRefusals(t *testing.T) {
	v := newVaulted(t)
	ent := &LedgerEntity{ID: 7}
	_, err := v.Append(context.Background(), evidence.Head{Digest: "seed"}, ent, []*LedgerVaultEntity{{}})
	require.ErrorContains(t, err, "no vaulted component")
	bare := NewLedgerVaulted(NewLedgerStore(nil, nil, LedgerStoreConfig{}), NewLedgerVaultStore(nil, nil, LedgerVaultStoreConfig{}))
	defer bare.Close()
	_, err = bare.Append(context.Background(), evidence.Head{Digest: "seed"}, &LedgerEntity{ID: 7}, []*LedgerVaultEntity{content("a")})
	require.ErrorContains(t, err, "no scheme")
	require.ErrorContains(t, v.Erase(context.Background(), 7, []uint64{1}, nil), "no executor")
	require.ErrorContains(t, v.Erase(context.Background(), 7, []uint64{1}, []string{"x'; DROP"}), "not a kind tag")
	require.NoError(t, v.Erase(context.Background(), 7, nil, nil), "nothing to erase")
}
