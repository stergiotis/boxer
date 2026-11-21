package example

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// TestDeviceStoreReadTable pins <Store>StoreConfig.ReadTable (ADR-0296
// §SD8): writes go to Table, every read and the schema check go to
// ReadTable, and ReadTable empty reads the written table. Two scratch
// tables over one executor prove the split: a row written through the
// store is absent on its read side until a copy lands there, and a row
// placed on the read side alone is what the store returns.
func TestDeviceStoreReadTable(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	t0 := time.Unix(1_600_000_000, 0).UTC()

	// Provision both tables through stores that write to each.
	wst := NewDeviceStore(exec, nil, DeviceStoreConfig{Table: "device_w"})
	defer wst.Close()
	require.NoError(t, wst.EnsureTable(ctx))
	rst := NewDeviceStore(exec, nil, DeviceStoreConfig{Table: "device_r"})
	defer rst.Close()
	require.NoError(t, rst.EnsureTable(ctx))

	split := NewDeviceStore(exec, nil, DeviceStoreConfig{Table: "device_w", ReadTable: "device_r"})
	defer split.Close()
	require.NoError(t, split.VerifySchema(ctx), "VerifySchema describes the read table")
	require.NoError(t, split.Begin(7, t0).AddBattery(Battery{ID: 7, Charge: 42}).Commit())
	_, err = split.Flush(ctx)
	require.NoError(t, err)

	_, found, err := split.Latest(ctx, 7)
	require.NoError(t, err)
	require.False(t, found, "a row written to Table is not read from ReadTable until it lands there")
	_, found, err = wst.Latest(ctx, 7)
	require.NoError(t, err)
	require.True(t, found, "the write went to Table")

	require.NoError(t, exec.Exec(ctx, "INSERT INTO device_r SELECT * FROM device_w"))
	ent, found, err := split.Latest(ctx, 7)
	require.NoError(t, err)
	require.True(t, found, "the copy on ReadTable is what the split store reads")
	require.EqualValues(t, 42, ent.Battery.Val.Charge)

	n := 0
	for _, serr := range split.ScanBattery(ctx, recordstore.ScanOpts{}) {
		require.NoError(t, serr)
		n++
	}
	require.Equal(t, 1, n, "scans read ReadTable")
}

// TestDeviceStoreReadTableRefusesMalformed: ReadTable is spliced into SQL
// unquoted like Table, so the same shapes are refused at construction.
func TestDeviceStoreReadTableRefusesMalformed(t *testing.T) {
	for _, bad := range []string{"a.b.c", "my table", "x;DROP TABLE y", "`q`"} {
		require.Panics(t, func() {
			NewDeviceStore(nil, nil, DeviceStoreConfig{ReadTable: bad})
		}, "%q must be refused", bad)
	}
}
