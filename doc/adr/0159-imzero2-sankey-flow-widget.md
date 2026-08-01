---
type: adr
status: proposed
date: 2026-08-01
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0159: a Sankey / alluvial flow widget on the implot custom-item lane

## Context

The widget zoo has forms for structure (`layeredgraph`, `pipelineview`,
`scctree`), for hierarchy-with-magnitude (`treemap`), and for distributions
(`ecdf`, `boxenplot`, `distsummary`). It has no form for **quantity that
moves** — a weighted flow where the width of a band *is* the value and the
value is conserved from stage to stage.

Two pressures make this the moment to add one rather than later.

**A deferral was left waiting for exactly this.**
[ADR-0119](./0119-imzero2-pipelineview-widget.md) §SD5 deferred the volume
overlay — "edge thickness and ribbons ∝ `Edge.Volume`, links at a node sorted
by far-end y" — "until a live consumer exists", and `Volume` already flows
through `EdgeLayout` untouched. That ADR also recorded d3-sankey's mechanics as
the intended source and Sankey-as-primary-idiom as a rejected alternative *for
a pipeline schematic* — leaving the flow form itself unbuilt.

**The substrate gaps that would have blocked it closed in the last three
days.** Two landings matter:

- The **custom-item lane** ([ADR-0149](./0149-implot-core-port-painter-lane.md),
  Update 2026-07-31): `Custom` / `CustomUnclipped` invoke a caller closure
  during emission with `DrawCtx{T Transform, Area*, W, H, Color, Weight,
  Highlighted}`. A closure gets the plot↔pixel transform, a palette slot, a
  legend row with a visibility toggle, legend-hover emphasis, and
  declaration-order z — so an adopting widget inherits pan, pointer-anchored
  wheel zoom, box-zoom, double-click fit, and plot-space readback
  (`HoverPlotPos`, `Clicked`) without re-rolling any of it.
- **Concave polygon fill** (`04999929`, 2026-08-01):
  `PaintPolygonFilled(xs, ys, col).Concave().Stroke(sc, sw)` ear-clips through
  `earcutr` into an `egui::Shape::Mesh`. Before it, the painter's only filled
  polygon was convex — and a flow ribbon bounded by two curves is not convex,
  so ribbons would have had to be decomposed into quads. A ribbon is now
  **one paint opcode**.

Load-bearing findings from the method survey (sources in
[§References](#references)):

- The layout is Sugiyama with a value-weighted twist, in four stages: layer
  assignment (x); ordering within a layer; y-positioning; ribbon geometry.
  Crossing minimization is NP-hard; the practical recipe is barycentre
  relaxation. Exact ILP formulations exist and stay viable past ~50 nodes /
  100 edges, at the cost of a solver.
- One rule does most of the visual work: **links at a node face are ordered by
  the y of their far end**. It removes the majority of local crossings for
  free, independent of the node ordering.
- The y scale is global, not per-column: `ky = min over stages of
  (H − (n−1)·pad) / Σvalue`. A per-column scale would make ribbons change
  width mid-flight and destroy the conservation reading.
- Two ribbon conventions exist. **Vertical extrusion** — the fill between two
  y-offset copies of the same curve — keeps a node bar an exact subdivision of
  its value. **Perpendicular stroke** (d3's `sankeyLinkHorizontal` with
  `stroke-width`) looks smoother but overstates vertical extent on steep
  segments, so the band meeting a node face is wider than the value it
  carries.
- Where a linear stage ordering exists, the Sankey outperforms the chord
  diagram: the CHI 2023 study (51 participants, five task types, four
  datasets) found chord produced more errors, ~3.7 s longer per question
  (>9 s worse on first exposure to each task type), higher rated effort, and
  a strong preference for Sankey.
- Known failure modes that need explicit answers, not silence: cycles
  (a plain Sankey cannot show them), sub-pixel flows (they vanish and
  conservation visibly breaks), mixed units, non-conserving nodes, and label
  crowding — the last aggravated here by the deferred text-measurement channel
  (ADR-0149 §SD6), which leaves widgets estimating glyph widths.

## Design space (QOC)

**Question.** What substrate and what layout depth should a flow-quantity
widget have, given that implot now has a custom-item lane and the painter has
concave fills?

**Options.**

- **O1** — **implot custom item**: a UI-free layout package plus a renderer
  that declares `Custom` closures into a caller-owned `*implot.Plot`.
- **O2** — **Standalone painter widget**: own canvas and fit-to-pane, no axes,
  in the `pipelineview` / `layeredgraph` mould.
- **O3** — **Layout package with both renderers**: one geometry core, an
  implot renderer and a bare-painter renderer.
- **O4** — **Extend `pipelineview`** with SD5's volume overlay and stop there.
- **O5** — **Extend `layeredgraph`** with value-weighted edges.

**Criteria.**

- **C1** — interaction inherited vs re-rolled (pan, zoom, legend, readback).
- **C2** — implot reuse, the stated goal of the second adoption wave.
- **C3** — reach: how many shapes of flow data the result can serve.
- **C4** — implementation weight and number of things kept in sync.
- **C5** — honesty of the encoding: does the form make width mean value.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | ++ | −− | ++ | −  | −  |
| C2 | ++ | −− | +  | −− | −− |
| C3 | ++ | ++ | ++ | −− | −  |
| C4 | +  | −  | −  | ++ | +  |
| C5 | ++ | ++ | ++ | +  | −  |

## Decision

We will add `public/thestack/imzero2/egui2/widgets/sankey` — a UI-free model
and layout core — plus `widgets/sankey/view`, a renderer that draws the
geometry as implot `Custom` items (**O1**). Layout is barycentre relaxation.
The widget covers two modes, Sankey and alluvial; the form family beyond that
is out of scope.

### SD1 — Layout emits a unit box; implot owns the viewport

Layout returns geometry in a **unit box** — x and y both in `[0, 1]`, y in
plot convention (larger y is higher on screen) — and the renderer pins both
axes to `[0, 1]` with `CondOnce`, hides them, and projects through
`dc.T.PxX` / `dc.T.PxY`.

The alternative, recomputing pixel geometry each frame from the area rect,
would make pan and zoom our problem instead of implot's. Under the unit box,
zoom is exact and free: ribbons, bars and node padding all scale together,
which is also the behaviour that helps a crowded diagram. Only text stays
pixel-sized, which is correct — labels should not grow with zoom.

The consequence to accept: node padding and bar width are expressed as
fractions of the box, not pixels, so their apparent size depends on the plot's
aspect. `RenderOpts` carries pixel-space overrides for bar width and label
gap, applied at draw time from `DrawCtx`.

### SD2 — Two ribbon fill routes, sharing one sampler

Both routes sample the same vertical-extrusion geometry — a cubic Bézier with
both control points at the x-midpoint, evaluated at `Samples` points, offset in
y by the link's thickness — and differ only in how they hand it to the painter:

- **`FillPolygon` (default)** — one
  `PaintPolygonFilled(...).Concave().Stroke(fill, 1)` per link. The outline is
  the top curve left-to-right followed by the bottom curve right-to-left: a
  single simple ring, which is exactly what the ear-clipper supports. It
  cannot self-intersect, because the bottom curve is the top curve translated
  by a strictly positive constant. The hairline stroke is not decoration — a
  mesh fill bypasses epaint's feathering, so without it the ribbon edge is
  unantialiased.
- **`FillColumns`** — every ribbon in the diagram rasterized as ~2 px vertical
  strips in a **single** batched `PaintRectsFilled`, with a per-column colour.
  This is the route that supports a source→target gradient at no extra opcode
  cost, and the route that renders in every export lane (see §SD5). Its cost is
  stair-stepping where a ribbon edge is steep, mitigated by stroking the
  boundary curves as polylines.

  Column boundaries are **snapped to whole canvas pixels**. Found by looking at
  the first capture: abutting rects at fractional x each get a feathered edge,
  and at ribbon alpha the half-covered seams read as vertical banding across
  the whole diagram. Rounding is safe for tiling because neighbouring columns
  share a boundary value and round identically, and a column that collapses to
  zero width drops out without opening a gap. Confirmed by re-capture — the
  banding is gone, and the dropped zero-width columns show up as ~13% fewer
  rects.

Vertical extrusion, not perpendicular stroke, so that a node bar is an exact
subdivision of its value at the face.

### SD3 — Hit testing is plot-space, so it is zoom-independent

Because all geometry lives in the unit box, hit tests read `HoverPlotPos` /
`Clicked` — already in plot space — and test against the layout directly. No
pixel geometry is cached and no `PlotAreaPrev` arithmetic is needed; the
one-frame register lag is the only staleness, imperceptible at interactive
rates. A ribbon test binary-searches the shared sampler's x array and lerps the
top and bottom edge, so the hit region is precisely the drawn region.

### SD4 — The two modes are two switches, not two engines

Calling the second mode "alluvial" risks implying more machinery than exists.
The honest difference is two switches on one pipeline:

| | Sankey | Alluvial |
| --- | --- | --- |
| Stage index | derived (longest path from sources) | given by the caller (`Node.Stage`) |
| Order within a stage | barycentre relaxation | caller key, or value-descending, stable across stages |

Everything else — the global `ky`, far-end-y link ordering, collision
resolution, ribbon geometry — is shared. Alluvial additionally requires links
to span adjacent stages, which `Validate` enforces in that mode.

### SD5 — Mesh fills reach the tour's SVG, but not the headless scene lane

A concave fill becomes an `egui::Shape::Mesh`, and the two SVG writers in the
tree treat that shape differently:

- `svgexport.rs` — the screenshot tour's exporter — **has** a `Mesh` arm and
  emits one `<polygon>` per triangle. Verified on the captured demo: the
  ribbons are present as 432 flat-shaded triangles.
- `headless_svg.rs` — the headless scene lane from
  [ADR-0154](./0154-headless-carrier-tree-and-driver.md) — has no `Mesh`
  handling at all, so a `FillPolygon` ribbon leaves no geometry there.

So the tour verifies this widget in both PNG and SVG, and only a *headless
scene* assertion would see an empty diagram. `FillColumns` emits ordinary
rects and polylines, so it renders in every lane; it is the escape hatch if a
headless scene check is ever wanted, and it is what a caller should pick when
the output is destined for that lane.

An earlier draft of this ADR asserted the tour's SVG dropped the ribbons. That
was wrong — it conflated the two writers — and the claim is corrected here
rather than left standing, because it would have sent the next reader looking
for a bug that does not exist.

### SD6 — Deferred, deliberately

- **Cycles.** `Validate` rejects them with the offending edge named. The
  feedback-arc-set plus wrap-around routing that a circular-flow diagram needs
  is a design of its own; a plain Sankey cannot show a cycle, and pretending
  otherwise by silently dropping edges would be worse than the error.
- **Exact ordering for small graphs.** The ILP route stays recorded here as
  the upgrade path if relaxation proves visibly poor at our sizes; it wants a
  solver dependency that nothing else in the tree needs.
- **Minimum ribbon thickness.** Opt-in through `RenderOpts`, defaulting off,
  documented as breaking proportionality when enabled. Layout reports the count
  of links below a caller-set fraction so a host can warn instead.
- **Holes and multi-ring outlines.** Not needed by this form; `earcutr`
  supports them via ring indices when something does.
- **A `play` panel.** Deferred to the milestone after the demo, so that the
  panel-channel plumbing is a separate, reviewable slice.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `widgets/sankey` (exported Go API under `public/`) | added — `Diagram`, `Node`, `Link`, `Mode`, `Layout`, `Compute`, `Validate`, `Report` | `package_props.go` registration (ADR-0080); no consumer yet |
| `widgets/sankey/view` (exported Go API under `public/`) | added — `RenderOpts`, `Draw`, `Show`, `Hit` | imports `implot` and the bindings; the only half that touches UI |
| demo registry (`egui2_hl_registrations.go`) | added — one `sankey` demo entry | registry is a named list; adding a member is not itself a decision |
| egui2 IDL | **unchanged** | no new paint opcode; `.Concave()` shipped in `04999929` |

## Alternatives

- **Standalone painter widget (O2).** Full control, but re-rolls pan, zoom,
  legend and readback that the custom-item lane already provides, and uses
  almost none of implot — the opposite of the second-wave goal.
- **Both renderers (O3).** Real flexibility, roughly 1.5× the view code and two
  renderers to keep in sync, for a second consumer that does not exist.
- **Extend `pipelineview` (O4).** Closes SD5's deferral but only for pipeline
  schematics; the form is worth having for arbitrary flow data.
- **Extend `layeredgraph` (O5).** Value-weighted edges on a structure-recovery
  layout give thick edges, not conserved flow: no global scale, no stacked node
  faces, so width would not honestly mean value.
- **Parallel sets as a third mode.** The form clutters badly as dimensions
  grow and has no consumer here; a categorical cross-tab is served today by
  `Heatmap`.
- **Chord diagram.** Only warranted when no linear stage ordering exists; the
  CHI 2023 comparison is against it on errors, time and preference.
- **Perpendicular-stroke ribbons.** Smoother mid-flight, but the band arriving
  at a node face is then wider than the value it carries.

## Consequences

### Positive

- The flow form arrives with pan, pointer-anchored zoom, box-zoom, legend rows
  with toggles, and plot-space readback already working, because implot owns
  them.
- A ribbon is one paint opcode (`FillPolygon`) or a whole diagram is one
  (`FillColumns`) — no per-segment opcode storm.
- ADR-0119 §SD5's deferral gains a concrete implementation to adopt: the
  geometry core is UI-free and takes an edge list with values.
- The second wave gains its second custom-item consumer after the lane chart,
  which is evidence about the seam rather than a single data point.

### Negative

- `FillPolygon` ribbons leave no geometry in the headless scene lane (§SD5),
  so a scene-level check of this widget has to select `FillColumns` or assert
  on the layout instead of the drawing.
- The unit-box choice ties node padding to plot aspect; a very wide, very short
  plot needs the pixel overrides to look right.
- Label placement rides the glyph-width estimate (ADR-0149 §SD6), so CJK labels
  will be under-measured and may overlap until a measurement channel exists.
- Cycles are rejected rather than drawn, so a caller with feedback flows must
  pre-aggregate them.

### Neutral

- Layout is deterministic and allocation-bounded, so it can be memoized on a
  diagram fingerprint the way `pipelineview` memoizes on its catalog
  fingerprint.
- The clean-room protocol of ADR-0119 §SD6 applies unchanged: implemented from
  papers and public reference documentation, all geometry re-derived, no
  external layout source consulted.

## Migration — Tier 1

- **Breaks.** Nothing. Both packages are new and have no consumers.
- **Path.** Nothing to migrate.
- **Regeneration.** None — no IDL change, so no `egui2gen` run. Note the
  standing hazard from `04999929`: that commit grew a `Build` terminator on the
  wire, so an old-Go/new-Rust pair desyncs. Anyone building this widget must
  have **both** sides rebuilt past that commit.
- **Old shape.** Not applicable.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the layout core: a golden-file layout
  (`testdata/*.golden`), a determinism check (two runs deep-equal), and
  invariants — conservation of stacked face height, global-`ky` consistency
  across stages, far-end-y ordering, no node overlap after collision
  resolution, `Validate` rejecting cycles / unknown refs / negative values /
  cross-stage links in alluvial mode. Plus the screenshot tour for the demo,
  whose PNG and SVG both carry the ribbons (§SD5).
- **What would fail.** A layout regression moves the golden; a broken global
  scale shows up as a stage whose stacked heights no longer sum consistently;
  a ribbon geometry regression shows as a hit test that disagrees with the
  drawn edge, which the shared-sampler test pins directly.
- **Gap.** The renderer has no automated check: `Custom` closures do not run
  on a detached plot, so the view's unit tests cover colour resolution,
  emphasis and option normalization, and what is actually painted rests on the
  tour capture being looked at. Crossing *quality* is not asserted either —
  only determinism and the invariants, so a relaxation that got worse but
  stayed deterministic would pass. The headless scene lane could close the
  first gap only under `FillColumns` (§SD5).

## Status

Proposed — awaiting review by @spx.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

### Method sources (clean room — papers and public documentation only)

| Source | Licence | Consulted | Extracted |
| --- | --- | --- | --- |
| [d3-sankey](https://github.com/d3/d3-sankey) | BSD-3-Clause | docs | global thickness scale, relaxation sweep shape, far-end-y link ordering |
| Zarate, Le Bodic, Dwyer, Gange, Stuckey, [*Optimal Sankey Diagrams via Integer Programming*](https://research.monash.edu/en/publications/optimal-sankey-diagrams-via-integer-programming/), PacificVis 2018 | paper | crossing minimization is NP-hard; ILP viable past ~50 nodes / 100 edges — recorded as the deferred upgrade path (§SD6) |
| Lupton & Allwood, [*Hybrid Sankey diagrams*](https://www.sciencedirect.com/science/article/pii/S0921344917301167), Resources, Conservation and Recycling 124 (2017) | paper | flow-data structure; layered layout over process stages |
| Schmidt, [*The Sankey Diagram in Energy and Material Flow Management*](https://onlinelibrary.wiley.com/doi/full/10.1111/j.1530-9290.2008.00004.x), J. Industrial Ecology (2008) | paper | conservation and unit-consistency conventions; failure modes |
| Gutwin, Mairena, Bandi, [*Showing Flow: Comparing Usability of Chord and Sankey Diagrams*](https://dl.acm.org/doi/10.1145/3544548.3581119), CHI 2023 | paper | the form-choice evidence against chord where a stage ordering exists |
| [Data Viz Catalogue — Sankey / parallel sets / alluvial](https://datavizcatalogue.com/blog/sankey-diagrams-parallel-sets-alluvial-diagrams-whats-the-difference/) | docs | the form-family distinctions behind §SD4 |

### Related ADRs

- [ADR-0149](./0149-implot-core-port-painter-lane.md) — the implot port; the
  custom-item lane this widget renders through, and the §SD6 text-measurement
  deferral it inherits.
- [ADR-0119](./0119-imzero2-pipelineview-widget.md) — pipelineview; its §SD5
  volume-overlay deferral is the nearest consumer, and its §SD6 clean-room
  protocol applies here unchanged.
- [ADR-0069](./0069-imzero2-layeredgraph-widget.md) — layeredgraph; the
  model / layout / `view` package split and the `RenderOpts` override pattern.
- [ADR-0156](./0156-qualitative-palette-dark-surface.md) — the qualitative
  palette node colours default to.
- [ADR-0080](./0080-packageprops-per-package-declarations.md) — the
  `package_props.go` registration both new packages carry.
