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
// executor behind every demand-driven node lane on the live render path — the
// Map raster (3f), the Timeline bands (4b), and observed intermediates (3d) —
// each with its own ExecOptions (stable query_id, SD5). The `main` node stays
// on QueryStore (Run-triggered, with history).

type clientExecutor struct {
	client *Client
	// opts carries the lane's stable query_id + replace_running_query (SD5
	// server-side supersession). nil for callers that don't supersede.
	opts *ExecOptions
}

var (
	_ nodeExecutorI          = clientExecutor{}
	_ progressAwareExecutorI = clientExecutor{}
)

// execute runs the compiled node synchronously and returns the single
// concatenated record plus the engine summary. The node's signal values ride
// the request URL beside the SET-bound constants (slice 5a). Callers that
// must not block the render thread (the live panels) wrap this in an async
// lane; the synchronous form is correct for tests and for the suspending
// scheduler's worker goroutine (SD5, slice 3).
func (inst clientExecutor) execute(ctx context.Context, c compiledNode, alloc memory.Allocator) (rec arrow.RecordBatch, schema *arrow.Schema, summary Summary, err error) {
	return inst.executeWithProgress(ctx, c, alloc, nil)
}

// executeWithProgress is execute plus the live-progress sink (ADR-0115
// plane A): a non-nil onProgress rides a per-call copy of the lane's
// stable ExecOptions, opting the request into the in-band progress
// headers. The lane's identity options (query_id, label, supersession)
// are untouched — OnProgress is the only per-run field.
func (inst clientExecutor) executeWithProgress(ctx context.Context, c compiledNode, alloc memory.Allocator, onProgress func(Summary)) (rec arrow.RecordBatch, schema *arrow.Schema, summary Summary, err error) {
	opts := inst.opts
	if onProgress != nil && opts != nil {
		o := *opts
		o.OnProgress = onProgress
		opts = &o
	}
	rdr, body, summary, xErr := inst.client.ExecuteArrowStream(ctx, c.SQL, alloc, opts, c.Params)
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
