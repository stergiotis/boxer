package example

import (
	"context"
	"testing"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/require"
)

// TestSeqStoreU64OrderRoundTrip is the u64-Order acceptance (ADR-0100
// Update 2026-08-30): write versions under a caller-supplied integer
// sequence, flush through clickhouse-local, and check that Latest, the
// Replay bounds (plain integer comparisons, server-evaluated), Scan and the
// state view all follow the declared Order.
func TestSeqStoreU64OrderRoundTrip(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	st := NewSeqStore(exec, nil, SeqStoreConfig{})
	defer st.Close()
	require.NoError(t, st.EnsureTable(ctx))
	require.NoError(t, st.VerifySchema(ctx), "fresh table must match the generated schema")

	require.NoError(t, st.Begin(1, 100).AddSeqReading(SeqReading{ID: 1, Value: 10}).Commit())
	require.NoError(t, st.Begin(1, 200).AddSeqReading(SeqReading{ID: 1, Value: 20}).Commit())
	require.NoError(t, st.Begin(2, 150).AddSeqReading(SeqReading{ID: 2, Value: 15}).Commit())
	n, err := st.Flush(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, n)

	got, found, err := st.Latest(ctx, 1)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, uint64(200), got.Ord, "Latest follows the integer Order")
	require.True(t, got.SeqReading.Has)
	require.Equal(t, uint64(20), got.SeqReading.Val.Value)

	replay := func(from uint64, opts recordstore.ReplayOptsU64) (ords []uint64) {
		for ent, rerr := range st.Replay(ctx, 1, from, opts) {
			require.NoError(t, rerr)
			ords = append(ords, ent.Ord)
		}
		return
	}
	require.Equal(t, []uint64{100, 200}, replay(0, recordstore.ReplayOptsU64{}))
	require.Equal(t, []uint64{200}, replay(150, recordstore.ReplayOptsU64{}))
	require.Equal(t, []uint64{100}, replay(0, recordstore.ReplayOptsU64{To: 200}), "To is exclusive")
	require.Equal(t, []uint64{100}, replay(0, recordstore.ReplayOptsU64{Limit: 1}))

	// Scan orders by (Order, Key) across keys.
	var scanned [][2]uint64
	for ent, serr := range st.ScanSeqReading(ctx, recordstore.ScanOpts{}) {
		require.NoError(t, serr)
		scanned = append(scanned, [2]uint64{ent.Ord, ent.ID})
	}
	require.Equal(t, [][2]uint64{{100, 1}, {150, 2}, {200, 1}}, scanned)

	// State view: the tombstone rides the same integer Order.
	require.NoError(t, st.Delete(1, 300))
	_, err = st.Flush(ctx)
	require.NoError(t, err)
	_, live, err := st.GetLive(ctx, 1)
	require.NoError(t, err)
	require.False(t, live, "deleted key must read absent through GetLive")
	tomb, found, err := st.Latest(ctx, 1)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, tomb.IsTombstone())
	require.Equal(t, uint64(300), tomb.Ord)
	other, live, err := st.GetLive(ctx, 2)
	require.NoError(t, err)
	require.True(t, live)
	require.Equal(t, uint64(150), other.Ord)
}
