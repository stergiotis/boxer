package example

import (
	"context"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/require"
)

// TestVerifySchemaReadsPastColumnsTheStoreDoesNotKnow pins the one thing
// that lets a store own a table and still index values its leeway
// attributes only encode: a table may carry derived columns added by ALTER
// after EnsureTable — a path's directory, its depth, a bit lifted out of a
// mode — and be read by the generated store unchanged.
//
// Reads select the generated columns by name and bind the readers to the
// result by name (ADR-0387), so no column beside the generated shape can
// shift the decode, whatever its kind: MATERIALIZED, ALIAS and EPHEMERAL
// columns, and stored ones too, pass the check and are reported as extra.
func TestVerifySchemaReadsPastColumnsTheStoreDoesNotKnow(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	st := NewDeviceStore(exec, nil, DeviceStoreConfig{})
	defer st.Close()
	require.NoError(t, st.EnsureTable(ctx))
	require.NoError(t, st.VerifySchema(ctx), "fresh table must match the generated schema")

	require.NoError(t, exec.Exec(ctx, `ALTER TABLE `+DeviceTableName+
		` ADD COLUMN derived_key UInt64 MATERIALIZED "id:id:u64:47::0:" * 2`))
	require.NoError(t, exec.Exec(ctx, `ALTER TABLE `+DeviceTableName+
		` ADD COLUMN derived_alias UInt64 ALIAS derived_key + 1`))
	require.NoError(t, exec.Exec(ctx, `ALTER TABLE `+DeviceTableName+
		` ADD COLUMN stored_extra UInt64 DEFAULT 0 FIRST`))
	v := st.JudgeSchema(ctx)
	require.Equal(t, recordstore.SchemaVerdictOk, v.Verdict, v.Summary())
	require.ElementsMatch(t, []string{"derived_key", "derived_alias", "stored_extra"}, v.Extra)

	// A stored column placed first is read past, not into: the round trip
	// still decodes.
	t0 := time.Unix(1_600_000_000, 0).UTC()
	require.NoError(t, st.Begin(ctx, 7, t0).AddBattery(Battery{ID: 7, Charge: 42}).Commit())
	_, err = st.Flush(ctx)
	require.NoError(t, err)
	ent, found, err := st.Latest(ctx, 7)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, uint64(42), ent.Battery.Val.Charge)
}

// TestReadsFailNamingAColumnTheTableLacks: a table older than the store —
// one lacking a column the store reads — fails the read (the server names
// the column; this executor keeps the server's text in a structured field),
// and the verdict names it and calls the table migratable.
func TestReadsFailNamingAColumnTheTableLacks(t *testing.T) {
	exec, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	st := NewDeviceStore(exec, nil, DeviceStoreConfig{})
	defer st.Close()
	require.NoError(t, st.EnsureTable(ctx))
	const gone = "tv:symbol:mrhp:mrhp:y:4:::0::data"
	require.NoError(t, exec.Exec(ctx, `ALTER TABLE `+DeviceTableName+` DROP COLUMN "`+gone+`"`))

	v := st.JudgeSchema(ctx)
	require.Equal(t, recordstore.SchemaVerdictMigratable, v.Verdict)
	require.Equal(t, []string{gone}, v.Missing)

	_, _, err = st.Latest(ctx, 1)
	require.Error(t, err)
	require.Error(t, v.Err())
	require.Contains(t, v.Summary(), gone)
}
