---
type: adr
status: accepted
date: 2026-09-10
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-12
---

# ADR-0224: graphview — the live graph widget as Go on the painter lane

## Context

The `graph` widget binds the `egui_graphs` crate: Go re-declares nodes and
edges every frame into Rust-side registers, the crate owns positions, layout
simulation, hit-testing and the camera, and three fetchers carry events,
selection and metrics back. Its in-tree consumers are gone — `fsmview` moved
to `layeredgraph` ([ADR-0069](./0069-imzero2-layeredgraph-widget.md)) and the
godepview app of [ADR-0064](./0064-godepview-go-dependency-explorer.md) no
longer exists — and the remaining consumers live in a downstream module, where
they use force-directed layout with centre gravity, hierarchical layout, node
and edge colours, edge labels, click and double-click events, a one-shot fit,
fast-forward, and the settle metric to freeze the simulation.

Three pressures make the binding the wrong home for that:

- **It pins the egui ring.** Every other ring member has a release against
  egui 0.36; `egui_graphs` does not. The Cargo manifest names it as the one
  crate the ring waits for.
- **Its quality is capped by the crate's stock shapes** — labels sized by the
  node radius, arrowheads in screen pixels, a screen-sized layout constant —
  and each fix is seam work in the IDL, Rust apply code and a fetcher.
  ADR-0069 recorded the cap and chose to route around the crate rather than
  through it.
- **The doctrine is settled.** [ADR-0149](./0149-implot-core-port-painter-lane.md)
  ported the ImPlot core to Go on the painter lane and retired `egui_plot`;
  ADR-0069 and [ADR-0204](./0204-leaflet-map-core-port.md) made the same
  move for layered graphs and maps. The snarl port analysis
  (`doc/adr-background-work/snarl-port-analysis.md`) found the painter lane
  needs no new IDL for a node-and-edge widget: circles, lines, cubic Béziers,
  filled polygons, anchored text, batched markers, the clip stack, sense
  regions, the per-canvas pointer rows (R24) and the hover-scoped wheel
  ([ADR-0140](./0140-imzero2-hover-scoped-wheel-capture.md)) are in stock.

The layout cost moves to Go with the port. A naive Fruchterman–Reingold step
is O(n²) in the node count. On one laptop, one crate-shaped scalar Go step on
1k nodes took about 10 ms; the same step written struct-of-arrays and split
across the machine's cores took about 10 ms on 5k nodes. Go 1.27's `simd`
package brought the single-thread figure to about 1 ms on 1k nodes, but the
package still requires `GOEXPERIMENT=simd`, which the build environment of
[ADR-0215](./0215-retire-mimalloc-reproducible-builds.md) does not set. These
are one-machine measurements, not a trial; they bound the shape of the
decision, not its numbers.

## Design space (QOC)

**Question.** Where does the live (force-directed and hierarchical) graph
widget live, and what happens to the `egui_graphs` binding?

**Options.**

- **O1** — Keep the binding; repair quality issues in Rust apply code.
- **O2** — A new Go package on the painter lane; the binding stays until the
  consumers have migrated, then a later decision retires it.
- **O3** — A first-party Rust module inside `rust/imzero2` replacing the crate.
- **O4** — Extend `layeredgraph/view` with a force layout engine instead of a
  new package.

**Criteria.**

- **C1** — Unblocks the egui ring bump.
- **C2** — Seam tax per feature (IDL, Rust, fetcher per change).
- **C3** — Where logic accumulates (Go-first doctrine).
- **C4** — Migration risk for downstream consumers.
- **C5** — Layout performance ceiling.

**Assessment.**

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | ++ | ++ | ++ |
| C2 | −− | ++ | −  | ++ |
| C3 | −  | ++ | −− | ++ |
| C4 | ++ | +  | +  | −  |
| C5 | −  | +  | +  | +  |

O4 scores like O2 on the technical criteria but couples a static, host-laid-out
widget whose layout is cached by content hash to a per-frame simulation with a
camera, node dragging and selection. `layeredgraph/view` takes a finished
`Layout`; a live widget owns positions. Merging them is a redesign of the
static widget for the benefit of the live one, so O4 loses on C4 and is
deferred, not killed: once graphview has stabilised, whether the two share a
painter core is a separate decision.

## Decision

We build **`widgets/graphview`**, a Go widget on the painter lane with the
`egui_graphs` binding's feature set, as a sibling package. The binding, its
IDL, fetchers and crate stay untouched until the downstream consumers have
migrated; their retirement is a later dated Update or ADR, because removing
the IDL nodes is a Tier 1 change this leaf decision does not make.

**SD1 — Go-authoritative topology, widget-owned geometry.** The caller
declares the full node and edge set every frame as slices of specs keyed by
`uint64` ids, exactly as it declares them to the binding today. The widget
reconciles against retained struct-of-arrays state — positions, colours,
labels, selection — inserting new ids, dropping vanished ones and keeping
positions the user dragged to. No opcode carries topology; only paint
commands cross the FFI.

**SD2 — The four layouts are ported, with two deliberate departures.**
Random, Fruchterman–Reingold, Fruchterman–Reingold with centre gravity, and
the crate's hierarchical tree walk keep their parameters and their meaning,
so a consumer's tuned `dt`, `damping`, `epsilon`, `kScale` and row/column
distances carry over (the crate declared `center_parent` and never applied
it; here it centres). Two knobs do not carry over unchanged and are named in
the skill's migration note: the wheel zoom follows the host's factor rather
than a fixed step, and the fit padding is a fraction of the canvas rather
than a scale on the graph's diagonal. The departures: the ideal edge length `k` derives from
the **canvas** area, not the whole screen as the crate does, so a graph in a
side pane spaces like a graph in a window; and the random and hierarchical
layouts are **deterministic** — random placement hashes the node id — so
demos capture stably and a hierarchical graph that grows re-lays out on
topology change instead of parking new nodes at the origin.

**SD3 — Hit-testing is Go-side over one canvas.** The canvas carries one
sense region; the pointer arrives through the canvas's R24 row and the
global pointer, and the widget picks the node under it by radius, then the
edge by segment distance. The alternative — one sense region per node, as
`layeredgraph` does at tens of nodes — is one `ui.interact` per node per
frame on the Rust side and an emission-order hit-test priority (the ADR-0149
M7 lesson); a Go-side pick over struct-of-arrays positions is cheaper and
puts the priority rule in one place. The pick loop is O(n); a spatial grid is
the upgrade path when it shows in a profile.

**SD4 — The camera keeps the binding's fit contract.** Continuous fit is an
option; the default is the one-shot latch the binding grew: fit while a
freshly laid-out graph settles, then latch off so manual pan and zoom stick.
`FitNow` and `ResetLayout` re-arm it; `FastForward` advances the simulation
before painting. Wheel zoom anchors on the hover point (R23); drag pans, or
moves a node when the press lands on one.

**SD5 — Events, selection and metrics are Go values, not fetchers.** The
event kinds keep the binding's numbering so a migration is a type rename.
They are read from the widget after `Render` in the same frame; the input
they reflect is one frame old, like every canvas register.

**SD6 — Performance posture.** Positions are struct-of-arrays `float32`.
Below a few hundred nodes repulsion is the exact full n² form rather than
the crate's symmetric pair loop: twice the arithmetic, but each row is
independent, so it splits across goroutines and vectorises when a SIMD
build is available. Above that threshold repulsion is **Barnes–Hut** over a
quadtree rebuilt every step, with d3-force's opening angle as the default
and the walk order fixed, so the result stays deterministic and splits
across goroutines the same way. The alternatives were weighed and are
recorded here so they are not re-investigated: Gove's random vertex
sampling (Computer Graphics Forum 2019; `d3-force-sampled`) is O(n) and
measured about three times faster than d3's Barnes–Hut at equal readability,
but it updates a random sample of vertices per iteration, which trades the
per-frame stability an interactive widget shows and the determinism the
gallery capture relies on; the fast multipole method (FM³) buys a better
asymptote at a constant that pays off past the node counts this widget can
draw legibly; quadtree reuse across iterations (`d3-force-reuse`) is a
refinement that fits on top of Barnes–Hut later. A `simd` implementation
of the exact rows is **deferred** until the build environment sets the
experiment.

**SD7 — Labels are screen-sized.** Node labels paint at a fixed point size
above the node, monospace optional, rather than at the node's screen radius.
This is the quality cap ADR-0069 named and the reason downstream tuned
`kScale` per node count; it is the one visible change a migrating consumer
should expect.

**SD8 — Home and provenance.** Package
`public/thestack/imzero2/egui2/widgets/graphview`. The layouts are
re-derivations of published algorithms and of `egui_graphs`' parameterisation
(MIT); the package doc names the crate as the source of the parameter
semantics.

**SD9 — Node donuts are ring sectors on the painter lane.** A node may carry
a `Donut`: proportional slices drawn as a ring around its disc, clockwise
from the top, one concave filled polygon per slice through the painter's
existing concave fill. The caller supplies values and, optionally, colours
and a total; a total larger than the sum leaves the remainder as a muted
track, so the same field draws a share breakdown or a progress ring. The
ring belongs to the node for picking, highlighting and label placement, and
it is not drawn while the node's own disc is under two pixels on screen —
at that zoom the ring would dwarf the node and cost a polygon per slice for
nothing legible. Per-slice hover and click are deferred until a consumer
needs them; the pick treats the ring as part of the node.

**SD10 — Pinned vertices follow d3-force's fixed-position model.** A node is
fixed either by the caller — `Pinned` with a world position on its
`NodeSpec`, re-stated every frame like the rest of the declaration — or by
the widget, which holds a node where the user dropped it when `PinOnDrag`
is set and until `UnpinNode` releases it; `PinNode` sets such a hold from
code. The force step leaves fixed nodes where they are, and a drag on a
declared-pinned node moves it for the gesture only: the node events carry
the node's world position, so `NodeDragEnd` tells the caller where the user
left it, and whether the pin follows is the caller's decision, not the
widget's. A hairline ring marks a fixed node. The hierarchical layout
re-places a widget-side pin when it re-runs; a declared pin wins over any
placement.

**SD11 — Auras are a scalar field per group, contoured on a screen-space
grid.** A node may name one or more **aura** ids on its `NodeSpec`; nodes
sharing an id are drawn over one translucent blob, so a grouping that is not
an edge — a team, a cluster label, a query result — reads at a glance. The
shape is not a hull. Every visible node emanates a radial ramp on the
screen — 1 at the centre, linear to 0 at an outer radius that is a multiple
of the node's on-screen radius plus a small screen-pixel pad — and the ramps
of one aura accumulate as a complementary product, `a ← a + f − a·f`, on an
alpha grid of `cellSize` screen pixels over the canvas. A cell belongs to the
aura where the accumulated value reaches `drawLimit`; with `Overlap` off a
cell belongs to the single aura with the largest value, ties to the smaller
id. The mask's iso-line is traced by marching squares **with linear
interpolation along cell edges**, smoothed, and painted as one concave filled
polygon per ring with a hairline stroke in the fill colour (the SD9 route),
beneath edges and nodes, in each aura's `zIndex` order; overlapping auras
blend by alpha. The field is recomputed when the camera, the membership or a
set position changes, or once the simulation has drifted a fraction of a
cell, and reused otherwise. The behaviour is a
re-derivation of the aura feature of ZoomCharts NetChart as measured
black-box in `doc/adr-background-work/netchart-aura-analysis.md`; the knob
names `cellSize`, `intensity`, `drawLimit` and `overlap` keep that meaning,
the constants are this widget's own, and the interpolated contour is a
deliberate departure — the reference joins cell centres, which turns a lone
node at a 10 px cell into a rounded square. Auras are paint only: they take
no part in layout or picking, and the camera fit adds their screen-space
extent to the node bounds so a fitted graph is not clipped. The legend that
lists auras and toggles them is **not** graphview's: it is a shared legend
package under `widgets/`, factored out with the ImPlot port's clickable
legend rows as its second consumer, and graphview exposes the aura ids and a
hidden set for it to drive. A hidden aura's nodes stay declared; the caller
that wants them gone omits them. Polygons with holes — an aura ringing an
empty pocket — fill the pocket, since the concave fill takes a single outer
ring.

**SD12 — The interaction surface is the reference viewers' common core,
without a navigation layer.** A gap analysis of the ZoomCharts NetChart
instance API against this widget (see References) sorted the missing
surface into two kinds. The widget-local kind is adopted here: a
programmatic selection (`SelectNode`, `SelectEdge`, `ClearSelection` and
their readers, silent like `HideAura`, since the caller asked); secondary
clicks — the right button, or a long touch — on nodes, edges and the
background, and clicks on the background carrying the world position, all
opt-in through `Options`; edge double-click and edge hover events beside the
node ones; a Shift-drag rectangle selection over node centres, replacing the
selection unless multi-selection adds; `FitNodes` to frame a subset, the
fit a focus action wants; `Positions` as the bulk read a caller saves a
layout with; zoom bounds as options; `Metrics.CameraMoved` in place of a
camera event; and `ForceParams.PauseOnSettle`, which holds a settled
simulation until a drag, a topology or parameter change, a setter or a
fast-forward moves it, so a graph at rest costs no step per frame. The
other kind — NetChart's navigation model, where the chart owns a visible
subset of a larger graph and focus nodes with relevance, expansion radius
and auto-unfocus decide it, with hide, show, expand and collapse by depth —
is **deferred**: it is a caller-side walk over data the widget never sees,
and under SD1 it belongs in a helper package above graphview that declares
the subset, with `placeNear` already seating expanded nodes beside their
parent. A radial layout around a focus node is deferred with it, since it
is the layout that model wants. Neither needs a change to this widget.

**SD13 — Edges may carry an id, a length and a strength.** `EdgeSpec.Id`
is optional and tells parallel edges of one ordered pair apart in hover,
click and selection, which are keyed by `EdgeRef{From, To, Id}`; without
ids the pair's edges share a ref and select together, which is what the
binding did. The id enters the topology hash, so an id change rebuilds the
edge structure. `Length` multiplies the ideal edge length `k` for that edge
and `Strength` its pull in the attraction pass, both 1 unless declared, so
the unweighted step is unchanged to the bit; the static layouts ignore
them. The adjacency keeps the edge index beside each neighbour entry to
find them, one `int32` per edge end.

## Alternatives

- **O1 — keep the binding.** Every quality fix is seam work in three places,
  the ring stays pinned, and the widget's logic keeps accumulating on the
  side of the boundary the architecture keeps thin. Killed.
- **O3 — first-party Rust module.** Drops the crate but keeps positions,
  camera and hit-testing in Rust, so events still need fetchers and every
  feature still crosses the IDL. Killed.
- **O4 — extend `layeredgraph/view`.** Deferred, see the QOC note.
- **A dedicated arc or ring-sector paint opcode for donuts.** One IDL node,
  one Rust apply and a regeneration for a shape the concave fill already
  draws at the sizes a node ring has. Killed until a profile shows the
  polygon path costing something.
- **Convex or concave hulls per aura.** A hull per group is cheaper than a
  field, but it cannot express the reference behaviour a consumer expects:
  overlapping groups blending, a node in two groups pulling both, a group
  splitting into islands as its nodes drift apart. Killed for SD11.
- **Cell-centre outlines, as the reference draws them.** Cheaper to trace
  than an interpolated iso-line, but visibly blocky at the cell sizes the
  grid needs to stay cheap. Killed for SD11.
- **Random vertex sampling instead of Barnes–Hut.** Faster in the
  published measurements, but stochastic per iteration; see SD6. Killed for
  the interactive widget, open as a warm-up phase for very large graphs.
- **Programmatic selection reporting Select / Deselect events.** NetChart's
  setter fires `selectionChange`; here the aura setters already established
  that code-driven state changes are silent, and a caller that wants the
  event has the id in hand. Killed for SD12.
- **A widget-owned navigation layer (focus, expand, collapse, hide).**
  Would give the widget a second, hidden node set beside the declaration
  and break SD1's one-source-of-topology rule. Deferred as a helper package;
  see SD12.

## Consequences

### Positive

- No IDL, Rust or fetcher change per feature; the whole widget is Go and
  testable without a client.
- Deterministic random and hierarchical layouts make the gallery demo
  capture-stable.
- Once the consumers have migrated, the binding, four crates and the ring pin
  can go.

### Negative

- Two live-graph widgets coexist until migration completes.
- The Barnes–Hut step is O(n log n) but still a per-frame simulation; tens
  of thousands of nodes animate at a reduced rate and want a fast-forward
  then a pause.
- Node labels change size and placement relative to the binding.

### Neutral

- `layeredgraph`, `pipelineview` and `sankey` are unaffected; the tree now
  holds one more node-and-edge renderer, with the O4 question open.

## Verification plan

- **Lane.** Default `go test` for the layouts (hierarchical placement,
  force-step convergence and displacement bookkeeping, the Barnes–Hut
  approximation against the exact sum, reconcile keeping dragged
  positions, camera fit), and the gallery demo under the screenshot tour
  for the painted result. The package benchmark compares the exact rows
  with the tree at several sizes and is what the threshold was read from.
- **Auras.** Unit tests for the kernel and accumulation against the
  measured tables in the background analysis, marching squares on synthetic
  masks, ownership on two-node cases with and without overlap; the force
  demo grouped by subtree under the screenshot tour for the painted result.
- **Interaction surface (SD12, SD13).** Unit tests drive `applyInput` with
  synthetic response flags and modifiers: secondary clicks on each target,
  background clicks carrying the world position, edge hover enter and
  leave, the rectangle selection replacing and adding, the parallel-edge
  refs, weighted attraction against the unweighted pull, and the
  pause-on-settle hold taken and lifted by each wake condition.
- **What would fail.** A layout regression shows in the unit tests; a
  painter or input regression shows in the tour capture and in driving the
  demo with egui-mcp.
- **Gap.** Gesture handling is verified interactively, not by a headless
  scene; a scene follows once the widget has consumers in this tree.

## Status

Accepted 2026-09-12. The widget ships with this record;
the downstream consumers migrate at their own pace, and the binding's
retirement is recorded when they have.

## Updates

### 2026-09-12 — pick grid, edge index, scripted scenes, edge-id scope

Four things that closed the last widget-local gaps before a first
consumer, recorded here rather than by editing the body.

**SD3's upgrade path is taken.** Node picking runs over a uniform grid in
world units, rebuilt when a slot, a position or a radius changes and
reused otherwise, so a graph at rest picks in the cells around the pointer
rather than over every node; a live force layout rebuilds per frame, one
counting sort, no worse than the scan it replaces. Edge picking keeps its
linear loop but rejects an edge on its padded endpoint box before resolving
its geometry; the padding carries the node radius the stroke is trimmed
by, since overlapping discs put the trimmed stroke past the far endpoint —
a case the property test against the plain scan found. The edge refs of
SD13 are indexed per topology change, so a programmatic select and the
selection pruning are lookups, not scans.

**Edge ids are scoped to the ordered pair.** An `EdgeSpec.Id` need only be
unique among the edges between one `(From, To)`, and `EdgeRef` — the full
triple — is the only key; there is no lookup by id alone. A caller whose
ids are global still has the endpoints in hand when it declares the edge,
and a per-pair scope keeps the index a map over the declaration rather
than a second registry that would have to be kept unique. The alternative,
id-only lookup with a uniqueness check, was weighed and not taken.

**The headless scene exists.** The bindings' state manager gained
`Script*` setters that place a value in a per-frame register as Sync does
when it drains the host, with `ScriptReset` as the frame boundary, and a
fffi2 channel that discards paint commands lets `Render` run without a
client. The package's scene tests drive whole frames that way — the fit
latch, a click, a secondary click on the background, a wheel zoom,
`FitNodes`, a node vanishing from the declaration, a drag waking the
pause-on-settle hold, auras with their legend — which closes the
verification gap the plan named. The recipe is not graphview's: any canvas
widget can script its registers the same way.

**Live check.** The secondary click on a node and on the background, the
Shift-drag rectangle and the anchored wheel zoom were driven against the
gallery demo on the desktop host with OS-level synthetic input and
screenshots, since the egui_mcp route was not registered in the session
that ran it, and each reported as designed in the demo's event log. Long
touch was not exercised: the OS-level route has no touch input.

### 2026-09-12 — a fifth layout, radial, and the navigation layer above

[ADR-0225](./0225-graphview-navigation-layer-and-radial-layout.md) takes
the two pieces §SD12 deferred. The widget gains `LayoutRadial` with
`RadialParams{Centers, RingDist}` — rings by undirected hop distance from
a centre set, angles by the radial tree allotment, static and re-run on a
topology or parameter change like the hierarchical layout, its value past
the binding's four so a cast still lands. The navigation model — roots,
expansions, hidden nodes, a focus list, and the visible set derived from
them — lives in the `nav` package beneath this one and declares into the
widget; nothing in the declaration or event surface changed for it.

### 2026-09-12 — the fade-and-ignore pair, and the bounds readers

Every reading in the gap-analysis series named one missing primitive, under
five names: NetChart's `focusAutoFadeout`, vis-network's per-node `opacity`,
Obsidian's dim-the-rest hover, force-graph's faded background nodes, and
Ogma's `setDisabled`. The last settles its shape, because a disabled node
there is faded *and* undetectable — so what is adopted is the pair, not
opacity alone. A faded item that still swallows the pointer is a bug rather
than a feature.

**SD14 — An item may fade and may decline the pointer; the widget's state
paint does neither.** `NodeSpec.Opacity` and `EdgeSpec.Opacity` scale the
item's *own* paint — a node's fill, stroke, donut and label, an edge's line,
head and label — to a fraction of its declared alpha. Zero is the unset
value and anything at or above 1 paints as declared, so a frame that
declares no opacity is unchanged to the bit; fully transparent has no
spelling, since an item that should not be seen is one the declaration
leaves out. That is the sentinel `EdgeSpec.Length` and `Strength` already
carry, with the same cost, named here rather than discovered later. The pin,
selection and hover rings, and an edge's selected or hovered colour, keep
full strength: they are the widget saying what it is doing, not the caller
saying what the item is, and a dimmed node one can still hover should still
show that it is hovered. `NoPick` takes an item out of every pointer path —
hover, click, drag and the rectangle selection — while `SelectNode` and
`SelectEdge` keep working, since the caller asked, as the aura setters
established in §SD12. The two fields are independent: the pair is the common
case, `NoPick` alone is a node that is scaffolding rather than content, and
`Opacity` alone is a background item that can still be reached. The fade is
applied where the fill is resolved rather than at the paint, so nodes at one
opacity remain one batched marker and the batch key does the separating.

Beside it, the two readers four analyses asked for: `Bounds` returns the
world box of the declaration's nodes, `BoundsOf` the box over a subset, and
`FitNodes` is expressed through the latter so the fit and the reader cannot
drift. Neither carries the auras, whose screen-space reach the fit adds
separately, nor the labels, which are screen-sized.

What this does **not** add is the hover neighbourhood highlight the same
readings asked for beside it, and it does not need to: with the adjacency
`nav` publishes (ADR-0225's update of this date) a caller derives the
neighbour set and dims the rest itself. What counts as a neighbour — one
hop, both directions, through a hidden node or not — is the consumer's
question, and a widget that answered it would be guessing.

### 2026-09-12 — the camera moved out of the package

§SD4's camera — the isotropic scale and translation, the fit, the
zoom-about-a-point and the zoom limits — is no longer this package's. It is
`widgets/camera`, shared with `layeredgraph/view`, which had composed the same
arithmetic inline from its fit and its user pan and zoom, and produced by
portolan's `View.Camera` so a graph can be drawn over a basemap in the map's
own pixels ([ADR-0228](./0228-hosted-canvas-rendering-and-graph-widget-sharing.md)
§SD5, phase 1). Nothing in this widget's behaviour or surface changed: the
fields are exported where they were unexported, `setLimits` is the two limit
fields plus `ClampZoom`, and the tests moved with the type.

### 2026-09-12 — the aura legend's place is declared, not assumed

§SD11 put the legend in the canvas's top-left at a fixed inset and made it a
boolean. That is one of three things a caller wants, and the other two matter:
a legend in the corner the picture leaves free, and a legend the *caller*
draws, outside the view entirely.

**SD15 — The legend has a mode and a corner, and its rows are published in
every mode.** `AuraParams.Legend` becomes `AuraLegendModeE`:
`AuraLegendOff`, `AuraLegendInside` — the old behaviour, now at
`LegendCorner` with `LegendInset` — and `AuraLegendExternal`, which paints
nothing and stamps no region. `View.AuraLegendItems` reports the rows in
every mode, so the mode chooses *who paints them*, not whether they exist: an
external caller paints them where it likes through the shared legend package
and toggles with `HideAura` and `ShowAura`, which were already silent because
the caller asked. A right- or bottom-anchored box is measured so it sits
inside the canvas, and on a canvas too small to hold it the near edge wins, so
its rows stay reachable rather than sliding off.

The zero value is unchanged in meaning — no legend — and `Enabled: false`
still reports no rows at all, which is the difference between the aura
machinery being off and its legend being someone else's.

External mode is what makes the legend survive a render into a canvas the
view does not own: a row's sense region emitted from a host's overlay slot
sits under the host's own area region and could never be clicked. That is the
limitation [ADR-0228](./0228-hosted-canvas-rendering-and-graph-widget-sharing.md)
§SD4 first recorded as a cost of hosting, and it is now a mode rather than a
cost.

### 2026-09-12 — O4 is taken up

The QOC deferred O4 — whether this widget and `layeredgraph/view` share a
painter core — to "once graphview has stabilised".
[ADR-0228](./0228-hosted-canvas-rendering-and-graph-widget-sharing.md) takes
it, and splits it: the two widgets do not merge, for the reasons the QOC gave
and which still hold; what they duplicate — the camera of §SD4, and the
canvas-ownership seam a widget needs to paint and pick inside another's canvas
— is shared instead. A shared painter core stays deferred, with the trigger
named. The same record answers the geo-mode composition the §SD12 gap analysis
and its follow-on both deferred, and it reports that the "injectable camera"
both reached for is not needed: portolan's transform already factors into this
widget's camera, measured.

## References

- [ADR-0069](./0069-imzero2-layeredgraph-widget.md) — the first graph widget
  to route around `egui_graphs`.
- [ADR-0140](./0140-imzero2-hover-scoped-wheel-capture.md) — the wheel
  register the camera reads.
- [ADR-0149](./0149-implot-core-port-painter-lane.md) — the painter-lane port
  this follows.
- [ADR-0227](./0227-play-graphview-panel.md) — the widget's first consumer in
  this tree: `play`'s Graphview tab.
- `doc/adr-background-work/snarl-port-analysis.md` — the substrate check.
- `doc/adr-background-work/netchart-aura-analysis.md` — the black-box
  measurements SD11 re-derives.
- ZoomCharts NetChart API reference,
  `https://zoomcharts.com/developers/en/net-chart/api-reference/api.html` —
  the instance API the SD12 gap analysis was read against.
- Fruchterman & Reingold, *Graph Drawing by Force-directed Placement*,
  Software: Practice and Experience 21(11), 1991.
- Barnes & Hut, *A hierarchical O(N log N) force-calculation algorithm*,
  Nature 324, 1986.
- Gove, *A Random Sampling O(n) Force-calculation Algorithm for Graph
  Layouts*, Computer Graphics Forum 38(3), 2019 — the alternative weighed in
  SD6.
- Hachul & Jünger, *Drawing Large Graphs with a Potential-Field-Based
  Multilevel Algorithm* (FM³), Graph Drawing 2004.
