package example

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/semistructured/leeway/jsonshred"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
)

// A JSON document is vaulted as the profile's shredded component
// (gen.Evidence.Shredded): one content row, exploded by jsonshred, behind
// one commitment; it reads back as the canonical document, verifies, and a
// changed leaf breaks the commitment.
func TestAShreddedDocumentIsOneVaultedComponent(t *testing.T) {
	v := newVaulted(t)
	doc := []byte(`{"tool": "fetch", "args": {"url": "https://example.test", "retries": 3, "ratio": 0.5, "flags": [true, null, {}, []]}}`)
	attrs, err := jsonshred.Explode(doc, jsonshred.Options{})
	require.NoError(t, err)
	shredded, err := EvidenceShreddedOf(attrs)
	require.NoError(t, err)
	row := &LedgerVaultEntity{}
	row.EvidenceShredded = option.Some(shredded)
	ent := &LedgerEntity{ID: 9}
	beside := content("beside it")
	digest, err := v.Append(context.Background(), evidence.Head{Digest: "seed"}, ent, []*LedgerVaultEntity{row, beside})
	require.NoError(t, err)
	require.NotEmpty(t, digest)
	require.Equal(t, EvidenceShreddedKind, row.LedgerVaultEnvelope.Component)
	require.ElementsMatch(t, []string{EvidenceShreddedKind, "ledgerNote"}, ent.EvidenceChain.Val.CommitComponents)

	composed, err := jsonshred.Compose(row.EvidenceShredded.Val.Attrs())
	require.NoError(t, err)
	canonical, err := jsonshred.Canonical(doc)
	require.NoError(t, err)
	require.Equal(t, string(canonical), string(composed), "the component composes back to the canonical document")

	rows, _ := v.Join([]*LedgerEntity{ent}, []*LedgerVaultEntity{row, beside})
	broken, why, err := evidence.Verify(ledgerScheme, "seed", rows)
	require.NoError(t, err)
	require.Zero(t, broken, why.String())

	s := row.EvidenceShredded.Val
	s.I64s[0].Value = 4
	row.EvidenceShredded = option.Some(s)
	broken, why, _ = evidence.Verify(ledgerScheme, "seed", rows)
	require.Equal(t, uint64(1), broken)
	require.Equal(t, evidence.BreakCommitment, why)
}

// The shredded component is one of the vault's components by name, and a
// content row may hold it or a hand-written component, not both.
func TestTheShreddedComponentIsOneOfTheVaults(t *testing.T) {
	require.Contains(t, LedgerVaultComponents, EvidenceShreddedKind)
	v := newVaulted(t)
	row := content("x")
	row.EvidenceShredded = option.Some(EvidenceShredded{})
	_, err := v.Append(context.Background(), evidence.Head{Digest: "seed"}, &LedgerEntity{ID: 9}, []*LedgerVaultEntity{row})
	require.ErrorContains(t, err, "more than one vaulted component")
}
