package example

import (
	"context"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/require"
)

// TestDeviceStoreScan exercises the Scan verb (ADR-0100 SD4 over the
// ADR-0066 artefacts): the baked Filter — presence prefilter AND exact
// validator with membership ids as literals — selects exactly the rows
// carrying a conforming component, across the whole history (Scan is
// row-level, not latest-collapsed), with ScanOpts narrowing further.
func TestDeviceStoreScan(t *testing.T) {
	local, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	ctx := context.Background()
	st := NewDeviceStore(local, nil, DeviceStoreConfig{})
	require.NoError(t, st.EnsureTable(ctx))

	t0 := time.Unix(1_600_000_000, 0).UTC()
	t1 := t0.Add(time.Hour)
	require.NoError(t, st.Begin(1, t0).
		AddIdentity(Identity{ID: 1, Status: "IDLE"}).
		AddBattery(Battery{ID: 1, Charge: 9000}).
		Commit())
	require.NoError(t, st.IngestIdentity(t0, []Identity{{ID: 2, Status: "CHARGING"}}))
	require.NoError(t, st.IngestBattery(t0, []Battery{{ID: 3, Charge: 150}}))
	require.NoError(t, st.Begin(1, t1).AddBattery(Battery{ID: 1, Charge: 800}).Commit())
	_, err = st.Flush(ctx)
	require.NoError(t, err)

	// Rows carrying Identity: entity 1's first version and entity 2.
	idents := make([]*DeviceEntity, 0, 2)
	for ent, rerr := range st.ScanIdentity(ctx, recordstore.ScanOpts{}) {
		require.NoError(t, rerr)
		require.True(t, ent.Identity.Has)
		idents = append(idents, ent)
	}
	require.Len(t, idents, 2)
	require.Equal(t, uint64(1), idents[0].ID)
	require.Equal(t, uint64(2), idents[1].ID)

	// Rows carrying Battery: entity 1 (both versions) and entity 3.
	batts := 0
	for _, rerr := range st.ScanBattery(ctx, recordstore.ScanOpts{}) {
		require.NoError(t, rerr)
		batts++
	}
	require.Equal(t, 3, batts)

	// Limit caps the row count (unlimited above: 3 rows).
	capped := 0
	for _, rerr := range st.ScanBattery(ctx, recordstore.ScanOpts{Limit: 2}) {
		require.NoError(t, rerr)
		capped++
	}
	require.Equal(t, 2, capped)

	// ExtraPredicate narrows over the physical columns.
	one := make([]*DeviceEntity, 0, 1)
	for ent, rerr := range st.ScanIdentity(ctx, recordstore.ScanOpts{ExtraPredicate: DeviceColKey + " = 2"}) {
		require.NoError(t, rerr)
		one = append(one, ent)
	}
	require.Len(t, one, 1)
	require.Equal(t, uint64(2), one[0].ID)
	require.Equal(t, "CHARGING", one[0].Identity.Val.Status)

	// A component nobody wrote scans empty.
	for ent, rerr := range st.ScanLocated(ctx, recordstore.ScanOpts{}) {
		require.NoError(t, rerr)
		t.Fatalf("unexpected Located row for entity %d — scan must be empty", ent.ID)
	}
}
