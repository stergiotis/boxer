---
type: adr
status: accepted
date: 2026-06-06
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-06
---

# ADR-0069: ImZero2 layered graph widget (`layeredgraph`) — static Graphviz layout via WASM

## Context

The existing ImZero2 `graph` widget ([`egui2_definition_d_graphs.go`](../../public/thestack/imzero2/egui2/definition/egui2_definition_d_graphs.go)) delegates everything to the `egui_graphs` crate and lays out **on the Rust client** with a force-directed simulation (Fruchterman–Reingold, optionally with centre gravity), plus egui_graphs' own basic random/hierarchical modes. [ADR-0062](0062-imzero2-render-cadence.md)/[ADR-0045](0045-imzero2-fsmview-widget.md) and the recent one-shot-fit work (`feat(imzero2/egui2): one-shot fit …`) made it behave, but they cannot change what it fundamentally is:

- **Force-directed is the wrong model for a directional graph.** A state machine (`fsmview`), a DAG, or the leeway describe→IR→map→DDL→marshal→query pipeline has an *inherent flow*. Force-directed optimises for even spacing and hides that flow; the operator sees a blob that has to be dragged into legibility. `egui_graphs`' built-in hierarchical mode is rudimentary — no cycle handling, self-loops, parallel-edge separation, or edge-label space reservation, all of which real state machines need.
- **Rendering quality is capped by `egui_graphs`' stock shapes** (`DefaultNodeShape`/`DefaultEdgeShape`): labels scale with zoom, arrowheads are crude, and the crate is effectively frozen at its current version. The fit-timing fix (option **A** from the prior analysis) did not touch this; options **B** (custom shapes) and **C** (a real layout engine + our own painter) were left open. This ADR is option **C**.

### Terminology

The class of layout `yFiles` (yWorks) is optimal for is the **layered layout** — yWorks brands it "**Hierarchical Layout**"; the algorithmic framework is the **Sugiyama** framework (cycle removal → layer assignment → crossing minimisation → coordinate assignment → edge routing). The graphs it suits are **directed graphs with an inherent flow/hierarchy** — **DAGs** in the acyclic case, cycles handled by temporary edge reversal. "**Static**" here means the layout is *computed once* (a batch/offline pass), as opposed to a force simulation that runs every frame. So the precise descriptor for this widget is **static layered (≡ hierarchical ≡ Sugiyama) layout for directed flow graphs** — hence the name `layeredgraph`.

`Graphviz` `dot` is the canonical, decades-refined implementation of exactly this — and the right baseline for flat directed graphs / state machines. (For *nested/compound* statecharts — composite states drawn as containers — ELK is arguably stronger on compound nodes + orthogonal routing; `yFiles` is the commercial quality ceiling. Both are out of scope; see Deferred.)

### What we already have to build on

- **A full immediate-mode painter.** [`egui2_definition_d_painter.go`](../../public/thestack/imzero2/egui2/definition/egui2_definition_d_painter.go) → the Rust `PaintCmd` enum exposes filled/stroked rounded rects, circles, lines, polylines, `CubicBezier`, `Arrow{ox,oy,dx,dy}`, `Text{px,py,anchor,font_size,monospace}`, and `SenseRegion` (hit-testing). This is the rendering substrate.
- **A DOT emitter.** [`graggle/dot/dot.go`](../../public/algebraicarch/pushout/graggle/dot/dot.go) already writes `digraph { … }`, and `doc/leeway-map/` already shells out to `fdp` — Graphviz is an accepted tool in the toolchain, just not yet in-process.
- **A cgo-free constraint.** The host builds with `CGO_ENABLED=0` ([`rust/imzero2/build_go.sh`](../../rust/imzero2/build_go.sh)). Any layout engine that runs in the Go host must be pure Go.
- **The register-drain + retained-state widget contract** ([ADR-0013](0013-imzero2-stateful-widget-contract.md)) and the demo-registry capture path ([ADR-0057](0057-demo-registry-and-drivers.md)).

This is a **sibling** to `graph` (not a replacement): `graph` stays for genuinely force-directed/live use; `layeredgraph` owns static layered layout.

## Design space (compact)

Three orthogonal questions:

**Q1 — Where does layout run?**
- **α (host-side, chosen):** the Go host computes the layout, ships *positioned geometry* over the FFI, the Rust client paints it. Reuses the painter; layout is deterministic and testable in Go; no graph engine in the client.
- **β (client-side):** ship the graph to Rust, run a layout engine (Graphviz-wasm via wasmtime, or a Rust crate like `layout-rs`) in the client. Duplicates capability the Go host can already host, and the DOT emitter is already in Go. Rejected.

**Q2 — Which engine, and dependency vs clean-room?**
- **D1 — `goccy/go-graphviz` (chosen):** Graphviz compiled to WebAssembly, executed via the `wazero` pure-Go runtime — **no cgo**, cross-platform, version-pinned by the embedded blob. Best-in-class layered layout immediately. (`rigtorp/go-graphviz` is a similar WASM-wrapping alternative; `goccy` is the more widely used.)
- **D2 — clean-room Sugiyama in Go:** sovereign, small, tailored. But layered layout done *well* (Brandes–Köpf coordinate assignment, crossing minimisation, spline routing, label-as-virtual-node placement) is a large, deep effort, and a v1 would ship visibly worse than Graphviz. Boxer's own bar is that reimplementations are justified by **isomorphism** (ImZero ⇄ ImGui) or **closed-loop observability** (spinnaker) — *not* sovereignty alone. Generic graph layout has neither hook, so a clean-room is **not** justified for v1.
- **D3 — small pure-Go Sugiyama dep** (e.g. [`gverger/go-graph-layout`](https://github.com/gverger/go-graph-layout)): no cgo, tiny — but immature and quality well below Graphviz.

**Q3 — What crosses the FFI / how is it drawn?**
- **R1 — layout coordinates → our primitives (chosen):** parse node centres + sizes, edge spline control points, and label positions; draw node = rounded `RectFilled`/`RectStroke` (box) or `CircleFilled`/`CircleStroke` (round); edge = `CubicBezier`/`Polyline`; arrowhead = the existing `Arrow` primitive synthesised from the spline's end tangent; label = `Text`. **This needs zero new `PaintCmd` types.**
- **R2 — replay Graphviz draw-ops (`xdot`/`_draw_`):** literal ellipses/polygons/beziers — higher fidelity but needs a filled-polygon and an ellipse primitive. Deferred fidelity, not v1.
- **R3 — render SVG/PNG, show as image:** simplest, but loses interactivity and needs an SVG/raster path in egui. A possible export/preview mode, not the primary widget.

## Decision

Build **`layeredgraph`**, a sibling widget to `graph`, with:

1. **Host-side layout (Q1-α) behind a `LayoutEngine` seam.** Define a Go interface roughly: `Layout(model GraphModel, opts LayoutOpts) (Layout, error)` where `Layout` carries per-node `{center, w, h, shape}`, per-edge an ordered list of points (straight or cubic-spline control points) + arrow tip + a label box, and an overall bounding box — all engine-neutral. The widget, the FFI payload, and the painter depend only on this interface.
2. **`goccy/go-graphviz` as the v1 engine (Q2-D1).** Build the graph via its `cgraph` API (avoids DOT-string escaping pitfalls) — optionally also accept a **raw DOT string** as a power-user input — run the `dot` engine, render the **`JSON`** format, and parse `pos`/spline/label coordinates into `Layout`. Pure-Go/cgo-free via `wazero`, satisfying the `CGO_ENABLED=0` constraint.
3. **Render from coordinates via the existing painter (Q3-R1).** No new `PaintCmd` variants for v1.
4. **Deterministic by construction.** The embedded Graphviz version is pinned, so layout is reproducible across machines → the demo registers *without* `DemoFlagNonDeterministic`, and `fsmview`'s graph tab (the first consumer — see below) becomes capture-stable.

The clean-room path (D2) and the small pure-Go Sugiyama dep (D3) are **explicitly deferred behind the seam**: because nothing above Graphviz's coordinates crosses the seam, swapping engines later (for blob-size or quality-of-nested-statecharts reasons) is non-breaking and touches only the `LayoutEngine` implementation.

## Subsidiary design decisions

- **Relationship to `graph`.** Sibling, not replacement. `graph` = client-side, live, force-directed (organic relationship graphs). `layeredgraph` = host-side, static, layered (flow graphs). They share the register-drain node/edge accumulator idiom but diverge on where layout runs and what crosses the FFI.
- **`fsmview` is the first consumer ([ADR-0045](0045-imzero2-fsmview-widget.md)).** ADR-0045 §M4 deferred "a richer FSM lib / layout if hierarchical use cases materialise" and chose `egui_graphs` force-directed for v1. This ADR realises the **layout** half of that, and `fsmview`'s Graph tab is the first surface to adopt `layeredgraph` — a state machine is the canonical directed-flow graph and the clearest demonstration. ADR-0045's core decision (two-level statetrooper-backed widget) stands; this is a cross-link and a layout upgrade for its Graph tab, not a supersession.
- **Node shapes (v1):** `box` (rounded rect) and `circle` only, both covered by existing primitives. True `ellipse` (Graphviz default) is deferred (would need an ellipse primitive); use `shape=box`/`shape=circle` in the meantime.
- **Arrowheads (v1):** synthesised with the existing `Arrow` primitive from the edge spline's terminal tangent, rather than replaying Graphviz's arrow polygons. Loses exact arrow-style fidelity (`vee`, `diamond`, …); acceptable for v1.
- **Edges:** Graphviz splines are piecewise cubic → emit one `CubicBezier` per segment; straight edges → `Polyline`. Edge labels → `Text` at the label box centre.
- **Interactivity (v1):** **fit-to-view**, plus node/edge **hover + click** via `SenseRegion` (same event surface as `graph`). **Pan/zoom is deferred to v2** (a client-side scene transform over the painted geometry). **Node dragging / topology editing is out** — a static layout is recomputed, not nudged (that is `egui-snarl`'s job, [ADR-0021](0021-imzero2-snarl-node-editor-binding.md)).
- **Re-layout / caching:** layout is recomputed only when the graph model or layout options change (keyed by a content hash); otherwise the cached `Layout` is reused. This bounds the host-side cost to topology changes, not every frame.
- **License (EPL) — resolved.** boxer is MIT; `goccy`'s embedded `graphviz.wasm` is Eclipse Public License. `goccy/go-graphviz` handles the EPL↔MIT boundary for us: the Go wrapper is MIT and bundles the EPL `graphviz.wasm` as an unmodified artifact, which is distribution-compatible with an MIT consumer (attribution preserved, no copyleft reaching boxer's own MIT code). The `LayoutEngine` seam remains the escape hatch (swap to D3/D2) if this stance ever changes.

## Deferred (defer until needed)

- **Nested / compound statecharts** (composite states as containers): ELK-class capability; Graphviz clusters are serviceable but not its strength. Revisit if hierarchical-state use cases land (ties to ADR-0045 §M4).
- **Higher-fidelity rendering** (R2): ellipse nodes, exact Graphviz arrow styles, filled-polygon arrowheads → add `Ellipse` + `PolygonFilled` `PaintCmd`s then.
- **Pan/zoom (v2)** and node drag-to-pin.
- **Engine swap** to a pure-Go Sugiyama (D3) or clean-room (D2) — gated on blob-size/quality pressure; non-breaking via the seam.
- **SVG/PNG export/preview mode** (R3) — cheap given `goccy` renders these directly; useful for docs/headless, not the interactive widget.

## Alternatives

The options are weighed per-question in [Design space (compact)](#design-space-compact); the rejected/deferred ones, and why:

- **Client-side layout** (Q1-β) — ship the graph to Rust and run a layout engine there. Rejected: it duplicates capability the Go host already has (the DOT emitter is in Go), and host-side layout is deterministic and testable in Go.
- **Clean-room Sugiyama in Go** (Q2-D2) — sovereign and tailored, but layered layout done *well* is a large, deep effort, and a v1 would ship visibly worse than Graphviz. Boxer's bar for reimplementation is isomorphism or closed-loop observability, neither of which generic graph layout has — so it is deferred behind the seam, not adopted for v1.
- **Small pure-Go Sugiyama dependency** (Q2-D3, e.g. `gverger/go-graph-layout`) — cgo-free and tiny, but immature and well below Graphviz quality. Kept as the fallback the `LayoutEngine` seam can swap to under blob-size/quality pressure.
- **Replay Graphviz draw-ops / `xdot`** (Q3-R2) — higher fidelity, but needs new filled-polygon and ellipse `PaintCmd`s; deferred as a fidelity upgrade.
- **Render SVG/PNG and show as an image** (Q3-R3) — simplest, but loses interactivity; a possible export/preview mode, not the primary widget.

## Consequences

- **+** Correct layout semantics for directed flow graphs (FSM, DAG, leeway pipeline) — the actual fix for "the state-machine view is hard to read", not just framing.
- **+** Deterministic, version-pinned layout → capture-stable demos; reuses the painter (no new primitives v1); cgo-free.
- **+** The seam keeps the engine swappable and confines the dependency to one implementation.
- **−** Embeds a multi-MB Graphviz wasm blob + `wazero` (host binary is already ~60 MB, so tolerable, but recorded). An opaque C-as-wasm dependency is a sovereignty/aesthetic cost the seam only partly mitigates.
- **−** Static: re-layout on topology change rather than live animation (intended for this class, but a behavioural difference from `graph`).
- **−** wazero-run Graphviz is slower than native `dot`; negligible at FSM/DAG scale (tens–hundreds of nodes), relevant only at thousands.

## Resolutions at acceptance (2026-06-06)

1. **Name** → `layeredgraph` (capability-honest; matches the "layered" terminology). `dotgraph` rejected as engine-locked; `hiergraph` considered as a shorter alias and set aside for clarity.
2. **First consumer** → `fsmview`'s Graph tab adopts `layeredgraph` (the canonical directed-flow demonstration).
3. **Interactivity** → v1 ships fit-to-view + hover/click; **pan/zoom deferred to v2**; node dragging out of scope.
4. **License** → accepted; `goccy/go-graphviz` handles the EPL↔MIT compatibility (MIT wrapper + unmodified EPL `graphviz.wasm` artifact).

## Status

Accepted (2026-06-06, reviewed by p@stergiotis). The decision is in force; the `LayoutEngine` seam keeps the engine choice (Graphviz → D3/D2) swappable without breaking consumers, and `fsmview`'s Graph tab is the first consumer (see [Resolutions at acceptance](#resolutions-at-acceptance-2026-06-06)). ADRs are append-only; a later engine or rendering change is a Tier 1/2 edit unless it reverses the decision, which would be a superseding ADR.

## Updates

### 2026-06-07 — why box metrics use Helvetica, not the render font

`goccyengine` lays out every node with `fontname=Helvetica` while the painter draws the label in the UI sans font (Noto Sans). This is a **constraint of the embedded engine, not a free choice** — recorded so it is not re-investigated:

- The embedded `graphviz.wasm` measures text during `dot` with its **built-in PostScript estimator**: average-character-width tables for the standard-35 PS families (Times, **Helvetica**, Courier, Symbol). `goccy` registers **no** Go-side text-layout engine (the `TextLayoutEngine` WASM bridge is generated but never wired), and no pango/cairo/gd plugin is compiled in — so nothing can make the WASM engine shape a real TrueType font; an unknown `fontname` (`NotoSans`) just maps back to a default PS family.
- `goccy`'s TrueType/freetype `FontLoader` lives only in the **PNG raster path** (`gvc/image_renderer.go`). We consume `XDOT` *geometry* (Q3-R1) and never call `RenderImage`, so a loaded font cannot reach our box sizes.
- Of the families the estimator knows, **Helvetica** (sans) is closest to Noto Sans; the default **Times** (a narrow serif) under-measures the wider sans and long labels overflow the box in the painter. A small node-`margin` bump absorbs the residual Helvetica↔Noto difference. See [`goccyengine.go`](../../public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine/goccyengine.go).

**Exact alignment would mean bypassing the estimator**: plumb the configured main-font TTF into the Go render loop (today it reaches only the Rust client, as a launch arg — [`application.go`](../../public/thestack/imzero2/application/application.go)), measure each label with a Go shaper matching egui's `ab_glyph`/`rustybuzz` advances (still approximate, cross-language), bake the variable font (`NotoSans[wght]`) to egui's weight, and feed Graphviz fixed `width`/`height` + `fixedsize=true`. A real project with cross-engine metric risk, for a gap the margin already hides — **deferred** behind the `LayoutEngine` seam (the natural home is a pure-Go Sugiyama that could share the renderer's shaper).

Also recorded (append-only, non-reversing): the view gained a per-node `NodeText` hook so a graph mixing light/dark node fills can pair label ink per node; `Layout` now reports the `FontSize` it sized boxes to and the view paints node labels at it — single-sourcing the box-metric and render font sizes so they cannot drift. `apps/capinspector`'s broker schematic is the **second consumer** after `fsmview`.

## References

- Existing widget: [`egui2_definition_d_graphs.go`](../../public/thestack/imzero2/egui2/definition/egui2_definition_d_graphs.go); painter: [`egui2_definition_d_painter.go`](../../public/thestack/imzero2/egui2/definition/egui2_definition_d_painter.go); DOT emitter: [`graggle/dot/dot.go`](../../public/algebraicarch/pushout/graggle/dot/dot.go).
- [ADR-0045 fsmview](0045-imzero2-fsmview-widget.md), [ADR-0013 stateful-widget contract](0013-imzero2-stateful-widget-contract.md), [ADR-0057 demo registry & capture](0057-demo-registry-and-drivers.md), [ADR-0021 snarl node editor](0021-imzero2-snarl-node-editor-binding.md).
- `goccy/go-graphviz` (WASM/`wazero`, cgo-free): <https://github.com/goccy/go-graphviz>; `wazero`: <https://wazero.io/>; Graphviz JSON output (coordinates): <https://graphviz.org/docs/outputs/json/>.
- Layered layout background: yWorks "Layered Graph Layout" <https://www.yworks.com/pages/layered-graph-layout>; Sugiyama / layered graph drawing <https://en.wikipedia.org/wiki/Layered_graph_drawing>.
- Pure-Go Sugiyama alternative (D3): [`gverger/go-graph-layout`](https://github.com/gverger/go-graph-layout).
