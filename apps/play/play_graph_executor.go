package play

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// play_graph_executor.go (ADR-0097): the real nodeExecutorI. clientExecutor
// runs a compiled query over HTTP and concatenates the Arrow IPC stream into a
// single record, mirroring QueryStore.Execute's reader→concat path. It is the
// executor the queryGraph runs SELF-EXECUTED nodes through — the splitter's
// nodes and the panel-local lanes (bands, map) land on it in slice 3. The live
// `main` node is still backed by QueryStore today (its async / loading /
// history / cancel / FSM machinery), so clientExecutor is not yet on the live
// render path; it is exercised by tests and ready for the self-executed nodes.

type clientExecutor struct {
	client *Client
	// opts carries the lane's stable query_id + replace_running_query (SD5
	// server-side supersession). nil for callers that don't supersede.
	opts *ExecOptions
}

var _ nodeExecutorI = clientExecutor{}

// execute runs sql synchronously and returns the single concatenated record
// plus the engine summary. Callers that must not block the render thread (the
// live panels) wrap this in an async lane; the synchronous form is correct for
// tests and for the suspending scheduler's worker goroutine (SD5, slice 3).
func (inst clientExecutor) execute(ctx context.Context, sql string, alloc memory.Allocator) (rec arrow.RecordBatch, schema *arrow.Schema, summary Summary, err error) {
	rdr, body, summary, xErr := inst.client.ExecuteArrowStream(ctx, sql, alloc, inst.opts)
	if xErr != nil {
		err = eh.Errorf("clientExecutor.execute: %w", xErr)
		return
	}
	defer func() {
		rdr.Release()
		_ = body.Close()
	}()
	batches := make([]arrow.RecordBatch, 0, 4)
	for rdr.Next() {
		b := rdr.Record()
		b.Retain()
		batches = append(batches, b)
	}
	rErr := rdr.Err()
	if rErr != nil {
		for _, b := range batches {
			b.Release()
		}
		err = eh.Errorf("clientExecutor.execute: read stream: %w", rErr)
		return
	}
	rec, schema, err = concatBatches(batches, alloc)
	for _, b := range batches {
		b.Release()
	}
	if err != nil {
		err = eh.Errorf("clientExecutor.execute: %w", err)
		return
	}
	if schema == nil {
		// Zero batches: an empty result still has a schema (the stream
		// header) — keep it so consumers can negotiate/show headers instead
		// of confusing "ran, empty" with "no result" (review finding).
		schema = rdr.Schema()
	}
	return
}
