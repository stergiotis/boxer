package recordstoretest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/internal/example"
)

// A generated store over a backend that answers no catalog read judges its
// own table ok once the catalog executor has seen its CREATE TABLE.
func TestCatalogExecutorJudgesAStoresOwnTableOk(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer alloc.AssertSize(t, 0)
	ctx := context.Background()
	exec := NewCatalogExecutor(nil, alloc)
	st := example.NewDeviceStore(exec, alloc, example.DeviceStoreConfig{})
	defer st.Close()

	v := st.JudgeSchema(ctx)
	assert.Equal(t, recordstore.SchemaVerdictAbsent, v.Verdict, "nothing created yet")

	require.NoError(t, st.EnsureTable(ctx))
	v = st.JudgeSchema(ctx)
	require.Equal(t, recordstore.SchemaVerdictOk, v.Verdict, v.Summary())
	assert.Equal(t, recordstore.StampAgeSame, v.Age)
	assert.True(t, v.DedupWindow)
	assert.False(t, v.LayoutDiffers, v.Notes)
	require.NoError(t, st.VerifySchema(ctx))

	// A second EnsureTable keeps the table; a drop forgets it.
	require.NoError(t, st.EnsureTable(ctx))
	require.NoError(t, exec.Exec(ctx, "DROP TABLE IF EXISTS "+example.DeviceTableName))
	_, held := exec.Facts(example.DeviceTableName)
	assert.False(t, held)
}

func TestCatalogExecutorQualifiesNames(t *testing.T) {
	ctx := context.Background()
	exec := NewCatalogExecutor(nil, nil)
	require.NoError(t, exec.Exec(ctx, "CREATE TABLE IF NOT EXISTS db1.t (\n\t\"a\" UInt8 CODEC(ZSTD(3))\n) ENGINE = MergeTree()\nORDER BY (\"a\")\nCOMMENT 'x\\'y'"))
	facts, err := recordstore.ReadTableFacts(ctx, exec, "db1.t")
	require.NoError(t, err)
	assert.True(t, facts.Exists)
	assert.Equal(t, "MergeTree", facts.Engine)
	assert.Equal(t, `("a")`, facts.SortingKey)
	assert.Equal(t, "x'y", facts.Comment)
	require.Len(t, facts.Columns, 1)
	assert.Equal(t, recordstore.LiveColumn{Name: "a", Type: "UInt8", Codec: "ZSTD(3)"}, facts.Columns[0])

	facts, err = recordstore.ReadTableFacts(ctx, exec, "t")
	require.NoError(t, err)
	assert.False(t, facts.Exists, "an unqualified name is the default database")
}

func TestCatalogExecutorForwardsToInner(t *testing.T) {
	ctx := context.Background()
	var execs, inserts int
	inner := &HookExecutor{
		OnExec:        func(context.Context, string) error { execs++; return nil },
		OnInsertArrow: func(context.Context, string, []arrow.RecordBatch) error { inserts++; return nil },
	}
	exec := NewCatalogExecutor(inner, nil)
	require.NoError(t, exec.Exec(ctx, "CREATE DATABASE IF NOT EXISTS x"))
	require.NoError(t, exec.InsertArrow(ctx, "t", nil))
	_, err := exec.InsertArrowWith(ctx, "t", nil, recordstore.InsertSettings{DeduplicationToken: "tok"})
	require.NoError(t, err)
	assert.Equal(t, 1, execs)
	assert.Equal(t, 2, inserts)
}

// A hook on either insert seam sees the inserts a store makes, whichever
// seam it chose.
func TestHookExecutorRoutesBothInsertSeams(t *testing.T) {
	ctx := context.Background()
	failed := false
	exec := &HookExecutor{
		Inner: NewCatalogExecutor(nil, nil),
		OnInsertArrow: func(context.Context, string, []arrow.RecordBatch) error {
			if !failed {
				failed = true
				return errors.New("synthetic")
			}
			return nil
		},
	}
	st := example.NewDeviceStore(exec, nil, example.DeviceStoreConfig{})
	defer st.Close()
	require.NoError(t, st.EnsureTable(ctx))
	require.NoError(t, st.VerifySchema(ctx), "the catalog reads reach the inner executor")
	t0 := time.Unix(1_600_000_000, 0).UTC()
	require.NoError(t, st.Begin(ctx, 1, t0).AddBattery(example.Battery{ID: 1, Charge: 1}).Commit())
	_, err := st.Flush(ctx)
	require.Error(t, err, "the store inserts through InsertArrowWith, and the InsertArrow hook still sees it")
	assert.True(t, failed)

	applied, err := exec.InsertArrowWith(ctx, "t", nil, recordstore.InsertSettings{DeduplicationToken: "tok"})
	require.NoError(t, err)
	assert.False(t, applied, "an InsertArrow hook cannot apply settings")
}
