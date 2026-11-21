package example

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

// landsThenFails inserts through the inner executor and then reports the
// first insert as failed: the ambiguous failure — the server took the rows,
// the client never heard — that a retry must not turn into duplicates.
type landsThenFails struct {
	*chexec.LocalExecutor
	failed bool
}

func (inst *landsThenFails) InsertArrowWith(ctx context.Context, table string, records []arrow.RecordBatch, settings recordstore.InsertSettings) (applied bool, err error) {
	applied, err = inst.LocalExecutor.InsertArrowWith(ctx, table, records, settings)
	if err == nil && !inst.failed {
		inst.failed = true
		err = errors.New("synthetic timeout after the insert landed")
	}
	return
}

// TestRetryOfALandedInsertIsDeduplicated: a table the store created keeps a
// deduplication window and carries the store's stamp; a Flush whose insert
// landed but reported failure is retried under the same token, and the
// table keeps one copy of the rows.
func TestRetryOfALandedInsertIsDeduplicated(t *testing.T) {
	local, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	exec := &landsThenFails{LocalExecutor: local}
	st := NewDeviceStore(exec, nil, DeviceStoreConfig{})
	defer st.Close()
	require.NoError(t, st.EnsureTable(ctx))

	v := st.JudgeSchema(ctx)
	require.Equal(t, recordstore.SchemaVerdictOk, v.Verdict, v.Summary())
	require.Equal(t, recordstore.StampAgeSame, v.Age, "the table carries the stamp of the schema that created it")
	require.True(t, v.DedupWindow)
	require.Empty(t, v.Notes)

	t0 := time.Unix(1_600_000_000, 0).UTC()
	require.NoError(t, st.Begin(ctx, 1, t0).AddBattery(Battery{ID: 1, Charge: 1}).Commit())
	require.NoError(t, st.Begin(ctx, 2, t0).AddBattery(Battery{ID: 2, Charge: 2}).Commit())
	_, err = st.Flush(ctx)
	require.Error(t, err)
	require.Equal(t, 2, countRows(t, ctx, local, DeviceTableName), "the first attempt landed")

	n, err := st.Flush(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	require.Equal(t, 2, countRows(t, ctx, local, DeviceTableName), "the retry carried the same token and was dropped")

	// A later insert of the same content under its own token is kept.
	require.NoError(t, st.Begin(ctx, 1, t0).AddBattery(Battery{ID: 1, Charge: 1}).Commit())
	require.NoError(t, st.Begin(ctx, 2, t0).AddBattery(Battery{ID: 2, Charge: 2}).Commit())
	_, err = st.Flush(ctx)
	require.NoError(t, err)
	require.Equal(t, 4, countRows(t, ctx, local, DeviceTableName))
}

// TestDDLTailGoesAheadOfTheStamp: a configured tail continues the
// statement's SETTINGS, ahead of the COMMENT clause ClickHouse wants last.
func TestDDLTailGoesAheadOfTheStamp(t *testing.T) {
	local, err := chexec.NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	st := NewDeviceStore(local, nil, DeviceStoreConfig{DDLTail: ", index_granularity = 128"})
	defer st.Close()
	require.NoError(t, st.EnsureTable(ctx))
	facts, err := recordstore.ReadTableFacts(ctx, local, DeviceTableName)
	require.NoError(t, err)
	require.Contains(t, facts.CreateQuery, "index_granularity = 128")
	require.Equal(t, DeviceTableStamp, facts.Comment)
}
