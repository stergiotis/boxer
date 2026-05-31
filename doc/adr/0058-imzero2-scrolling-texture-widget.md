---
type: adr
status: accepted
date: 2026-04-23
reviewed-by: "p@stergiotis"
reviewed-date: 2026-04-23
---

# ADR-0058: `scrollingTexture` — Ring-Buffer Pixel Widget with Go-Side Colormap

## Context

ImZero2 is a Go-driven, Rust-rendered UI layer over [egui](https://www.egui.rs/). Widgets are declared in hand-written IDL files under [`public/thestack/imzero2/egui2/definition/`](../../public/thestack/imzero2/egui2/definition); a codegen pass (`./generate.sh`) produces the Go wrappers (`*.out.go`) and the Rust dispatch in [`rust/imzero2/src/imzero2/interpreter.rs`](../../rust/imzero2/src/imzero2/interpreter.rs). Today the widget catalogue covers scalar inputs, line and bar plots (`plot`), virtual-scrolled tables (`table2`), geospatial walkers (`walkers`), and a low-level painter escape hatch (`paintCanvas`) that issues egui primitive draw calls (rects, lines, circles, beziers, text). There is no texture-backed widget: no code path uploads a CPU-side pixel buffer to an [`egui::TextureHandle`](https://docs.rs/egui/latest/egui/struct.TextureHandle.html).

Two concrete near-term consumers need pixel-data rendering:

- **Audio spectrograms**, ported from [`../egui_spectrogramm`](../../../egui_spectrogramm/) — scrolling time × frequency × dB magnitude, typically 512 wide × 1024 tall, redrawing at tens of Hz.
- **RF waterfalls** — the convention for SDR applications ([GQRX](https://github.com/gqrx-sdr/gqrx), [SDRangel](https://github.com/f4exb/sdrangel), [inspectrum](https://github.com/miek/inspectrum)); same data shape, vertical scroll, different axis units.

The general pattern is streaming 2D intensity display: time × N bins × scalar intensity. Thermal imaging, radar, sonar, rolling network-traffic heatmaps all fit. A landscape survey of comparable widgets ([ImPlot](https://github.com/epezent/implot), [matplotlib](https://matplotlib.org/), [PyQtGraph](https://pyqtgraph.readthedocs.io/), [Makie.jl](https://docs.makie.org/), [Observable Plot](https://observablehq.com/plot/), SDR apps, [Grafana heatmap panel](https://grafana.com/docs/grafana/latest/panels-visualizations/visualizations/heatmap/)) produced convergent findings:

- Texture-backed rendering is the consensus implementation; per-cell draw calls cap out quickly (ImPlot's 16-bit `ImDrawIdx` limit hits at ~65k cells, i.e. 256×256).
- Perceptually uniform colormap defaults (Viridis, Cividis) are baseline; `jet` lingers in some toolkits and is now a red flag for scientific audiences.
- NaN-aware bad-value coloring is a first-class API in every serious toolkit (matplotlib `set_bad`, Makie `:transparent` default, PyQtGraph `nanPolicy`).
- Widget-owned axes couple tightly to layout and prevent composition with external rulers, markers, or linked panels; SDR apps all split spectrum, waterfall, and markers into separate composable layers.
- Bilinear texture sampling is routinely misread by users as data interpolation; toolkits have had to publish documentation pages specifically to disabuse that reading.

Forces that the decision must respect:

- **FFFI2 rules.** Generated files under `components/*.out.go` and [`rust/imzero2/src/imzero2/interpreter.rs`](../../rust/imzero2/src/imzero2/interpreter.rs) are off-limits; all changes land in hand-written definition files and the generator. FFFI2 reserves argument names matching `[a-zA-Z][0-9]*` ([`fffi2/ir/idl/fffi2_ir_idl_arguments.go:23`](../../public/thestack/fffi2/ir/idl/fffi2_ir_idl_arguments.go)). Documented in [`doc/skills/fffi2/SKILLS.md`](../skills/fffi2/SKILLS.md).
- **Execution model.** The runtime is not a linear opcode stream: deferred blocks are recorded Go-side and spliced in at a different position later; the Rust side can cull whole blocks ([`doc/skills/imzero2/SKILLS.md`](../skills/imzero2/SKILLS.md) §11). Any stateful encoding scheme that assumes ordered execution breaks under this model.
- **Continuous rendering.** The Rust `logic()` pass unconditionally calls `ctx.request_repaint()` each iteration; the loop is not reactive. Widgets never need to trigger their own repaints; conversely, widgets cannot assume that "no new data" means "no redraw".
- **FFFI databindings reset each Sync.** `r9_*` bindings carry a one-frame lag; hover readouts delivered via registers are one frame behind the pixels that produced them.
- **Unified color type.** [ADR-0052](0052-imzero2-unified-color-type.md) established `egui2.Color` as the Go type for single-argument colors, with `.AsColor()` annotation on IDL args. Bulk pixel buffers (arrays of per-pixel colors) are a distinct surface not addressed by that ADR.

The question this ADR settles: how does ImZero2 introduce its first pixel-data primitive, given no texture-upload infrastructure exists today, multiple future consumers exist, and the scientific-viz framing demands specific defaults?

## Design space (QOC)

**Question.** How should pixel-data rendering enter the ImZero2 widget catalogue?

**Options.**

- **O1 — Purpose-built `scrollingTexture` widget with encapsulated texture cache _(chosen)_.** One new `BuilderFactoryNode`. A hand-written Rust module `scrolling_texture.rs` owns `HashMap<id, TextureHandle>`, ring-buffer write arithmetic, and split-UV draw. Texture is not exposed; caller ships RGBA columns and a head cursor.
- **O2 — General `imageUpload` primitive + separate `scrollingDisplay` widget.** An orthogonal "upload a pixel buffer, get a handle" op, and a scrolling widget that takes a handle plus scroll parameters. Composable, reusable for non-scrolling images (thumbnails, static heatmaps, preview panes).
- **O3 — Extend `paintCanvas` with a texture-upload opcode.** Callers composite pixel data alongside existing painter primitives (lines, rects, text) inside a single canvas.
- **O4 — Wrap `egui_plot::PlotImage`.** Go streams packed RGBA; widget lives inside an `egui_plot` plot. No new Rust texture-management code; reuses plot axes and zoom/pan.

**Criteria.**

- **C1 — IDL surface added now.** Nodes, opcodes, register buckets introduced in this ADR's implementation. Lower is better.
- **C2 — Extensibility to future pixel-data widgets.** Cost to factor out a general image primitive later; cost to add non-scrolling variants (static heatmap, thumb-strip, webcam preview).
- **C3 — Performance ceiling.** Highest `widthSlots × heightSlots × redrawRate` that the approach sustains without architectural rework.
- **C4 — Fit with existing ImZero2 patterns.** Reuse of per-widget encapsulated-module posture (`plot`, `table2`, `walkers`, `paintCanvas` each own their state), generator seams, register conventions.
- **C5 — Scientific-viz requirements.** Honest sample → pixel mapping; NEAREST sampling reachable; no autoscaling surprises; no widget-owned axes that steal layout control.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | −  | +  | ++ |
| C2 | +  | ++ | −  | −  |
| C3 | ++ | +  | −− | +  |
| C4 | ++ | +  | +  | +  |
| C5 | ++ | +  | −  | −  |

O1 wins on C1 (one node plus one small release opcode), C3 (texture + `set_partial` + split-UV draw is the convergent fast path; no per-cell primitive ceiling), C4 (encapsulated per-widget module matches existing pattern), and C5 (the caller owns data→color mapping and has no layout surprises from widget-owned axes). O2 edges O1 on C2 only; the advantage materialises exclusively if and when a second pixel-data consumer appears. O3 loses C3 hard: per-cell `paintCanvas` primitives blow up at spectrogram resolutions (ImPlot's documented 16-bit index ceiling is the canonical failure mode). O4 loses C2 and C5 — a `PlotImage`-wrapped widget is permanently stuck inside an `egui_plot` `Plot`, its axes inseparable from the plot's, and cannot serve non-plot consumers (webcam preview, free-floating thumbstrip).

## Decision

We introduce `scrollingTexture` as a purpose-built widget that owns a per-widget-id `egui::TextureHandle` in a hand-written Rust module `rust/imzero2/src/imzero2/scrolling_texture.rs`, accepts a pre-packed `u32[]` RGBA column payload per frame plus a caller-supplied head cursor, and renders via a two-call split-UV `Painter::image` draw. The widget is domain-neutral; it has no knowledge of spectrograms, frequencies, intensities, or colormaps.

All `f32 → RGBA` mapping — colormap lookup, intensity scaling (Linear / Log / Db), bad / underflow / overflow substitution — lives in a new Go-side package `public/thestack/imzero2/egui2/widgets/colormap/`. A thin wrapper package `public/thestack/imzero2/egui2/widgets/heatmapscroll/` composes the colormap with the `scrollingTexture` widget for the common "scientific heatmap from an `f32` stream" caller.

### Subsidiary design decisions

- **SD1 — Colormap in Go, not Rust.** The Go-side colormap package runs the full `[]f32 → []uint32` pipeline (intensity scale, LUT lookup, bad/under/over substitution). The Rust widget never sees raw f32 samples. Rationale: (a) wire cost is identical (4 bytes per row either way); (b) Go retains full control over user-supplied LUTs, custom transfer functions, and live gradient swaps without requiring Rust-side enum additions; (c) the Rust widget stays domain-neutral and reusable for any scrolling-pixel purpose (webcam, thumbstrip, procedural imagery); (d) matplotlib/PyQtGraph-style caller-owned LUTs are the convergent convention across toolkits surveyed. Trade-off: live gradient swap on a populated ring re-ships `widthSlots` columns (~2 MB for a 512×1024 ring at 4 B/pixel) — a one-time blip on user-initiated config change, not per-frame churn.

- **SD2 — Caller owns head cursor; widget is stateless w.r.t. scroll.** Go tracks the head position and passes it on each `scrollingTexture` call; Rust writes `newCount` columns starting at `head` modulo `widthSlots` and draws with UV split at `(head + newCount) mod widthSlots`. Rationale: the runtime's deferred/culled block model forbids hidden Rust-side state that assumes ordered execution ([ADR-0052 SD2](0052-imzero2-unified-color-type.md) addresses the same concern for color-reuse caches); caller-owned state gives deterministic replay, pause/rewind, and random-access partial-column refresh with no additional API surface. Rust does arithmetic only.

- **SD3 — Filter enum (`FilterNearestE | FilterLinearE`), not `bilinear: bool`, with `FilterNearestE` default.** Rationale: the survey is emphatic that "bilinear=true" is routinely misread as data interpolation across columns. Naming the concept as GPU texture sampling (mirroring egui's `TextureOptions::{NEAREST, LINEAR}`) is honest; defaulting to NEAREST matches the scientific-correctness posture.

- **SD4 — No axis rendering inside the widget.** Axis ticks and labels are composed externally (Go-side), using either the existing `plot` widget, the `paintCanvas` escape hatch, or Talbot tick generation from boxer (via the `finddivisions` package). Rationale: axis rules are domain-specific (Hz log ticks for spectrograms, elapsed-time `hh:mm:ss` for rolling metrics, sample-index ints for DSP debugging); embedding any single scheme constrains the widget's generality. The survey confirms widget-owned axes prevent layered composition (SDR apps all separate axes, waterfall, and markers).

- **SD5 — Bad / underflow / overflow coloring lives in the Go `colormap.Config`.** Three distinct `color.NRGBA` fields (`BadColor` for NaN / ±inf, `UnderflowColor` for samples below `DataMin`, `OverflowColor` for samples above `DataMax`), matching matplotlib's `set_bad` / `set_under` / `set_over` API. Rationale: consequence of SD1 — since Go does the `f32 → RGBA` mapping, it is the only layer that can observe non-finite or out-of-range samples; exposing three knobs matches established convention and costs only a few additional LOC in the Go package. No IDL impact: Rust never sees these.

- **SD6 — Per-column stats returned from the Go wrapper.** The `heatmapscroll` package's column-push method returns a `ColumnStats` value with `BadSamples`, `Underflow`, `Overflow` uint32 counts. Rationale: science-friendly — callers can assert `stats.BadSamples == 0` in tests, log rates in production, or surface them in the UI. No FFFI register traffic needed; counts are naturally available to Go during the `f32 → RGBA` pass.

- **SD7 — Frame-LRU eviction by default, explicit `scrollingTexture.release(id)` override.** Texture entries not touched for ≥N frames (default N = 600, ~10 s at 60 Hz) are dropped from the Rust-side cache and their `TextureHandle` is released at the next frame boundary. Callers with predictable lifecycle (tab close, demo teardown) can force eviction via the sibling opcode. Rationale: forgiving default for demos and transient widgets; predictable fast path for lifecycle-managed callers; no per-widget disposal API required at the Go surface for the common case.

- **SD8 — Four orientations in one byte.** `OrientationE` enum: `ScrollLeftE` (append right, scroll left — classical audio spectrogram), `ScrollRightE`, `ScrollUpE`, `ScrollDownE` (append top, scroll down — classical RF waterfall). Rationale: both mainline conventions must be supported; horizontal and vertical symmetry is cheap — only the draw-side UV and rect calculation differ. No runtime cost beyond a branch.

- **SD9 — Column payload is raw `u32[]`, not an array of `egui2.Color`.** [ADR-0052](0052-imzero2-unified-color-type.md) establishes `egui2.Color` as the unified type for single-argument colors; its construction involves either a literal `u32` or an `EvaluatedArg` retained holder. For a bulk per-pixel column (typically 1024 entries, updated tens of times per second), per-element construction of `egui2.Color` values would allocate and dispatch through a union unpacker in the hot loop. Per-pixel bulk buffers are therefore out of scope for ADR-0052's convention; the Go `colormap` package writes directly into a reusable `[]uint32` slice and hands the slice to the `scrollingTexture` call. The `egui2.Color` type remains the convention for individual color arguments (e.g. a future border-tint on the widget, if introduced).

- **SD10 — No general `imageUpload` primitive introduced in this ADR.** The texture cache is private to `scrollingTexture`. When a second consumer materialises (non-scrolling image display, static heatmap, rolling webcam preview, matte thumbnail panel), a follow-up ADR extracts a general texture primitive and migrates `scrollingTexture` to consume it. Rationale: YAGNI; the extraction cost is bounded (the texture cache is ~100 LOC), the second-consumer shape is unknown, and designing against hypothetical requirements produces worse abstractions than designing against concrete ones.

- **SD11 — Hover readout returns `(row, col)` in screen-axis index space.** `row` is the screen-y index, `col` is the screen-x index, packed into a single `r9_u64` entry as `((row as u64) << 32) | (col as u64)`; sentinel `u64::MAX` means "pointer outside the widget". For horizontal orientations this aligns with the underlying data ring (row = bin, col = ring_position); for vertical orientations the screen axes are rotated relative to the ring layout, so the mapping swaps (row = ring_position, col = bin). Callers that need orientation-independent data-space coordinates can swap based on the configured `OrientationE`. Rationale: screen-axis indices are the intuitive reading while watching the widget, and data-space reinterpretation is a one-line swap for callers that need it — returning data-unit values would require pushing domain semantics to Rust or round-tripping through Go (per SD1). Bucket choice: the interpreter's r9 register exposes `u64`/`i64`/`f64`/`s` variants only — `r9_u64` holds both u32 coordinates together in one ID↔value entry per widget per frame.

- **SD12 — Click reported as a boolean via `r10`.** Raw click semantics; any richer interaction (selection rectangle, region-of-interest, modifier-aware clicks) is deferred. Rationale: matches the existing `r10` convention; avoids committing to an interaction model before a real consumer needs one.

## Alternatives

Rejection rationale for the top-level options is in the QOC matrix; the notes below capture detail not visible in the ratings.

- **O2 — General `imageUpload` + `scrollingDisplay`.** Viable and future-extensible; rejected on speculative-scope grounds. No second consumer exists today. The texture-cache abstraction fits in ~100 LOC; extracting it later, when a second consumer has concrete requirements, produces a better-shaped abstraction than committing now. SD10 preserves this option as an additive migration.
- **O3 — Extend `paintCanvas`.** The existing `paintCanvas` is the right escape hatch for small-count primitive drawing; for pixel data it hits the per-cell-primitive ceiling documented by ImPlot (~65k cells on 16-bit `ImDrawIdx`) well before spectrogram resolutions. A texture-upload opcode inside `paintCanvas` would also need its own cache keyed by canvas id, reintroducing per-widget encapsulation under a misleading banner.
- **O4 — Wrap `egui_plot::PlotImage`.** `PlotImage` textures a quad in plot coordinates; the `Plot` container owns axes, zoom, and pan. The widget becomes inseparable from a plot and cannot serve standalone use cases (webcam preview, free-floating thumbstrip). The survey's strongest pattern — separate axes from raster for composability — is forfeited.
- **Rust-side colormap (rejected per SD1).** Considered; rejected because payload parity removes the only real argument in its favour while Go retention unlocks user-supplied LUTs, live gradient swaps, and keeps the Rust widget domain-neutral.
- **Rust-owned ring cursor (rejected per SD2).** Hidden Rust state breaks under deferred/culled blocks and removes the deterministic-replay property.
- **Single bad-value color (rejected per SD5).** Initially proposed; the user preference is three colors matching matplotlib's `set_bad` / `set_under` / `set_over`. Cost is a few LOC in the Go package; the convention is well-established.

## Consequences

### Positive

- **ImZero2 gains its first pixel-data primitive.** One `BuilderFactoryNode` + one `release` opcode, ~100 LOC of hand-written Rust, no changes to the generator. Enables spectrogram, waterfall, heatmap, and any future scrolling-pixel consumer.
- **Rust widget stays domain-neutral.** Serves audio spectrogram, RF waterfall, thermal imaging, network-traffic heatmap, rolling webcam preview, thumbstrip — all with one code path. Thin Go wrappers package the domain semantics.
- **Live gradient and data-range changes are cheap.** Go recomputes RGBA and pushes a fresh set of columns; Rust re-renders the texture. No Rust-side invalidation protocol, no cache bust across consumers.
- **Deterministic replay, pause/rewind, random-access refresh fall out for free.** Caller-owned cursor (SD2) makes these zero-API-cost.
- **Scientific defaults baked in.** The Go `colormap` package ships perceptually uniform gradients (Viridis, Inferno, Magma, Plasma, Turbo, Cividis, Greys, RdBu), `FilterNearestE` default (SD3), caller-supplied range (no autoscaling), and three-way bad/under/over coloring (SD5). Removes "did the caller remember to configure this?" as a failure mode.
- **Stats surface per-column.** `ColumnStats` (SD6) makes data-quality issues observable in tests and production without extra instrumentation.
- **Composable with existing plot widget.** A typical "FFT slice above scrolling waterfall" layout stacks the existing `plot` widget on top of `scrollingTexture` with a shared frequency range held Go-side — no cross-widget coordination needed in Rust.

### Negative

- **Three Go packages to maintain.** `colormap/` (pure data), the `widgets/heatmapscroll/` wrapper, and the `egui2/definition/egui2_definition_d_scrolling_texture.go` IDL definition. Each is small; the total surface is larger than a single-package shim would be.
- **No shared texture pool.** Each live `scrollingTexture` id holds its own GPU texture. LRU reap (SD7) mitigates waste but does not eliminate it; a UI with dozens of concurrent heatmaps pays a linear GPU-memory cost.
- **Live gradient swap re-ships the full buffer.** One-time 2 MB blip for a 512×1024 ring on user-initiated config change. Acceptable for interactive use; would need revisiting if config changes become per-frame.
- **Future general-image widget requires migration.** If a second pixel-data consumer produces enough shape to warrant extracting a shared texture primitive (SD10), the `scrollingTexture` caller API stays stable but the Rust internals refactor. Extraction cost is bounded but real.
- **Hover readout one frame behind pixels.** Consequence of FFFI r9 lag (see context); callers that need zero-lag readout must read pointer position Go-side and index their own retained ring. Documented in the how-to; not a widget bug.

### Neutral

- **`./generate.sh` seam is unchanged.** `interpreter.rs` remains codegen'd; all new Rust logic lives in the hand-written `scrolling_texture.rs` module, called from one-line apply snippets injected via `.WithApplyCodeClientRust(...)`.
- **`paintCanvas` is unaffected.** `scrollingTexture` does not pass through it, nor replace any existing painter primitive.
- **`egui2.Color` (ADR-0052) is unaffected.** The widget has no single-argument color surface today; if one is added later (e.g. border tint), it uses the `.AsColor()` annotation per that ADR. SD9 carves a narrow exception for the bulk column payload.
- **The audio-engine-in-Rust question is orthogonal.** Whether sample acquisition lives in Rust (rodio/cpal) or Go (`gopxl/beep`) is decided separately; `scrollingTexture` consumes RGBA regardless of where the `f32` samples originated.

### Derived practices

- **"Caller-owned state for streaming widgets" generalises.** For any future widget with a ring-buffer-like progression (rolling line plots, log tails, thumbstrips), the caller should own the advancement cursor and the widget should do arithmetic only. This matches ImZero2's broader posture — the Rust side is authoritative over GPU/texture objects, not application state — and composes correctly with deferred/culled execution.
- **"Colormap-in-Go" generalises.** For any future widget where a Go-side data → pixel mapping is cheap and the alternative would add Rust-side enum knobs, prefer Go. Reserve Rust-side color work for cases where GPU-compute colormapping becomes the performance floor; none exist today.
- **Scientific defaults are the canonical reference.** The `colormap` package (no jet, `NEAREST` default, explicit range, three-way bad/under/over) is the reference implementation for any future perceptually-uniform visualisation in this repo. New colormap consumers should reuse it rather than re-invent LUTs.
- **Bulk pixel buffers bypass `egui2.Color`.** The per-pixel payload convention established by SD9 applies to any future widget that ships a bulk array of per-element colors. Individual color arguments continue to use `egui2.Color` + `.AsColor()` per ADR-0052.

## Status

Accepted — 2026-04-23, reviewed by `p@stergiotis`. Design frozen; implementation begins with the IDL definition, the hand-written `rust/imzero2/src/imzero2/scrolling_texture.rs` module skeleton, and `./generate.sh` seam verification (milestone 1 of the planning thread).

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## References

- [`doc/adr/0003-imzero2-unified-color-type.md`](0052-imzero2-unified-color-type.md) — `egui2.Color` unified type; SD9 of this ADR documents the narrow exception for bulk pixel buffers.
- [`doc/adr/0005-regex-explorer-offset-authority.md`](0054-regex-explorer-offset-authority.md), [`doc/adr/0006-adopt-boxer-standards.md`](0055-adopt-boxer-standards.md), [`doc/adr/0007-walkers-map-h3-binding.md`](0056-walkers-map-h3-binding.md) — prior ADRs; template shape followed here.
- [`boxer/doc/DOCUMENTATION_STANDARD.md`](../../../boxer/doc/DOCUMENTATION_STANDARD.md) — Diátaxis + ADR conventions followed by this document (canonical copy lives in boxer per `CLAUDE.md`).
- [`doc/skills/fffi2/SKILLS.md`](../skills/fffi2/SKILLS.md) — FFFI2 widget-definition rules: argument naming, allowed types, return-type conventions, generated vs hand-written file split.
- [`doc/skills/imzero2/SKILLS.md`](../skills/imzero2/SKILLS.md) — ImZero2 runtime conventions; §11 covers deferred/culled blocks and register usage.
- [`public/thestack/imzero2/egui2/definition/`](../../public/thestack/imzero2/egui2/definition) — hand-written IDL definition files; new definition lands here.
- [`public/thestack/imzero2/egui2/definition/egui2_definition_d_plot.go`](../../public/thestack/imzero2/egui2/definition/egui2_definition_d_plot.go) — structural precedent (homogeneous-array payload + `.WithApplyCodeClientRust(...)` apply snippets).
- [`rust/imzero2/src/imzero2/interpreter.rs`](../../rust/imzero2/src/imzero2/interpreter.rs) — generated dispatch; `scrollingTexture` apply snippets land here after `./generate.sh`, calling into the new hand-written `scrolling_texture.rs` module.
- [`../../../egui_spectrogramm/`](../../../egui_spectrogramm/) — first-consumer source; port target for the audio-spectrogram Go wrapper that composes `heatmapscroll` in a follow-up PR.
- [egui `TextureHandle`](https://docs.rs/egui/latest/egui/struct.TextureHandle.html), [`TextureHandle::set_partial`](https://docs.rs/egui/latest/egui/struct.TextureHandle.html#method.set_partial) — partial-upload API used for O(heightSlots) per-column writes.
- [egui `TextureOptions`](https://docs.rs/egui/latest/egui/struct.TextureOptions.html) — `NEAREST`/`LINEAR` filter constants mirrored by `FilterNearestE`/`FilterLinearE` (SD3).
- [ImPlot `PlotHeatmap`](https://github.com/epezent/implot/blob/master/implot.h), [Makie `heatmap`](https://docs.makie.org/stable/reference/plots/heatmap), [PyQtGraph `ImageItem`](https://pyqtgraph.readthedocs.io/en/latest/api_reference/graphicsItems/imageitem.html), [Observable `Plot.raster`](https://observablehq.com/plot/marks/raster), [Grafana heatmap panel](https://grafana.com/docs/grafana/latest/panels-visualizations/visualizations/heatmap/), [SDRangel spectrum reference](https://github.com/f4exb/sdrangel/blob/master/sdrgui/gui/spectrum.md) — toolkit survey informing the QOC criteria and the scientific-default choices in SD3–SD6.
