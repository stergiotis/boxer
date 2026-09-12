---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-09-11 against the
> `widgets/graphview` package as it stood at
> [ADR-0224 §SD1–SD11](../adr/0224-graphview-go-graph-widget-painter-lane.md)
> and the vendor's public API and settings reference for NetChart, and
> **re-baselined 2026-09-12** against the package as it stands. This page was
> acted on: ADR-0224 §SD12 and §SD13 adopted its widget-local half, the ADR's
> 2026-09-12 update took the picking upgrade, and
> [ADR-0225](../adr/0225-graphview-navigation-layer-and-radial-layout.md) built
> the navigation helper and the radial layout §4 asked for. Rows that closed
> name the decision that closed them. Nothing here is a decision; it is the
> inventory a later ADR update or SD would pick from. Provenance: the vendor's
> documentation pages only — no library code was read.

# ZoomCharts NetChart → graphview: feature gap analysis

## 1 Question and scope

Which capabilities of NetChart's public surface does graphview lack, once
the surface is stripped of what is web-specific or visual polish? The
comparison is against the widget's **contract** — what a Go caller can
declare, read back or trigger — not against the vendor's defaults or its
rendering quality.

Out of scope by the user's framing, and not tabulated below: themes,
credits, title, toolbar, breadcrumb, fullscreen, image/PDF/CSV export,
localisation, HTML info popups, node and link context menus as UI (the
*trigger* is in scope, see §4), data sources (URL loading, caching,
prefetch, random generators), shadows, gradients, cursors, fade and zoom
animations, resize handles, the two-finger rotation gesture, subcharts,
paint scheduling (`paintNow`, `suspendPaint`, `updateSize`), the profiler
and DOM teardown, and the navigation history (`back`, `clearHistory`).

Two framing facts shape every row:

- **Go-authoritative topology (SD1).** The caller declares the node and
  edge set every frame. Anything NetChart does by *filtering* its own
  data cache — `nodeFilter`, `linkFilter`, `hideNode`, `showNode`,
  navigation modes that decide which nodes are visible — is the caller's
  by construction and is not a widget gap. What remains a gap is the
  **visual affordance** such a feature needs (a dimmed node, a "has more"
  marker) when the widget cannot draw it.
- **Painter-lane substrate.** The lane already offers dashed lines, image
  paint, secondary / middle / long / triple-click flags and keyboard
  modifiers. So most rows below are graphview-local Go work, not IDL or
  Rust work; the exception is noted where it applies.

Legend for the tables: **✓** covered, **≈** covered with a different
shape or partially, **caller** the caller does it under SD1, **gap** not
available.

## 2 Data and topology

| NetChart | graphview | Status | Note |
|---|---|---|---|
| Node `id` | `NodeSpec.Id` | ✓ | |
| Link `id` | `EdgeSpec.Id`; hover, click and selection keyed by `EdgeRef{From, To, Id}` | ✓ | Closed by ADR-0224 §SD13. The id need only be unique among the edges of one ordered pair; without one, a pair's edges share a ref and select together, as the binding did. |
| Node `x`, `y` from data; layout `"static"` | `Pinned` + `PinX/PinY`; `SetNodePosition` | ≈ | A permanent pin covers the static layout. A one-shot *initial* position (place here, then let the force layout run) needs `SetNodePosition` after the first `Render`, so the first frame places the node itself. |
| `hideNode` / `showNode` / `getHiddenNodes` | omit from the declaration | caller | |
| `hiddenLinks` hint style (a node has links to undeclared nodes) | `nav.HiddenNeighbours(id)` counts them; nothing draws them | ≈ | Half closed by ADR-0225 §SD3: the count is derived and reaches the caller at the `Style` hook, which can spend it on a radius, a colour or a label. The affordance is the widget half and is still missing; Ogma's badge slot is the concrete specification for it. |
| `filters.*`, `multilinkProcessor` | caller builds the slices | caller | |
| `exportData(exportCoordinates)` / `saveState` / `restoreState` | `Positions`, `SelectedNodes`, `SelectedEdges`, `Camera`; `IsPinned` per id | ≈ | ADR-0224 §SD12 added the bulk position read and the selection iterators. Pins are still one id at a time, and no single value round-trips a saved view. |
| `nodes()` / `links()` / `getNode` / `getLink` | caller owns the declaration | caller | |
| `getNodeDimensions` | `NodePosition`, `NodeCanvasPosition`, radius from the spec | ✓ | |

## 3 Layout

| NetChart | graphview | Status | Note |
|---|---|---|---|
| `mode: "dynamic"` | `LayoutForceDirected`, `LayoutForceDirectedCG` | ✓ | Fruchterman–Reingold; Barnes–Hut above a few hundred nodes. |
| `nodeSpacing` (desired distance, radius-aware) | `KScale` on `k = sqrt(area/n)` | ≈ | FR spaces by an ideal edge length that ignores node radius, so large or donut-bearing nodes overlap in dense graphs. A radius-aware repulsion or a collision pass is the missing piece. |
| per-link `length`, `strength`, `linkLengthExtent`, `linkStrengthExtent`, auto-scaling | `EdgeSpec.Length`, `EdgeSpec.Strength` | ✓ | Closed by ADR-0224 §SD13: both are multipliers, 1 (or 0) meaning the default, so the unweighted step is unchanged to the bit. The extents and their auto-scaling stay the caller's mapping. |
| `gravity` (`from` node/cluster, `to` graph/cluster/locked nodes, `strength`) | `CenterGravity` toward the canvas centre | ≈ | Only the `to: "graph"` case. Cluster-aware and locked-node-anchored gravity is what keeps disconnected components and pinned scaffolds coherent; medium work. |
| `layoutFreezeTimeout`, `freezeLayout`, `unfreezeLayout` | `Epsilon`, `IsSettled`, `Paused` | ✓ | Inactivity timing is the caller's. |
| `initialLayoutMaxTime`, `incrementalLayoutMaxTime` | `FastForward(steps)` | ≈ | Step count rather than a time budget. A time-budgeted variant is a few lines. |
| `globalLayoutOnChanges` | incremental placement near a neighbour; hierarchy re-runs on topology change | ≈ | No "global re-layout on every change" switch; `ResetLayout` is the manual form. |
| `aspectRatio` (fit network to viewport aspect) | canvas-area `k` | ≈ | Not a knob. |
| `mode: "hierarchy"` | `LayoutHierarchical` | ✓ | Tree walk over roots (no incoming edge); cycles start their own tree. |
| `rotation` (degrees), `scaleX`, `scaleY` (mirror) | `Orientation` top-down / left-right | ≈ | Two of the eight useful orientations. A rotation + mirror pair on `HierParams` is cheap. |
| `centerNodes` | `CenterParent` | ✓ | |
| `sortNodes` (false = keep data order) | always sorted by id | ≈ | Deterministic by id (SD2). A caller who orders siblings meaningfully cannot express it; an `Order` field or a "declaration order" switch would. |
| `sortForestBySize` | forests in root-id order | ≈ | |
| `rowSpacing`, `groupSpacing` (between siblings of different parents) | `RowDist`, `ColDist` | ≈ | No distinct inter-group gap. |
| per-link `definesLayout`, `direction` (U/D/L/R) | every edge is a tree edge | **gap** | Excluding an edge from the hierarchy (a "reference" edge) or forcing its direction is common in dependency views. |
| `mode: "radial"`, `twoRingRadialLayout` | `LayoutRadial`, `RadialParams{Centers, RingDist}` | ✓ | Closed by ADR-0225 §SD6: rings by undirected hop distance from a centre set, each subtree allotted an angular sector by its leaf count. The focus concept it depended on is `nav.FocusNodes`, which is what a caller passes as `Centers`. |
| `mode: "swimlane"` | — | **gap** | Nodes placed by two categorical axes into lanes with headers. A new static layout plus lane painting. |
| `mode: "categoryHierarchy"`, `onCycleDetect` | — | **gap** | Levels assigned by a node category rather than by tree depth. Could be a variant of the hierarchical walk given a per-node level. |
| `lockNode` / `unlockNode`, `userLock` | `PinNode` / `UnpinNode`, `Pinned`, `PinnedStroke` | ✓ | |
| `lockNodesOnMove` | `PinOnDrag` | ✓ | |
| `nodesMovable` | `NoDragging` | ✓ | |
| per-node `draggable` | — | **gap** | Small: a `NodeSpec` flag. |
| `resetLayout` | `ResetLayout` | ✓ | |
| `allowMoveNodesOffscreen`, `shouldKeepGraphOnScreen` | — | gap | Minor: pan / drag clamping. |

## 4 Navigation, focus and camera

NetChart's `navigation` modes (`showall`, `manual`, `focusnodes`) decide
**which** nodes are visible from a focus set, an expansion radius and a
relevance score, and fade low-relevance nodes. Under SD1 the visibility
decision belongs to the caller; this section called for a small Go helper
(focus set + radius → visible set + relevance per node), and
[ADR-0225](../adr/0225-graphview-navigation-layer-and-radial-layout.md) built
it as the `nav` package, where the three modes carry their own names —
`ModeShowAll`, `ModeManual`, `ModeFocus`. What the widget still lacks is the
affordance layer the fade needs:

| NetChart | graphview | Status | Note |
|---|---|---|---|
| `expandNode` / `collapseNode` / `closeNode`, `expandOnClick`, `defaultExpandMode` | `nav.Expand(id, depth, dir)`, `Collapse`, `Close`; `nav.Apply` wires the double-click | ✓ | Closed by ADR-0225 §SD2 and §SD4, in the helper. Collapse is the inverse of expand by construction, which the reference's own documentation does not promise. |
| `addFocusNode` / `removeFocusNode` / `clearFocus`, `numberOfFocusNodes`, `autoUnfocus` | `nav.Focus` / `Unfocus` / `ClearFocus` / `FocusNodes`; `Options.MaxFocusNodes`, `NoAutoUnfocus` | ✓ | Closed by ADR-0225 §SD2, keeping the reference's parameter names. |
| `focusAutoFadeout` (relevance < 1 drawn faded and smaller) | `nav.Relevance`, `NodeInfo.Relevance` at the `Style` hook; radius is settable, opacity is not | **gap** | Half closed by ADR-0225 §SD3: the number exists and reaches the caller per frame. The fade itself still needs per-node **opacity** on `NodeSpec` / `EdgeSpec` — and, per the Ogma reading, a non-pickable flag beside it, since a faded node that still answers the pointer is a bug. The single most reusable missing primitive in the series. |
| `nodeExpanded` / `nodeNotLoaded` state styles | `nav` publishes `Expanded(id)` and `NodeInfo.Stub`, `Focused` | ≈ | The states are derived and handed to the `Style` hook; nothing draws a marker for them. Same widget half as the hidden-links hint in §2. |
| `autoZoomOnFocus`, `scrollIntoView(nodes, margins)` | `FitNodes(ids)`, padded by `FitPadding` | ✓ | Closed by ADR-0224 §SD12; it also releases the pending fit so the framing sticks. |
| `zoom()` get/set, `home()` | `Camera`, `SetCamera`, `FitNow` | ✓ | |
| `zoomExtent` (manual zoom clamp) | `Options.ZoomMin`, `Options.ZoomMax` | ✓ | Closed by ADR-0224 §SD12; zero takes 0.01 and 100. |
| `autoZoomExtent`, `initialAutoZoom`, `autoZoomSize` | fit latch (SD4), `FitPadding`, `FitToScreen` | ✓ | |
| `doubleClickZoom` on empty space, `zoomInOnDoubleClick` | `EventKindBackgroundDoubleClick` carries the world position; the caller calls `SetCamera` | caller | The event it waited on landed in ADR-0224 §SD12, so this is now two lines of caller code. |
| wheel `sensitivity`, `wheel`, `fingers` | `ZoomSpeed`, `NoZoomAndPan` | ✓ | |
| `positionChange` event | diff `Camera()` between frames | caller | |

## 5 Selection, picking and events

| NetChart | graphview | Status | Note |
|---|---|---|---|
| `selection()` get | `SelectedNodes`, `SelectedEdges` | ✓ | |
| `selection(set)` programmatic set | `SelectNode`, `SelectEdge`, `DeselectNode`, `DeselectEdge`, `ClearSelection` | ✓ | Closed by ADR-0224 §SD12, and deliberately silent: a code-driven change reports no event, as the aura setters already established. |
| `nodesSelectable`, `linksSelectable`, multi | `NodeSelection[Multi]`, `EdgeSelection[Multi]` | ✓ | |
| multi-select by modifier key | `NodeSelectionMulti` / `EdgeSelectionMulti` as a mode | ≈ | Modifiers are readable from the substrate, but accumulate is a mode rather than a held key. Note that Shift now carries the rectangle selection below, so the accumulating key wants to be named rather than assumed. |
| `dragSelect` rectangle, `dragSelectClearsSelection`, `onLassoChange` | `Options.RectSelection`: a Shift-drag over node centres | ✓ / **gap** | The rectangle closed in ADR-0224 §SD12, replacing the selection unless `NodeSelectionMulti` adds — which is `dragSelectClearsSelection`. The lasso did not: it is a polygon paint and a point-in-polygon test over the same positions. |
| `tolerance` (click distance) | node: radius floored at `pickMinPx` (6 px); edge: `max(width, 4)` px | ≈ | The floor landed with the picking upgrade of ADR-0224's 2026-09-12 update, so a node zoomed small is still hittable. Neither number is an option. |
| `click`, `doubleClick` on node / link | `NodeClick`, `NodeDoubleClick`, `EdgeClick` | ✓ | No edge double-click. |
| click / double-click on **empty canvas**, with chart coordinates | `EventKindBackgroundClick` / `BackgroundDoubleClick` under `Options.BackgroundClicking`, carrying the world position in `X`, `Y` | ✓ | Closed by ADR-0224 §SD12. |
| `rightClick`, `longPress`, `tripleClick`, middle click | `EventKindNodeSecondaryClick` / `EdgeSecondaryClick` / `BackgroundSecondaryClick` — the right button or a long touch | ✓ / **gap** | Secondary click closed by ADR-0224 §SD12 on all three targets, with long press folded into it. Triple and middle click are still not read off the lane, which offers both flags. |
| `hoverChange` | `NodeHoverEnter/Leave`, `EdgeHoverEnter/Leave`, `HoveredNode`, `HoveredEdge` | ✓ | Edge hover became an event in ADR-0224 §SD12. |
| `selectionChange` | `NodeSelect/Deselect`, `EdgeSelect/Deselect` | ✓ | |
| pointer down / up / move / drag raw events | `NodeDragStart/End` with world position | ≈ | Raw pointer is the host's; drag-to-reparent or drop-on-node would need a "dropped over node" hint. |
| `dataUpdated`, `settingsChange`, `error`, `chartUpdate` | n/a (Go values, no async source) | — | |

## 6 Styling: the functional subset

| NetChart | graphview | Status | Note |
|---|---|---|---|
| `nodeStyleFunction`, `linkStyleFunction`, classes, rules | caller fills the spec fields every frame | ✓ | Strictly more direct. |
| per-node `radius`, `fillColor` | `NodeSpec.Radius`, `Color` | ✓ | |
| per-node `lineColor`, `lineWidth`, `lineDash` | global `NodeStroke`, `NodeStrokeW` | ≈ | A per-node stroke breaks the one-batch-per-colour paint; acceptable as an opt-in. |
| per-node / per-link `opacity` | — | **gap** | See §4; the single most reusable missing primitive. |
| `display` shapes (rectangle, rhombus, droplet, text-only, rounded text, custom) | circle markers only | **gap** | The marker batch is called with one shape code; the batch API may carry others. Rectangle and diamond would cover most "kind" encodings; text-only nodes are a separate paint path. |
| `image`, `imageCropping` | — | **gap** | The lane has an image paint opcode; graphview never calls it. Icon or avatar nodes are functional in people, host and package graphs. Needs a texture handle on `NodeSpec` and a clip. |
| `invisible` node / link | omit from the declaration, or keep for layout only | ≈ | An invisible node that still takes part in layout (a layout scaffold) cannot be expressed. Small. |
| `label`, `nodeLabel` style | `Label`, `LabelFontSize`, `LabelsAlways`, `Monospace` | ✓ | Fixed screen size by SD7 — a deliberate departure from `nodeLabelScaleBase`. |
| `nodeDetailMinSize`, `nodeDetailMinZoom`, `linkDetailMinSize` | none; labels follow hover / selection / `LabelsAlways` | **gap** | Level-of-detail culling by on-screen size: labels, donuts (already culled under 2 px), arrow heads. Cheap and it protects frame time on large graphs with `LabelsAlways`. |
| label `overlapStrategy` (skip / move) | — | gap | Nice to have; medium. |
| `items` layer (badges, secondary text, icons at anchored offsets around a node or along a link) | `Donut` | ≈ | Donut is one item kind. A general "items" slot (text or image at a normalised offset, optional background) is what turns a node into a card. Medium. |
| `aura` | `Auras`, `AuraParams`, `AuraStyle`, legend | ✓ | SD11. Legend `mode: "highlight"` (dim others rather than hide) is missing; small once opacity exists. |
| `toDecoration` (arrow) | arrow head at `To` | ✓ | |
| `fromDecoration`, decoration kinds (circle, open arrow, hollow arrow), bidirectional | — | **gap** | Cheap: a per-edge or per-style decoration pair. Undirected graphs currently draw an arrow they should not. |
| per-link `lineDash` | — | **gap** | The lane has a dashed-line opcode. Cheap: a per-edge dash flag or pattern. |
| per-link `radius` (width) | `EdgeSpec.Width` | ✓ | |
| per-link `arcAmount`, `arcOffset` | automatic bulge for parallel edges only | ≈ | A per-edge curvature override is small. |
| `multilinkSpacing`, `multilinkAutoCurve` | `CurveSize` | ✓ | |
| `selfLinkAngle`, `selfLinkShape`, height / width factors | `LoopSize` | ≈ | Radius only; the loop's angle is fixed. |
| `orthogonalMode` links | — | gap | Flow-chart routing; niche for this widget, and `layeredgraph` is the better home. |
| `toPieValue` / `toPieColor` | — | — | Treated as polish. |
| link `label`, `linkLabel` style | `EdgeSpec.Label`, `EdgeLabelFontSize` | ✓ | Painted horizontally at the midpoint; not rotated along the edge. |
| `scaleObjectsWithZoom: false` (constant screen-size nodes) | node radius is world units | ≈ | A screen-size radius mode is small but touches picking and auras. |
| state styles: hovered, selected, locked | `Highlight`, `Selected`, `PinnedStroke` | ✓ | Focused / expanded / not-loaded: see §4. |
| `nodeRadiusExtent` + `nodeAutoScaling` | caller maps a value to `Radius` | caller | |
| `legend` for node / link classes | aura legend only | ≈ | The shared `widgets/legend` package exists; graphview could accept caller items beside the aura rows, or the caller paints its own. |

## 7 Reading

**What this page bought, first.** Of the rows it marked **gap**, ADR-0224
§SD12 and §SD13 took eleven and ADR-0225 took the whole of §4's navigation
model: edge identity, programmatic selection, background click and
double-click with world coordinates, secondary click on all three targets,
edge double-click and edge hover as events, the rectangle selection,
fit-to-subset, the zoom clamp, the bulk position read, per-edge length and
strength, a pick floor in screen pixels, the radial layout, and the helper
package that owns a visible subset with focus, relevance, expansion and
hiding. That is most of what the page called small, and the reason to read
the tiers below rather than the ones this section first carried.

**What is left, by the size of the change.**

*Small, self-contained, no design question.* Per-node and per-edge opacity —
with the non-pickable flag the Ogma reading found travels with it;
from-decoration and decoration kinds, including an arrows-off switch;
per-edge dash; label and detail level-of-detail thresholds; per-node
`draggable`; a lasso beside the rectangle; triple and middle click; a
time-budgeted fast-forward; per-node stroke; a per-edge curvature override;
the self-loop angle.

*Medium, one design decision each.* Radius-aware spacing; node shapes and
image nodes; the items / badge slot, for which Ogma's four-corner
specification is the concrete form; hierarchical rotation, mirror, group
spacing, declaration-order siblings and per-edge `definesLayout`;
modifier-key multi-select, now that Shift carries the rectangle; the "has
hidden neighbours" marker, whose count `nav` already derives; a screen-size
radius mode.

*Large, each its own SD or ADR.* Swimlane and category-hierarchy layouts;
cluster- and pin-anchored gravity; label overlap avoidance.

**The two dependencies this page named have diverged.** Edge identity landed
(§SD13) and every edge-level row got cleaner for it, as predicted. Opacity
did not, and four later analyses voted for it again; it remains the first
primitive to add, and the Ogma reading sharpened it from one field into a
pair — a fade and a non-pickable flag — because the reference makes a
disabled node ignore the pointer and a faded node that still swallows clicks
is a bug. `nav`'s relevance (ADR-0225 §SD2) is now a number per visible node
with nothing to spend it on until that pair exists, which is the clearest
case for it in the tree.

Nothing in what remains requires IDL, Rust or fetcher work: dashed lines,
images, the extra pointer-button flags and keyboard modifiers are already on
the lane, which is the SD1/ADR-0149 posture paying off.

## 8 Other references worth the same treatment

NetChart is one point in the space. The candidates below were checked on
2026-09-11 for a public API reference deep enough to tabulate the same way;
each is listed for what it would add that §2–§7 does not already cover, so
a later analysis can skip the overlap. The five marked **done** were
written; the Weight column names the page.

| Library | Public API docs | What it adds over NetChart | Weight |
|---|---|---|---|
| **Cytoscape.js** | yes, one page | Compound (parent/child) nodes with padding and drag-and-drop; CSS-like selector stylesheet; edge curve families (haystack, unbundled bezier, segments, taxi); expand-collapse, automove and edge-handles extensions; the extension seam itself as a model for "caller-side helper packages". | **done** — [graph-viewer-gap-analysis-cytoscape-ogma.md](./graph-viewer-gap-analysis-cytoscape-ogma.md) |
| **vis-network** | yes | Clustering as a first-class API (`cluster`, `clusterByHubsize`, `clusterOutliers`, `openCluster`, join conditions); four physics solvers with `hierarchicalRepulsion`; hierarchical `sortMethod` hubsize/directed and `shakeTowards`; the widest node-shape and arrow-type set; stabilisation events with progress. | **done** — [vis-network-graphview-gap-analysis.md](./vis-network-graphview-gap-analysis.md) |
| **force-graph** (vasturiano) | yes, README | The closest shape to graphview — canvas + d3-force + a flat option list. DAG mode with `radialout`/`radialin`, `dagNodeFilter` and cycle error; per-link curvature and dash; directional particles; `warmupTicks`/`cooldownTicks`/`cooldownTime` and `autoPauseRedraw`; the background-click and right-click event set. A good check that graphview's small tier matches a minimal widget's baseline. | **done** — [force-graph-graphview-gap-analysis.md](./force-graph-graphview-gap-analysis.md) |
| **AntV G6 v5** | yes | Combos; behaviours as composable units (brush and lasso select, activate-relations, create-edge); hull plugin (a convex/concave-hull cousin of auras); edge bundling; fisheye; history; node/edge state model. | second |
| **Ogma** (Linkurious) | yes, no page login-gated | Transformations as a pipeline over the declared graph (node grouping, edge grouping, filters, neighbour merging, virtual properties); badges, halos and pulses; style rules with selectors; geo mode; embedded algorithms (shortest path, components, betweenness — no PageRank or Louvain on the public page). | **done** — [graph-viewer-gap-analysis-cytoscape-ogma.md](./graph-viewer-gap-analysis-cytoscape-ogma.md) |
| **Sigma.js v3 + graphology** | yes | WebGL posture for the SD6 direction; reducers (per-frame node/edge attribute override, the opacity/dim pattern); node and edge program variants; graphology's layout and metrics packages as the "helper package" precedent. | second |
| **yFiles for HTML** | yes | The layout ceiling: edge routers (orthogonal, bus, channel, organic), integrated labeling, grouping with folding, ports, incremental layout. Most of it belongs to `layeredgraph` rather than graphview; analyse the layout taxonomy only. | third, layout only |
| **Cosmograph / cosmos** | docs site; verify | GPU force simulation for 10⁵–10⁶ nodes: cluster forces, friction/decay parameters, rectangle and polygon select, fit-by-ids. Relevant to SD6's "very large graphs" deferral, not to the widget contract. | third |
| **ReGraph / KeyLines** (Cambridge Intelligence) | **login-gated** | Combos, donuts, halos, glyphs, time bar, ghost items, SNA measures — the product graphview's SD9 and SD11 already resemble. Cannot be analysed under this repository's public-sources-only provenance rule unless a public page is found. | blocked |
| **Neo4j NVL** | landing page only; verify | Layouts, box/lasso select, minimap, icon and HTML node styles. Modest surface; low priority. | third |
| **Gephi**, **Graphviz**, **ELK**, **WebCola** | yes | Not viewers, but layout catalogues: Noverlap, Yifan Hu, OpenOrd, ForceAtlas2 (Gephi); constraint-based alignment, separation and group layout (WebCola); layered and stress layouts (ELK). Mine for layout rows only. | as needed |
| **Obsidian graph view** | settings UI, no API | Not a library; a consumer's expectation: filter by query, colour groups by query (aura-like), arrows, text-fade threshold, node size by links, local graph by depth, four force sliders. Worth one page because this repository ingests Obsidian vaults. | **done** — [obsidian-graph-view-graphview-gap-analysis.md](./obsidian-graph-view-graphview-gap-analysis.md) |

The five first-wave candidates were written, and between them they settled
the two questions this section posed: grouping-as-transformation belongs
partly in `nav` and partly in a container the widget does not have, and a
minimal widget's baseline is met except for the rows §7 still lists. What
remains is second- and third-wave, and none of it is blocking: G6 for
combos and behaviours-as-units, Sigma.js for reducers and the WebGL posture,
the layout catalogues as the corresponding SD comes up. ReGraph stays out of
reach under the public-sources-only rule.

## 9 References

- [ADR-0224](../adr/0224-graphview-go-graph-widget-painter-lane.md) —
  the widget and its design decisions; §SD12 and §SD13 are this page's
  widget-local half, adopted.
- [ADR-0225](../adr/0225-graphview-navigation-layer-and-radial-layout.md) —
  the navigation helper and the radial layout §4 and §3 asked for.
- [vis-network-graphview-gap-analysis.md](./vis-network-graphview-gap-analysis.md),
  [force-graph-graphview-gap-analysis.md](./force-graph-graphview-gap-analysis.md),
  [obsidian-graph-view-graphview-gap-analysis.md](./obsidian-graph-view-graphview-gap-analysis.md)
  and [graph-viewer-gap-analysis-cytoscape-ogma.md](./graph-viewer-gap-analysis-cytoscape-ogma.md)
  — the rest of the series this page opened, in the order §8 suggested.
- [netchart-aura-analysis.md](./netchart-aura-analysis.md) — the earlier,
  measured analysis of the one NetChart feature already re-derived.
- ZoomCharts NetChart API reference and settings reference (public
  documentation pages, read 2026-09-11).
