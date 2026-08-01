---
type: adr
status: accepted
date: 2026-08-01
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-01
---

# ADR-0159: a Sankey / alluvial flow widget on the implot custom-item lane

## Context

The widget zoo has forms for structure (`layeredgraph`, `pipelineview`,
`scctree`), hierarchy-with-magnitude (`treemap`) and distributions (`ecdf`,
`boxenplot`, `distsummary`). It has none for **quantity that moves** — a
weighted flow where a band's width *is* the value, conserved from stage to
stage. [ADR-0119](./0119-imzero2-pipelineview-widget.md) §SD5 deferred a
volume overlay of exactly this shape "until a live consumer exists", and its
`Edge.Volume` still flows through `EdgeLayout` untouched.

Two pieces of substrate decide what such a widget costs:

- The **custom-item lane** ([ADR-0149](./0149-implot-core-port-painter-lane.md))
  invokes a caller closure during emission with the frame's plot↔pixel
  transform, a palette slot, a legend row with a visibility toggle, and
  declaration-order z. An adopting widget inherits pan, pointer-anchored wheel
  zoom, box-zoom, double-click fit and plot-space readback without
  re-implementing any of it.
- **Concave polygon fill** — `PaintPolygonFilled(…).Concave().Stroke(…)`
  ear-clips into an `egui::Shape::Mesh`. The painter's filled polygon was
  convex-only before it, and a ribbon bounded by two curves is not convex, so
  ribbons would otherwise have to be decomposed into quads. A ribbon is now one
  paint opcode.

Load-bearing findings from the method survey (sources in
[§References](#references)):

- The layout is Sugiyama with a value-weighted twist: layer assignment (x),
  ordering within a layer, y-positioning, ribbon geometry. Crossing
  minimization is NP-hard, so the practical recipe is barycentre relaxation;
  exact ILP formulations stay viable into the low hundreds of nodes and edges,
  at the cost of a solver dependency.
- One rule does most of the visual work: **links at a node face are ordered by
  the y of their far end**, which removes most local crossings independently of
  the node ordering.
- The y scale is **global, not per-column**: `ky = min over stages of
  (H − (n−1)·pad) / Σvalue`. A per-column scale would let a ribbon change width
  mid-flight and destroy the conservation reading.
- Two ribbon conventions exist. **Vertical extrusion** — the fill between two
  y-offset copies of one curve — keeps a node bar an exact subdivision of its
  value. **Perpendicular stroke** (d3's `sankeyLinkHorizontal` with
  `stroke-width`) looks smoother but overstates vertical extent on steep
  segments, so the band meeting a node face is wider than the value it carries.
- Where a linear stage ordering exists, Sankey beats the chord diagram on
  errors, time and stated preference (Gutwin et al., CHI 2023).
- Failure modes that need an explicit answer rather than silence: cycles (a
  stage-ordered diagram cannot show one), sub-pixel flows, mixed units,
  non-conserving nodes, and label crowding — the last aggravated by the
  text-measurement channel ADR-0149 §SD6 deferred, which leaves widgets
  estimating glyph advances.

## Design space (QOC)

**Question.** What substrate and what layout depth should a flow-quantity
widget have?

**Options.**

- **O1** — **implot custom item**: a UI-free layout package plus a renderer
  that declares `Custom` closures into a caller-owned plot.
- **O2** — **Standalone painter widget**: own canvas and fit-to-pane, no axes,
  in the `pipelineview` / `layeredgraph` mould.
- **O3** — **Layout package with both renderers**: one geometry core, an
  implot renderer and a bare-painter renderer.
- **O4** — **Extend `pipelineview`** with its deferred volume overlay, and stop.
- **O5** — **Extend `layeredgraph`** with value-weighted edges.

**Criteria.**

- **C1** — interaction inherited vs re-rolled (pan, zoom, legend, readback).
- **C2** — implot reuse.
- **C3** — reach: how many shapes of flow data the result serves.
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
and layout core — plus `widgets/sankey/view`, which draws the geometry as
implot `Custom` items (**O1**). Layout is barycentre relaxation. The widget
covers two modes, Sankey and alluvial; the wider flow-form family is out of
scope.

### SD1 — Layout emits a unit box; implot owns the viewport

Layout returns geometry in a **unit box** — x and y both in `[0, 1]`, y in plot
convention — and the renderer pins both axes to it, hides them, and projects
through the frame transform.

The alternative, recomputing pixel geometry each frame from the area rect,
makes pan and zoom our problem instead of implot's. Under the unit box, zoom is
exact and free: ribbons, bars and node padding scale together, which is also
what helps a crowded diagram. Only text stays pixel-sized, which is right —
labels should not grow with zoom.

The consequence to accept: node padding and bar width are **fractions of the
box**, so their apparent size follows the plot's aspect. They are tunable, in
those fractions; there is no pixel override.

### SD2 — Two ribbon fill routes, sharing one sampler

Both routes sample the same vertical-extrusion geometry — a cubic Bézier with
both control points at the x-midpoint, offset in y by the link's thickness —
and differ only in how they reach the painter:

- **`FillPolygon` (default)** — one concave filled polygon per link. The
  outline runs along the top edge and back along the bottom: a single simple
  ring, which is what the ear-clipper supports, and it cannot self-intersect
  because the bottom edge is the top edge translated by a strictly positive
  constant. Its hairline stroke is not decoration — a mesh fill bypasses
  epaint's feathering, so without it the ribbon edge is unantialiased.
- **`FillColumns`** — every ribbon in the diagram rasterized as narrow vertical
  strips in a **single** batched rect call. This is the route that carries a
  source→target gradient at no extra opcode cost, and the one that renders in
  every export lane (§SD5). Its cost is stair-stepping where a ribbon edge is
  steep, mitigated by stroking the boundary curves as polylines.

  Column boundaries **snap to whole canvas pixels**. Abutting rects at
  fractional x each get a feathered edge, and at ribbon alpha the half-covered
  seams read as vertical banding across the whole diagram. Rounding is safe for
  tiling: neighbouring columns share a boundary value and round identically,
  and a column that collapses to zero width drops out without opening a gap.

Vertical extrusion, not perpendicular stroke, so a node bar is an exact
subdivision of its value at the face.

### SD3 — Hit testing is plot-space, so it is zoom-independent

Because all geometry lives in the unit box, hit tests read the plot-space
pointer registers and probe the layout directly — no cached pixel geometry, no
previous-frame area arithmetic. A ribbon test binary-searches the shared
sampler's x array and interpolates its two edges, so the region tested is the
region drawn. The usual one-frame register lag is the only staleness.

### SD4 — The two modes are two switches, not two engines

Calling the second mode "alluvial" risks implying more machinery than exists.
The difference is two switches on one pipeline:

| | Sankey | Alluvial |
| --- | --- | --- |
| Stage index | derived (longest path from sources) | given by the caller |
| Order within a stage | barycentre relaxation | caller key, then value-descending, stable across stages |

Everything else — the global scale, far-end-y face ordering, collision
resolution, ribbon geometry — is shared. Alluvial additionally requires links
to join adjacent stages, which validation enforces in that mode.

### SD5 — Mesh fills reach the tour's SVG, but not the headless scene lane

A concave fill becomes an `egui::Shape::Mesh`, and the tree's two SVG writers
treat that shape differently. `svgexport.rs`, the screenshot tour's exporter,
**has** a `Mesh` arm and emits one polygon per triangle. `headless_svg.rs`, the
scene lane from [ADR-0154](./0154-headless-carrier-tree-and-driver.md), has no
`Mesh` handling, so a `FillPolygon` ribbon leaves no geometry there.

So the tour verifies this widget in both PNG and SVG, and only a headless
*scene* assertion would see an empty diagram. `FillColumns` emits ordinary
rects and polylines and renders everywhere; it is what to select when the
output is destined for that lane. The two writers are easy to conflate — they
are not interchangeable.

### SD6 — Deferred, deliberately

- **Cycles.** Validation rejects them, naming the offending edge. The
  feedback-arc-set plus wrap-around routing a circular-flow diagram needs is a
  design of its own, and silently dropping edges would misstate the quantities.
- **Exact ordering for small graphs.** The ILP route is the upgrade path if
  relaxation proves visibly poor at our sizes; it wants a solver dependency
  nothing else in the tree needs.
- **Minimum ribbon thickness.** Not implemented. Sub-pixel flows draw at their
  true thickness and the layout reports how many there are, so a host can
  aggregate upstream or warn. A clamp would break proportionality, which is the
  one thing this form promises.
- **Holes and multi-ring outlines.** Not needed by this form; the ear-clipper
  supports them via ring indices when something needs them.
- **A `play` panel.** A separate slice, so the panel-channel plumbing is
  reviewable on its own.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `widgets/sankey` (exported Go API under `public/`) | added — the model (`Diagram`, `Node`, `Link`, `Mode`, `Align`, `Options`), the geometry (`Layout`, `NodeLayout`, `LinkLayout`, `Ribbon`) and `Compute` / `Validate` / `Report` | `package_props.go` registration (ADR-0080) |
| `widgets/sankey/view` (exported Go API under `public/`) | added — `Opts`, `FillMode`, `Hit`, `Renderer`, and `Setup` / `Probe` / `Draw` / `Show` | imports `implot` and the bindings; the only half that touches UI |
| demo registry | added — one `sankey` entry | adding a member to a named list is not itself a decision |
| egui2 IDL | **unchanged** | no new paint opcode; the concave fill already exists |

## Alternatives

- **Standalone painter widget (O2).** Re-rolls pan, zoom, legend and readback
  the custom-item lane already provides, and uses almost none of implot.
- **Both renderers (O3).** Roughly 1.5× the view code and two renderers to keep
  in sync, for a second consumer that does not exist.
- **Extend `pipelineview` (O4).** Serves pipeline schematics only; the form is
  worth having for arbitrary flow data.
- **Extend `layeredgraph` (O5).** Value-weighted edges on a structure-recovery
  layout give thick edges, not conserved flow — no global scale, no stacked
  node faces, so width would not honestly mean value.
- **Parallel sets as a third mode.** Clutters badly as dimensions grow and has
  no consumer here; a categorical cross-tab is served by `Heatmap`.
- **Chord diagram.** Warranted only when no linear stage ordering exists, and
  it loses to Sankey on errors, time and preference when one does.
- **Perpendicular-stroke ribbons.** Smoother mid-flight, but the band arriving
  at a node face is then wider than the value it carries.

## Consequences

### Positive

- The form arrives with pan, anchored zoom, box-zoom, legend toggles and
  plot-space readback already working, because implot owns them.
- A ribbon is one paint opcode, or a whole diagram is one — no per-segment
  opcode storm.
- ADR-0119 §SD5's deferral gains something concrete to adopt: the geometry core
  is UI-free and takes an edge list with values.

### Negative

- `FillPolygon` ribbons leave no geometry in the headless scene lane (§SD5), so
  a scene-level check must select `FillColumns` or assert on the layout instead
  of the drawing.
- The unit-box choice ties node padding to plot aspect; a very wide, very short
  plot needs its fractions retuned.
- Label placement rides a glyph-width estimate (ADR-0149 §SD6), so scripts that
  estimate badly — CJK especially — will overlap until a measurement channel
  exists.
- Cycles are rejected rather than drawn, so a caller with feedback flows must
  pre-aggregate them.

### Neutral

- Layout is deterministic and allocation-bounded, so a host can memoize it on a
  diagram fingerprint the way `pipelineview` memoizes on its catalog
  fingerprint.
- ADR-0119 §SD6's clean-room protocol applies unchanged: implemented from
  papers and public reference documentation, all geometry re-derived, no
  external layout source consulted.

## Migration — Tier 1

- **Breaks.** Nothing. Both packages are new and have no consumers.
- **Path.** Nothing to migrate.
- **Regeneration.** None — no IDL change, so no `egui2gen` run.
- **Old shape.** Not applicable.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the layout core: golden layouts, a
  determinism check (two runs deep-equal), and the invariants — one global
  scale, faces tiling their bar exactly, far-end-y ordering, no node overlap
  after collision resolution, the sampler agreeing with the hit test, and
  validation rejecting cycles, unknown refs, non-positive values and
  cross-stage links in alluvial mode. Plus the screenshot tour for the demo.
- **What would fail.** A layout regression moves a golden; a broken global
  scale shows as stacked heights that no longer sum consistently; a ribbon
  geometry regression shows as a hit test disagreeing with the drawn edge.
- **Gap.** The renderer has no automated check — `Custom` closures do not run
  on a detached plot, so the view's tests cover colour resolution, emphasis and
  option normalization, and what is actually painted rests on the tour capture
  being looked at. Crossing *quality* is not asserted either, so a relaxation
  that got worse but stayed deterministic would pass. Only `FillColumns` could
  ever be checked in the headless scene lane (§SD5).

## Status

Accepted 2026-08-01, with the layout core, the renderer and the gallery demo
built and verified the same day.

- **Layout core** — `widgets/sankey`: model, validation, barycentre layout and
  the shared ribbon sampler, under golden and invariant tests.
- **Renderer** — `widgets/sankey/view`: both fill routes, plot-space hit
  testing, and the optional layer legend.
- **Demo** — one gallery entry carrying both modes and both fill routes,
  captured by the screenshot tour.

Both §SD5 lanes were checked by capture, and §SD2's pixel snapping came out of
looking at one. No consumer is wired yet; ADR-0119 §SD5's volume overlay is the
nearest candidate, and a `play` panel stays deferred per §SD6.

## Update 2026-08-01 — an adversarial review; three silent defects, and two claims this ADR got wrong

A review of the shipped packages against this ADR found three defects, each
of which failed quietly rather than visibly.

- **`Report.Total` counted links, not quantity.** A flow crossing three
  stages is three links but one quantity, so the sum overstated a conserved
  diagram by roughly its stage count: 199 PJ for the 80 PJ energy balance,
  2400 accounts for the 1200-account cohort. The demo printed that as the
  diagram's total *and* divided by it for "% of total", so every share on
  screen was wrong too. It is now the outflow of every node with no inflow.
  This is the one number the widget published that contradicted §C5, the
  criterion the form was chosen on.
- **`Options.NodeWidth` was never clamped**, though `Layout.NodeWidth`'s
  doc claimed it reported a clamped value. Past roughly `1/NodeWidth`
  stages a bar reaches into the next stage and every ribbon runs backwards.
  That is not cosmetic: the sampler's x then descends, which breaks the
  simple-ring precondition §SD2 relies on *and* the binary search §SD3
  relies on, so ribbons fill as self-intersecting meshes and the hit test
  stops finding them. Capped at half the stage pitch. The other box
  fractions now take their default on any value a fraction cannot be.
- **Double-click fit was a no-op.** Custom items do not contribute to
  implot's auto-fit, so with the axes pinned `CondOnce` and no extent
  declared, `dataOk` stayed false and the fit was skipped — leaving a
  panned-away diagram with no way back. `Draw` now declares the unit box
  and `Setup` bounds the gestures to it. §Consequences claimed this lane
  gave us fit "already working"; it gives us the *mechanism*, and the
  extent is the adopter's to declare. `icicle/view`, built on the same lane
  afterwards, had already learned this.

Two statements in this ADR were also wrong on the facts:

- The **CJK label** bullet under §Consequences said badly-estimated scripts
  "will overlap". The estimate counts bytes, so CJK is charged about 1.8 em
  per glyph against a true advance near 1.0 — it over-measures, and those
  labels are *dropped*, not overlapped. Separately, the estimate only ever
  decides whether a label leaves the plot area; nothing deconflicts a label
  from its vertical neighbour, so label crowding — named in §Context as a
  failure mode needing an explicit answer — is answered only by a 7 px
  minimum bar height against an 11 pt font. Aggregating upstream remains
  the honest answer, as it is for the thin ribbons those bars belong to.
- §SD3's "the region tested is the region drawn" is exact only for
  `FillPolygon`. `FillColumns` raises the sample count to about one strip
  per two pixels and steps its edge, so the two disagree — by well under a
  pixel, but the word "exactly" was not earned.

Verification moved with it: goldens for the total (geometry unchanged,
which is what confirms the width clamp leaves a fitting diagram alone), and
new cases for the clamp on a long chain, an over-wide bar and a bar wider
than the box, asserting ribbons stay forward and the sampler stays
ascending. The fit extent is still unverified — `dataXMin` is unexported,
so only implot's own tests can observe a fit contribution. §Verification
plan's "Gap" grows that item.

## Update 2026-08-02 — `Hit` grows a kind; the glyph estimate moves to the lane

Two follow-ups from the same review, both changing the §Surfaces row for
`widgets/sankey/view`. Nothing outside this tree consumes either, so
§Migration still reads "breaks nothing".

- **`Hit` is now a kind plus an index**, with `NodeHit` / `LinkHit`
  constructors and `Node()` / `Link()` / `None()` accessors; `NoHit` is gone,
  because the zero value now means nothing on its own. The old pair of
  `-1`-defaulted indices had a zero value that read as "node 0", which is why
  `Draw` needed a normalization step purely to fold `Opts{}` back to
  "nothing". [ADR-0160](./0160-imzero2-icicle-flamegraph-widget.md)'s widget,
  landing on the same lane, avoided that with an `Ok` bool for its single
  kind. The two are now the same discipline and the same constructor
  vocabulary, without forcing a shared type on two widgets whose hits differ
  in arity — the one-kind case does not want a kind enum. Normalization
  survives, but only to drop an index a swapped layout no longer holds.
- **Label width comes from `implot.EstimateTextWidth`**, one estimate for the
  lane, replacing five per-package copies at three ratios. It counts runes and
  charges the East Asian wide blocks a full em.

That last point retires the CJK claim in the Update above, and a broader one
this ADR made: **text measurement is not deferred**. `bindings.MeasureText` is
live, and `widgets/colorscale` drives it with a one-frame-lag cache.
[ADR-0149](./0149-implot-core-port-painter-lane.md) §SD6 defers a fetcher
*inside implot* for its own tick and legend sizing — not the channel. So the
estimate here is a choice, and a defensible one at a few percent of width for
a drop-or-keep decision; it is no longer the only option, and §Consequences'
"until a measurement channel exists" was wrong.

## Update 2026-08-02 (2) — the palette wrap is reported; the legend rows carry their layer's ink

The last two review items, both in `widgets/sankey/view`, both adding to its
§Surfaces row.

- **`PaletteRepeats`** reports how many bars fall back to a hue another bar
  already carries. [ADR-0156](./0156-qualitative-palette-dark-surface.md)'s
  palette is seven entries and its own guidance is that a consumer needing
  more should vary a second channel, not expect an eighth hue — but a flow
  diagram routinely has more than seven nodes, and since a ribbon defaults to
  its source's colour the repeat lands on the flows too. The gallery demo's
  own energy diagram has nine, so it was shipping duplicate hues silently.

  Inventing a hue is out, and so is darkening one: a shade tier reads as
  "related", which is a claim about the data, and it is not checked against
  the palette's contrast floor. So this follows the §SD6 precedent set for
  sub-pixel flows — report it, let the host answer, and keep the encoding
  honest. `Opts.NodeColor` is the answer, and the demo now prints the count
  rather than hiding it.

  The cycle also advances only on nodes that actually reach the palette. It
  was advancing per node index, so hues were burned on nodes a caller had
  already coloured and the wrap came sooner than it needed to.

- **The lane's item style is wired up.** With `Layers` on, each legend row's
  swatch is now set from what its layer draws — the first resolved node
  colour at ribbon alpha, at full alpha, and the label ink — instead of
  whatever series slot implot handed out; these rows are toggles, and an
  unrelated colour beside "flows" said nothing. `DrawCtx.Highlighted` now
  emphasises a whole layer while its legend row is hovered, and the ribbon
  outline's weight comes from `DrawCtx.Weight` via `SetNextWeight`, so the
  lane's built-in ×2 hover emphasis applies for free.

  The diagram's own colours stay the diagram's: `DrawCtx.Color` arrives as
  the swatch just set and the closures ignore it, because a node's fill is
  data and not a slot. That is deliberate, and noted in the code so it is not
  "fixed" the other way round.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-08-01 — pipelineview's overlay shipped without consuming this package

§Consequences claims ADR-0119 §SD5's deferral "gains something concrete to
adopt". That deferral has now been closed
([ADR-0119 Update](./0119-imzero2-pipelineview-widget.md#updates)) and it did
**not** adopt this package: the overlay is a thickness encoding on
pipelineview's own routed orthogonal edges.

The claim was optimistic in a specific way worth naming, because it will
recur. Adopting this layout core means adopting stacked node faces and
value-proportional ribbons — the Sankey read — and ADR-0119 §SD2 chose against
exactly that for a schematic. So the deferral's *quantity* half was servable by
a stroke width, while its *ribbon* half was never really pipelineview's to
implement; it was this widget's, and now lives here.

The useful division: a schematic whose wires carry a weight is that overlay;
"where did the quantity go" is this widget. A shared lineage of ideas, not
shared code.

## References

### Method sources (clean room — papers and public documentation only)

Implemented from the documents below. `d3-sankey` (BSD-3-Clause) was read as
documentation only; everything else is published research. No external layout
source was consulted, per ADR-0119 §SD6.

| Source | Extracted |
| --- | --- |
| [d3-sankey](https://github.com/d3/d3-sankey) (docs) | global thickness scale, relaxation sweep shape, far-end-y link ordering |
| Zarate, Le Bodic, Dwyer, Gange, Stuckey, [*Optimal Sankey Diagrams via Integer Programming*](https://research.monash.edu/en/publications/optimal-sankey-diagrams-via-integer-programming/), PacificVis 2018 | crossing minimization is NP-hard; the ILP upgrade path recorded in §SD6 |
| Lupton & Allwood, [*Hybrid Sankey diagrams*](https://www.sciencedirect.com/science/article/pii/S0921344917301167), Resources, Conservation and Recycling 124 (2017) | flow-data structure; layered layout over process stages |
| Schmidt, [*The Sankey Diagram in Energy and Material Flow Management*](https://onlinelibrary.wiley.com/doi/full/10.1111/j.1530-9290.2008.00004.x), J. Industrial Ecology (2008) | conservation and unit-consistency conventions; failure modes |
| Gutwin, Mairena, Bandi, [*Showing Flow: Comparing Usability of Chord and Sankey Diagrams*](https://dl.acm.org/doi/10.1145/3544548.3581119), CHI 2023 | the form-choice evidence against chord where a stage ordering exists |
| [Data Viz Catalogue — Sankey / parallel sets / alluvial](https://datavizcatalogue.com/blog/sankey-diagrams-parallel-sets-alluvial-diagrams-whats-the-difference/) (docs) | the form-family distinctions behind §SD4 |

### Related ADRs

- [ADR-0149](./0149-implot-core-port-painter-lane.md) — the implot port: the
  custom-item lane this widget renders through, and the §SD6 text-measurement
  deferral it inherits.
- [ADR-0119](./0119-imzero2-pipelineview-widget.md) — pipelineview: its §SD5
  volume-overlay deferral is the nearest consumer, and its §SD6 clean-room
  protocol applies here unchanged.
- [ADR-0069](./0069-imzero2-layeredgraph-widget.md) — layeredgraph: the
  model / layout / `view` package split and the render-override pattern.
- [ADR-0156](./0156-qualitative-palette-dark-surface.md) — the qualitative
  palette node colours default to.
- [ADR-0080](./0080-packageprops-per-package-declarations.md) — the
  `package_props.go` registration both new packages carry.
