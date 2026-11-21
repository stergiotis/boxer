package chexec

import (
	"context"
	"iter"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// A batch QueryArrow yields is released once by its consumer and frees all
// it holds, on the happy path and when the consumer stops early.
func TestLocalExecutor_QueryReleasesEveryBatch(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	exec, err := NewLocalExecutor(t.TempDir(), alloc)
	if err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}
	ctx := context.Background()
	for rec, qerr := range exec.QueryArrow(ctx, "SELECT number FROM numbers(3)") {
		require.NoError(t, qerr)
		rec.Release()
	}
	alloc.AssertSize(t, 0)

	// A catalog read whose answer has an unexpected shape: the reader
	// refuses it and releases what it was handed.
	require.NoError(t, exec.Exec(ctx, "CREATE TABLE t (a UInt8) ENGINE = MergeTree ORDER BY a"))
	_, err = recordstore.ReadTableFacts(ctx, wrongShape{exec}, "t")
	require.Error(t, err)
	alloc.AssertSize(t, 0)
}

// wrongShape answers every query with a numeric column, which no catalog
// reader accepts.
type wrongShape struct{ *LocalExecutor }

func (inst wrongShape) QueryArrow(ctx context.Context, _ string) iter.Seq2[arrow.RecordBatch, error] {
	return inst.LocalExecutor.QueryArrow(ctx, "SELECT number AS engine FROM numbers(2)")
}
