// Package ipcexec is the write-only recordstore executor: a generated store
// pointed at it appends every flushed batch to one Arrow IPC *stream* — the
// shape `INSERT INTO t FORMAT ArrowStream` loads — instead of a ClickHouse
// table. It serves the "record to a file now, load later" deployment that
// has no server and no clickhouse binary at capture time, with the same
// store code the online deployment runs.
//
// Exec accepts any statement as a no-op so provisioning code (EnsureTable)
// stays uniform across executors; QueryArrow refuses with [ErrWriteOnly],
// so the read verbs of a store bound to this executor fail loudly rather
// than returning nothing. The schema is written eagerly, so a run that
// inserts nothing still leaves a valid empty stream behind.
package ipcexec

import (
	"context"
	"io"
	"iter"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// ErrWriteOnly is what QueryArrow yields: a stream cannot be queried.
var ErrWriteOnly = eh.New("the Arrow IPC stream executor is write-only")

var _ recordstore.ExecutorI = (*StreamExecutor)(nil)

// StreamExecutor appends batches to one Arrow IPC stream. It is
// single-goroutine like the store that drives it.
type StreamExecutor struct {
	w      *ipc.Writer
	closer io.Closer
}

// NewStreamExecutor writes batches of schema to w. The caller keeps
// ownership of w: Close finishes the stream but does not close w — wrap
// with [Owning] to hand the writer's lifetime over. A nil alloc selects the
// Go allocator.
func NewStreamExecutor(w io.Writer, schema *arrow.Schema, alloc memory.Allocator) (inst *StreamExecutor) {
	if alloc == nil {
		alloc = memory.NewGoAllocator()
	}
	inst = &StreamExecutor{
		w: ipc.NewWriter(w, ipc.WithSchema(schema), ipc.WithAllocator(alloc)),
	}
	return
}

// Owning makes Close also close c after finishing the stream — the file
// the stream was opened on, typically. It returns inst for chaining.
func (inst *StreamExecutor) Owning(c io.Closer) *StreamExecutor {
	inst.closer = c
	return inst
}

// Exec accepts any statement as a no-op: a stream has no table to provision.
func (inst *StreamExecutor) Exec(ctx context.Context, sql string) error { return nil }

// QueryArrow yields ErrWriteOnly once and ends.
func (inst *StreamExecutor) QueryArrow(ctx context.Context, sql string) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) {
		yield(nil, ErrWriteOnly)
	}
}

// InsertArrow appends records to the stream; table is ignored — the stream
// is the table. Records must carry the executor's schema.
func (inst *StreamExecutor) InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) (err error) {
	for _, rec := range records {
		if e := inst.w.Write(rec); e != nil {
			err = eh.Errorf("write record to arrow stream: %w", e)
			return
		}
	}
	return
}

// Close finishes the stream (writing the schema when nothing was inserted)
// and closes the owned closer, if any.
func (inst *StreamExecutor) Close() (err error) {
	if e := inst.w.Close(); e != nil {
		err = eh.Errorf("close arrow stream: %w", e)
	}
	if inst.closer != nil {
		if e := inst.closer.Close(); e != nil && err == nil {
			err = eh.Errorf("close arrow stream sink: %w", e)
		}
	}
	return
}
