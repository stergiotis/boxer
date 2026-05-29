---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# ecdf — Explanation

`ecdf` is the imzero2 widget that renders an empirical CDF
together with a finite-sample exact simultaneous confidence band
on F. It is a thin composition layer over two pieces that own their
own EXPLANATION docs:

- **`boxer/public/analytics/stats/ecdfbands`** — the math seam:
  band inversion (Berk-Jones default, plus DKW-Massart /
  equal-precision / higher-criticism), crossing-probability
  engines (Moscovich-Nadler Poissonized DP + Steck-Noé
  determinant), and the cache.
- **`egui2_definition_d_plot.go::plotPolygon`** — the FFFI2
  primitive added for this widget. The drain orders polygons
  *before* lines/scatters inside the egui_plot block so the
  confidence band sits visually under the ECDF curve.

## Composition

For each `Render(sorted)` call:

1. **Compute the band.** Calls `ecdfbands.BandsForSample(sorted,
   alpha, method)` once. The library caches by (n, α, method); the
   per-call cost is bounded by a slice copy out of the cache.
2. **Emit n−1 PlotPolygon rectangles.** One per ECDF plateau:
   `[Xs[i], Xs[i+1]] × [LowerCDF[i], UpperCDF[i]]`, four vertices
   each. The whole band is a sequence of convex rectangles rather
   than a single closed staircase polygon — `egui_plot`'s
   tessellator produces visible triangulation artifacts on highly
   non-convex shapes, so per-rectangle emission is correct by
   construction.
3. **Emit the ECDF step polyline.** 2n vertices alternating
   pre-/post-jump at each order statistic, starting at (X₁, 0) and
   ending at (Xₙ, 1). Pushed via `c.PlotLine`.

Both emits go into Rust-side registers; the enclosing `c.Plot`
block drains them — polygons first, then lines (the explicit
ordering inside the plot block's apply code).

## Rendering model

The simultaneous band has the interpretation: at every x where
F_n(x) = i/n, the true CDF F satisfies F(x) ∈ [a_i, b_i] with the
chosen (1-α)·100% simultaneous coverage.

For visualisation, the band is a piecewise-constant step function
on the F-axis: between adjacent order statistics X_(i) and X_(i+1),
F is bounded by [LowerCDF[i], UpperCDF[i]]. Each plateau becomes
one filled rectangle on the plot.

The ECDF itself is the canonical 2n-vertex polyline: vertical jump
of 1/n at each X_(i), horizontal extension between consecutive X's.

## Invariants

- **Renderer is value-typed and stateless.** Every fluent setter
  returns a modified copy; concurrent goroutines can call Render
  on independent Renderers without coordination.
- **Caller owns the Plot block.** Render must be invoked while a
  `c.Plot(id)` call is queued for the same frame; the polygon and
  line primitives accumulate in registers that the Plot drain
  consumes.
- **Render emits exactly n FFFI2 primitives** (n−1 PlotPolygon
  rectangles + 1 PlotLine). No id-consuming fetchers are invoked —
  the widget is safe to compose inside deferred-block surfaces
  (dock tabs, table rows, hover tooltips).
- **n < 2 short-circuits** with no emit and no error. A
  single-sample band is geometrically degenerate; the underlying
  library would still compute, but the visualisation would be one
  vertical jump with a single band rectangle — not useful.

## Cursor crosshair

A second API surface — `Crosshair`, `Renderer.At` / `Renderer.AtGrid`,
`Renderer.PaintCrosshair`, and the package-free `WriteStatusLine` —
reads the egui_plot hover register every frame and surfaces the
derived ECDF statistics at the cursor:

- `F_n(cursor.x)` via `fnAtXSorted` (binary search) or `fnAtXGrid`
  (piecewise-linear, matching what egui_plot's `PlotLine` draws).
- The simultaneous band `[lower, upper]` at `cursor.x` via `bandAtX`
  (binary search across the n−1 plateau staircase emitted by
  `emitBandRectangles`).
- The nearest order statistic `X_(NearestIdx+1)` (right-biased
  tie-break on equidistant ordering).

The Renderer is value-typed and stateless — every call re-reads
`c.CurrentApplicationState.StateManager.GetPlotPointer()`. The cache
is populated by `StateManager.Sync` after the previous frame's drain,
so the crosshair carries the canonical r15 one-frame lag (same lag
as click-readout via `PlotFluid.SendResp`). `BandsForSample` is
cached by `(n, α, method)` upstream, so calling `At` and `Render` in
the same frame computes the band once.

### Plot-id filter

`Crosshair.Valid` is false unless `hover.HoverPlotId ==
plotID.Derive()`. Callers must pass a `c.AbsoluteWidgetId`
(constructed via `c.MakeAbsoluteIdStr` or sibling) and reuse the
same id in their `c.Plot(plotID).Send()` call. `ids.PrepareStr` is
not interchangeable: `PrepareStr → Derive` XORs the surrounding
`WidgetIdStack` top into the hash, so `hashLabelToId("ecdf-plot")`
(what the caller would otherwise compute) and the Rust-side stored
`{{Id}}.value()` (what the fetcher returns) are different
bit-patterns. Multiple ECDF plots in one frame need distinct
absolute ids — each filters on its own.

### Drain order

`PaintCrosshair` emits a single `c.PlotVLine`. The Rust-side drain
inside `c.Plot(...).Send()` renders elements in fixed order:
polygons → lines → scatters → bars → hlines → vlines → texts →
boxes. The crosshair therefore sits visually above the band
(polygon) and ECDF curve (line) — the right z-order without
explicit layering work in the Go-side emit code.

### Multi-plot hover register

The r15 hover register is single-slot. When several plot blocks
render in one frame, only the plot the cursor is *over* writes its
`(id, x, y)`. A plot that previously held the hover blanks *its
own* `x/y` to NaN when its `response.hover_pos()` returns None
(`else if self.r15_plot_hover_id == {{Id}}.value()`) — preventing a
later plot's render from clobbering plot-A's hover when the cursor
hasn't moved. This is the cleaner counterpart to the canvas-pointer
multi-canvas semantics noted in CLAUDE.md.

## Trade-offs

- **Per-rectangle vs single staircase polygon.** A single closed
  staircase polygon (4n−2 vertices) would be one FFFI2 emit but
  triangulates badly in egui_plot's tessellator — visible diagonal
  artifacts across the band interior. Per-rectangle emission costs
  n−1 emits and ~4(n−1) vertices total (same order) but every
  rectangle is convex, so the fill is correct by construction.
- **The widget owns no axis configuration.** Caller wraps the
  Render call inside `c.Plot` and supplies axis labels, ranges,
  legend toggles, zoom/drag policies. The widget contributes one
  band polygon + one ECDF polyline per Render and otherwise stays
  out of the Plot's way.
- **No streaming variant in the widget.** `BandsForGrid` is
  available in the underlying library for callers holding a
  t-digest sketch; the widget here always consumes a sorted
  sample. A `RenderStreaming(digest, gridN)` arm is straightforward
  to add if a use case appears.

## Further reading

- Library: [boxer ecdfbands EXPLANATION](https://pkg.go.dev/github.com/stergiotis/boxer/public/analytics/stats/ecdfbands).
- Primitive: `src/go/public/thestack/imzero2/egui2/definition/egui2_definition_d_plot.go::plotPolygon`.
- Demo: `apps/widgets/egui2_hl_ecdf_demo.go` (carousel entry `ecdf`).
