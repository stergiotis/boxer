---
type: explanation
audience: package maintainer
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-04-23
---

# H3 bridge — theory and invariants

The design rationale for this package is in [ADR-0003](../../../../doc/adr/0003-h3-wasm-bridge.md).
This document covers the timeless parts — properties of the H3 grid itself,
and the invariants callers of this package rely on — that remain true
independent of the current code.

## The H3 grid in one paragraph

H3 partitions the Earth into hexagonal cells organised into 16 hierarchical
resolutions (0 coarsest, 15 finest). At each resolution the grid contains
approximately 7× more cells than the next-coarser one. Each cell has a
deterministic 64-bit integer identifier; resolution and spatial address are
packed into the low bits of the identifier. See
[h3geo.org](https://h3geo.org) for the full specification.

Two anomalies deserve explicit mention because they trip consumers who
assume perfect hex tiling:

- **Pentagons.** Twelve cells in the grid are pentagons rather than
  hexagons, located at the vertices of the icosahedron used as the base
  projection. Properties that hold for hexagons (exactly six neighbours,
  exactly seven children at the next resolution) do **not** hold for
  pentagons: a pentagon has five neighbours and six children at one
  resolution step. Code that assumes hexagonal uniformity will silently
  produce wrong results near these 12 points.
- **Antimeridian.** Cells that straddle the ±180° meridian are still
  contiguous in cell-space; `h3o` (and this bridge) returns coordinates in
  the canonical range `[-180°, 180°]`, but round-tripping near the
  antimeridian can flip sign. The round-trip distance bound used in the
  test suite accounts for this.

## Invariants of this bridge

These are properties the Go API guarantees to callers. They are enforced
by the tests under `public/science/geo/h3/*_test.go`; violating them
indicates a bug in this package, not in the caller.

### Status codes and bulk errors

Per-element failure is reported via `StatusE` bytes in a slice parallel to
the output values. Bulk-level `error` is reserved for three categories:

- **WASM traps / I/O.** The wasm guest aborted unexpectedly, the runtime
  was closed, or a memory access crossed the guest memory bounds
  (`ErrMemoryOOB` — a bug in this package, not in the caller).
- **Caller-visible invariants violated.** Length mismatches between
  parallel input slices, invalid CSR offsets on input, use-after-release
  on a handle (`ErrHandleReleased`, caught centrally in
  `ensureScratchE`), malformed polygon geometry
  (`ErrBadPolygonGeometry`), out-of-enum containment mode
  (`ErrBadContainmentMode`).
- **Whole-batch semantic failures** — operations whose output has no
  stable 1:1 mapping back to inputs cannot report per-element status and
  so surface set-level errors instead (see the deviations below).

A well-formed call with partially invalid data (out-of-range lat/lng,
bit-garbage cell indices, finer-than-target cells in
`UncompactCellsE`) returns `nil` bulk error and per-element `StatusE`
codes. Callers filter rows by status.

### Two local deviations from the status-code default

- **`CompactCellsE` (SD13).** `compact_cells` collapses N input cells at
  one resolution into M ≤ N outputs at mixed (coarser) resolutions,
  with deduplication and reordering on the Rust side. There is no
  stable input→output mapping, so a per-element `StatusE` has no
  natural interpretation. Whole-batch failures (mixed-resolution input,
  duplicate cells) surface as `ErrCompactMixedResolution` /
  `ErrCompactDuplicateInput`.
- **`UncompactCellsE` (SD14).** Output is a flat `[]uint64` + per-input
  `StatusE`, not CSR. Consumers almost always want the expanded set as
  a unit; per-input → children provenance, when genuinely needed, is
  already available via `CellsToChildrenE`'s CSR output.

### CSR (Compressed Sparse Row) layout

Variable-arity outputs use CSR. The full set of CSR-shaped methods:

- `CellsToChildrenE` — one `values []uint64` + `offsets`.
- `GridDisksE` — one `values []uint64` + `offsets`.
- `CellsToStringsE` — one `buf []byte` + `offsets` (H3 hex strings, no separators, no NUL).
- `CellsToBoundariesE` — two parallel `lats []float64` / `lngs []float64` sharing one `offsets` (SD15; open rings, typically 5–6 vertices per row, up to 10 for pentagons whose boundary crosses an icosahedron face edge).

CSR invariants (identical across all four):

- `values` holds the flat concatenation of all rows' payloads.
- `offsets` has length N+1 where N is the batch size.
- `offsets[0] == 0`.
- `offsets[N] == len(values)`.
- `offsets[i] <= offsets[i+1]` for all i (monotone non-decreasing).
- Row i's payload is `values[offsets[i]:offsets[i+1]]`.

An empty row (e.g., `StatusInvalidResolution` on a row whose child count
cannot be computed, `StatusInvalidCell` on a bit-garbage boundary input)
has `offsets[i] == offsets[i+1]`. Consumers should iterate through
[iter.AllCSRRowsU64], [iter.AllCSRRowsString], or
[iter.AllCSRRowsLatLng] rather than recomputing row bounds by hand.

`PolygonToCellsE` (SD11) and `UncompactCellsE` (SD14) return flat
`[]uint64` rather than CSR — see the deviations note above.

### Grow protocol

Variable-arity calls use a one-retry growth loop against the wasm guest:

1. The Go side presents the caller's destination slice with some capacity.
2. The guest either writes (`rc = growOK`) and reports the actual size
   via a `needed` out-pointer, or returns `rc = growNeedMore` and writes
   the required capacity to the same out-pointer.
3. On `growNeedMore`, the Go side reallocates to `needed` elements and
   re-issues the call **exactly once**. A second `growNeedMore` is treated
   as a protocol violation and surfaces as `ErrGrowProtocol`. By
   construction `needed` is exact on the guest side, so the retry always
   settles in one step.

This means variable-arity calls are at most two host↔guest crossings per
batch, regardless of output size. The CSR layout keeps output allocation
linear in the total number of values rather than in the number of rows.

### Memory model and zero-copy paths

The `Handle.write*E` / `Handle.read*E` helpers use `unsafe.Slice` to
reinterpret the backing byte arrays of `[]float64`, `[]uint64`, and
`[]int32` slices. This is correct only if:

- The host is little-endian. The repository targets Linux x86-64 and arm64
  (both little-endian) per the [portability clause] of `CODINGSTANDARDS.md`.
  Big-endian hosts are not supported.
- The float / integer alignment is at least 1 byte. Go guarantees this.
- The wasm guest agrees on byte order. `wasm32-unknown-unknown` produced
  by LLVM is always little-endian.

Under these assumptions, host→guest transfer of a `[]float64` batch is a
single `wazero.api.Memory.Write` call — one `memmove`. The alternative
(encoding one value at a time via `encoding/binary`) multiplies per-call
CPU cost by batch size and defeats the bulk ABI.

The `readF64sE` helper keeps a small `encoding/binary`-based decode loop
because `wazero.api.Memory.Read` returns a byte slice that aliases the
guest memory — the returned float view must be a Go-owned copy so that
subsequent calls into the guest do not race with the caller's reading.

### Module pool concurrency contract

- `*Runtime` is safe for concurrent use. Multiple goroutines can call
  `AcquireE` simultaneously.
- `*Handle` is **not** safe for concurrent use. A goroutine that acquires
  a handle owns it until it calls `Release`; two concurrent bulk calls on
  the same handle are undefined behaviour.
- `Handle.Release` is idempotent: a double-release silently drops the
  second call, so deferred `Release()` statements at multiple error-exit
  sites do not panic.
- `Runtime.Close` is idempotent but requires every acquired handle to
  have been released first. Closing with handles still acquired is
  undefined behaviour.

Pool size is set by `RuntimeConfig.PoolSize` (default: `runtime.GOMAXPROCS(0)`).
Each pooled module instance has its own linear memory, so aggregate memory
footprint scales linearly with pool size. A single handle with 10^6-element
batches is usually faster and more memory-efficient than sharding a batch
across handles; the pool exists to serve concurrent *callers*, not to
parallelise a single batch.

### Input shapes and iter variants

The canonical input shape is Struct-of-Arrays: parallel `[]float64`
lat/lng slices, or `[]uint64` cell slices. For callers whose source is
naturally Array-of-Structs (an Arrow column extractor, a channel, a
parsed event stream), `LatLngsIterToCellsE` (SD16) accepts an
`iter.Seq2[int, LatLng]` plus an explicit `n` and drains into
reusable Handle-local staging buffers before a single batch write into
wasm scratch. This is an ergonomic convenience — there is still one
unavoidable AoS → linear-memory copy — not a throughput optimisation.
The iterator must yield every index in `[0, n)` exactly once; gaps,
duplicates, or out-of-range indices surface as caller-visible errors.

Iter variants for the other bulk methods are deliberately not added
until a real consumer exposes AoS input for them.

### Arrow interop

The companion subpackage
[`h3arrow`](h3arrow) provides zero-copy adapters from the h3 package's
slice / CSR outputs to arrow-go arrays (`CellsAsArrowUint64`,
`Float64sAsArrowFloat64`, `CSRAsArrowListUint64E`,
`CSRAsArrowListFloat64E`). The adapters wrap caller slices without
copying: the returned arrow arrays hold memory.Buffer references over
the underlying Go backing arrays, so the caller must keep those
slices reachable until the arrow arrays are Released. The subpackage
is separate so consumers of `h3` that do not use arrow-go do not
inherit its import graph.

## Why Rust → WASM → wazero (one paragraph)

Summarised; the full argument is in [ADR-0003](../../../../doc/adr/0003-h3-wasm-bridge.md).
The h3o Rust crate has upstream-parity semantics (pentagons, antimeridian,
boundary behaviour). Compiling it to `wasm32-unknown-unknown` and embedding
the artifact lets the Go side consume H3 without cgo and without a C
toolchain in the build. The ABI is deliberately batch-shaped: per-element
host↔guest crossings dominate runtime at small batch sizes, so the API
refuses to offer single-element entry points. Consumers are nudged towards
batching by the shape of the interface itself.
