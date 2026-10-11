// Package projection turns a run's journal prefix into the model's request
// (ADR-0309): a pipeline of passes over the entries — the latest prompt
// section per name in the profile's declared order, the latest compaction
// cut with its summary, the latest edit per target, then the message
// entries from the cut on — returning one row per message with its
// provenance and label, never a wire string (§SD3). The function is
// written twice: the Go fold, which the coordinator projects with in
// production (ADR-0308 §SD3), and the SQL of sql.go, its twin for the
// engine, which the test lanes run in a clickhouse-local worker and hold
// to the fold's digest (law L4). The ADR's placement, the SQL in a worker
// on every call with the fold as a cache (§SD1, Q2), is not what runs:
// the fold is the production path until the worker's cost is measured.
//
// Identity is canonical (§SD3): each message digests as deterministic CBOR
// under the canonical form's keyed hash, and a request digests over the
// ordered message digests and the profile fingerprint; the wire bytes are
// the route's and are not the identity. The passes declare their
// properties (§SD2): sections, compaction and edits rewrite, everything
// else is append-stable, and the tests check both over generated journals.
package projection
