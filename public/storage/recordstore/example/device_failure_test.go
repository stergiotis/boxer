package example

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/require"
)

// flakyExecutor fails the first failFirst InsertArrow calls, then delegates.
// It drives the write-side failure paths against a real clickhouse-local.
type flakyExecutor struct {
	inner     recordstore.ExecutorI
	failFirst int
	inserts   int
}

func (inst *flakyExecutor) Exec(ctx context.Context, sql string) error {
	return inst.inner.Exec(ctx, sql)
}

func (inst *flakyExecutor) QueryArrow(ctx context.Context, sql string) ([]arrow.RecordBatch, error) {
	return inst.inner.QueryArrow(ctx, sql)
}

func (inst *flakyExecutor) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) error {
	inst.inserts++
	if inst.inserts <= inst.failFirst {
		return errors.New("synthetic insert failure")
	}
	return inst.inner.InsertArrow(ctx, table, records)
}

func newFlakyStore(t *testing.T, failFirst int) (st *DeviceStore, ctx context.Context) {
	t.Helper()
	local, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	ctx = context.Background()
	st = NewDeviceStore(&flakyExecutor{inner: local, failFirst: failFirst}, nil, DeviceStoreConfig{})
	require.NoError(t, st.EnsureTable(ctx))
	return
}

// TestDeviceStoreFlushRetryShipsPending pins the Flush failure contract: a
// failed insert retains the transferred records, and the retry ships them
// (no silent loss, no lying n).
func TestDeviceStoreFlushRetryShipsPending(t *testing.T) {
	st, ctx := newFlakyStore(t, 1)
	t0 := time.Unix(1_600_000_000, 0).UTC()
	require.NoError(t, st.Begin(1, t0).AddBattery(Battery{ID: 1, Charge: 42}).Commit())

	_, err := st.Flush(ctx)
	require.Error(t, err, "first flush fails (synthetic)")
	require.Equal(t, 1, st.Buffered(), "the row is still owed")

	n, err := st.Flush(ctx)
	require.NoError(t, err, "retry must succeed")
	require.Equal(t, 1, n)
	require.Equal(t, 0, st.Buffered())

	_, found, err := st.Latest(ctx, 1)
	require.NoError(t, err)
	require.True(t, found, "the retried flush must have shipped the row")
}

// TestDeviceStoreDiscardPending pins the op-scoped alternative: after a
// failed Flush, DiscardPending drops the rows and the store carries on
// clean — nothing ships later behind the caller's back.
func TestDeviceStoreDiscardPending(t *testing.T) {
	st, ctx := newFlakyStore(t, 1)
	t0 := time.Unix(1_600_000_000, 0).UTC()
	require.NoError(t, st.Begin(1, t0).AddBattery(Battery{ID: 1, Charge: 42}).Commit())

	_, err := st.Flush(ctx)
	require.Error(t, err)
	st.DiscardPending()
	require.Equal(t, 0, st.Buffered())

	n, err := st.Flush(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, n, "nothing may ship after a discard")
	_, found, err := st.Latest(ctx, 1)
	require.NoError(t, err)
	require.False(t, found)

	// The store remains fully usable.
	require.NoError(t, st.Begin(2, t0).AddBattery(Battery{ID: 2, Charge: 7}).Commit())
	n, err = st.Flush(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

// TestDeviceStoreCommitErrorRecovers pins the wedge fix: an unbalanced
// Begin poisons the open frame, the next Commit reports it once and rolls
// the frame back, and everything after that works.
func TestDeviceStoreCommitErrorRecovers(t *testing.T) {
	st, ctx := newFlakyStore(t, 0)
	t0 := time.Unix(1_600_000_000, 0).UTC()

	st.Begin(1, t0) // never committed — a caller bug

	// The second Begin lands in the still-open frame; its Commit surfaces
	// the accumulated state error and rolls the poisoned frame back.
	require.Error(t, st.Begin(2, t0).AddBattery(Battery{ID: 2, Charge: 1}).Commit())

	// From here on the store is healthy again.
	require.NoError(t, st.Begin(3, t0).AddBattery(Battery{ID: 3, Charge: 2}).Commit())
	n, err := st.Flush(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	_, found, err := st.Latest(ctx, 3)
	require.NoError(t, err)
	require.True(t, found)

	// Same recovery through the state-view Delete.
	st.Begin(4, t0)
	require.Error(t, st.Delete(9, t0), "Delete inside an open frame must error")
	require.NoError(t, st.Delete(9, t0), "…and must work right after the rollback")
}

// TestDeviceStoreBuilderRollback pins the explicit abandon verb.
func TestDeviceStoreBuilderRollback(t *testing.T) {
	st, ctx := newFlakyStore(t, 0)
	t0 := time.Unix(1_600_000_000, 0).UTC()

	require.NoError(t, st.Begin(1, t0).AddBattery(Battery{ID: 1, Charge: 42}).Rollback())
	require.Equal(t, 0, st.Buffered())

	require.NoError(t, st.Begin(2, t0).AddBattery(Battery{ID: 2, Charge: 7}).Commit())
	n, err := st.Flush(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	_, found, err := st.Latest(ctx, 1)
	require.NoError(t, err)
	require.False(t, found, "the rolled-back entity must not exist")
}

// TestDeviceStoreFlushOpenFrameKeepsPending pins that a Flush refused
// because of an open frame does not touch the retained records.
func TestDeviceStoreFlushOpenFrameKeepsPending(t *testing.T) {
	st, ctx := newFlakyStore(t, 1)
	t0 := time.Unix(1_600_000_000, 0).UTC()
	require.NoError(t, st.Begin(1, t0).AddBattery(Battery{ID: 1, Charge: 42}).Commit())
	_, err := st.Flush(ctx)
	require.Error(t, err, "synthetic insert failure retains the records")

	b := st.Begin(2, t0) // open frame blocks the next Flush
	_, err = st.Flush(ctx)
	require.Error(t, err, "flush with an open frame must refuse")

	require.NoError(t, b.Rollback())
	n, err := st.Flush(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n, "the retained row ships once the frame is gone")
	_, found, err := st.Latest(ctx, 1)
	require.NoError(t, err)
	require.True(t, found)
}

// TestDeviceStoreNoStaleRecacheBetweenCommitAndFlush pins the dirty-key
// suppression: a Get between Commit and Flush must not re-cache the
// pre-write row (the review's probe-3 scenario, inverted).
func TestDeviceStoreNoStaleRecacheBetweenCommitAndFlush(t *testing.T) {
	st, ctx := newFlakyStore(t, 0)
	c := NewDeviceCache[string](st, DeviceCacheConfig{Capacity: 8})
	t0 := time.Unix(1_600_000_000, 0).UTC()
	t1 := t0.Add(time.Hour)

	require.NoError(t, st.Begin(1, t0).AddBattery(Battery{ID: 1, Charge: 1}).Commit())
	_, err := st.Flush(ctx)
	require.NoError(t, err)

	// New version committed but not flushed; a fetch in this window sees
	// the old row in ClickHouse and must refuse to cache it.
	require.NoError(t, st.Put(1, t1).AddBattery(Battery{ID: 1, Charge: 2}).Commit())
	_, has := c.Get(1)
	require.False(t, has)
	for range c.IterateRestWorkItems(ctx) {
	}
	_, has = c.Get(1)
	require.False(t, has, "the pre-write row must not enter the cache while the key is dirty")

	_, err = st.Flush(ctx)
	require.NoError(t, err)
	for range c.IterateRestWorkItems(ctx) {
	}
	ent, has := c.Get(1)
	require.True(t, has)
	require.Equal(t, uint64(2), ent.Battery.Val.Charge, "post-flush fetch must serve the new version")
	require.Equal(t, t1, ent.Ts)
}

// TestDeviceStoreIngestRejectsDuplicateKeys: duplicates within one call
// would tie on Order — the gate returns ErrDuplicateIngestKey; the rows
// buffered before the duplicate stay buffered (DiscardPending drops
// them).
func TestDeviceStoreIngestRejectsDuplicateKeys(t *testing.T) {
	st, _ := newFlakyStore(t, 0)
	defer st.Close()
	err := st.IngestBattery(time.Unix(1_600_000_000, 0).UTC(), []Battery{
		{ID: 1, Charge: 1}, {ID: 2, Charge: 2}, {ID: 1, Charge: 3},
	})
	require.ErrorIs(t, err, recordstore.ErrDuplicateIngestKey)
	require.Equal(t, 2, st.Buffered(), "rows before the duplicate stay buffered")
	st.DiscardPending()
}

// TestDeviceStoreVerifySchemaDetectsDrift: EnsureTable is IF NOT EXISTS
// and the decode is positional, so drift between regenerated code and a
// live table must be caught explicitly — VerifySchema is that gate.
func TestDeviceStoreVerifySchemaDetectsDrift(t *testing.T) {
	local, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	ctx := context.Background()
	st := NewDeviceStore(local, nil, DeviceStoreConfig{})
	defer st.Close()
	require.NoError(t, st.EnsureTable(ctx))
	require.NoError(t, st.VerifySchema(ctx))

	require.NoError(t, local.Exec(ctx, "ALTER TABLE "+DeviceTableName+" ADD COLUMN drift UInt8"))
	require.ErrorContains(t, st.VerifySchema(ctx), "schema drift")
}

// TestDeviceStoreCloseReleases: under a checked allocator the store's
// long-lived Arrow builder and any retained pending records must be
// fully released by Close.
func TestDeviceStoreCloseReleases(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	// failFirst with no inner: the insert fails before any backend is
	// touched, retaining the transferred records for Close to release.
	st := NewDeviceStore(&flakyExecutor{failFirst: 1}, alloc, DeviceStoreConfig{})
	t0 := time.Unix(1_600_000_000, 0).UTC()
	require.NoError(t, st.Begin(1, t0).AddBattery(Battery{ID: 1, Charge: 1}).Commit())
	_, err := st.Flush(context.Background())
	require.Error(t, err, "synthetic insert failure retains the records")
	st.Close()
	alloc.AssertSize(t, 0)
}
