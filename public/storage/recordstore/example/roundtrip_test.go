package example

import (
	"context"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/require"
)

// TestDeviceStoreRoundTrip is the ADR-0100 slice-S1 acceptance: build →
// flush → clickhouse-local → Get (cached, via the work-item replay
// contract) / Latest / Replay, plus the state view (Put versions, Delete
// tombstone, GetLatest). MergeTree under a --path directory keeps the data
// durable across the executor's one-shot processes.
func TestDeviceStoreRoundTrip(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	ctx := context.Background()
	st := NewDeviceStore[string](exec, nil, DeviceStoreConfig{CacheCapacity: 8})
	require.NoError(t, st.EnsureTable(ctx))

	t0 := time.Unix(1_600_000_000, 0).UTC()
	t1 := t0.Add(time.Hour)
	t2 := t0.Add(2 * time.Hour)

	// Entity 1: all four components (Identity carries the optional Nick);
	// entity 2: identity only, Nick absent; entity 3: battery + tags via
	// the per-kind ingest verbs.
	require.NoError(t, st.Begin(1, t0).
		AddIdentity(Identity{ID: 1, Status: "IDLE", Nick: option.Some("alpha")}).
		AddBattery(Battery{ID: 1, Charge: 9000}).
		AddTagged(Tagged{ID: 1, Tags: []string{"survey", "urgent"}}).
		AddLocated(Located{ID: 1, Lat: 47.5, Lng: 8.5, Cell: 12345}).
		Commit())
	require.NoError(t, st.IngestIdentity(t0, []Identity{{ID: 2, Status: "CHARGING"}}))
	require.NoError(t, st.IngestBattery(t0, []Battery{{ID: 3, Charge: 150}}))
	require.NoError(t, st.IngestTagged(t0, []Tagged{{ID: 3, Tags: []string{"idle"}}}))
	require.Equal(t, 4, st.Buffered())
	n, err := st.Flush(ctx)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	// Get through the cache: misses queue under a work item, the forced
	// fetch batches one IN (…) lookup, the replay then hits.
	for range st.WorkItem("frame-1") {
		for _, key := range []uint64{1, 2} {
			has, _ := st.Get(key)
			require.False(t, has, "key %d must miss before the batch fetch", key)
		}
	}
	replayed := 0
	for w := range st.IterateRestWorkItems(ctx) {
		require.Equal(t, "frame-1", w)
		replayed++
		has, e1 := st.Get(1)
		require.True(t, has)
		require.Equal(t, []string{"identity", "battery", "tagged", "located"}, e1.Archetype())
		require.Equal(t, option.Some(Identity{ID: 1, Status: "IDLE", Nick: option.Some("alpha")}), e1.Identity)
		require.Equal(t, option.Some(Battery{ID: 1, Charge: 9000}), e1.Battery)
		require.Equal(t, option.Some(Tagged{ID: 1, Tags: []string{"survey", "urgent"}}), e1.Tagged)
		require.Equal(t, option.Some(Located{ID: 1, Lat: 47.5, Lng: 8.5, Cell: 12345}), e1.Located)
		require.Equal(t, t0, e1.Ts)

		has, e2 := st.Get(2)
		require.True(t, has)
		require.Equal(t, []string{"identity"}, e2.Archetype())
		require.False(t, e2.Identity.Val.Nick.Has, "absent Option scalar reads back as None")
		require.False(t, e2.Battery.Has)
	}
	require.Equal(t, 1, replayed)

	// Entity 3 got two single-component rows; the cached Get collapses
	// newest-first to one row — battery and tags landed on separate rows,
	// so the fetched version carries exactly one of them.
	for range st.WorkItem("frame-2") {
		_, _ = st.Get(3)
	}
	for range st.IterateRestWorkItems(ctx) {
		has, e3 := st.Get(3)
		require.True(t, has)
		require.Len(t, e3.Archetype(), 1)
	}

	// State view: a new version for entity 1 carrying only Battery.
	require.NoError(t, st.Put(1, t1).AddBattery(Battery{ID: 1, Charge: 8500}).Commit())
	_, err = st.Flush(ctx)
	require.NoError(t, err)

	latest, found, err := st.Latest(ctx, 1)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, t1, latest.Ts)
	require.Equal(t, option.Some(Battery{ID: 1, Charge: 8500}), latest.Battery)
	require.False(t, latest.Identity.Has, "a version is a whole row; the new one carries no Identity")

	got, found, err := st.GetLatest(ctx, 1)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, latest.Ts, got.Ts)

	// Delete = tombstone: GetLatest reads absent, Latest still sees the row.
	require.NoError(t, st.Delete(1, t2))
	_, err = st.Flush(ctx)
	require.NoError(t, err)

	_, found, err = st.GetLatest(ctx, 1)
	require.NoError(t, err)
	require.False(t, found)

	tomb, found, err := st.Latest(ctx, 1)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, deviceLifecycleTombstone, tomb.Lifecycle)
	require.Empty(t, tomb.Archetype())

	// Replay: the full ordered history of entity 1 (v1, v2, tombstone).
	history, err := st.Replay(ctx, 1, time.Time{})
	require.NoError(t, err)
	require.Len(t, history, 3)
	require.Equal(t, []time.Time{t0, t1, t2}, []time.Time{history[0].Ts, history[1].Ts, history[2].Ts})
	require.True(t, history[0].Identity.Has)
	require.False(t, history[1].Identity.Has)
	require.Equal(t, deviceLifecycleTombstone, history[2].Lifecycle)

	// Replay from t1 skips the first version.
	tail, err := st.Replay(ctx, 1, t1)
	require.NoError(t, err)
	require.Len(t, tail, 2)

	// Latest of a never-written key reports not-found.
	_, found, err = st.Latest(ctx, 99)
	require.NoError(t, err)
	require.False(t, found)
}
