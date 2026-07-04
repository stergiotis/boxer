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
	st := NewDeviceStore(counting, nil, DeviceStoreConfig{})
	c := NewDeviceCache[string](st, DeviceCacheConfig{
		Capacity:      16,
		FetchCriteria: caching.FetchCriteria{MinKeys: 4},
	})
	require.NoError(t, st.EnsureTable(ctx))

	t0 := time.Unix(1_600_000_000, 0).UTC()
	for id := uint64(1); id <= 4; id++ {
		require.NoError(t, st.Begin(id, t0).AddIdentity(Identity{ID: id, Status: "IDLE"}).Commit())
	}
	_, err = st.Flush(ctx)
	require.NoError(t, err)

	for range c.WorkItem("frame-A") {
		for _, key := range []uint64{1, 2} {
			_, has := c.Get(key)
			require.False(t, has)
		}
	}
	for range c.IterateReadyWorkItems(ctx) {
		t.Fatal("2 queued keys are below MinKeys=4 — nothing may be ready")
	}
	require.Equal(t, 0, counting.queryCalls, "no fetch below the Min threshold")

	for range c.WorkItem("frame-B") {
		for _, key := range []uint64{3, 4} {
			_, has := c.Get(key)
			require.False(t, has)
		}
	}
	var replayed []string
	for w := range c.IterateReadyWorkItems(ctx) {
		replayed = append(replayed, w)
		for _, key := range []uint64{1, 2, 3, 4} {
			ent, has := c.Get(key)
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
	st := NewDeviceStore(failing, nil, DeviceStoreConfig{})
	c := NewDeviceCache[string](st, DeviceCacheConfig{Capacity: 8})

	for range c.WorkItem("w") {
		_, has := c.Get(7)
		require.False(t, has)
	}
	replays := 0
	for range c.IterateRestWorkItems(ctx) {
		replays++
		_, has := c.Get(7) // still failing: error state, inside the backoff window
		require.False(t, has)
	}
	require.Equal(t, 1, replays)
	require.Equal(t, 1, failing.queryCalls)

	// The key sits in the circuit-breaker cooling-off window: forcing
	// another round replays the pending work item but must not re-query.
	for range c.IterateRestWorkItems(ctx) {
		_, has := c.Get(7)
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
	st := NewDeviceStore(local, nil, DeviceStoreConfig{})
	c := NewDeviceCache[string](st, DeviceCacheConfig{Capacity: 8})
	require.NoError(t, st.EnsureTable(ctx))

	t0 := time.Unix(1_600_000_000, 0).UTC()
	t1 := t0.Add(time.Hour)
	t2 := t0.Add(2 * time.Hour)

	require.NoError(t, st.Begin(1, t0).AddBattery(Battery{ID: 1, Charge: 9000}).Commit())
	_, err = st.Flush(ctx)
	require.NoError(t, err)

	for range c.WorkItem("warm") {
		_, _ = c.Get(1)
	}
	for range c.IterateRestWorkItems(ctx) {
		ent, has := c.Get(1)
		require.True(t, has)
		require.Equal(t, uint64(9000), ent.Battery.Val.Charge)
	}

	// A new version invalidates the entry; the refetch sees the new row.
	require.NoError(t, st.Put(1, t1).AddBattery(Battery{ID: 1, Charge: 100}).Commit())
	_, has := c.Get(1)
	require.False(t, has, "local Put must invalidate the cached entry")
	_, err = st.Flush(ctx)
	require.NoError(t, err)
	for range c.IterateRestWorkItems(ctx) {
	}
	ent, has := c.Get(1)
	require.True(t, has)
	require.Equal(t, uint64(100), ent.Battery.Val.Charge)
	require.Equal(t, t1, ent.Ts)

	// A tombstone invalidates too; the refetched row is the tombstone
	// (Get is row-level and tombstone-blind by design — GetLatest is the
	// state-view read).
	require.NoError(t, st.Delete(1, t2))
	_, has = c.Get(1)
	require.False(t, has, "local Delete must invalidate the cached entry")
	_, err = st.Flush(ctx)
	require.NoError(t, err)
	for range c.IterateRestWorkItems(ctx) {
	}
	ent, has = c.Get(1)
	require.True(t, has)
	require.Equal(t, recordstore.LifecycleTombstone, ent.Lifecycle)
	require.Empty(t, ent.Archetype())
	_, found, err := st.GetLatest(ctx, 1)
	require.NoError(t, err)
	require.False(t, found)
}

// TestDeviceCacheLatestAndStaleness pins the cached state-view reads and
// the external-writer staleness controls (ADR-0100 Update): GetLatest is
// exact under the local single writer, tombstones read as absent,
// MarkStale enables stale-while-revalidate via GetLatestAcceptStale, and
// Invalidate / InvalidateAll drop entries on the caller's signal.
func TestDeviceCacheLatestAndStaleness(t *testing.T) {
	local, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	counting := &countingExecutor{inner: local}
	ctx := context.Background()
	st := NewDeviceStore(counting, nil, DeviceStoreConfig{})
	require.NoError(t, st.EnsureTable(ctx))
	c := NewDeviceCache[string](st, DeviceCacheConfig{Capacity: 8})

	t0 := time.Unix(1_600_000_000, 0).UTC()
	t1 := t0.Add(time.Hour)
	t2 := t0.Add(2 * time.Hour)

	require.NoError(t, st.Put(1, t0).AddBattery(Battery{ID: 1, Charge: 9000}).Commit())
	_, err = st.Flush(ctx)
	require.NoError(t, err)

	// Warm the entry, then read it through the cached state view.
	for range c.WorkItem("warm") {
		_, _ = c.Get(1)
	}
	for range c.IterateRestWorkItems(ctx) {
	}
	ent, found := c.GetLatest(1)
	require.True(t, found)
	require.Equal(t, uint64(9000), ent.Battery.Val.Charge)

	// Local single writer: the write hook evicts, the refetch serves the
	// new version — GetLatest is exact without any staleness signal.
	require.NoError(t, st.Put(1, t1).AddBattery(Battery{ID: 1, Charge: 100}).Commit())
	_, err = st.Flush(ctx)
	require.NoError(t, err)
	_, found = c.GetLatest(1)
	require.False(t, found, "local write must evict the cached entry")
	for range c.IterateRestWorkItems(ctx) {
	}
	ent, found = c.GetLatest(1)
	require.True(t, found)
	require.Equal(t, uint64(100), ent.Battery.Val.Charge)

	// External-writer signal: MarkStale keeps serving the old value on
	// the accept-stale read while the strict read queues the refetch.
	before := counting.queryCalls
	c.MarkStale(1)
	ent, found, stale := c.GetLatestAcceptStale(1)
	require.True(t, found)
	require.True(t, stale)
	require.Equal(t, uint64(100), ent.Battery.Val.Charge)
	for range c.IterateRestWorkItems(ctx) {
	}
	require.Equal(t, before+1, counting.queryCalls, "MarkStale must force exactly one refetch")
	ent, found, stale = c.GetLatestAcceptStale(1)
	require.True(t, found)
	require.False(t, stale, "refetched entry is fresh again")
	require.Equal(t, uint64(100), ent.Battery.Val.Charge)

	// Tombstone: the cached state view reads it as absent while the
	// row-level Get still surfaces the tombstone row.
	require.NoError(t, st.Delete(1, t2))
	_, err = st.Flush(ctx)
	require.NoError(t, err)
	for range c.WorkItem("reload") {
		_, _ = c.Get(1)
	}
	for range c.IterateRestWorkItems(ctx) {
	}
	row, found := c.Get(1)
	require.True(t, found)
	require.Equal(t, recordstore.LifecycleTombstone, row.Lifecycle)
	_, found = c.GetLatest(1)
	require.False(t, found, "tombstone reads as absent through the state view")
	_, found, _ = c.GetLatestAcceptStale(1)
	require.False(t, found)

	// Invalidate / InvalidateAll drop entries on the caller's signal.
	c.Invalidate(1)
	_, found = c.Get(1)
	require.False(t, found)
	for range c.IterateRestWorkItems(ctx) {
	}
	_, found = c.Get(1)
	require.True(t, found)
	c.InvalidateAll()
	_, found = c.Get(1)
	require.False(t, found, "InvalidateAll drops every entry")
	for range c.IterateRestWorkItems(ctx) {
	}
	_, found = c.Get(1)
	require.True(t, found, "the rebuilt cache fetches and serves again")
}

// TestDeviceCacheGetFetch pins the single-lookup read: a miss fetches
// immediately and caches, an absent key is found=false with err=nil
// (authoritative), and a dirty-window fetch serves the flushed row
// without re-caching it.
func TestDeviceCacheGetFetch(t *testing.T) {
	local, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	counting := &countingExecutor{inner: local}
	ctx := context.Background()
	st := NewDeviceStore(counting, nil, DeviceStoreConfig{})
	defer st.Close()
	c := NewDeviceCache[struct{}](st, DeviceCacheConfig{Capacity: 8})
	require.NoError(t, st.EnsureTable(ctx))

	t0 := time.Unix(1_600_000_000, 0).UTC()
	require.NoError(t, st.Begin(1, t0).AddBattery(Battery{ID: 1, Charge: 7}).Commit())
	_, err = st.Flush(ctx)
	require.NoError(t, err)

	// Miss → immediate fetch → cached.
	ent, found, err := c.GetFetch(ctx, 1)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, uint64(7), ent.Battery.Val.Charge)
	queries := counting.queryCalls
	_, hit := c.Get(1)
	require.True(t, hit, "GetFetch must have cached the fetched row")
	require.Equal(t, queries, counting.queryCalls, "the follow-up Get must not query")

	// Absent key: authoritative found=false with err=nil.
	_, found, err = c.GetFetch(ctx, 99)
	require.NoError(t, err)
	require.False(t, found)

	// Dirty window: the flushed row is served but must not re-enter the
	// cache while the key has an unflushed local write.
	require.NoError(t, st.Put(1, t0.Add(time.Hour)).AddBattery(Battery{ID: 1, Charge: 8}).Commit())
	ent, found, err = c.GetFetch(ctx, 1)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, uint64(7), ent.Battery.Val.Charge, "reads see only flushed rows")
	_, hit = c.Get(1)
	require.False(t, hit, "a dirty key must not re-enter the cache")
	st.DiscardPending()
}

// TestDeviceCacheGetFetchError: fetch failures surface as errors instead
// of reading as authoritative misses (the plain Get swallows them).
func TestDeviceCacheGetFetchError(t *testing.T) {
	failing := &failingExecutor{}
	ctx := context.Background()
	st := NewDeviceStore(failing, nil, DeviceStoreConfig{})
	defer st.Close()
	c := NewDeviceCache[struct{}](st, DeviceCacheConfig{Capacity: 8})
	_, _, err := c.GetFetch(ctx, 7)
	require.Error(t, err)
}
