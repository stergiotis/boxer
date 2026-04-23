---
type: adr
status: accepted
date: 2026-04-22
reviewed-by: "p@stergiotis"
reviewed-date: 2026-04-22
---

# ADR-0003: H3 Geospatial Indexing via Rust→WASM→wazero Bridge

## Context

Several downstream workloads in this repository are converging on a need for hierarchical geospatial indexing: bucketing trajectories into cells, computing k-ring neighbourhoods for spatial joins, and carrying cell identifiers alongside the Arrow-backed analytics pipelines that permeate [`public/analytics`](../../public/analytics) and [`public/science`](../../public/science). The selected choice for this workload is Uber's H3 system.

No geospatial indexing exists in the codebase today; the `public/science/geo/` subtree is empty prior to this ADR. We therefore have a clean slate on layout and API shape, but are bound by the conventions established in [`CODINGSTANDARDS.md`](../../CODINGSTANDARDS.md): Struct-of-Arrays over Array-of-Structs, named returns, `E`-suffixed fallible functions, pre-allocation, no cgo bloat in consumer builds, and `iter` for exposed collections.

Observed pressures:

- **Throughput-oriented, not object-oriented.** Consumers will process batches of 10^4–10^7 coordinates per operation, stored SoA. A per-element FFI call is categorically unacceptable; the ABI itself has to be batch-shaped.
- **No cgo.** The repository compiles pure-Go today. Introducing cgo for one feature would break cross-compilation flows used elsewhere in the repo and contradicts the "lower cognitive load" motivation of [`CODINGSTANDARDS.md`](../../CODINGSTANDARDS.md).
- **Reference-implementation fidelity matters.** A hand-ported H3 drifts from upstream. The H3 specification encodes many edge cases (pentagonal cells, resolution-0 boundaries, antimeridian crossings) that are easy to subtly miss.
- **wazero is already evaluated.**
- **Rust is not yet used in this repo.** This ADR introduces the first `.rs` files and the first Rust toolchain requirement. That cost has to be justified rather than assumed.

## Design space (QOC)

**Question.** How should Uber H3 functionality be integrated into this Go codebase given the throughput, no-cgo, and upstream-fidelity constraints above?

**Options.**

- **O1** — cgo binding to Uber's reference C library (`github.com/uber/h3-go`).
- **O2** — Pure-Go port of H3 (hand-authored or a community port).
- **O3** — [`h3o`](https://github.com/HydroniumLabs/h3o) (Rust reimplementation) compiled to `wasm32-unknown-unknown` and executed through `github.com/tetratelabs/wazero` *(chosen)*.
- **O4** — Out-of-process H3 service (gRPC / HTTP sidecar).

**Criteria.**

- **C1 — Toolchain neutrality for consumers:** can a downstream `go build ./...` succeed with nothing beyond the Go toolchain?
- **C2 — Bulk throughput:** can the integration sustain amortised per-element cost comparable to a native call, i.e., a batched ABI with negligible per-batch overhead?
- **C3 — Cross-compilation:** does the build work under `GOOS=linux GOARCH=arm64`, Arrow tooling images, and similar cross targets exercised elsewhere in the repo?
- **C4 — Upstream fidelity:** is the H3 implementation maintained with parity to Uber's reference semantics (pentagons, antimeridian, boundary behaviour)?
- **C5 — Blast radius isolation:** is H3 confined behind a single, inspectable surface so bugs cannot leak into unrelated packages?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | ++ | ++ | −  |
| C2 | +  | +  | ++ | −− |
| C3 | −− | ++ | +  | +  |
| C4 | ++ | −  | ++ | ++ |
| C5 | +  | −  | ++ | +  |

O3 is the unique Pareto optimum: dominant on C1, C2, and C5, co-dominant on C4, and only marginally behind the pure-Go options on C3 (one extra build step for the `.wasm`, not a cross-compilation blocker).

## Decision

We integrate H3 via a Rust companion crate using [`h3o`](https://github.com/HydroniumLabs/h3o), compiled to `wasm32-unknown-unknown` and executed through `wazero`. The Go package lives at `public/science/geo/h3`. The public API is Struct-of-Arrays throughout, with CSR-style flattened values plus offsets for variable-arity outputs, and per-element `StatusE` codes for partial-failure reporting.

### Subsidiary design decisions

Each of the following fixes a load-bearing detail of the implementation. Where the viable-option count stayed below the QOC threshold (≥3×≥3), the alternative is captured in prose instead of a matrix.

- **SD1 — WASM target: `wasm32-unknown-unknown`.** Freestanding; smaller binary; Rust crate exports its own `ext_alloc`/`ext_free`. Rejected `wasm32-wasip1` because h3o needs no host syscalls and WASI preview1 imports would be dead weight. (`imzero` uses WASI because it truly needs stdio and clocks; this package does not.)
- **SD2 — Artifact vendoring: commit the built `h3.wasm` under `public/science/geo/h3/internal/h3o_wasm/`, embedded via `//go:embed`.** A CI script (`scripts/ci/h3_wasm_parity.sh`) rebuilds from the Rust sources and byte-diffs against the committed artifact. Rejected build-at-`go build`-time: it would force every contributor to install a Rust toolchain for changes that do not touch the bridge.
- **SD3 — Package location: `public/science/geo/h3`.** `geo` is introduced as a new subtree under `science`. Rejected a top-level `public/geo` subtree: geospatial indexing is a scientific primitive, not a product domain, and siblinghood with [`public/science/units`](../../public/science/units) is the more accurate classification.
- **SD4 — Concurrency model: pool of instantiated modules, checked out via `AcquireE` / `Release`.** `wazero.Runtime` is shared across goroutines; instantiated modules are not — a shared runtime plus a pool of per-goroutine module handles is the canonical shape. Rejected a single-module-plus-mutex design: it serialises bulk calls and negates the reason for the data-oriented ABI.
- **SD5 — Error model: per-element `StatusE uint8` alongside output slices; bulk-level `error` reserved for WASM traps and I/O.** Rejected bulk-only error because a single bad row would discard an entire batch — pathological for ETL workloads where partial results are the norm.
- **SD6 — Variable-arity output layout: CSR (flat values + `[]int32` offsets with `len(offsets) == N+1` and `offsets[0] == 0`, monotone non-decreasing).** Rejected `[][]uint64`: one allocation per row fights SoA discipline and is hostile to Arrow `List` interop.
- **SD7 — MVP surface.** `LatLngsToCellsE`, `CellsToLatLngsE`, `CellsToParentsE`, `CellsToChildrenE`, `GridDisksE`, `CellsToStringsE`, `StringsToCellsE`, `AreValidCellsE`, `GetResolutionsE`. Deferred to a later ADR / package revision: polyfill, cell-boundary polygons, directed edges, compactness. These are non-trivial in their own right and would inflate the first cut beyond reviewable size.
- **SD8 — IDL: hand-written Go bindings over the WASM ABI.** Rejected describing the bridge via [`public/semistructured/leeway/canonicaltypes`](../../public/semistructured/leeway/canonicaltypes): leeway models RPC/FFI contracts with value-level semantics, while the H3 bridge is a byte-level linear-memory ABI whose alloc/grow protocol falls outside leeway's remit.
- **SD9 — Output-slice grow semantics: `slices.Grow` on caller-provided destinations, return the (possibly grown) slice.** Consistent with [`CODINGSTANDARDS.md`](../../CODINGSTANDARDS.md) §Memory Reuse. Rejected pre-sized-only because it puts the arithmetic burden on the caller for operations (children, gridDisk) whose output size is data-dependent.
- **SD10 — Variable-arity ABI: one-retry grow protocol.** Rust writes up to the caller's `cap`, reports `needed` via an out-pointer; if `needed > cap`, Go regrows once and re-calls. At most two crossings per batch; still O(N).

## Alternatives

Rejection rationale for the top-level options is in the QOC matrix; the notes below capture nuance not visible in the ratings.

- **O1 — cgo to `h3-go`.** Eliminated by the no-cgo policy and the resulting cross-compilation degradation. Even with cgo, the per-call boundary would have to be batched manually, landing us in the same ABI-design problem without the isolation benefits.
- **O2 — Pure-Go port.** Either requires authoring a port (unacceptable upstream-fidelity risk for pentagons and antimeridian edge cases) or depending on a community port whose maintenance cadence is uncertain. Neither is a strict win over running the h3o crate as a black box.
- **O4 — Out-of-process service.** Incompatible with the in-process analytics model used by existing packages (Arrow in the same address space); adds deployment surface for a primitive that belongs in a library.

## Consequences

### Positive

- **Bulk throughput by construction.** The ABI is batch-shaped; there is no single-element path for consumers to regress toward.
- **No cgo.** Consumer `go build ./...` remains pure-Go; cross-compilation to the targets exercised elsewhere in the repo stays unaffected.
- **Blast radius is one package plus one embedded wasm.** Any H3 bug is inspectable without stepping into C.
- **Upstream fidelity via h3o.** A maintained Rust reimplementation tracks Uber's H3 semantics (including pentagons and antimeridian) with its own test suite; our Go tests re-validate against golden vectors produced by the Rust side.
- **SoA + CSR outputs align with Arrow.** Variable-arity results can be adapted to `arrow.List` columns without reshaping.

### Negative

- **Rust toolchain for contributors who touch the bridge.** Not needed for pure Go changes (the `.wasm` is embedded), but anyone modifying `rust/h3bridge/` needs `rustup` and the `wasm32-unknown-unknown` target. A `rust-toolchain.toml` pins the version so reproducibility is mechanised.
- **One additional checked-in binary artifact.** The `h3.wasm` blob (expected ~few hundred KiB) lives in the repo. CI parity prevents drift but adds a dedicated check.
- **Per-batch WASM call overhead.** There is no escape from the host↔guest crossing cost; the ABI amortises it across the batch. Very small batches will still look inefficient relative to a native call. Benchmarks will quantify the break-even batch size.
- **Coupling to h3o's evolution.** Breaking changes in `h3o` flow through a Rust source bump and a rebuild; semantically, they also require parity re-checking against upstream H3.

### Neutral

- This is the **first Rust code in the repository.** Future Rust-based bridges (other scientific primitives, codecs with strong Rust ecosystems) become meaningfully cheaper because the build and vendor machinery will already exist.
- The `public/science/geo/` subtree is opened by this ADR. Sibling packages (geodesy, projections, other grid systems) are likely to land here over time; this ADR does not prejudge their shape.

### Derived practices

The following operational heuristics follow from the decision and are recorded here rather than as separate ADRs, so that practice and rationale stay together:

- **Golden vectors are authoritative on the Rust side.** `rust/h3bridge/` ships a `cargo test` harness that emits `testdata/*.ndjson`; Go tests consume those vectors. Any disagreement is a bug in the Go bridge, not in the data.
- **Four test categories per bulk function.**
  (1) Explicit input/expected pairs from golden vectors.
  (2) Round-trip invariants (`cellToLatLng(latLngToCell(p)) ≈ p` within cell edge length; `parent(child(c)) == c`).
  (3) CSR shape invariants on every variable-arity call.
  (4) Grow-protocol fuzz (undersized caps on purpose, verify one-retry settles).
- **Benchmarks cover compiler and interpreter wazero configurations** and include a degenerate "one call per element" negative baseline so throughput regressions in the bulk path show up loudly.
- **`h3.wasm` is rebuilt by CI in a clean workdir and byte-compared** to the committed artifact. Drift fails the pipeline.

## Status

Accepted — 2026-04-22. Design frozen; implementation begins on the `public/science/geo/h3` package skeleton and the `rust/h3bridge/` companion crate.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## Updates

### 2026-04-23 — Implement SD7-deferred surface: polyfill + compactness

SD7 deferred polyfill, cell-boundary polygons, directed edges, and compactness to "a later ADR / package revision." Two of those — polyfill and compactness (both `compact` and `uncompact`) — are the next operations consumer workloads need, so this entry records their API and ABI shape. The core decision (Rust→WASM→wazero; SoA; CSR for variable-arity; per-element `StatusE`; module pool; one-retry grow protocol) is unchanged, so this refinement is recorded inline rather than in a superseding ADR.

**Scope.** Adds `PolygonToCellsE`, `CompactCellsE`, `UncompactCellsE`, and — as of the SD15 addendum below — `CellsToBoundariesE`. Still deferred: bulk-of-polygons, directed edges.

**Subsidiary design decisions (extending the SD series).**

- **SD11 — Polyfill API shape: single-polygon per call.** `PolygonToCellsE` takes flat `vertsLat`/`vertsLng` parallel `[]float64` slices and a `[]int32` `ringOffsets` slice of length `ringCount+1` where `ringOffsets[0]==0` and `ringOffsets[ringCount]==len(verts)`; the first ring is the exterior, subsequent rings are holes. Rejected double-CSR bulk-of-polygons for v1: the incremental ABI complexity (per-polygon ring offsets plus per-polygon cell offsets) outweighs the per-call crossing savings for the expected workload of ~10² polygons, and the Go caller can loop. A bulk form lands in a future update if profiling shows loop cost dominating.
- **SD12 — Containment modes: expose all four h3o variants.** `ContainmentModeE uint8` mirrors h3o's `Centroid`, `Full`, `Overlap`, `OverlapBoundary` with the same ordinal values. Rejected a `Centroid`-only MVP: the differences are semantic (boundary handling, strictness), not performance, and callers unfamiliar with H3 benefit from an explicit choice rather than inheriting a silent default.
- **SD13 — `CompactCellsE` error model: bulk `error` only; no per-element `StatusE`.** `compact_cells` collapses N inputs into M ≤ N outputs with no stable 1:1 mapping, so a per-input status slice has no natural interpretation. Whole-batch failures (mixed resolutions, duplicate inputs) surface as `error`. This is a deliberate local deviation from SD5's per-element-status convention, driven by the operation's semantics rather than by preference.
- **SD14 — `UncompactCellsE` output layout: flat `[]uint64` with per-input `StatusE`; no CSR provenance.** Consumers almost always want the expanded set as a unit; preserving per-input → children provenance doubles the ABI surface (offsets + values) for a case no current workload has requested. Provenance is already available via `CellsToChildrenE`'s CSR output when a caller needs it.
- **SD15 — Cell boundaries: CSR lat/lng + offsets + per-input `StatusE`.** `CellsToBoundariesE` returns the polygonal boundary of each H3 cell as parallel flat `[]float64` lat/lng slices plus an `[]int32` `offsets` slice of length N+1; row i's vertex ring is `lats[offsets[i]:offsets[i+1]]` / `lngs[offsets[i]:offsets[i+1]]`. Typical vertex count is 6 (hexagon) or 5 (pentagon); pentagons whose boundary crosses an icosahedron face edge can reach up to 10 vertices (h3o's documented `MAX_BNDRY_VERTS`). The ring is open — callers that need a closed ring append `row[0]`. Invalid input cells flag `StatusInvalidCell` and produce a zero-length row (`offsets[i+1] == offsets[i]`). Rejected a fixed-6-vertex layout because of pentagons and edge crossings; rejected a tuple-of-(lat,lng) row format because that would break SoA and lose Arrow-`ListOf(FloatArray)` interop.

**New ABI exports (in lock-step with `rust/h3bridge/src/lib.rs`).**

- `h3_polygon_to_cells(lats_ptr, lngs_ptr, ring_offsets_ptr, ring_count, res, mode, out_ptr, cap, needed_ptr) -> u32` — grow-protocol status. Return codes: `0` ok, `1` need-more, `2` bad mode, `3` bad geometry.
- `h3_compact_cells(cells_ptr, n, out_ptr, out_count_ptr) -> u32` — no grow protocol; output fits in `n` slots by construction. Return codes: `0` ok, `1` mixed resolution, `2` duplicate input.
- `h3_uncompact_cells(cells_ptr, n, res, out_ptr, cap, needed_ptr, status_ptr) -> u32` — grow-protocol identical to `h3_cell_to_children`.
- `h3_cell_to_boundary(cells_ptr, n, lats_ptr, lngs_ptr, offsets_ptr, cap, needed_ptr, status_ptr) -> u32` — CSR vertex output; grow-protocol identical to the children ABI. Safe upper-bound capacity is `10*n` vertices (h3o `MAX_BNDRY_VERTS`); the Go-side caller picks a tighter heuristic (`6*n`) and lets the one-retry protocol settle the pentagon-with-edge-crossing cases.

**Testing additions (same four categories as the original SD7 Derived practices).**

1. Golden vectors: US-shaped bounding box, a simple rectangle, and a rectangle-with-hole × three resolutions × all four containment modes for polyfill; small pre-compacted and re-expanded reference sets for compact/uncompact.
2. Round-trip invariants: `uncompact(compact(cells), r) == set(cells)`; every cell in `PolygonToCellsE(P, r, Centroid)` has its center inside `P` on a sampled check.
3. Grow-protocol fuzz: undersized `cellsDst` for polyfill; undersized `expandedDst` for uncompact.
4. Error paths: malformed polygon ring (e.g., `ringOffsets` not monotone), mixed-resolution compact input, uncompact target resolution finer than some input cell.

**Consequences.** Three new user-facing methods, three new ABI exports, one new enum (`ContainmentModeE`). Pulling in h3o's polygon machinery is expected to grow the committed `h3.wasm` by a few tens of KiB; the `CONST_RANDOM_SEED`-pinned build remains byte-reproducible and CI parity continues to gate drift. The `CompactCellsE` deviation from SD5 (bulk-only error) is the first per-operation status-model exception and is documented inline here rather than in `EXPLANATION.md` so that the rationale stays co-located with the decision.

## References

- [`CODINGSTANDARDS.md`](../../CODINGSTANDARDS.md) — Go coding standard (SoA, `E` suffix, pre-allocation, `iter` package, no cgo policy).
- [`doc/DOCUMENTATION_STANDARD.md`](../DOCUMENTATION_STANDARD.md) — Diátaxis + ADR conventions followed by this document.
- [`doc/adr/0001-adopt-diataxis.md`](0001-adopt-diataxis.md) — framework ADR referenced by `status` / `reviewed-by` fields.
- [`doc/adr/0002-nanopass-discipline.md`](0002-nanopass-discipline.md) — prior ADR; shape of QOC + derived-practices pattern.
- [`public/imzero/wasm/wasm.go`](../../public/imzero/wasm/wasm.go) — existing wazero runtime usage; contrast: WASI-based, while this ADR uses `wasm32-unknown-unknown`.
- H3 specification: https://h3geo.org
- `h3o` crate: https://github.com/HydroniumLabs/h3o
- `wazero`: https://github.com/tetratelabs/wazero
