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
