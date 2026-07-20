// Package introspect exposes keelson runtime state as Apache Arrow
// tables that ClickHouse can query (ADR-0094).
//
// A Provider names one table, declares its Arrow schema, and snapshots
// its current rows. Two transports share that provider core:
//
//   - the HTTP table source serves each table as ArrowStream, so a
//     clickhouse-local or clickhouse-server can pull it via the
//     url() table function and JOIN it with other data (§SD3);
//   - the in-process query path analyses a SQL query, snapshots only
//     the referenced tables (projected to the referenced columns), and
//     feeds them to the chlocal broker as TEMPORARY tables (§SD4).
//
// Tables live in a `keelson` namespace by convention — never `system`,
// which ClickHouse reserves for its own introspection tables.
package introspect

import "github.com/apache/arrow-go/v18/arrow"

// FreshnessClass declares how long a provider's snapshot stays valid.
type FreshnessClass uint8

const (
	// FreshnessStatic marks data that is stable for the process
	// lifetime (compile-time registries, build info). The engine may
	// cache its Arrow bytes for the whole run.
	FreshnessStatic FreshnessClass = iota
	// FreshnessLive marks data that reflects mutable runtime state
	// (open windows, live env values). Snapshot per query; do not cache
	// across queries.
	FreshnessLive
)

func (f FreshnessClass) String() (s string) {
	switch f {
	case FreshnessStatic:
		s = "static"
	case FreshnessLive:
		s = "live"
	default:
		s = "unknown"
	}
	return
}

// Provider exposes one introspection table. Implementations register
// with a Registry and are enumerated by the HTTP source and the query
// engine.
type Provider interface {
	// Name is the table name: a ClickHouse identifier with no
	// `keelson.` prefix. It is the URL path segment and the TEMPORARY
	// table name, so it must match validTableName.
	Name() string
	// Schema is the full, unprojected Arrow schema. It serves the HTTP
	// endpoint's shape and lets the query analyser expand `SELECT *`.
	Schema() *arrow.Schema
	// Freshness declares the caching class.
	Freshness() FreshnessClass
	// Snapshot materialises the table's current rows. proj is a
	// best-effort column filter — a provider MAY ignore it and emit all
	// columns; the engine re-validates via clickhouse-local, so a
	// superset is always safe (ADR-0094 §SD4). The returned batch is
	// owned by the caller, which must Release it.
	Snapshot(proj Projection) (arrow.RecordBatch, error)
}

// EncryptedDatasetI marks a Provider whose rows are not snapshotted in
// process but streamed from a chunk-encrypted file through the chlocal
// broker at query time (ADR-0134). The in-process engine detects this
// kind by type assertion and routes it to
// chlocalbroker.ExecRequest.EncryptedInputs instead of snapshotting; the
// HTTP table source refuses it, so plaintext never rides HTTP and
// exactly one decrypt path exists. Its Snapshot always errors.
type EncryptedDatasetI interface {
	Provider
	// Structure is the explicit ClickHouse structure string the
	// ArrowStream read requires (schema inference over a pipe fails).
	Structure() string
	// Path is the absolute path to the chunk-encrypted Arrow file.
	Path() string
	// Revision is the dataset revision; a republish bumps it.
	Revision() uint64
}

// Projection selects which columns a Snapshot materialises. The zero
// value selects nothing; use AllColumns() for "every column". Column
// pruning is only an optimisation: an over-broad projection is always
// safe, and an empty one falls back to all columns at Build time.
type Projection struct {
	all  bool
	cols map[string]struct{}
}

// AllColumns selects every column. The HTTP table source and any
// best-effort fallback use this.
func AllColumns() Projection { return Projection{all: true} }

// Columns selects exactly the named columns. Empty selects nothing
// (which Build treats as "all", never a zero-column table).
func Columns(names ...string) (p Projection) {
	if len(names) == 0 {
		return
	}
	p.cols = make(map[string]struct{}, len(names))
	for _, n := range names {
		p.cols[n] = struct{}{}
	}
	return
}

// IsAll reports whether the projection selects every column.
func (p Projection) IsAll() bool { return p.all }

// wants reports whether column name is selected.
func (p Projection) wants(name string) (ok bool) {
	if p.all {
		return true
	}
	_, ok = p.cols[name]
	return
}
