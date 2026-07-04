// Package recordstore is the runtime support layer for generated leeway
// record stores (ADR-0100). A generated store composes one leeway TableDesc
// (the fact table), a set of component mappingplans, the batching
// read-through cache (public/caching) and a ClickHouse executor into a
// high-level API: ingest, create, retrieve (batched KV), persist — with an
// optional state view (Put/Delete/GetLatest) layered over the append-only
// substrate.
//
// This package holds only what generated code and adapters share: the
// executor seam and common errors. The store types themselves are emitted
// per schema by recordstore/gen; concrete ClickHouse executors live in
// recordstore/chexec.
//
// A store instance — like the cache and the DML builders it composes — is
// single-goroutine; use one instance per goroutine.
package recordstore

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow"
)

// ExecutorI is the seam between a generated store and ClickHouse. The store
// emits SQL text and Arrow batches; the executor moves them. Implementations
// decide the transport (HTTP server, clickhouse-local process, in-proc
// broker).
//
// Contract:
//   - Exec runs a statement for its side effect (DDL, admin). It returns
//     after the server acknowledged the statement.
//   - QueryArrow runs a SELECT with FORMAT Arrow semantics and returns the
//     decoded record batches. The caller must Release every returned batch.
//   - InsertArrow appends the given batches to table. It returns after the
//     insert is acknowledged; over a durable engine (with asynchronous
//     inserts disabled) the rows are durable when it returns — the property
//     the ADR-0100 state view and the pushout adapter rely on.
type ExecutorI interface {
	Exec(ctx context.Context, sql string) error
	QueryArrow(ctx context.Context, sql string) (records []arrow.RecordBatch, err error)
	InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) error
}
