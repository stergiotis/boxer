---
type: adr
status: accepted
date: 2026-04-23
reviewed-by: "p@stergiotis"
reviewed-date: 2026-04-23
---

# ADR-0056: Slippy map + H3 cell overlays via `walkers` + `h3o`/`h3o-wazero`

## Context

ImZero2 had no way to render geographic data. Several in-flight uses needed an interactive basemap plus the ability to overlay markers, tracks, cell-based heatmaps, and aggregated region outlines on top. The design target was the "uniform heatmap" workload: per-frame viewport → cells in viewport → per-cell value → colormap → overlay, at interactive rates on thousands of cells.

Constraints the design had to respect:

- **FFFI2 execution model.** ImZero2 is a register-drain, opcode-stream protocol with deferred-block capture and frame-level culling (see [`SKILLS.md`](../skills/imzero2/SKILLS.md) §11). Any binding must cooperate with those invariants; it cannot rely on synchronous call semantics or persistent stateful encoders.
- **CGO-free Go build.** `rust/imzero2/build_go.sh` sets `CGO_ENABLED=0` deliberately. Go-side geospatial libraries that require CGO (notably `github.com/uber/h3-go/v4`) are out-of-bounds.
- **Eframe backend.** The repo uses eframe with the `glow` feature. Any map library requiring `wgpu` would force a backend switch with cross-cutting effects on every widget.
- **Screenshot-based testing.** The demo test harness relies on headless `IMZERO2_SCREENSHOT_DIR` runs; animated / HTTP-async widgets need to tolerate the 4-frame tour without panicking.
- **Non-UI consumers of H3.** H3 is used (or planned) in ETL pipelines, CLI tools, and services that never spin up a GUI. The binding must not tie H3 availability to a running Rust client process.

## Design space (QOC)

**Question.** How should ImZero2 surface an interactive basemap with H3-cell overlays so that the same cell representation works end-to-end (drawing, interaction, data exchange), without violating the CGO-free and backend constraints?

**Options.**

- **O1 — Walkers map widget + `h3o` on the Rust render side + `h3o-wazero` on the Go data side (chosen).** Native-Rust slippy map (`walkers = "0.53"`), native-Rust H3 (`h3o = "0.9"` with the `geo` feature) for render-side aggregation and boundary computation, and the pure-Go + wasm h3o bridge at `boxer/public/science/geo/h3` for server-side cell work. H3 cell IDs (`u64`) are the wire primitive for ROIs, heatmaps, and click-targets.
- **O2 — Walkers map widget + polygon wire format.** Same basemap; ROIs exchanged as lat/lng ring arrays. H3 appears only as a client implementation detail (or not at all).
- **O3 — Galileo map engine + `galileo-egui`.** Full GIS stack with vector tiles, projections, styled feature layers, and 3D. Requires switching eframe to `wgpu`.
- **O4 — Walkers + `uber/h3-go/v4` CGO binding on Go.** Identical render side to O1; Go-side H3 via Uber's reference C implementation with CGO enabled.

**Criteria.**

- **C1 — CGO-free Go build preserved.** Hard requirement.
- **C2 — Eframe backend unchanged.** Hard-ish; switching has cross-cutting cost.
- **C3 — Cross-platform correctness of H3 operations.** Go-side H3 results must match Rust-side H3 results bit-for-bit (otherwise ROI storage/compare breaks across consumers).
- **C4 — Scope fit for expected workloads.** ROIs, markers, tracks, choropleth, uniform heatmap. Not: styled vector tiles, 3D terrain, custom projections.
- **C5 — Dev cost.** Lines of glue, number of new deps, maintenance surface of the binding.
- **C6 — Forward path.** Can the chosen shape admit the future items (Mapbox vector tiles, editable ROI drawing, projection flexibility) without rework of the wire format?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | ++ | ++ | −− |
| C2 | ++ | ++ | −− | ++ |
| C3 | ++ | +  | +  | +  |
| C4 | ++ | +  | ++ | ++ |
| C5 | +  | ++ | −− | +  |
| C6 | +  | −  | ++ | +  |

O1 is the Pareto optimum for this repo's constraints: the only option that satisfies C1+C2+C3 simultaneously while keeping C5 bounded. O3 is the strongest on future-looking criteria (C4, C6) but requires the eframe backend switch and has an order-of-magnitude higher dev cost. O4 matches O1 on render-side but trips C1. O2 is cheaper than O1 in the short term but loses the "H3 as exchange format" property that the uniform-heatmap challenger was designed around (see [`SKILLS.md`](../skills/imzero2/SKILLS.md) §16.1).

## Decision

We bind the `walkers` crate as a plain ImZero2 widget with a register-drain overlay plugin; `h3o` as the Rust-side H3 implementation for cell boundary computation, multipolygon dissolve, and bbox culling; and `boxer/public/science/geo/h3` (h3o compiled to wasm32-unknown-unknown, driven through wazero) as the Go-side H3 for cell computation off the render path.

Overlay model: all overlays are register-drain nodes — `mapMarker`, `mapPolyline`, `h3CellsColored` (bulk choropleth), `h3Region` (dissolved-outline ROI). A `walkersMap` opcode rendered later in the frame drains all pending overlays into its `OverlayPlugin` and paints them under the plugin's clip rect.

Viewport read-back is a single fetcher node (`fetchR15WalkersCamera`) returning the last-rendered map's camera, bbox, pointer state, and a quantized `viewHash` that Go uses to gate expensive recomputation (cells-in-viewport, colormap).

Tile servers are Go-configurable at the call site via fluid methods (`.TileUrl`, `.TileAttribution`, `.TileMaxZoom`, `.TileSize`). Rust detects config change via a stable signature and rebuilds `HttpTiles` in place, preserving `MapMemory` (pan/zoom) and the H3 outline cache. Empty URL template falls back to the built-in `walkers::sources::OpenStreetMap`.

### Subsidiary design decisions

- **SD1 — H3 cell IDs (`u64`) are the canonical exchange format.** Both sides treat a cell set as an opaque `[]u64` (or a roaring bitmap when sparseness warrants). Lat/lng polygon wire formats are available (`mapPolyline`) but not the recommended path for ROIs. Rationale: deterministic rasterization, exact set operations, free antimeridian and polar correctness, compact storage. Polygon-exchange rejected because two consumers rasterizing the same polygon at different densification thresholds produce different cell sets; cell-set-as-ground-truth avoids the interoperability trap.
- **SD2 — Overlays drain into the next `walkersMap`, not a preceding one.** Each pending-overlay Vec is drained at `walkersMap` apply time. Orphaned overlays (no `walkersMap` in the frame) are logged as a warning and cleared in `prepare_next_frame`. This matches the existing FFFI2 register-drain idiom for plots, graphs, and painter.
- **SD3 — Single `walkers_last_camera` register.** Multi-map frames see the last render's camera. Rejected per-id camera HashMap — complicates the common (single-map) case for a rare multi-map need. Recognised limitation; documented with workarounds in [`SKILLS.md §16.5`](../skills/imzero2/SKILLS.md).
- **SD4 — Non-`'static` `OverlayPlugin<'p>` borrowing a stack-local camera out-slot.** Walkers 0.53 changed the `Plugin` trait from the `'static` bound in earlier versions to a `'c` lifetime, enabling `&'p mut Option<WalkersCamera>` to flow into the plugin without `Arc<Mutex<…>>`. Simpler than the alternative; pins the binding to walkers ≥ 0.53.
- **SD5 — Overlay projection uses absolute viewport coordinates directly.** Walkers' `Projector::project`/`unproject` take/return absolute screen pixels (internally adjust for `clip_rect.center()`). The overlay plugin does **not** add `rect.center()` on either path. First implementation added it twice, producing a zoom-dependent drift that's imperceptible at low zoom and kilometre-scale at high zoom — the bug surfaced under interactive testing and cost one iteration to diagnose. Documented in [`SKILLS.md §16.10`](../skills/imzero2/SKILLS.md) to prevent re-introduction.
- **SD6 — Custom tile source is a `CustomTileSource` struct with leaked-once `&'static str` attribution.** Walkers' `Attribution` type requires `&'static str`. Runtime-supplied strings are `Box::leak`'d at source-construction time (once per unique tile config), not per `attribution()` call. Bounded growth in practice; eliminates the attribution cost from the fetch hot path. Documented in [`SKILLS.md §16.8`](../skills/imzero2/SKILLS.md).
- **SD7 — Naive AABB bbox culling, no antimeridian handling.** Overlays are coarse-culled by AABB intersection in lat/lng space with a 5% viewport margin. Polygons crossing ±180° longitude cull incorrectly (their bbox degenerates or inverts). Deferred: the first real dataset hitting this needs an antimeridian-aware splitter; until then, documented as a known limitation.
- **SD8 — Concave `h3Region` fill renders as per-cell hexes.** Drawing the dissolved multipolygon outline would require `lyon_tessellation` for concave-safe fill. For the expected cell counts (hundreds to low thousands per ROI) per-cell fill is fast enough and visually communicates the H3 grid. Lyon can be added later when a real country-scale workflow demands smooth fills.
- **SD9 — H3 compute on Rust side for render operations; h3o-wazero on Go side for data operations.** Boundaries + dissolve run on the Rust side (they're needed for rendering, and Rust-native is fastest). Cells-in-viewport, grid-disk expansion, cell-from-lat-lng run on the Go side via h3o-wazero when the Go code has its own consumer (ETL, heatmap pre-compute). Both sides use the same `h3o` crate (one native, one wasm), so cell-ID semantics match bit-for-bit — the central property that makes `u64` a viable exchange format.
- **SD10 — One shared `h3.Runtime` + `Handle` per process.** ImZero2's Go side is single-threaded, so a `PoolSize: 1` runtime with one long-lived handle is sufficient and amortises wazero's scratch allocation. Lazy `sync.Once` init on first demo frame; errors surface as a graceful UI fallback rather than a panic.
- **SD11 — `walkers_map` uses a thick-client apply path, not a single `{{Instance}}` template.** The generated dispatch calls a hand-written `ImZeroFffi::render_walkers_map(...)` method with all IDL args as parameters. Rationale: the render body does non-trivial state reconciliation (tile-config signature check, HttpTiles rebuild, overlay drain + pre-projection, plugin construction with borrow-split of `WalkersState`) that doesn't fit the generator's single-expression apply-code template. Same pattern used by `Table` / `Graph` render bodies.

## Alternatives

- **O2 — Polygon wire format.** Simpler IDL, same Rust side minus the H3 aggregator. Loses bit-exact cross-consumer cell agreement (SD1), antimeridian / pole correctness, and the O(1) "is point in ROI" test. Would require a Go-side H3 library anyway for any workflow that joins ROI cells against H3-indexed datasets, so the cost saved on the wire is paid back on the data side.
- **O3 — Galileo.** Strictly more powerful — vector tiles, custom projections, GPU-batched feature layers, scales to millions of features. Costs: eframe `glow → wgpu` flip (affects every widget), order-of-magnitude more wire surface, a separate `galileo-egui` integration that owns its own render target. Worth revisiting if a real need materialises for projection flexibility or ≥ 100 k interactive features; until then the complexity isn't justified.
- **O4 — Uber `h3-go/v4` with CGO.** Battle-tested H3 reference implementation. Rejected because `build_go.sh` explicitly disables CGO and flipping that switch affects cross-compilation, Docker images, the race detector, and every existing pure-Go consumer. The wazero bridge is a one-time setup cost that preserves the CGO-free invariant and gives bit-exact parity with the Rust side (which Uber's C library does not guarantee at the least-significant-bit level for all operations).

## Consequences

### Positive

- **H3 cell IDs flow through the entire stack unchanged.** Go computes a cell set, ships it as `[]u64`, Rust dissolves it for rendering, Rust fetches viewport bbox back, Go recomputes cells for the new viewport. One data type, one meaning, no lossy conversions.
- **Uniform heatmap workload works end-to-end.** The challenger app that shaped the design (per-frame viewport → polygonToCells → per-cell value → colormap → render) is implemented in the demo with a `viewHash`-gated cache that keeps still cameras at zero CPU.
- **Tile servers are Go-driven.** Users pass any XYZ URL template at the call site; the Rust side swaps `HttpTiles` in place without losing pan/zoom state. No recompile for new tile providers.
- **No CGO, no backend switch.** The whole binding drops into the existing build with `walkers`, `h3o`, `lyon_tessellation` (currently not used but planned), and `boxer/public/science/geo/h3` (already a dep via go.mod). Binary grows by ~5 MB.
- **Forward path preserved.** Future work (Mapbox vector tiles, additional tile sources, editable ROI drawing, antimeridian handling, Lyon tessellation) lands additively on the existing IDL surface; no wire-format break anticipated.

### Negative

- **Bug 2 — walkers' Ctrl+Wheel semantics across multiple maps.** When two `walkersMap` widgets are visible simultaneously, Ctrl+Wheel zooms both even if the pointer is over one. Walkers-side issue; documented in [`SKILLS.md §16.4`](../skills/imzero2/SKILLS.md) with a `.ZoomGesture(false)` workaround for secondary maps. Upstream PR candidate if the workaround proves insufficient.
- **Single `walkers_last_camera`.** Multi-map frames lose per-map camera distinction (SD3). Workarounds documented in [`SKILLS.md §16.5`](../skills/imzero2/SKILLS.md).
- **Antimeridian / polar polygons cull incorrectly (SD7).** Deferred until first real dataset exhibits the bug.
- **Concave fill is per-cell hexes (SD8).** Fine at H3-native scales; visible grid at country scale. Lyon tessellation is the escape hatch when needed.
- **Attribution leaks `&'static str` at tile-config changes (SD6).** Bounded growth — one leak per unique attribution across the process lifetime. Not a leak in any useful sense unless an adversary forces unbounded tile configs.
- **Tokio + reqwest pulled in by walkers.** ~5 MB binary growth on native; no wasm impact. Acceptable per the dep footprint budget.
- **Walkers ≥ 0.53 pinned.** SD4 (non-`'static` plugin) relies on the trait-bound change in walkers 0.53. Earlier versions would require refactoring the plugin to use `Arc<Mutex<…>>` for the camera out-slot.

### Neutral

- **`OverlayPlugin` clips to the map rect via `ui.painter_at(rect)`.** Necessary for correctness (overlays previously bled onto surrounding UI) and well-isolated from walkers' own rendering (which happens before the plugin runs).
- **`h3o-wazero` scratch is pool-checked-out.** `PoolSize: 1` is sufficient for the single-threaded UI path; data pipelines running in parallel goroutines acquire additional handles from a larger-pool runtime they construct separately.
- **Naive point-cull for markers, per-cell cull for choropleth.** Matches the shape of the cost surface: markers are cheap to skip individually; choropleths amortise a bbox check across all their cells. Per-ring cull for multi-polygon regions is included for the rare multi-ring ROI case.

### Derived practices

- **New overlay types follow the same shape.** Register-drain node → pending Vec on `ImZeroFffi` → cleared in `prepare_next_frame` → drained by `render_walkers_map` → pre-projected into a renderable form → fed to `OverlayPlugin`. Any new overlay (e.g. heatmap grid lines, animated markers) adds one struct, one pending Vec, one prerender function, and a branch in the plugin's `run()`.
- **H3 on the data side routes through `boxer/public/science/geo/h3`.** Direct `//go:build cgo` dependencies on `uber/h3-go/v4` are out of scope for a downstream consumer's Go code. If a data pipeline needs H3 operations not yet exposed by the boxer wrapper, extend the wrapper rather than bypassing it.
- **Tile server presets live in call sites, not in the binding.** `walkers::sources::Mapbox` / `Geoportal` / `OpenFreeMap` exist on the Rust side but aren't wired in; users pass `{z}/{x}/{y}` templates via `.TileUrl(...)`. A named-preset enum is scope creep with no payoff given the custom URL path.
- **The uniform heatmap demo is the reference implementation.** Future work adjusting the viewport→cells→colormap pipeline should modify that demo first and then generalise to library code if patterns stabilise.

## Status

Accepted — 2026-04-23. Implementation shipped across commits `8669a813` (initial binding), `0556800f` (tile server config), `1bdbebc8` (RadioButton UX cleanup), `c5f163ba` (h3o-wazero integration + uniform heatmap), and `7162f955` (SKILLS.md §16 + this ADR). Known limitations documented here and in [`SKILLS.md §16`](../skills/imzero2/SKILLS.md) are deferred work with explicit triggers — no blocker for the current scope.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## References

- [`doc/skills/imzero2/SKILLS.md`](../skills/imzero2/SKILLS.md) §16 — walkers binding limitations and gotchas (companion to this ADR).
- [`doc/adr/0003-imzero2-unified-color-type.md`](0052-imzero2-unified-color-type.md) — prior ImZero2 binding ADR; template shape followed here.
- [`public/thestack/imzero2/egui2/definition/egui2_definition_d_walkers.go`](../../public/thestack/imzero2/egui2/definition/egui2_definition_d_walkers.go) — walkers IDL definitions (walkersMap, mapMarker, mapPolyline, h3CellsColored, h3Region, fetchR15WalkersCamera).
- [`rust/imzero2/src/imzero2/interpreter.rs`](../../rust/imzero2/src/imzero2/interpreter.rs) — `WalkersState`, `CustomTileSource`, `OverlayPlugin`, `render_walkers_map`, `aggregate_h3_region`, `bbox_of_rings`.
- [`public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_walkers_demo.go`](../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_walkers_demo.go) — reference demo including the uniform-heatmap challenger.
- [`boxer/public/science/geo/h3/`](https://github.com/stergiotis/boxer/tree/main/public/science/geo/h3) — h3o compiled to wasm + wazero runtime (Go-side H3 without CGO).
- [`walkers = "0.53"`](https://crates.io/crates/walkers) — slippy map widget for egui.
- [`h3o = "0.9"`](https://crates.io/crates/h3o) — pure-Rust H3 implementation (native + wasm).
- ADR-0052's SKILLS.md reference block for §11 block-skipping / culling remains load-bearing for the overlay drain semantics here.
