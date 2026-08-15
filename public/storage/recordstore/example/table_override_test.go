package example

import (
	"context"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/require"
)

// TestDeviceStoreTableOverride pins the <Store>StoreConfig.Table override
// end to end: with it set, EnsureTable provisions the override (and its
// database when qualified), VerifySchema describes it, writes land in it
// and reads come back from it — and the baked DeviceTableName is never
// touched. Two stores over one executor prove the isolation directly: a
// row written through the override is invisible to a store on the baked
// name, which is what a scratch table for a test needs.
func TestDeviceStoreTableOverride(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse-local unavailable: %v", err)
	}
	ctx := context.Background()
	t0 := time.Unix(1_600_000_000, 0).UTC()

	for _, override := range []string{"device_scratch", "scratchdb.device"} {
		t.Run(override, func(t *testing.T) {
			over := NewDeviceStore(exec, nil, DeviceStoreConfig{Table: override})
			defer over.Close()
			require.NoError(t, over.EnsureTable(ctx))
			require.NoError(t, over.VerifySchema(ctx), "the override table must carry the generated schema")
			require.NoError(t, over.Begin(7, t0).AddBattery(Battery{ID: 7, Charge: 42}).Commit())
			_, err = over.Flush(ctx)
			require.NoError(t, err)

			ent, found, err := over.Latest(ctx, 7)
			require.NoError(t, err)
			require.True(t, found, "the row must read back from the override table")
			require.True(t, ent.Battery.Has)
			require.EqualValues(t, 42, ent.Battery.Val.Charge)

			// The baked table is untouched: not provisioned by the override
			// store, and empty once a baked-name store provisions it.
			require.Error(t, exec.Exec(ctx, "SELECT count() FROM "+DeviceTableName),
				"EnsureTable under an override must not create the baked table")
			baked := NewDeviceStore(exec, nil, DeviceStoreConfig{})
			defer baked.Close()
			require.NoError(t, baked.EnsureTable(ctx))
			_, found, err = baked.Latest(ctx, 7)
			require.NoError(t, err)
			require.False(t, found, "a row written through the override must be invisible on the baked table")
			require.NoError(t, exec.Exec(ctx, "DROP TABLE "+DeviceTableName))
		})
	}
}

// TestDeviceStoreTableOverrideRefusesMalformed: the override is spliced
// into SQL unquoted, so a non-identifier is refused at construction, not
// discovered as a syntax error on the first statement.
func TestDeviceStoreTableOverrideRefusesMalformed(t *testing.T) {
	for _, bad := range []string{"a.b.c", "my table", "x;DROP TABLE y", "`q`"} {
		require.Panics(t, func() {
			NewDeviceStore(nil, nil, DeviceStoreConfig{Table: bad})
		}, "%q must be refused", bad)
	}
}
