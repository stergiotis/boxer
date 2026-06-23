package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func int64Batch(mem memory.Allocator, schema *arrow.Schema, vals ...int64) arrow.RecordBatch {
	b := array.NewInt64Builder(mem)
	defer b.Release()
	b.AppendValues(vals, nil)
	arr := b.NewArray()
	defer arr.Release()
	return array.NewRecord(schema, []arrow.Array{arr}, int64(len(vals)))
}

func TestConcatBatchesEmpty(t *testing.T) {
	rec, schema, err := concatBatches(nil, memory.NewGoAllocator())
	if err != nil || rec != nil || schema != nil {
		t.Fatalf("empty: rec=%v schema=%v err=%v, want all nil", rec, schema, err)
	}
}

func TestConcatBatchesSingle(t *testing.T) {
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64}}, nil)
	b := int64Batch(mem, schema, 1, 2, 3)
	defer b.Release()
	rec, gotSchema, err := concatBatches([]arrow.RecordBatch{b}, mem)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Release()
	if rec.NumRows() != 3 {
		t.Errorf("rows=%d, want 3", rec.NumRows())
	}
	if gotSchema != schema {
		t.Errorf("schema should be the input batch's schema")
	}
}

func TestConcatBatchesMulti(t *testing.T) {
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{{Name: "n", Type: arrow.PrimitiveTypes.Int64}}, nil)
	b1 := int64Batch(mem, schema, 1, 2)
	b2 := int64Batch(mem, schema, 3, 4, 5)
	defer b1.Release()
	defer b2.Release()
	rec, _, err := concatBatches([]arrow.RecordBatch{b1, b2}, mem)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Release()
	if rec.NumRows() != 5 {
		t.Fatalf("rows=%d, want 5", rec.NumRows())
	}
	col := rec.Column(0).(*array.Int64)
	for i, w := range []int64{1, 2, 3, 4, 5} {
		if col.Value(i) != w {
			t.Errorf("row %d = %d, want %d", i, col.Value(i), w)
		}
	}
}
