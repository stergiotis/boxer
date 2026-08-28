package ipcexec_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/storage/recordstore/ipcexec"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

var testSchema = arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil)

// tb is what the helpers need from *testing.T and *rapid.T alike.
type tb interface {
	require.TestingT
	Helper()
}

func batch(t tb, alloc memory.Allocator, vals ...int64) arrow.RecordBatch {
	t.Helper()
	b := array.NewInt64Builder(alloc)
	defer b.Release()
	b.AppendValues(vals, nil)
	col := b.NewArray()
	defer col.Release()
	return array.NewRecordBatch(testSchema, []arrow.Array{col}, int64(len(vals)))
}

func readAll(t tb, buf []byte) (rows int64, batches int) {
	t.Helper()
	r, err := ipc.NewReader(bytes.NewReader(buf), ipc.WithSchema(testSchema))
	require.NoError(t, err)
	defer r.Release()
	for r.Next() {
		rows += r.RecordBatch().NumRows()
		batches++
	}
	require.NoError(t, r.Err())
	return
}

func TestEmptyRunIsAValidStream(t *testing.T) {
	var buf bytes.Buffer
	exec := ipcexec.NewStreamExecutor(&buf, testSchema, nil)
	require.NoError(t, exec.Exec(context.Background(), "CREATE TABLE ignored"))
	require.NoError(t, exec.Close())
	rows, batches := readAll(t, buf.Bytes())
	require.Zero(t, rows)
	require.Zero(t, batches)
}

func TestQueryArrowIsWriteOnly(t *testing.T) {
	exec := ipcexec.NewStreamExecutor(&bytes.Buffer{}, testSchema, nil)
	n := 0
	for rec, err := range exec.QueryArrow(context.Background(), "SELECT 1") {
		require.Nil(t, rec)
		require.True(t, errors.Is(err, ipcexec.ErrWriteOnly))
		n++
	}
	require.Equal(t, 1, n)
}

type countingCloser struct{ n int }

func (c *countingCloser) Close() error { c.n++; return nil }

func TestOwningClosesTheSink(t *testing.T) {
	cc := &countingCloser{}
	exec := ipcexec.NewStreamExecutor(&bytes.Buffer{}, testSchema, nil).Owning(cc)
	require.NoError(t, exec.Close())
	require.Equal(t, 1, cc.n)
}

// Every batch inserted, in any count and size, reads back with the same
// row total and batch count.
func TestInsertRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		alloc := memory.NewGoAllocator()
		var buf bytes.Buffer
		exec := ipcexec.NewStreamExecutor(&buf, testSchema, alloc)
		nBatches := rapid.IntRange(0, 8).Draw(t, "batches")
		var wantRows int64
		for i := 0; i < nBatches; i++ {
			vals := rapid.SliceOfN(rapid.Int64(), 1, 16).Draw(t, "vals")
			rec := batch(t, alloc, vals...)
			if err := exec.InsertArrow(context.Background(), "t", []arrow.RecordBatch{rec}); err != nil {
				t.Fatal(err)
			}
			rec.Release()
			wantRows += int64(len(vals))
		}
		if err := exec.Close(); err != nil {
			t.Fatal(err)
		}
		rows, batches := readAll(t, buf.Bytes())
		if rows != wantRows || batches != nBatches {
			t.Fatalf("read back %d rows in %d batches, want %d in %d", rows, batches, wantRows, nBatches)
		}
	})
}
