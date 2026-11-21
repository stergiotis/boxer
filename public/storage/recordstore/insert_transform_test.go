package recordstore_test

import (
	"context"
	"errors"
	"iter"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/recordstoretest"
)

// oneColumn is a batch of one int64 column named name holding vals.
func oneColumn(alloc memory.Allocator, name string, vals ...int64) arrow.RecordBatch {
	schema := arrow.NewSchema([]arrow.Field{{Name: name, Type: arrow.PrimitiveTypes.Int64}}, nil)
	b := array.NewInt64Builder(alloc)
	defer b.Release()
	b.AppendValues(vals, nil)
	arr := b.NewArray()
	defer arr.Release()
	return array.NewRecordBatch(schema, []arrow.Array{arr}, int64(len(vals)))
}

// renameTo is a transform that sends the batches to target with their one
// column renamed to "carried", built in alloc.
func renameTo(alloc memory.Allocator, target string) recordstore.InsertTransformFunc {
	return func(_ string, records []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
		out := make([]arrow.RecordBatch, 0, len(records))
		for _, r := range records {
			col := r.Column(0).(*array.Int64)
			out = append(out, oneColumn(alloc, "carried", col.Int64Values()...))
		}
		return target, out, nil
	}
}

type sentInsert struct {
	table    string
	columns  []string
	rows     int64
	settings recordstore.InsertSettings
}

// recorder is an inner executor that records each insert.
func recorder(sent *[]sentInsert) *recordstoretest.HookExecutor {
	return &recordstoretest.HookExecutor{
		OnInsertArrowWith: func(_ context.Context, table string, records []arrow.RecordBatch, settings recordstore.InsertSettings) (bool, error) {
			s := sentInsert{table: table, settings: settings}
			for _, r := range records {
				s.rows += r.NumRows()
				s.columns = s.columns[:0]
				for _, f := range r.Schema().Fields() {
					s.columns = append(s.columns, f.Name)
				}
			}
			*sent = append(*sent, s)
			return true, nil
		},
	}
}

func TestTransformExecutor_RewritesOnlyTheTransformedTable(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer alloc.AssertSize(t, 0)
	var sent []sentInsert
	exec := recordstore.TransformExecutor(recorder(&sent), "boxer.facts", renameTo(alloc, "carrier.rows"))

	in := oneColumn(alloc, "id", 1, 2, 3)
	defer in.Release()
	settings := recordstore.InsertSettings{AsyncInsert: recordstore.ToggleOff}.WithDeduplicationToken("tok-1")
	applied, err := exec.InsertArrowWith(context.Background(), "boxer.facts", []arrow.RecordBatch{in}, settings)
	require.NoError(t, err)
	assert.True(t, applied)
	require.NoError(t, exec.InsertArrow(context.Background(), "boxer.other", []arrow.RecordBatch{in}))

	require.Len(t, sent, 2)
	assert.Equal(t, sentInsert{table: "carrier.rows", columns: []string{"carried"}, rows: 3, settings: settings}, sent[0],
		"the facts insert reaches the inner executor under the transform's table and schema, with its settings intact")
	assert.Equal(t, sentInsert{table: "boxer.other", columns: []string{"id"}, rows: 3}, sent[1],
		"an insert into another table is not transformed")
}

func TestTransformExecutor_ForwardsStatementsAndQueries(t *testing.T) {
	var execd, queried string
	inner := &recordstoretest.HookExecutor{
		OnExec: func(_ context.Context, sql string) error { execd = sql; return nil },
	}
	inner.OnQueryArrow = func(_ context.Context, sql string) iter.Seq2[arrow.RecordBatch, error] {
		queried = sql
		return func(func(arrow.RecordBatch, error) bool) {}
	}
	exec := recordstore.TransformExecutor(inner, "boxer.facts", renameTo(memory.NewGoAllocator(), "carrier.rows"))
	require.NoError(t, exec.Exec(context.Background(), "ALTER TABLE boxer.facts DELETE WHERE 1"))
	for range exec.QueryArrow(context.Background(), "SELECT * FROM boxer.facts") {
	}
	assert.Equal(t, "ALTER TABLE boxer.facts DELETE WHERE 1", execd)
	assert.Equal(t, "SELECT * FROM boxer.facts", queried)
}

func TestTransformExecutor_RefusesBeforeSending(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer alloc.AssertSize(t, 0)
	in := oneColumn(alloc, "id", 1)
	defer in.Release()
	for name, tr := range map[string]recordstore.InsertTransformI{
		"transform error": recordstore.InsertTransformFunc(func(string, []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
			return "", nil, errors.New("no mapping for this kind")
		}),
		"malformed target": renameTo(alloc, "carrier.rows; DROP TABLE x"),
		"empty target":     renameTo(alloc, ""),
	} {
		t.Run(name, func(t *testing.T) {
			var sent []sentInsert
			exec := recordstore.TransformExecutor(recorder(&sent), "boxer.facts", tr)
			err := exec.InsertArrow(context.Background(), "boxer.facts", []arrow.RecordBatch{in})
			require.Error(t, err)
			assert.Empty(t, sent, "nothing reaches the server when the transform fails")
		})
	}
}

func TestTransformExecutor_EmptyInsertIsNotTransformed(t *testing.T) {
	called := false
	var sent []sentInsert
	exec := recordstore.TransformExecutor(recorder(&sent), "boxer.facts", recordstore.InsertTransformFunc(func(string, []arrow.RecordBatch) (string, []arrow.RecordBatch, error) {
		called = true
		return "carrier.rows", nil, nil
	}))
	require.NoError(t, exec.InsertArrow(context.Background(), "boxer.facts", nil))
	assert.False(t, called)
	require.Len(t, sent, 1)
	assert.Equal(t, "boxer.facts", sent[0].table)
}

func TestRegisterInsertTransform(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer alloc.AssertSize(t, 0)
	var sent []sentInsert
	exec := recordstore.RegisteredTransforms(recorder(&sent))
	in := oneColumn(alloc, "id", 7)
	defer in.Release()

	require.NoError(t, exec.InsertArrow(context.Background(), "scratch.facts", []arrow.RecordBatch{in}))
	unregister := recordstore.RegisterInsertTransform("scratch.facts", renameTo(alloc, "scratch.carrier"))
	_, ok := recordstore.RegisteredInsertTransform("scratch.facts")
	assert.True(t, ok)
	assert.Panics(t, func() { recordstore.RegisterInsertTransform("scratch.facts", renameTo(alloc, "x.y")) }, "a second transform for one table is refused")
	assert.Panics(t, func() { recordstore.RegisterInsertTransform("not a ref", renameTo(alloc, "x.y")) })
	assert.Panics(t, func() { recordstore.RegisterInsertTransform("scratch.other", nil) })
	require.NoError(t, exec.InsertArrow(context.Background(), "scratch.facts", []arrow.RecordBatch{in}))
	unregister()
	_, ok = recordstore.RegisteredInsertTransform("scratch.facts")
	assert.False(t, ok)
	require.NoError(t, exec.InsertArrow(context.Background(), "scratch.facts", []arrow.RecordBatch{in}))

	require.Len(t, sent, 3)
	assert.Equal(t, []string{"scratch.facts", "scratch.carrier", "scratch.facts"}, []string{sent[0].table, sent[1].table, sent[2].table},
		"the registry is read at each insert")
}
