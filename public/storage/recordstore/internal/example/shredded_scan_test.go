package example

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/jsonshred"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// ScanEvidenceShredded selects the rows carrying the shredded component and
// no other: its Filter renders the tuple by ReadRow's claim rule (ADR-0326
// §SD4), so a row whose sections carry only another component's attributes
// is not selected, and the selected row decodes as it was written.
func TestScanSelectsTheShreddedComponent(t *testing.T) {
	local, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	st := NewLedgerVaultStore(local, nil, LedgerVaultStoreConfig{})
	require.NoError(t, st.EnsureTable(ctx))

	attrs, err := jsonshred.Explode([]byte(`{"tool": "fetch", "args": {"retries": 3, "flags": [true, null, {}, []]}}`), jsonshred.Options{})
	require.NoError(t, err)
	shredded, err := EvidenceShreddedOf(attrs)
	require.NoError(t, err)
	require.NoError(t, st.Begin(1, 1, LedgerVaultEnvelope{Component: EvidenceShreddedKind}).AddEvidenceShredded(shredded).Commit())
	require.NoError(t, st.Begin(2, 1, LedgerVaultEnvelope{Component: "ledgerNote"}).AddLedgerNote(LedgerNote{Text: "a note"}).Commit())
	_, err = st.Flush(ctx)
	require.NoError(t, err)

	shredded.Id = 1 // the store reads the entity id onto the component
	var ids []uint64
	for ent, serr := range st.ScanEvidenceShredded(ctx, recordstore.ScanOpts{}) {
		require.NoError(t, serr)
		ids = append(ids, ent.ID)
		require.True(t, ent.EvidenceShredded.Has)
		require.Equal(t, shredded, ent.EvidenceShredded.Val)
	}
	require.Equal(t, []uint64{1}, ids)

	var notes []uint64
	for ent, serr := range st.ScanLedgerNote(ctx, recordstore.ScanOpts{}) {
		require.NoError(t, serr)
		notes = append(notes, ent.ID)
	}
	require.Equal(t, []uint64{2}, notes)
}
