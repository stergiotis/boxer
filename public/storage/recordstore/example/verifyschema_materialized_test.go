package example

import (
	"context"
	"testing"

	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
	"github.com/stretchr/testify/require"
)

// TestVerifySchemaChecksTheReadersProjection pins the one thing that lets a
// store own a table and still index values its leeway attributes only encode.
//
// A store's decode is positional over `SELECT *`. ClickHouse does not widen
// that for MATERIALIZED, ALIAS or EPHEMERAL columns, so a table may carry
// derived columns added by ALTER after EnsureTable — a path's directory, its
// depth, a bit lifted out of a mode — and be decoded by the generated store
// unchanged. VerifySchema therefore describes the *projection*, not the table:
// DESCRIBE TABLE lists all three kinds, and before this it counted four
// columns the decode never sees and failed the table it was meant to bless.
// Measured on the ADR-0198 M0 trial (2026-08-19), where provisioning as
// designed and verifying as generated could not both hold.
//
// A DEFAULT column is deliberately NOT skipped: it is stored and it *is* in
// SELECT *, so it shifts the positional decode and must still be caught. That
// distinction is what makes describing the projection the right check rather
// than filtering DESCRIBE TABLE by column kind — the projection draws the line
// where the decode does, without a vocabulary of kinds to keep current.
func TestVerifySchemaChecksTheReadersProjection(t *testing.T) {
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
		` ADD COLUMN derived_ephemeral UInt64 EPHEMERAL 7`))
	require.NoError(t, st.VerifySchema(ctx),
		"MATERIALIZED, ALIAS and EPHEMERAL columns are absent from SELECT *, so they cannot shift the positional decode")

	// The guard still guards: a stored column added beside the generated shape
	// widens SELECT * and must be reported.
	require.NoError(t, exec.Exec(ctx, `ALTER TABLE `+DeviceTableName+
		` ADD COLUMN stored_extra UInt64 DEFAULT 0`))
	require.Error(t, st.VerifySchema(ctx),
		"a stored column is in SELECT * and shifts the decode; VerifySchema must not let it pass")
}
