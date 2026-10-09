//go:build integration

package storeexec

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/db/clickhouse/logcomment"
	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/storage/recordstore"
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

// TestStamp_ReachesQueryLog_LiveServer is the ADR-0295 §SD3 acceptance: a
// QueryArrow and an InsertArrow issued with a call identity on the context
// appear in system.query_log carrying it as log_comment, and under the batch
// id the observing executor reported. A statement's own SETTINGS log_comment
// wins over the stamp, which is what the queryrunsd self-capture exclusion
// relies on.
func TestStamp_ReachesQueryLog_LiveServer(t *testing.T) {
	exec, alloc := liveExecutor(t)
	const table = "default.storeexec_stamp_test"
	bare := context.Background()
	require.NoError(t, exec.Exec(bare, "DROP TABLE IF EXISTS "+table))
	require.NoError(t, exec.Exec(bare, "CREATE TABLE "+table+" (k UInt64) ENGINE = MergeTree() ORDER BY k"))
	t.Cleanup(func() { _ = exec.Exec(context.Background(), "DROP TABLE IF EXISTS "+table) })

	corr := "stamp-it-" + string(recordstore.NewBatchId())
	ci := callident.CallIdentity{
		Origin: callident.Origin{Run: "run-it", App: "storeexec.it", Instance: 5},
		Claims: callident.Claims{Principal: "p:it", Purpose: "acceptance", Correlation: corr},
	}
	ctx := callident.WithCallIdentity(bare, ci)
	var events []recordstore.CallEvent
	observed := recordstore.ObserveExecutor(exec, recordstore.ObserverFunc(func(ev recordstore.CallEvent) error {
		events = append(events, ev)
		return nil
	}))

	b := array.NewUint64Builder(memory.NewGoAllocator())
	b.AppendValues([]uint64{1, 2}, nil)
	col := b.NewArray()
	rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "k", Type: arrow.PrimitiveTypes.Uint64}}, nil), []arrow.Array{col}, 2)
	require.NoError(t, observed.InsertArrow(ctx, table, []arrow.RecordBatch{rec}))
	rec.Release()
	col.Release()
	b.Release()
	for batch, err := range observed.QueryArrow(ctx, "SELECT k FROM "+table+" ORDER BY k") {
		require.NoError(t, err)
		batch.Release()
	}
	ownTag := corr + "-own"
	for batch, err := range exec.QueryArrow(ctx, "SELECT 1 SETTINGS log_comment='"+ownTag+"'") {
		require.NoError(t, err)
		batch.Release()
	}
	require.Len(t, events, 2)

	require.NoError(t, exec.Exec(bare, "SYSTEM FLUSH LOGS"))
	type row struct{ kind, comment string }
	read := func(where string) (rows []row) {
		sql := "SELECT query_kind, log_comment FROM system.query_log WHERE type = 'QueryFinish' AND event_date >= yesterday() AND " +
			where + " ORDER BY event_time_microseconds SETTINGS output_format_arrow_string_as_string=1, output_format_arrow_low_cardinality_as_dictionary=0"
		for batch, err := range exec.QueryArrow(bare, sql) {
			require.NoError(t, err)
			kinds := batch.Column(0).(*array.String)
			comments := batch.Column(1).(*array.String)
			for i := range int(batch.NumRows()) {
				rows = append(rows, row{kinds.Value(i), comments.Value(i)})
			}
			batch.Release()
		}
		return
	}

	// A server with async_insert on logs an AsyncInsertFlush row beside the
	// Insert, carrying the same stamp; the kinds under test are the two below.
	rows := read("query_kind IN ('Insert', 'Select') AND log_comment LIKE '%" + corr + "\"%'")
	require.Len(t, rows, 2, "the insert and the select, each stamped")
	assert.Equal(t, "Insert", rows[0].kind)
	assert.Equal(t, "Select", rows[1].kind)
	for i, r := range rows {
		st, ok := logcomment.Parse(r.comment)
		require.True(t, ok, r.comment)
		assert.Equal(t, logcomment.FromCallIdentity(ci, string(events[i].BatchId)), st,
			"row %d carries the identity and the batch id its event reported", i)
	}

	own := read("log_comment = '" + ownTag + "'")
	assert.Len(t, own, 1, "the statement's own SETTINGS log_comment wins over the stamp")
	alloc.AssertSize(t, 0)
}
