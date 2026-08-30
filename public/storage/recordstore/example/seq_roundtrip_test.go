package example

import (
	"context"
	"testing"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/require"
)

// seqTombstone is the marker value the fixture's tombstone pair writes and
// recognises — the schema has no u8 Lifecycle role, so the pair is the
// whole state-view marker (ADR-0100 Update 2026-08-30).
const seqTombstone = ^uint64(0)

func seqPairConfig() SeqStoreConfig {
	return SeqStoreConfig{
		TombstoneDetect: func(e *SeqEntity) bool {
			return e.SeqReading.Has && e.SeqReading.Val.Value == seqTombstone
		},
		TombstoneWrite: func(b *SeqEntityBuilder) {
			b.AddSeqReading(SeqReading{Value: seqTombstone})
		},
	}
}

// TestSeqStoreU64OrderRoundTrip is the u64-Order plus predicate-tombstone
// acceptance (ADR-0100 Updates 2026-08-30): write versions under a
// caller-supplied integer sequence, flush through clickhouse-local, and
// check that Latest, the Replay bounds (plain integer comparisons,
// server-evaluated), Scan and the pair-driven state view all follow the
// declared Order.
func TestSeqStoreU64OrderRoundTrip(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	st := NewSeqStore(exec, nil, seqPairConfig())
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
	scan := func() (rows [][2]uint64) {
		for ent, serr := range st.ScanSeqReading(ctx, recordstore.ScanOpts{}) {
			require.NoError(t, serr)
			rows = append(rows, [2]uint64{ent.Ord, ent.ID})
		}
		return
	}
	require.Equal(t, [][2]uint64{{100, 1}, {150, 2}, {200, 1}}, scan())

	// State view: Delete appends the marker row the pair recognises, at the
	// same integer Order lane.
	require.NoError(t, st.Delete(1, 300))
	_, err = st.Flush(ctx)
	require.NoError(t, err)
	_, live, err := st.GetLive(ctx, 1)
	require.NoError(t, err)
	require.False(t, live, "deleted key must read absent through GetLive")
	tomb, found, err := st.Latest(ctx, 1)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, uint64(300), tomb.Ord)
	require.True(t, tomb.SeqReading.Has && tomb.SeqReading.Val.Value == seqTombstone,
		"the tombstone-blind Latest hands back the marker row")
	other, live, err := st.GetLive(ctx, 2)
	require.NoError(t, err)
	require.True(t, live)
	require.Equal(t, uint64(150), other.Ord)

	// The marker is an ordinary row: a Scan whose filter it satisfies
	// returns it — the documented caveat of an attribute-shaped marker.
	require.Equal(t, [][2]uint64{{100, 1}, {150, 2}, {200, 1}, {300, 1}}, scan())
}

// TestSeqStoreRequiresTombstonePair: a TombstoneView store binds no u8
// Lifecycle role, so the constructor refuses a missing or torn pair.
func TestSeqStoreRequiresTombstonePair(t *testing.T) {
	require.PanicsWithValue(t,
		"SeqStore: generated with TombstoneView and no u8 Lifecycle role — the tombstone pair (TombstoneDetect, TombstoneWrite) is required",
		func() { NewSeqStore(nil, nil, SeqStoreConfig{}) })
	half := seqPairConfig()
	half.TombstoneWrite = nil
	require.PanicsWithValue(t,
		"SeqStore: the tombstone pair comes whole — supply both TombstoneDetect and TombstoneWrite, or neither",
		func() { NewSeqStore(nil, nil, half) })
}
