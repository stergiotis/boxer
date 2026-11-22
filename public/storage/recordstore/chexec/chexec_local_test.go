package chexec

import (
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/logcomment"
	"github.com/stergiotis/boxer/public/identity/callident"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// The context's identity reaches the process as its log_comment setting
// (ADR-0295 §SD3), and a bare context sets none.
func TestLocalExecutor_StampsLogComment(t *testing.T) {
	exec, err := NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	read := func(ctx context.Context) (lc string) {
		const sql = "SELECT getSetting('log_comment') AS lc SETTINGS output_format_arrow_string_as_string=1"
		for rec, qerr := range exec.QueryArrow(ctx, sql) {
			require.NoError(t, qerr)
			lc = rec.Column(0).(*array.String).Value(0)
			rec.Release()
		}
		return
	}
	assert.Equal(t, "", read(context.Background()))

	ci := callident.CallIdentity{Claims: callident.Claims{Principal: "p:local", Purpose: "test"}}
	ctx := recordstore.WithBatchId(callident.WithCallIdentity(context.Background(), ci), "B-local")
	st, ok := logcomment.Parse(read(ctx))
	require.True(t, ok)
	assert.Equal(t, logcomment.FromCallIdentity(ci, "B-local"), st)
}

// An insert into a table a transform is registered for lands in the
// transform's table, in its shape (ADR-0402 §SD2), as through the HTTP
// executor; one marked as already transformed lands where it names.
func TestLocalExecutor_AppliesTheRegisteredTransform(t *testing.T) {
	exec, err := NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	for _, stmt := range []string{
		"CREATE DATABASE scratch_chexec",
		"CREATE TABLE scratch_chexec.facts (v Int64) ENGINE = MergeTree ORDER BY v",
		"CREATE TABLE scratch_chexec.carrier (carried Int64) ENGINE = MergeTree ORDER BY carried",
	} {
		require.NoError(t, exec.Exec(ctx, stmt))
	}
	alloc := memory.NewGoAllocator()
	batch := func(name string, v int64) arrow.RecordBatch {
		b := array.NewInt64Builder(alloc)
		defer b.Release()
		b.Append(v)
		arr := b.NewArray()
		defer arr.Release()
		return array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: name, Type: arrow.PrimitiveTypes.Int64}}, nil), []arrow.Array{arr}, 1)
	}
	unregister := recordstore.RegisterInsertTransform("scratch_chexec.facts", recordstore.InsertTransformFunc(
		func(_ string, records []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
			return "scratch_chexec.carrier", []arrow.RecordBatch{batch("carried", records[0].Column(0).(*array.Int64).Value(0)+100)}, nil
		}))
	defer unregister()

	in := batch("v", 1)
	defer in.Release()
	require.NoError(t, exec.InsertArrow(ctx, "scratch_chexec.facts", []arrow.RecordBatch{in}))
	require.NoError(t, exec.InsertArrow(ctx, "`scratch_chexec`.`facts`", []arrow.RecordBatch{in}))
	require.ErrorIs(t, exec.InsertArrow(ctx, "facts", []arrow.RecordBatch{in}), recordstore.ErrUnqualifiedTransformedTable)
	require.NoError(t, exec.InsertArrow(recordstore.WithTransformApplied(ctx), "scratch_chexec.facts", []arrow.RecordBatch{in}))

	count := func(sql string) (n int64) {
		for rec, qerr := range exec.QueryArrow(ctx, sql) {
			require.NoError(t, qerr)
			n = int64(rec.Column(0).(*array.Uint64).Value(0))
			rec.Release()
		}
		return
	}
	assert.EqualValues(t, 2, count("SELECT count() FROM scratch_chexec.carrier WHERE carried = 101"))
	assert.EqualValues(t, 1, count("SELECT count() FROM scratch_chexec.facts"))
}

// The catalogue lists a view's columns whether or not a read of it is
// accepted: a view that casts to a LowCardinality over a number, created in
// a session that allowed it, is refused at every later read. The judgement
// reads the relation once and reports that; the same view with the setting
// closing its SELECT is ok.
func TestLocalExecutor_AViewWhoseReadIsRefusedIsIncompatible(t *testing.T) {
	exec, err := NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	const view = "CREATE OR REPLACE VIEW v AS SELECT CAST(x AS LowCardinality(UInt64)) AS x FROM t"
	require.NoError(t, exec.Exec(ctx, "CREATE TABLE t (x UInt64) ENGINE = MergeTree ORDER BY x"))
	require.NoError(t, exec.Exec(ctx, "SET allow_suspicious_low_cardinality_types = 1; "+view))
	c := recordstore.TableContract{Kind: "k", Table: "v", ColumnsOnly: true,
		Columns: []recordstore.ColumnSpec{{Name: "x", Type: "LowCardinality(UInt64)"}}}

	v := recordstore.JudgeTable(ctx, exec, c)
	require.Equal(t, recordstore.SchemaVerdictIncompatible, v.Verdict, v.Summary())
	assert.Contains(t, v.ReadError, "SUSPICIOUS_TYPE_FOR_LOW_CARDINALITY")
	assert.Contains(t, v.Summary(), "allow_suspicious_low_cardinality_types")
	assert.Empty(t, v.Missing, "the columns themselves are as declared")

	require.NoError(t, exec.Exec(ctx, view+" SETTINGS allow_suspicious_low_cardinality_types = 1"))
	v = recordstore.JudgeTable(ctx, exec, c)
	require.Equal(t, recordstore.SchemaVerdictOk, v.Verdict, v.Summary())
	assert.Empty(t, v.ReadError)

	table := recordstore.JudgeTable(ctx, exec, recordstore.TableContract{Kind: "k", Table: "t", ColumnsOnly: true,
		Columns: []recordstore.ColumnSpec{{Name: "x", Type: "UInt64"}}})
	require.Equal(t, recordstore.SchemaVerdictOk, table.Verdict, table.Summary())
}

// An insert names its batch's columns (ADR-0387): a column the table lacks
// is refused rather than dropped, which is what a newer build writing to an
// older table would otherwise lose without an error; a column the batch
// lacks takes the table's default, so an older build keeps writing to a
// migrated table.
func TestLocalExecutor_AnInsertNamesItsColumns(t *testing.T) {
	exec, err := NewLocalExecutor(t.TempDir(), nil)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	require.NoError(t, exec.Exec(ctx, "CREATE TABLE default.t (a UInt64, c UInt64 DEFAULT 7) ENGINE = MergeTree ORDER BY a"))
	batch := func(names ...string) arrow.RecordBatch {
		fields := make([]arrow.Field, 0, len(names))
		cols := make([]arrow.Array, 0, len(names))
		for _, n := range names {
			b := array.NewUint64Builder(memory.NewGoAllocator())
			b.Append(1)
			fields = append(fields, arrow.Field{Name: n, Type: arrow.PrimitiveTypes.Uint64})
			cols = append(cols, b.NewArray())
			b.Release()
		}
		rec := array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, 1)
		for _, c := range cols {
			c.Release()
		}
		return rec
	}
	newer := batch("a", "c", "b")
	defer newer.Release()
	err = exec.InsertArrow(ctx, "default.t", []arrow.RecordBatch{newer})
	require.Error(t, err, "a column the table lacks is refused, not dropped")
	assert.Contains(t, err.Error(), "NO_SUCH_COLUMN_IN_TABLE")

	older := batch("a")
	defer older.Release()
	require.NoError(t, exec.InsertArrow(ctx, "default.t", []arrow.RecordBatch{older}))
	var got []uint64
	for rec, qerr := range exec.QueryArrow(ctx, "SELECT c FROM default.t") {
		require.NoError(t, qerr)
		got = append(got, rec.Column(0).(*array.Uint64).Value(0))
		rec.Release()
	}
	assert.Equal(t, []uint64{7}, got, "a column the batch lacks takes its default")
}
