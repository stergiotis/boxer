//go:build integration

package storeexec

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The scratch table this lane owns. Named for the package so a concurrent
// member of the lane cannot collide with it on the shared server.
const testTable = "default.storeexec_roundtrip_test"

// liveExecutor returns an Executor over the CLICKHOUSE_* server, skipping when
// it is unreachable — the lane's convention, so a machine without ClickHouse
// reports skips rather than failures.
func liveExecutor(t *testing.T) (*Executor, *memory.CheckedAllocator) {
	t.Helper()
	cfg := chclient.ConfigFromEnv()
	client := chclient.New(cfg, nil)
	if err := client.Ping(context.Background()); err != nil {
		t.Skipf("ClickHouse not reachable at %s: %v", cfg.URL, err)
	}
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	exec, err := New(client, alloc)
	require.NoError(t, err)
	return exec, alloc
}

// TestExecutor_RoundTrip_LiveServer exercises all three verbs against a real
// server: DDL through Exec, an Arrow write through InsertArrow, and the
// decoded read back through QueryArrow.
func TestExecutor_RoundTrip_LiveServer(t *testing.T) {
	exec, alloc := liveExecutor(t)
	ctx := context.Background()

	require.NoError(t, exec.Exec(ctx, "DROP TABLE IF EXISTS "+testTable))
	require.NoError(t, exec.Exec(ctx,
		"CREATE TABLE "+testTable+" (k UInt64, v Int64) ENGINE = MergeTree() ORDER BY k"))
	t.Cleanup(func() {
		_ = exec.Exec(context.Background(), "DROP TABLE IF EXISTS "+testTable)
	})

	// Build one batch and hand it over; InsertArrow does not retain it, so the
	// release below is the caller's own.
	buildAlloc := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "k", Type: arrow.PrimitiveTypes.Uint64},
		{Name: "v", Type: arrow.PrimitiveTypes.Int64},
	}, nil)
	kb := array.NewUint64Builder(buildAlloc)
	vb := array.NewInt64Builder(buildAlloc)
	kb.AppendValues([]uint64{1, 2, 3}, nil)
	vb.AppendValues([]int64{10, 20, 30}, nil)
	ka, va := kb.NewArray(), vb.NewArray()
	rec := array.NewRecordBatch(schema, []arrow.Array{ka, va}, 3)
	require.NoError(t, exec.InsertArrow(ctx, testTable, []arrow.RecordBatch{rec}))
	rec.Release()
	ka.Release()
	va.Release()
	kb.Release()
	vb.Release()

	// Read back through the settings suffix the generated stores use, so the
	// SETTINGS-then-FORMAT ordering is exercised against the real grammar and
	// not only against the unit test's expectation.
	const settings = " SETTINGS output_format_arrow_string_as_string=1, output_format_arrow_low_cardinality_as_dictionary=0"
	var keys []uint64
	var values []int64
	for batch, err := range exec.QueryArrow(ctx, "SELECT k, v FROM "+testTable+" ORDER BY k"+settings) {
		require.NoError(t, err)
		ks := batch.Column(0).(*array.Uint64)
		vs := batch.Column(1).(*array.Int64)
		for i := range int(batch.NumRows()) {
			keys = append(keys, ks.Value(i))
			values = append(values, vs.Value(i))
		}
		batch.Release()
	}
	assert.Equal(t, []uint64{1, 2, 3}, keys)
	assert.Equal(t, []int64{10, 20, 30}, values)
	alloc.AssertSize(t, 0)
}

// TestQueryArrow_ZeroRows_LiveServer pins the shape the unit test's empty-body
// case is deliberately *not* about: a real zero-row result still carries its
// Arrow schema, so it decodes cleanly and simply yields no batch.
func TestQueryArrow_ZeroRows_LiveServer(t *testing.T) {
	exec, alloc := liveExecutor(t)
	n := 0
	for _, err := range exec.QueryArrow(context.Background(), "SELECT 1 AS a WHERE 0") {
		require.NoError(t, err)
		n++
	}
	assert.Zero(t, n)
	alloc.AssertSize(t, 0)
}

// TestExec_RejectsMultiStatement_LiveServer pins the precondition the package
// comment states. It is a server behaviour, not ours, so it belongs where a
// server upgrade that changed it would be noticed.
func TestExec_RejectsMultiStatement_LiveServer(t *testing.T) {
	exec, _ := liveExecutor(t)
	err := exec.Exec(context.Background(), "SELECT 1; SELECT 2;")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Multi-statements are not allowed",
		"the HTTP interface rejects the multi-statement DDL script a generated EnsureTable emits")
}
