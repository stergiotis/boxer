//go:build llm_generated_opus47

// Package h3 provides bulk, Struct-of-Arrays access to Uber H3 hierarchical
// geospatial indexing.
//
// The implementation drives the h3o Rust crate compiled to
// wasm32-unknown-unknown through [github.com/tetratelabs/wazero]. Consumers
// allocate a shared [Runtime], check out a per-goroutine [Handle] via
// [Runtime.AcquireE], and call bulk methods that take Struct-of-Arrays inputs
// and write Struct-of-Arrays outputs. Variable-arity results use a CSR layout
// (flat values + []int32 offsets); per-element failures are reported through
// a [StatusE] slice that parallels the output values.
//
// # API shape
//
// Two access patterns, same underlying WASM calls:
//
//   - Bulk methods ([Handle.LatLngsToCellsE], [Handle.GridDisksE],
//     [Handle.PolygonToCellsE], …) take Struct-of-Arrays inputs and
//     reusable destination buffers. Primary API; use when N > ~8 elements.
//   - Scalar convenience wrappers ([Handle.LatLngToCellE],
//     [Handle.CellToLatLngE], [Handle.GridDiskE],
//     [Handle.PolygonToCellsSimpleE]) cover the common "one input /
//     one output" case without the 1-element-slice ceremony. Thin shims
//     over the bulk form; suitable for UI glue, REPL-style scripting,
//     and anywhere the call-site clarity matters more than per-call
//     amortisation.
//
// # Status triage
//
// Bulk methods surface per-element validity via a [StatusE] slice that
// parallels the output. Callers decide how to react: drop rows, abort
// the whole batch, or compute-anyway. [AnyFailure], [FirstFailure], and
// [CountFailures] cover the common "any failure is a hard error" shape
// without forcing every caller to re-implement the scan.
//
// # Single-threaded UI usage
//
// When the consumer is an interactive UI loop (single frame-thread;
// [Handle] never shared across goroutines), the intended pattern is:
//
//   - One package-level `*Runtime` + `*Handle` pair created lazily via
//     [sync.Once] on first frame.
//   - [RuntimeConfig.PoolSize] of 1 — no concurrency means no pool.
//   - `context.Background()` for every call — no meaningful ctx on the
//     frame path.
//   - [Handle.Release] called in process-exit plumbing (optional: the
//     process exits either way).
//
// Data pipelines running work in parallel goroutines build a separate
// Runtime with `PoolSize == GOMAXPROCS` (the default) and check out one
// [Handle] per goroutine. The UI and pipeline use cases can coexist in
// one process with two independent Runtimes, or share one Runtime with
// a pool size that covers both demands.
//
// See doc/adr/0003-h3-wasm-bridge.md for the design rationale and
// EXPLANATION.md for the H3-specific theory and invariants consumers rely on.
package h3
