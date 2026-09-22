package example

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// TestDeviceStoreScanLive exercises the state view's scan (ADR-0105 Update
// 2026-08-15, P3). Every key below is shaped so that a verb testing the
// component BEFORE collapsing to the newest row would answer differently —
// that ordering is what the verb exists to get right, and what would fail
// is a deleted or superseded key coming back.
func TestDeviceStoreScanLive(t *testing.T) {
	local, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	st := NewDeviceStore(local, nil, DeviceStoreConfig{})
	require.NoError(t, st.EnsureTable(ctx))

	t0 := time.Unix(1_600_000_000, 0).UTC()
	t1 := t0.Add(time.Hour)
	t2 := t1.Add(time.Hour)
	ident := func(id uint64, ts time.Time, status string) {
		require.NoError(t, st.Begin(id, ts).AddIdentity(Identity{ID: id, Status: status}).Commit())
	}
	// 1: written, then deleted — the tombstone carries no component.
	ident(1, t0, "IDLE")
	require.NoError(t, st.Delete(1, t1))
	// 2: its newest row carries only Battery — the component switched.
	ident(2, t0, "IDLE")
	require.NoError(t, st.Begin(2, t1).AddBattery(Battery{ID: 2, Charge: 500}).Commit())
	// 3: two versions; only the newer is live.
	ident(3, t0, "IDLE")
	ident(3, t1, "CHARGING")
	// 4: deleted, then written again — live once more.
	ident(4, t0, "IDLE")
	require.NoError(t, st.Delete(4, t1))
	ident(4, t2, "RETURNED")
	// 5: never touched after its one write.
	ident(5, t0, "IDLE")
	_, err = st.Flush(ctx)
	require.NoError(t, err)

	collect := func(seq func(func(*DeviceEntity, error) bool)) (got map[uint64]string, order []uint64) {
		got = map[uint64]string{}
		for ent, rerr := range seq {
			require.NoError(t, rerr)
			got[ent.ID] = ent.Identity.Val.Status
			order = append(order, ent.ID)
		}
		return
	}

	got, order := collect(st.ScanLiveIdentity(ctx, recordstore.ScanOpts{}))
	require.Equal(t, map[uint64]string{3: "CHARGING", 4: "RETURNED", 5: "IDLE"}, got,
		"1 (deleted) and 2 (switched to Battery) must be absent, not represented by their older Identity rows")
	require.Equal(t, []uint64{3, 4, 5}, order, "live entities come out ordered by key")

	// The switched key is live under the component its newest row carries.
	batts := 0
	for ent, rerr := range st.ScanLiveBattery(ctx, recordstore.ScanOpts{}) {
		require.NoError(t, rerr)
		require.Equal(t, uint64(2), ent.ID)
		batts++
	}
	require.Equal(t, 1, batts)

	// ExtraPredicate narrows the collapsed rows. Selecting live rows by
	// lifecycle would, if applied before the collapse, drop key 1's
	// tombstone and let its older Identity row through.
	got, _ = collect(st.ScanLiveIdentity(ctx, recordstore.ScanOpts{
		ExtraPredicate: DeviceColLifecycle + " = " + strconv.Itoa(int(recordstore.LifecycleLive)),
	}))
	require.NotContains(t, got, uint64(1), "ExtraPredicate must not uncover a superseded row")
	require.Contains(t, got, uint64(5))

	// Limit caps the live set.
	_, order = collect(st.ScanLiveIdentity(ctx, recordstore.ScanOpts{Limit: 2}))
	require.Equal(t, []uint64{3, 4}, order)

	// A uint64 key has no prefix: both scan verbs refuse rather than
	// quietly returning every key.
	for _, seq := range []func(func(*DeviceEntity, error) bool){
		st.ScanLiveIdentity(ctx, recordstore.ScanOpts{KeyPrefix: "1"}),
		st.ScanIdentity(ctx, recordstore.ScanOpts{KeyPrefix: "1"}),
	} {
		n := 0
		for ent, rerr := range seq {
			require.Nil(t, ent)
			require.ErrorIs(t, rerr, recordstore.ErrKeyPrefixNumericKey)
			n++
		}
		require.Equal(t, 1, n, "the refusal is one final error pair")
	}
}
