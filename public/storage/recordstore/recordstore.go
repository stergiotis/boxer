// Package recordstore is the runtime support layer for generated leeway
// record stores (ADR-0100). A generated store composes one leeway TableDesc
// (the fact table), a set of component mappingplans, the batching
// read-through cache (public/caching) and a ClickHouse executor into a
// high-level API: ingest, create, retrieve (batched KV), persist — with an
// optional state view (Delete/GetLive) layered over the append-only
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
	"iter"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/identity/identifier"
)

// ReferenceStamper is the ADR-0112 M1 seam. A generated store consults its
// configured stampers once per Begin; each yields the surrogate ids to stamp as
// additive HighCardRef memberships onto every attribute the entity writes (via
// the DML ambient-membership primitive). Current captures whatever context it
// needs at that point — for provenance, the writer's host and call stack — and
// interns it, so the yielded TaggedId is the compact reference to a descriptor
// fact. A composite over several dimensions satisfies the same interface by
// yielding the concatenation. Stampers registered on a store must not write to
// that same store, or interning a fact would recurse.
type ReferenceStamper interface {
	Current(ctx context.Context) iter.Seq2[identifier.TaggedId, error]
	// Flush makes the descriptor facts the stamped ids reference durable. A
	// payload store calls it before its own insert (ordered flush, ADR-0112
	// SD5), so a referencing row is never durable ahead of its descriptor fact.
	// A stamper with no backing store returns (0, nil).
	Flush(ctx context.Context) (n int, err error)
}

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
//   - QueryArrow runs a SELECT with FORMAT Arrow semantics and streams the
//     decoded record batches: the sequence is single-use, an error ends it
//     as a final (nil, err) pair (the convention the generated stores'
//     iterator verbs share), and ownership of each yielded batch transfers
//     to the consumer — the consumer must Release every batch it receives,
//     including one it breaks on; batches never yielded stay the
//     implementation's to release. A buffered implementation trivially
//     satisfies the shape by iterating a materialized slice; the shape
//     exists so a streaming implementation needs no interface change.
//   - InsertArrow appends the given batches to table. The executor does
//     not retain the records — the caller releases them after return. It
//     returns after the insert is acknowledged; over a durable engine
//     (with asynchronous inserts disabled) the rows are durable when it
//     returns — the property the ADR-0100 state view and the pushout
//     adapter rely on.
type ExecutorI interface {
	Exec(ctx context.Context, sql string) error
	QueryArrow(ctx context.Context, sql string) iter.Seq2[arrow.RecordBatch, error]
	InsertArrow(ctx context.Context, table string, records []arrow.RecordBatch) error
}

// ReplayOpts parameterizes the generated Replay verb beyond its
// positional lower bound. The zero value replays everything from
// fromOrder on.
type ReplayOpts struct {
	// To is the exclusive upper Order bound — "state as of To" folds
	// replay rows with Order < To. Zero means unbounded.
	To time.Time
	// Limit caps the number of returned rows; zero means no limit.
	Limit int
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
