package example

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// Prepare, Check and Write are the writer's three steps when the digest
// must be known before the act (ADR-0314 §SD1): a client prepares, a
// service checks what it was handed against the previous digest and
// writes it as it stands. What Prepare makes checks; a row from another
// head, a content row that was changed, re-opened or re-keyed, and a row
// with the wrong number of commitments do not.
func TestPreparedRowsCheckAndWrite(t *testing.T) {
	client := newVaulted(t)
	service := newVaulted(t)
	ctx := context.Background()
	ent := &LedgerEntity{ID: 11}
	ent.LedgerMeta = option.Some(LedgerMeta{Label: "x"})
	shredded := &LedgerVaultEntity{}
	shredded.EvidenceShredded = option.Some(EvidenceShredded{Strings: []EvidenceShreddedString{{Path: "/k", Value: "v"}}})
	// One row per component: the note and the shredded document.
	rows := []*LedgerVaultEntity{content("a"), shredded}
	digest, err := client.Prepare(ctx, evidence.Head{Digest: "seed"}, ent, rows)
	require.NoError(t, err)
	require.Equal(t, digest, ent.EvidenceChain.Val.Digest)
	require.Zero(t, client.Ledger().Buffered(), "nothing is written by Prepare")

	require.NoError(t, service.Check("seed", ent, rows))
	require.NoError(t, service.Write(ctx, ent, rows))
	require.Equal(t, 1, service.Ledger().Buffered())
	require.Equal(t, 2, service.Vault().Buffered())
	joined, _ := service.Join([]*LedgerEntity{ent}, rows)
	broken, why, _ := evidence.Verify(ledgerScheme, "seed", joined)
	require.Zero(t, broken, why.String())

	require.ErrorContains(t, service.Check("other", ent, rows), "digest is not the chain's")
	require.ErrorContains(t, service.Check("seed", ent, rows[:1]), "differ in number")

	changed := *rows[0]
	changed.LedgerNote = option.Some(LedgerNote{Text: "A"})
	require.ErrorContains(t, service.Check("seed", ent, []*LedgerVaultEntity{&changed, rows[1]}), "does not open")

	rekeyed := *rows[0]
	rekeyed.Ord = 9
	require.ErrorContains(t, service.Check("seed", ent, []*LedgerVaultEntity{&rekeyed, rows[1]}), "not the ledger row's")

	reopened := *rows[0]
	reopened.EvidenceOpening = option.Some(EvidenceOpening{Kind: EvidenceOpeningKind, Opening: evidence.OpeningText(evidence.NewOpening())})
	require.ErrorContains(t, service.Check("seed", ent, []*LedgerVaultEntity{&reopened, rows[1]}), "does not open")

	swapped := []*LedgerVaultEntity{rows[1], rows[0]}
	require.ErrorContains(t, service.Check("seed", ent, swapped), "not named by the component")

	twice := &LedgerEntity{ID: 13}
	_, err = client.Prepare(ctx, evidence.Head{Digest: "seed"}, twice, []*LedgerVaultEntity{content("a"), content("b")})
	require.ErrorContains(t, err, "same vaulted component")

	unprepared := &LedgerEntity{ID: 11}
	require.ErrorContains(t, service.Write(ctx, unprepared, nil), "not prepared")
	require.ErrorContains(t, service.Check("seed", unprepared, nil), "no chain component")
}

// A prepared row's written-by component is the preparing context's and is
// inside the digest; a service that vouches for it compares the claimed
// origin with its sender and refuses a mismatch before Write.
func TestAPreparedRowCarriesTheClaimedIdentity(t *testing.T) {
	client := newVaulted(t)
	ctx := evidence.WithAttestation(context.Background(), evidence.Attestation{OriginBy: evidence.OriginByEnvelope})
	ent := &LedgerEntity{ID: 12}
	_, err := client.Prepare(ctx, evidence.Head{Digest: "seed"}, ent, nil)
	require.NoError(t, err)
	require.Equal(t, "envelope", ent.EvidenceWrittenBy.Val.OriginBy)
	w := ent.EvidenceWrittenBy.Val
	w.Run = option.Some("someone-else")
	ent.EvidenceWrittenBy = option.Some(w)
	require.ErrorContains(t, client.Check("seed", ent, nil), "digest is not the chain's")
}
