package example

import (
	"context"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/recordstore/chexec"
	"github.com/stretchr/testify/require"
)

// TestDeviceStoreScan exercises the Scan verb (ADR-0100 SD4 over the
// ADR-0066 artefacts): the baked Filter — presence prefilter AND exact
// validator with membership ids as literals — selects exactly the rows
// carrying a conforming component, across the whole history (Scan is
// row-level, not latest-collapsed), with extraPredicate narrowing further.
func TestDeviceStoreScan(t *testing.T) {
	local, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	ctx := context.Background()
	st := NewDeviceStore[string](local, nil, DeviceStoreConfig{CacheCapacity: 8})
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
	idents, err := st.ScanIdentity(ctx, "")
	require.NoError(t, err)
	require.Len(t, idents, 2)
	for _, ent := range idents {
		require.True(t, ent.Identity.Has)
	}
	require.Equal(t, uint64(1), idents[0].ID)
	require.Equal(t, uint64(2), idents[1].ID)

	// Rows carrying Battery: entity 1 (both versions) and entity 3.
	batts, err := st.ScanBattery(ctx, "")
	require.NoError(t, err)
	require.Len(t, batts, 3)

	// extraPredicate narrows over the physical columns.
	one, err := st.ScanIdentity(ctx, deviceColKey+" = 2")
	require.NoError(t, err)
	require.Len(t, one, 1)
	require.Equal(t, uint64(2), one[0].ID)
	require.Equal(t, "CHARGING", one[0].Identity.Val.Status)

	// A component nobody wrote scans empty.
	locs, err := st.ScanLocated(ctx, "")
	require.NoError(t, err)
	require.Empty(t, locs)
}
