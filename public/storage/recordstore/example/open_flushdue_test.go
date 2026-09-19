package example

import (
	"context"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/require"
)

// countRows reads the flushed row count of a table through the executor.
func countRows(t *testing.T, ctx context.Context, exec recordstore.ExecutorI, table string) (n int) {
	t.Helper()
	for rec, err := range exec.QueryArrow(ctx, "SELECT * FROM "+table) {
		require.NoError(t, err)
		n += int(rec.NumRows())
		rec.Release()
	}
	return
}

// TestDeviceStoreOpenAndFlushIfDue pins the two startup/streaming helpers
// (ADR-0100 Update 2026-09-18): OpenDeviceStore leaves a provisioned,
// verified store, and FlushIfDue inserts exactly when FlushEvery rows are
// buffered — five commits under FlushEvery 3 leave three rows durable and
// two buffered until the end-of-run Flush.
func TestDeviceStoreOpenAndFlushIfDue(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	t0 := time.Unix(1_600_000_000, 0).UTC()
	const table = "device_flushdue"

	st, err := OpenDeviceStore(ctx, exec, nil, DeviceStoreConfig{Table: table, FlushEvery: 3})
	require.NoError(t, err)
	defer st.Close()

	for i := range 5 {
		id := uint64(i + 1)
		require.NoError(t, st.Begin(id, t0).AddBattery(Battery{ID: id, Charge: 42}).Commit())
		_, err = st.FlushIfDue(ctx)
		require.NoError(t, err)
	}
	require.Equal(t, 3, countRows(t, ctx, exec, table), "the third commit made a flush due")
	require.Equal(t, 2, st.Buffered())

	n, err := st.Flush(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, 5, countRows(t, ctx, exec, table))
	require.Zero(t, st.Buffered())
}

// TestDeviceStoreFlushIfDueZeroIsNoOp: the zero FlushEvery keeps the
// pre-helper behaviour — nothing is inserted until Flush.
func TestDeviceStoreFlushIfDueZeroIsNoOp(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	t0 := time.Unix(1_600_000_000, 0).UTC()
	const table = "device_flushdue_zero"

	st, err := OpenDeviceStore(ctx, exec, nil, DeviceStoreConfig{Table: table})
	require.NoError(t, err)
	defer st.Close()

	for i := range 4 {
		id := uint64(i + 1)
		require.NoError(t, st.Begin(id, t0).AddBattery(Battery{ID: id, Charge: 1}).Commit())
		n, ferr := st.FlushIfDue(ctx)
		require.NoError(t, ferr)
		require.Zero(t, n)
	}
	require.Zero(t, countRows(t, ctx, exec, table))
	require.Equal(t, 4, st.Buffered())
}

// TestDeviceStoreOpenRefusesDrift: Open over a table of another shape fails
// with the schema-drift error instead of handing back a store whose
// positional decode would misread it.
func TestDeviceStoreOpenRefusesDrift(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	const table = "device_drifted"
	require.NoError(t, exec.Exec(ctx, "CREATE TABLE "+table+" (x UInt8) ENGINE = MergeTree() ORDER BY x"))

	st, err := OpenDeviceStore(ctx, exec, nil, DeviceStoreConfig{Table: table})
	require.Error(t, err)
	require.ErrorContains(t, err, "schema drift")
	require.Nil(t, st)
}
