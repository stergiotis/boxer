// Package recordstore is the runtime support layer for generated leeway
// record stores (ADR-0100). A generated store composes one leeway TableDesc
// (the fact table), a set of component mappingplans, the batching
// read-through cache (public/caching) and a ClickHouse executor into a
// high-level API: ingest, create, retrieve (batched KV), persist — with an
// optional state view (Put/Delete/GetLatest) layered over the append-only
// substrate.
//
// This package holds only what generated code and adapters share: the
// executor seam, the Scan options and the synthetic-sequence Order
// helpers. The store types themselves are emitted per schema by
// recordstore/gen; concrete ClickHouse executors live in
// recordstore/chexec.
//
// A store instance — like the cache and the DML builders it composes — is
// single-goroutine; use one instance per goroutine.
package recordstore

import (
	"context"
	"errors"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
)

// Lifecycle marker values of the envelope Lifecycle column (the state
// view; ADR-0100 SD2/SD4). Generated stores write LifecycleLive on Begin
// and LifecycleTombstone on Delete; readers of the raw tombstone-blind
// verbs compare against these (or use the generated IsTombstone helper).
const (
	LifecycleLive      uint8 = 0
	LifecycleTombstone uint8 = 1
)

// ErrDuplicateIngestKey reports two rows sharing a key within one
// Ingest<Kind> call — they would tie on Order (all rows of a call share
// ts) and Latest would pick among them arbitrarily. Match with
// errors.Is.
var ErrDuplicateIngestKey = errors.New("duplicate key within one ingest batch")

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

// ScanOpts parameterizes the generated Scan<Kind> verbs. The zero value
// scans everything: no extra predicate, no limit.
type ScanOpts struct {
	// ExtraPredicate further restricts the scan: raw SQL over the physical
	// (leeway-encoded) column names, ANDed with the store's baked Filter
	// artefact. It is concatenated into the statement verbatim — trusted
	// input only, never user-supplied text.
	ExtraPredicate string
	// Limit caps the number of returned rows; zero means no limit.
	Limit int
}

// SeqTs renders a synthetic per-key sequence number (1, 2, 3, …) as the
// envelope Order timestamp (Unix nanosecond seq, UTC) — a total append
// order without wall clocks, the idiom the ADR-0100 consumer adapters
// use. Callers own the SD2 contract: Order values must stay strictly
// monotonic per key. SeqTs(0) is the "replay everything" lower bound.
func SeqTs(seq uint64) time.Time {
	return time.Unix(0, int64(seq)).UTC()
}

// SeqOf is the inverse of SeqTs: it recovers the synthetic sequence
// number from an Order timestamp read back from the store.
func SeqOf(ts time.Time) uint64 {
	return uint64(ts.UnixNano())
}
