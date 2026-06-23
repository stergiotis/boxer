package introspect

import (
	"bytes"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// EncodeStream serialises batch as Arrow IPC ArrowStream bytes (no
// footer) — the form ClickHouse reads via FORMAT ArrowStream and the
// url() table source serves over HTTP (ADR-0094 §SD3).
func EncodeStream(batch arrow.RecordBatch) (b []byte, err error) {
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(batch.Schema()), ipc.WithAllocator(memory.DefaultAllocator))
	if err = w.Write(batch); err != nil {
		_ = w.Close()
		return nil, eh.Errorf("introspect: write arrow stream: %w", err)
	}
	if err = w.Close(); err != nil {
		return nil, eh.Errorf("introspect: close arrow stream: %w", err)
	}
	b = buf.Bytes()
	return
}

// EncodeFile serialises batch as Arrow IPC file bytes (with footer) —
// the `Arrow` file format read by file('...','Arrow'), used to feed the
// chlocal broker's InputTables on the in-process path (ADR-0094 §SD4).
func EncodeFile(batch arrow.RecordBatch) (b []byte, err error) {
	var buf bytes.Buffer
	w, werr := ipc.NewFileWriter(&buf, ipc.WithSchema(batch.Schema()), ipc.WithAllocator(memory.DefaultAllocator))
	if werr != nil {
		return nil, eh.Errorf("introspect: new arrow file writer: %w", werr)
	}
	if err = w.Write(batch); err != nil {
		_ = w.Close()
		return nil, eh.Errorf("introspect: write arrow file: %w", err)
	}
	if err = w.Close(); err != nil {
		return nil, eh.Errorf("introspect: close arrow file: %w", err)
	}
	b = buf.Bytes()
	return
}

// SnapshotStream snapshots p under proj and returns ArrowStream bytes,
// releasing the intermediate batch. Used by the HTTP table source.
func SnapshotStream(p Provider, proj Projection) (b []byte, err error) {
	batch, err := p.Snapshot(proj)
	if err != nil {
		return nil, err
	}
	defer batch.Release()
	return EncodeStream(batch)
}

// SnapshotFile snapshots p under proj and returns Arrow file bytes,
// releasing the intermediate batch. Used to populate chlocalbroker
// InputTables.
func SnapshotFile(p Provider, proj Projection) (b []byte, err error) {
	batch, err := p.Snapshot(proj)
	if err != nil {
		return nil, err
	}
	defer batch.Release()
	return EncodeFile(batch)
}
