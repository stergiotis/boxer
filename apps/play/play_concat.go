//go:build llm_generated_opus47

package play

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// concatBatches fuses all record batches into a single one. The returned
// record takes ownership of a fresh arrow.Record; the input batches can be
// released by the caller once this function returns.
func concatBatches(batches []arrow.RecordBatch, alloc memory.Allocator) (out arrow.RecordBatch, schema *arrow.Schema, err error) {
	if len(batches) == 0 {
		return nil, nil, nil
	}
	schema = batches[0].Schema()
	if len(batches) == 1 {
		batches[0].Retain()
		return batches[0], schema, nil
	}
	ncols := int(batches[0].NumCols())
	cols := make([]arrow.Array, 0, ncols)
	var totalRows int64
	for _, b := range batches {
		totalRows += b.NumRows()
	}
	for c := 0; c < ncols; c++ {
		arrs := make([]arrow.Array, 0, len(batches))
		for _, b := range batches {
			arrs = append(arrs, b.Column(c))
		}
		var merged arrow.Array
		merged, err = array.Concatenate(arrs, alloc)
		if err != nil {
			for _, a := range cols {
				a.Release()
			}
			err = eh.Errorf("unable to concatenate arrow column %d: %w", c, err)
			return
		}
		cols = append(cols, merged)
	}
	rec := array.NewRecord(schema, cols, totalRows)
	// NewRecord retains each col; we can release our local refs.
	for _, a := range cols {
		a.Release()
	}
	out = rec
	return
}
