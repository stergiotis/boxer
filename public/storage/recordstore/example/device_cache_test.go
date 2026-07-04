package example

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/caching"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/require"
)

// countingExecutor counts the point-lookup queries reaching ClickHouse so
// the tests can assert the cache's batching behaviour.
type countingExecutor struct {
	inner      recordstore.ExecutorI
	queryCalls int
}

func (inst *countingExecutor) Exec(ctx context.Context, sql string) error {
	return inst.inner.Exec(ctx, sql)
}

func (inst *countingExecutor) QueryArrow(ctx context.Context, sql string) ([]arrow.RecordBatch, error) {
	inst.queryCalls++
	return inst.inner.QueryArrow(ctx, sql)
}

func (inst *countingExecutor) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) error {
	return inst.inner.InsertArrow(ctx, table, records)
}

// failingExecutor accepts writes and fails every read — the fetch-error
// path without a ClickHouse dependency.
type failingExecutor struct {
	queryCalls int
}

func (inst *failingExecutor) Exec(ctx context.Context, sql string) error { return nil }

func (inst *failingExecutor) QueryArrow(ctx context.Context, sql string) ([]arrow.RecordBatch, error) {
	inst.queryCalls++
	return nil, errors.New("synthetic fetch failure")
}

func (inst *failingExecutor) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) error {
	return nil
}

// TestDeviceStoreCacheBatchesFetches exercises the ADR-0100 S2 cache
// contract end to end: misses from several work items accumulate below
// the Min threshold (IterateReadyWorkItems yields nothing, no query
// runs), then one batched IN (…) lookup serves every pending work item
// on replay.
func TestDeviceStoreCacheBatchesFetches(t *testing.T) {
	local, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	counting := &countingExecutor{inner: local}
	ctx := context.Background()
	st := NewDeviceStore[string](counting, nil, DeviceStoreConfig{
		CacheCapacity: 16,
		FetchCriteria: caching.FetchCriteria{MinKeys: 4},
	})
	require.NoError(t, st.EnsureTable(ctx))

	t0 := time.Unix(1_600_000_000, 0).UTC()
	for id := uint64(1); id <= 4; id++ {
		require.NoError(t, st.Begin(id, t0).AddIdentity(Identity{ID: id, Status: "IDLE"}).Commit())
	}
	_, err = st.Flush(ctx)
	require.NoError(t, err)

	for range st.WorkItem("frame-A") {
		for _, key := range []uint64{1, 2} {
			has, _ := st.Get(key)
			require.False(t, has)
		}
	}
	for range st.IterateReadyWorkItems(ctx) {
		t.Fatal("2 queued keys are below MinKeys=4 — nothing may be ready")
	}
	require.Equal(t, 0, counting.queryCalls, "no fetch below the Min threshold")

	for range st.WorkItem("frame-B") {
		for _, key := range []uint64{3, 4} {
			has, _ := st.Get(key)
			require.False(t, has)
		}
	}
	var replayed []string
	for w := range st.IterateReadyWorkItems(ctx) {
		replayed = append(replayed, w)
		for _, key := range []uint64{1, 2, 3, 4} {
			has, ent := st.Get(key)
			require.True(t, has, "key %d must hit after the batch fetch", key)
			require.Equal(t, key, ent.ID)
		}
	}
	require.ElementsMatch(t, []string{"frame-A", "frame-B"}, replayed)
	require.Equal(t, 1, counting.queryCalls, "all four keys must land in one batched lookup")
}

// TestDeviceStoreCacheFetchErrorBackoff drives the fetch-error path: a
// failing executor marks the keys with the circuit-breaker error state;
// during the backoff window replays keep missing without issuing new
// queries.
func TestDeviceStoreCacheFetchErrorBackoff(t *testing.T) {
	failing := &failingExecutor{}
	ctx := context.Background()
	st := NewDeviceStore[string](failing, nil, DeviceStoreConfig{CacheCapacity: 8})

	for range st.WorkItem("w") {
		has, _ := st.Get(7)
		require.False(t, has)
	}
	replays := 0
	for range st.IterateRestWorkItems(ctx) {
		replays++
		has, _ := st.Get(7) // still failing: error state, inside the backoff window
		require.False(t, has)
	}
	require.Equal(t, 1, replays)
	require.Equal(t, 1, failing.queryCalls)

	// The key sits in the circuit-breaker cooling-off window: forcing
	// another round replays the pending work item but must not re-query.
	for range st.IterateRestWorkItems(ctx) {
		has, _ := st.Get(7)
		require.False(t, has)
	}
	require.Equal(t, 1, failing.queryCalls, "no re-fetch inside the error backoff")
}

// TestDeviceStoreLocalWritesInvalidateCache pins the SD4/SD5 hardening:
// Put / Delete / Commit on a key drop its cache entry, so a local write
// never leaves a stale L1 value behind.
func TestDeviceStoreLocalWritesInvalidateCache(t *testing.T) {
	local, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	ctx := context.Background()
	st := NewDeviceStore[string](local, nil, DeviceStoreConfig{CacheCapacity: 8})
	require.NoError(t, st.EnsureTable(ctx))

	t0 := time.Unix(1_600_000_000, 0).UTC()
	t1 := t0.Add(time.Hour)
	t2 := t0.Add(2 * time.Hour)

	require.NoError(t, st.Begin(1, t0).AddBattery(Battery{ID: 1, Charge: 9000}).Commit())
	_, err = st.Flush(ctx)
	require.NoError(t, err)

	for range st.WorkItem("warm") {
		_, _ = st.Get(1)
	}
	for range st.IterateRestWorkItems(ctx) {
		has, ent := st.Get(1)
		require.True(t, has)
		require.Equal(t, uint64(9000), ent.Battery.Val.Charge)
	}

	// A new version invalidates the entry; the refetch sees the new row.
	require.NoError(t, st.Put(1, t1).AddBattery(Battery{ID: 1, Charge: 100}).Commit())
	has, _ := st.Get(1)
	require.False(t, has, "local Put must invalidate the cached entry")
	_, err = st.Flush(ctx)
	require.NoError(t, err)
	for range st.IterateRestWorkItems(ctx) {
	}
	has, ent := st.Get(1)
	require.True(t, has)
	require.Equal(t, uint64(100), ent.Battery.Val.Charge)
	require.Equal(t, t1, ent.Ts)

	// A tombstone invalidates too; the refetched row is the tombstone
	// (Get is row-level and tombstone-blind by design — GetLatest is the
	// state-view read).
	require.NoError(t, st.Delete(1, t2))
	has, _ = st.Get(1)
	require.False(t, has, "local Delete must invalidate the cached entry")
	_, err = st.Flush(ctx)
	require.NoError(t, err)
	for range st.IterateRestWorkItems(ctx) {
	}
	has, ent = st.Get(1)
	require.True(t, has)
	require.Equal(t, deviceLifecycleTombstone, ent.Lifecycle)
	require.Empty(t, ent.Archetype())
	_, found, err := st.GetLatest(ctx, 1)
	require.NoError(t, err)
	require.False(t, found)
}
