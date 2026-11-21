package recordstore

import (
	"context"
	"iter"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shapeExecutor answers every query with batches built by mk.
type shapeExecutor struct {
	mk func() []arrow.RecordBatch
}

func (inst shapeExecutor) Exec(context.Context, string) error { return nil }
func (inst shapeExecutor) InsertArrow(context.Context, string, []arrow.RecordBatch) error {
	return nil
}
func (inst shapeExecutor) QueryArrow(context.Context, string) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) {
		recs := inst.mk()
		for i, rec := range recs {
			if !yield(rec, nil) {
				for _, r := range recs[i+1:] {
					r.Release()
				}
				return
			}
		}
	}
}

// A catalog answer of an unexpected shape — a missing column, a column of
// another type, several batches — is refused without leaking a batch.
func TestReadTableFactsReleasesAWrongShapedAnswer(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer alloc.AssertSize(t, 0)
	numeric := func() arrow.RecordBatch {
		b := array.NewRecordBuilder(alloc, arrow.NewSchema([]arrow.Field{{Name: "engine", Type: arrow.PrimitiveTypes.Int64}}, nil))
		defer b.Release()
		b.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2}, nil)
		return b.NewRecordBatch()
	}
	for name, mk := range map[string]func() []arrow.RecordBatch{
		"wrong type":      func() []arrow.RecordBatch { return []arrow.RecordBatch{numeric()} },
		"several batches": func() []arrow.RecordBatch { return []arrow.RecordBatch{numeric(), numeric(), numeric()} },
	} {
		_, err := ReadTableFacts(context.Background(), shapeExecutor{mk: mk}, "t")
		require.Error(t, err, name)
		v := JudgeTable(context.Background(), shapeExecutor{mk: mk}, TableContract{Table: "t"})
		assert.Equal(t, SchemaVerdictUnchecked, v.Verdict, name)
	}
}
