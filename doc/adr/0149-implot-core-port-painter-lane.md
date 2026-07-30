---
type: adr
status: accepted
date: 2026-07-29
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-29
---

# ADR-0149: porting the ImPlot core to Go on the painter lane

## Context

Charting in imzero2 lives on two substrates today, and both are showing their
limits from opposite directions.

**The egui_plot bridge is narrow and grows by seam work.**
`egui2_definition_d_plot.go` bridges eight elements (line, scatter, bars,
h/v-line, boxes, polygon, text) into a Rust-side `egui_plot` drain. Roughly
eight call sites use it (imztop's cpu/gpu/mem/rate panels, imzrt's sched/heap
panels, play's projection pane, fibscope, terrainscope, the ecdf widget). Every
missing feature — heatmaps, error bars, annotations, drag tools, subplots,
linked axes, legend interaction readback — costs a bespoke IDL element plus a
bespoke readback channel, and the plotting logic accumulates in the Rust layer
that the architecture otherwise keeps thin. `egui_plot` itself is a
community-maintained crate since its split from egui, on a slower cadence than
egui proper.

**The canvas widget zoo re-rolls plot machinery.** The widget layer holds ~48
Go widgets on the painter lane; among them `axisruler`, `ecdf`, `boxenplot`,
`distsummary`, `spectrumdisplay` and `timeline` each hand-roll some subset of
axes, tick placement, zooming and legends. The duplication is visible and the
interaction idioms have started to diverge.

What is missing is a shared plotting *core* — transforms, axis/tick machinery,
an interaction state machine, legends, and a breadth of item renderers — in Go,
where every other widget already lives. Rather than design one from scratch,
this ADR proposes porting the core of [ImPlot](https://github.com/epezent/implot)
(MIT, Evan Pezent), chosen because it is single-author consistent, proven, and
immediate-mode first — its frame protocol maps onto imzero2's per-frame
declaration model as a transliteration, not a paradigm inversion.

Load-bearing findings, measured against upstream ImPlot v1.1-WIP (commit
`d65a2be`) and the current tree:

- ImPlot is ~12.6k LOC of C++ excluding its demo (`implot.h` 1.4k, internal
  header 1.7k, core 5.9k, items 3.5k) with 159 public entry points, most
  instantiated over ten scalar types by macro — a surface Go generics collapse
  to one implementation each.
- Its Dear-ImGui contract is four seams: raw triangle emission (the item
  renderers write vertices directly; 111 `_VtxWritePtr` / 72 `_IdxWritePtr`
  sites), text draw + measure (23 `CalcTextSize` sites), mouse input with
  anchored wheel and cursor shape (15 `SetMouseCursor` sites), and ID-keyed
  per-plot state (`ImPool`/`ImGuiStorage` — plain maps in Go). There is no
  draw-list splitter; series drag-drop is an optional feature (18 sites).
- A large share of `implot.cpp` (estimated ~40%) is chrome, not core: the
  style editor, metrics window, user guide, and ImGui-widget context menus.
- The painter lane (`egui2_definition_d_painter.go`) already carries
  implot-shaped primitives: polyline and filled polygon **with homogeneous
  array arguments**, line/dashed line, rect, circle, ellipse, bezier, anchored
  text, sense regions — accumulated Rust-side and drained by `paintCanvas`
  into an `allocate_painter` canvas in relative coordinates.
- Input readback is essentially complete for a plot: per-canvas hover (r14),
  response flags (r7), and hover-scoped wheel scroll/zoom **with the hover
  anchor** (r23, [ADR-0140](./0140-imzero2-hover-scoped-wheel-capture.md)) —
  the anchored-zoom primitive ImPlot's pan/zoom needs. Modifier and pointer
  fetchers exist; cursor-shape control shipped separately.
- Two gaps are real: `drain_paint_cmds_to_painter` applies no per-command clip
  (ImPlot needs an inner plot-area clip with tick labels outside it), and no
  text-measurement channel exists — widgets estimate character widths
  (`timeline.go`'s ASCII-only estimate, documented as underestimating CJK
  2–4×). For numeric tick labels the estimation idiom is nearly exact.
- In-plot raster composition has one in-tree precedent, and it is not
  general: the play map's `MapRaster` overlay (bounds-pinned texture,
  version bump + starved-texture re-ship) drains inside the walkers widget
  only; the painter lane has no textured-image command. Complex (concave,
  holed) polygon fill is likewise served today by CPU rasterization in the
  `worldmap` widget — supersampled scanline even-odd fill plus a per-pixel
  index buffer for O(1) hover picking — precisely because egui's polygon
  fill is convex-only.
- The mesh draw-stream lane
  ([ADR-0128](./0128-imzero2-mesh-draw-stream-codec-lane.md)) is the
  remote-access *output* codec (egui → viewer). It is not a Go-side triangle
  API and does not serve as the ImDrawList replacement.
- An ImGui-based predecessor of imzero2 bound ImPlot's C++ API to Go over
  generated FFI wrappers, so the API surface is familiar territory; that
  approach's premise (a live Dear ImGui context) vanished with the move to
  egui.

## Design space (QOC)

**Question.** Where does the plotting engine live, and where does its design
come from?

**Options.**

- **O1** — **Deep-bridge `egui_plot`.** Keep the engine Rust-side; extend the
  IDL element-by-element toward ImPlot's feature set.
- **O2** — **Port the ImPlot core to Go** on the painter lane; re-idiomize the
  API, preserve the frame protocol and interaction model.
- **O3** — **Continue the organic widget zoo**, extracting shared axis/scale
  packages opportunistically (`axisruler` as the seed).
- **O4** — **Adopt a retained-mode Go library** (gonum/plot or similar).
- **O5** — **Render-to-texture** via a Rust plotting backend, shown as an
  image.
- **O6** — **FFI-bind ImPlot's C++** as the predecessor did.

**Criteria.** Seam tax per new feature; where logic accumulates (Go-first
doctrine); interaction fidelity (hover, anchored zoom, box-select, legend
interaction); immediate-mode fit; performance ceiling; dependency sovereignty;
effort.

## Decision

Adopt **O2**: port the ImPlot core to a new Go package on the painter lane.

### SD1 — Port, don't bind

ImPlot cannot run without a live Dear ImGui context (`GetCurrentWindow`,
`ButtonBehavior`, IO, font atlas). Binding it beside egui means embedding a
second UI runtime — second input pipeline, second font atlas, second draw
list — inside one surface. The predecessor's binding was sound *under ImGui*;
under egui the approach is structurally dead. The port carries the design, not
the object code.

### SD2 — Port contract: core, not chrome; protocol, not API

Ported: the plot frame (`BeginPlot`/`Setup*`/`Plot*`/`EndPlot` ordering
semantics), f64 transforms, axis/tick locators and formatters, the interaction
state machine (pan, anchored zoom, box-select, double-click fit,
axis-constrained variants), legends, and the item renderers.

Not ported: the style editor, metrics window, user guide, demo shell, and the
ImGui-widget context menus — menus are re-expressed with native egui2 widgets
in a later milestone; series drag-drop is deferred (SD6).

The public API is re-idiomized Go, not a mirror: generics replace the
ten-type macro instantiation, option builders replace flag soup, `c.IdScope`
carries the id discipline. What is preserved verbatim is the *frame protocol*
and interaction semantics — that is the proven part.

### SD3 — Substrate prerequisites (M0)

Two small painter-lane additions, both generally useful beyond plotting:

- **Clip push/pop opcode** (`PaintCmd::PushClip`/`PopClip` over
  `Painter::with_clip_rect`) — the inner plot-area clip. Also retires the
  manual overflow workarounds in existing canvas widgets.
- **Batched markers and rects** (`paintMarkers(xs, ys, shape, size, col)`,
  `paintRectsFilled(...)` with homogeneous arrays) — scatter and small
  heatmaps at one opcode per series instead of one per point.

### SD4 — Precision split: f64 plot space, f32 emission

Plot-space math stays `float64` Go-side; projection to `f32` happens only at
paint-command emission. This is ImPlot's own double/float split and is what
keeps deep zoom correct; the painter lane's f32 relative coordinates are
post-projection, so nothing is lost.

### SD5 — Dense-raster routing and in-plot image composition

Small heatmaps draw as batched rects; above a cell-count threshold they route
to a texture (the spectrogram and play-map rasters are the data-path
precedents). The threshold is measured, not guessed, during M4.

The texture route needs raster, vector and axes composited inside one plot,
and no general primitive exists for that today (see Context: `MapRaster` is
walkers-only). M4 therefore adds a **`paintImage` opcode** — a textured rect
by texture id, clipped like any other paint command — generalizing the
`MapRaster` protocol (version bump + starved-texture re-ship) rather than
inventing a new one. The same opcode serves `PlotImage` and future raster
underlays (maps, geo panels). It is deliberately not in M0: M0 carries only
what M1–M2 consume, and `paintImage`'s first consumer is M4.

### SD6 — Deferrals, recorded

- **Text-measurement fetcher.** Numeric ticks are served by the estimation
  idiom; a frame-lagged `fetchTextSize` would polish legend sizing. Deferred.
- **Per-vertex-color mesh opcode** (`paintMesh` over `epaint::Mesh`) — needed
  only for colormap-gradient fills (two call sites upstream), concave fills,
  and a future ImPlot3D port. Deferred, with a proven fallback: the
  `worldmap` widget already CPU-rasterizes concave, holed polygons
  (supersampled scanline fill plus a per-pixel index buffer for picking)
  because egui's fill is convex-only. Filled contours and similar geoms take
  that path — rendered through SD5's `paintImage` — until a tessellating
  opcode exists.
- **Series drag-drop** and the re-expressed context menus follow the core.

### SD7 — Coexistence and migration

The egui_plot bridge stays untouched while the port lands. New chart work
targets the port once M1 ships; the existing bridge users migrate
opportunistically; bespoke widgets (`axisruler` first) adopt the shared core
where it pays. Deprecating the bridge is a separate later decision, taken only
when the port covers the subset actually in use.

### SD8 — Home, license, provenance

Package `implot` under `public/thestack/imzero2/egui2/widgets/implot` — the
name states the provenance honestly. The port is a derivative work: it carries
upstream's MIT license text and attribution in-package. Authorship provenance
follows the git-trailer discipline
([ADR-0083](./0083-retire-llm-generated-build-tags.md)); no in-file markers.

### Milestones

- **M0** — SD3 primitives in the painter IDL + regen.
- **M1** — plot frame core: transforms, linear axes, tick locator/formatter,
  grid, line series, pan / anchored zoom / box-select / double-click fit,
  hover readout.
- **M2** — legend (toggle, hover-highlight) + item breadth: scatter/markers,
  bars, shaded, stairs, stems, infinite lines.
- **M3** — scales and time: log/symlog, time-axis locators and formatting
  (Go `time` replaces the C localtime machinery).
- **M4** — heatmap + histograms (1D/2D) with SD5 routing; the `paintImage`
  opcode lands here with its first consumer; colormap integration with the
  existing `colormap`/`colorscale` widgets.
- **M5** — tools: drag lines/points/rects, annotations, tags; native context
  menus.
- **M6** — subplots and linked axes.
- **M7** — remainder: error bars, pie, digital, images; bridge-user migration
  begins.

Each milestone ports its sections of `implot_demo.cpp` (3k lines — the de
facto spec) into the demo registry
([ADR-0057](./0057-demo-registry-and-drivers.md)) as the acceptance corpus,
with TestDriver screenshot goldens.

## Alternatives

- **O1 — deep-bridge `egui_plot`.** Cheapest per *element*, but every
  interactive feature (legend clicks, drag tools, linked axes, subplots)
  needs its own IDL seam and readback channel; the seam tax compounds, and
  plotting logic accumulates Rust-side against the Go-first doctrine, in a
  community crate on a slower cadence. Killed as the growth path; the
  existing bridge is retained as-is during migration (SD7).
- **O6 — FFI-bind ImPlot C++.** Requires a Dear ImGui runtime beside egui:
  two input pipelines, two font atlases, two draw lists. Structurally dead
  since the ImGui→egui move. Killed.
- **O4 — retained-mode Go libraries (gonum/plot et al.).** Figure-oriented,
  allocation-per-frame, no interaction model; inverting them to immediate
  mode discards the thing adopted. Killed.
- **O5 — render-to-texture backends.** Forfeits per-element hover and
  interaction granularity, is resolution-coupled, and reintroduces a
  Rust-side plotting dependency. Killed as a general path; the texture lane
  remains exactly for dense rasters where interaction is per-region anyway
  (SD5).
- **O3 — organic widget zoo only.** Bespoke widgets stay right for bespoke
  domains, but as the *only* path it keeps re-rolling axes/zoom/legend with
  diverging idioms. Not killed — demoted from only path to sibling path; the
  ported core becomes its shared substrate.
- **Other port sources.** ImPlot3D: same author family, plausible follow-on,
  blocked on the deferred mesh opcode (SD6) — deferred, not killed. uPlot /
  ECharts and similar: wrong substrate (DOM/JS) and wrong paradigm. Killed.

## Consequences

### Positive

- One coherent, Go-native plotting layer with a proven interaction model;
  the hard-to-design part (zoom/fit/select/legend semantics) arrives
  pre-designed and battle-tested.
- Consolidation target for the axis/tick/zoom machinery currently re-rolled
  across the chart-like widgets.
- Sovereignty: the plot engine moves out of a community Rust crate into code
  governed by the repo's own tooling; upstream ImPlot is stable and
  slow-moving, so the port does not chase a target.
- The M0 primitives (clip, batched markers/rects) benefit every canvas
  widget, not just plots.
- Go generics land the core meaningfully smaller than the C++ it ports.
- The demo corpus doubles as an acceptance suite that slots directly into
  the demo-registry screenshot pipeline.

### Negative

- A substantial port: estimated 8–12k LOC of Go across the milestones
  (roughly five timeline-widgets); each milestone ships usable value, but
  the total is real.
- Performance ceiling: the port inherits epaint tessellation rather than
  ImPlot's raw pre-tessellated vertex path. This matches the status quo
  (the egui_plot bridge pays the same cost) but not native C++ ImPlot;
  very large series need Go-side decimation (min-max/LTTB), which is
  desirable anyway. Estimated wire cost is not the bottleneck (a 100k-point
  polyline is ~800 KB/frame of bulk f32 copy during interaction).
- Two plotting systems coexist until migration completes (SD7).
- Chrome re-expression (context menus, legend popups) is a second pass;
  part of ImPlot's feel lives there and M1–M4 will feel spartan.
- MIT attribution and derivative-work bookkeeping must be carried
  in-package (SD8).

### Neutral

- The egui_plot bridge and its call sites are untouched short-term.
- ADR-0128's mesh lane is unaffected; it remains the remote-access codec.
- The `axisruler`/`colormap`/`colorscale` widgets continue to work; they
  become adoption candidates, not casualties.

## Status

Accepted (2026-07-29). The upstream dependency profile and the
painter-lane/readback inventory above are measured against the tree; the
port-size, chrome-share, and wire-cost figures are estimates. Next concrete
step: M0 (SD3) — the clip and batch opcodes — which is small, independently
useful, and de-risks the item-renderer wire costs before M1 commits to them.

## Update 2026-07-30 — M0 and M1 landed; the substrate grew a per-canvas pointer register

M0 shipped as specified (commit fd225712): the clip stack, marker batch and
rect batch opcodes, with a `painter_m0` gallery demo as the acceptance
capture. M1 shipped as `widgets/implot`: the Begin/Setup*/items/End protocol
with setup locking, the ported nice-number locator and step-precision
formatter, the f64→f32 transform split, layout/grid/ticks/border, NaN-split
line series clipped by the M0 stack, and the four gestures — drag pan,
pointer-anchored wheel zoom, double-click fit, Shift+drag box-zoom — all
verified interactively (egui-mcp driving; the box transform additionally
verified numerically against a logged probe).

Implementing the gestures surfaced substrate needs beyond SD3, now part of
the painter lane:

- **R24, a per-canvas pointer register.** The existing R14 canvas pointer is
  a single-slot, last-canvas-wins register — workable only while one canvas
  renders per interpreted frame. R24 mirrors ADR-0140's R23 shape: every
  `paintCanvas` and every `paintSenseRegion` stamps a row keyed by its
  widget id — screen origin, canvas-relative pointer, and a modifier
  bitmask — drained by `fetchR24CanvasPointers` into
  `StateManager.GetCanvasCursor`.
- **Event-exact anchoring.** Machine-speed input batches press, moves and
  release into one or two frames, which breaks frame-end sampling twice
  over: a gesture position read at frame end starts mid-drag, and a
  modifier pressed and released within a batch is invisible to the
  frame-end modifier state. The sense-region row therefore reports the
  press origin (`pointer.press_origin`) and the press event's own
  modifiers on the drag-started frame. Human-speed input never notices;
  driven input requires it.
- **u8h wire write.** The modifier column is the first u8 array a fetcher
  returns; the FFFI io layer gained `write_plain_u8h` and the Go runtime
  the matching slice iterator.

Deviations recorded in the package doc: box-zoom is Shift+drag rather than
upstream's right-drag (the R7 response flags do not say which button
dragged; a `DraggedBy` flag is a candidate substrate addition), and the
y-axis label renders horizontally (no rotated-text paint command). One
verification caveat: the egui-mcp driver does not stamp modifiers onto
synthesized pointer events, so the Shift-gated path itself was verified by
temporarily inverting the gesture mapping; the modifier routing is
code-reviewed for held-shift input but has not been exercised by a real
hand.

## Update 2026-07-30 (2) — M1 through M6 landed; the estimate was high

The remaining structural milestones shipped the same day, one commit each:
M2 items + interactive legend (03cae65e), M3 time/log/symlog scales
(c5a43940), M4 heatmaps/histograms + the `paintImage` opcode (afa77f85),
M5 drag tools, annotations, tags and the native context menu (fd03b35c),
M6 subplots + linked axes via the upstream `SetupAxisLinks` pointer
contract (06d5020f). Each milestone landed with unit tests, a gallery
demo captured by the tour, and — for every interactive feature — an
egui-mcp driving session against the live app; the driving sessions
caught three real gesture bugs the tour structurally cannot see.

Calibration against the proposal: the estimate of 8–12k lines of Go was
high by roughly 4×. The package stands at ~2.3k lines including tests
for the M0–M6 feature set, because the painter lane and egui absorb what
`implot.cpp` spends most of its bulk on (draw-list management, text
shaping, input plumbing) and Go generics collapse the ten-type getter
expansion to nothing. One design change earned its keep beyond the
port: gestures compose in a pixel-space window inverted through the
transform once, which made pan/zoom/box-zoom scale-agnostic — log and
symlog axes got correct interaction with zero scale-specific gesture
code.

Two repo-level cautions surfaced en route, neither implot-specific:
widgets addressed by absolute ids render and take clicks but do not
read back through `SendResp` (the context menu uses relative ids; the
bindings quirk deserves a root-cause pass), and a committed hand-patch
was found living inside interpreter.rs's generated region
(`WINDOW_TOPMOST`), which every regen silently deletes — re-applied and
flagged in afa77f85; it needs a home in the IDL or the hand-maintained
section.

Remaining: M7 (error bars, pie, digital, image items; the egui_plot
bridge call-site migration begins) and the deferred set of SD6 — all
still descoped, none blocking. Accrued contract deviations live in the
package doc.

## Update 2026-07-30 (3) — M7 landed; bridge migration opened; a legend hit-test bug found and fixed

M7 shipped the remaining items: vertical and horizontal error bars
(asymmetric; symmetric by passing the same slice twice), pie charts
(plot-space geometry like upstream, so anisotropic axes render an
ellipse; slices wider than a half circle are split so the painter's
convex-only fill renders them correctly, where upstream accepts the
artifact), digital channels (bottom-pinned in pixel space, x-fit only,
visible channels stack in declaration order), and the image item on the
M4 `paintImage` opcode (caller-owned RGBA pixels plus a content version
driving the ship-once protocol — the substrate has no GPU texture
handles to pass, so upstream's texture-id parameter re-expresses as
pixels + version). The `implot_m7` gallery demo is the acceptance
capture; unit tests cover the new pure logic (pie spans, arc chunking,
digital run merging, error-bar fit).

Three API additions came from porting fidelity rather than new scope:
same-label items now share one palette slot and one legend entry (the
upstream label→item registry semantics — error bars merge into the
series they decorate), `SetNextColor`/`SetNextWeight` port the two
halves of upstream's `SetNextLineStyle`, and `Begin` honors the
ImGui-family `"##id"` hidden-title convention.

The egui-mcp driving pass for M7 caught a bug present since M2: the
legend's sense regions were emitted before the plot-area region, and
sense-region emission order is hit-test priority (later wins), so the
area region swallowed every legend click — the legend rendered but
never toggled. Legend regions now emit last, after the area and the
drag tools, matching upstream's z-order where the legend outranks plot
interaction. A second latent fit bug fell out of the new tests: the
shared fit loop keyed its bound to `len(xs)`, so `InfLinesH` (nil xs)
never contributed its y extent to auto-fit.

SD7 migration opened with fibscope: its egui_plot-bridge trade-off plot
now renders through the port (the advisor's pick line became
`InfLinesV`; the pinned series colors ride `SetNextColor`). Seven-ish
bridge call sites remain (imztop cpu/gpu/mem/rate, imzrt sched/heap/gc,
play's projection pane, terrainscope, the ecdf widget); bridge
deprecation remains a separate later decision per SD7.

## References

- Upstream: [ImPlot](https://github.com/epezent/implot) v1.1-WIP, commit
  `d65a2be`, MIT — Evan Pezent.
- Painter lane: `public/thestack/imzero2/egui2/definition/egui2_definition_d_painter.go`;
  drain: `rust/imzero2/src/imzero2/interpreter.rs`
  (`drain_paint_cmds_to_painter`).
- egui_plot bridge: `public/thestack/imzero2/egui2/definition/egui2_definition_d_plot.go`.
- In-plot raster precedent: `apps/play/play_map.go` (`MapRaster` overlay,
  viewport raster node); concave-fill precedent:
  `public/thestack/imzero2/egui2/widgets/worldmap/raster.go`.

### Related ADRs

- [ADR-0140](./0140-imzero2-hover-scoped-wheel-capture.md) — hover-scoped
  wheel capture; supplies the anchored-zoom input this port consumes.
- [ADR-0128](./0128-imzero2-mesh-draw-stream-codec-lane.md) — mesh
  draw-stream codec lane; disambiguated: outward codec, not a drawing API.
- [ADR-0057](./0057-demo-registry-and-drivers.md) — demo registry; the
  acceptance-corpus vehicle.
- [ADR-0083](./0083-retire-llm-generated-build-tags.md) — provenance via git
  trailers.
