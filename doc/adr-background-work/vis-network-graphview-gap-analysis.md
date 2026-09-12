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
> and the public vis-network documentation (the network index page and its
> nodes, edges, layout, physics, interaction, manipulation, groups and
> configure module pages), and **re-baselined 2026-09-12** against the package
> as it stands: ADR-0224 §SD12 and §SD13 and
> [ADR-0225](../adr/0225-graphview-navigation-layer-and-radial-layout.md)
> landed between the two dates and closed a dozen rows below, each of which
> now names the decision that closed it. Nothing here is a decision; it is the
> inventory a later ADR update or SD would pick from. Provenance: the library's
> documentation pages only — no library code was read.

# vis-network → graphview: feature gap analysis

## 1 Question and scope

Which capabilities of vis-network's public surface does graphview lack,
once the surface is stripped of what is web-specific or visual polish?
This is the second analysis in the series the
[NetChart analysis](./netchart-graphview-gap-analysis.md) opened; its
§2–§7 rows are treated as known and are not repeated here unless
vis-network's shape of the same feature adds information, in which case
the row says so. The comparison is against the widget's **contract** —
what a Go caller can declare, read back or trigger.

What vis-network adds over NetChart, and what the tables below dwell on:
a **clustering** API that folds nodes into a cluster node and back; **four
physics solvers** with a shared parameter set and a hierarchical solver
that keeps levels while nodes settle; a hierarchical layout with two
level-assignment rules, compaction passes and a leaf/root alignment
switch; a node-shape set whose label-inside shapes size themselves to
their text; per-edge physics, smoothing families and inherited colour;
and an **event payload** that carries both DOM and canvas coordinates and
a z-ordered list of everything under the pointer.

Out of scope by the same framing as before: `title` tooltips, `showPopup`
/ `hidePopup`, `tooltipDelay`, `locale`, `autoResize`, `clickToUse`,
`setSize`, `destroy`, `redraw`, the `configure` module and `configChange`,
`navigationButtons` as a widget, `shadow`, `brokenImage`, image URLs and
`arrows.*.src`, animation and `easingFunction` on `fit` / `focus` /
`moveTo` and `animationFinished`, `controlNodeStyle`, the manipulation
toolbar, and `getOptionsFromConfigurator`. The manipulation **gestures**
stay in scope (§8).

Legend: **✓** covered, **≈** covered with a different shape or partially,
**caller** the caller does it under SD1, **gap** not available.

## 2 Data and topology

| vis-network | graphview | Status | Note |
|---|---|---|---|
| edge `id` (auto-assigned when omitted); `getClusteredEdges`, `getBaseEdges`, `updateEdge` all address edges by id | `EdgeSpec.Id`; hover, click and selection keyed by `EdgeRef{From, To, Id}` | ✓ | Closed by ADR-0224 §SD13, scoped to the ordered pair rather than globally unique — which is enough for the clustering helper below, since a meta-edge knows its endpoints when it declares itself. |
| node `hidden` (not drawn, still in physics); edge `hidden` | omit from the declaration | ≈ | Confirms NetChart §6 "invisible": a layout-only node or edge cannot be expressed. |
| node `physics: false` (drawn and draggable, ignored by the simulation) | `Pinned` | ≈ | A pinned node is held at a place; a physics-less node is merely unaffected. Close enough for most uses. |
| edge `physics: false` (drawn, no spring) | `EdgeSpec.Strength` scales the pull, but 0 means *default*, not none | **gap** | Narrowed rather than closed by ADR-0224 §SD13: the multiplier arrived and zero is its unset sentinel, so "no spring" still has no spelling. A flag, or a documented epsilon. The force-layout twin of NetChart §3 `definesLayout`: "reference" edges that should not pull. |
| node `mass` | uniform mass | **gap** | Small: a per-node weight in the repulsion term and in centre gravity. Hub emphasis and cluster-node weight both want it. |
| node `value` + `scaling` (min, max, `customScalingFunction`); edge `value` + `scaling` | caller maps a value to `Radius` / `Width` | caller | |
| node `x`, `y` (initial, then simulated) | `SetNodePosition` after the first `Render` | ≈ | Confirms NetChart §2 initial position. |
| node `fixed: {x, y}` per axis | `Pinned` fixes both; `NodeSpec.Pull` constrains one axis softly | ≈ | ADR-0224 §SD16 gives the soft form — a spring on X with Y free is the lane or level in practice. An *exact* per-axis pin still needs the force step to fix one axis, where `fixed` is per node. |
| `group` string + `groups` module, `useDefaultGroups` colour cycle | caller fills `Color` per frame | caller | The aura cycle in `AuraParams.Styles` is the analogous widget-side cycle; nodes have none, deliberately. |
| `getPositions()`, `storePositions()` | `Positions()` over every placed node; `SetNodePosition` writes | ✓ | Closed by ADR-0224 §SD12. |
| `moveNode` | `SetNodePosition` | ✓ | |
| `getBoundingBox` (label bounding box) | `NodePosition` + radius | ≈ | Label extent is not readable; needed to place caller-drawn adornments beside a label. Small once a paint hook exists (§6). |
| `getConnectedNodes(id, direction)`, `getConnectedEdges` | `nav.Neighbours(id, dir)`, `nav.Degree`; no edge-list form | ✓ / ≈ | The node half closed with ADR-0225's 2026-09-12 update, over the adjacency the helper already built. `getConnectedEdges` has no counterpart: the caller has the edges it declared. |
| `getNodeAt({x, y})`, `getEdgeAt({x, y})` | `CanvasToWorld`, no pick entry point | **gap** | Small: expose the SD3 pick as a method. Drop targets and caller-side context menus want it. |
| `DOMtoCanvas` / `canvasToDOM` | `CanvasToWorld`; `NodeCanvasPosition` for nodes only | ≈ | Small: a general `WorldToCanvas`. |
| `setData`, `setOptions` | `Render(nodes, edges)`, `Options` read every frame | ✓ | |

## 3 Clustering

vis-network's clustering (`cluster`, `clusterByConnection`,
`clusterByHubsize`, `clusterOutliers`, `openCluster`) is a **data
transformation** inside the library: a join condition selects nodes, a
cluster node replaces them, the edges crossing the boundary are re-created
as clustered edges with new ids, and `openCluster` reverses it with a
`releaseFunction` that places the released nodes from the cluster's
position. Under SD1 all of that is the caller's: a Go helper package
(membership rule → collapsed node and edge slices, plus the id mapping
`findNode` / `getBaseEdges` provide) is the natural home, and it is not a
widget concern. That helper now exists in part:
[ADR-0225](../adr/0225-graphview-navigation-layer-and-radial-layout.md)'s `nav`
owns a universe and derives the declaration from a small state, which is the
shape this paragraph describes — but it expands and hides, it does not group.
Closed grouping as a stage in it is the first item of the Cytoscape/Ogma
reading's work order. The rows below are what the widget would still need.

| vis-network | graphview | Status | Note |
|---|---|---|---|
| `cluster({joinCondition, processProperties, clusterNodeProperties, clusterEdgeProperties})` | caller builds the collapsed slices | caller | Helper package. `allowSingleNodeCluster` and hub-size selection (mean + 2σ by default) are helper policy. |
| `clusterByConnection`, `clusterByHubsize`, `clusterOutliers` | caller | caller | |
| `isCluster`, `getNodesInCluster`, `findNode` (path through nested clusters), `getBaseEdges`, `getClusteredEdges` | caller | caller | Needs edge ids (§2) to be expressible at all. |
| cluster node drawn as "stands for N nodes" (`processProperties` typically sets a label, size or `mass` from the children) | `Label`, `Radius`, `Donut` | ≈ | A count badge or a stacked-disc marker is the missing affordance; it is the same items slot as NetChart §6 and the "has hidden neighbours" marker of NetChart §2. |
| cluster node placed at the children's position; `openCluster` `releaseFunction(clusterPosition, containedNodesPositions)` places released nodes | `NodePosition` to read, `SetNodePosition` after the first `Render` to place | ≈ | Works, with the one-frame gap of NetChart §2. A "declare with initial position" field on `NodeSpec` would close it and is the one small widget change clustering needs. |
| `updateClusteredNode`, `updateEdge` | re-declare next frame | caller | |
| `layout.improvedLayout` + `clusterThreshold`: above the threshold the library clusters automatically, lays out the coarse graph, then opens the clusters | hashed-random initial placement, then FR | **gap** | Medium–large. A coarse-then-refine warm-up is a layout accelerator, not a data feature; it belongs beside the SD6 deferral on very large graphs. |

## 4 Layout and physics

The `physics` module is one parameter vocabulary — `gravitationalConstant`,
`centralGravity`, `springLength`, `springConstant`, `damping`,
`avoidOverlap`, `theta` — applied by one of four solvers. graphview's
`ForceParams` covers the vocabulary with one solver.

| vis-network | graphview | Status | Note |
|---|---|---|---|
| `solver: barnesHut` (quadratic repulsion, quadtree) | `LayoutForceDirected[CG]`, Barnes–Hut above a threshold | ✓ | |
| `gravitationalConstant`, `centralGravity`, `springLength`, `springConstant`, `damping`, `timestep`, `maxVelocity`, `minVelocity`, `theta` | `CRepulse`, `CenterGravity`, `KScale`, `CAttract`, `Damping`, `Dt`, `MaxStep`, `Epsilon`, `Theta` | ✓ | Different parametrisation (FR's `k` rather than a spring rest length), same knobs. |
| `solver: forceAtlas2Based` (linear repulsion, distance-independent gravity, degree-weighted mass) | — | **gap** | Medium. ForceAtlas2 is the layout users of Gephi and Obsidian expect for social and citation graphs; its degree-weighted mass is what spreads hubs. Needs `mass` (§2) or a degree term. |
| `solver: repulsion` (linear field to 0 at `2 × nodeDistance`) | — | ≈ | A cheaper, bounded repulsion. Low value on its own; `nodeDistance` is the radius-aware knob of NetChart §3. |
| `solver: hierarchicalRepulsion` (levels fixed, nodes settle on the free axis, forces normalised) | `LayoutHierarchical` is static | **gap** | Medium. A hierarchy whose siblings settle by force is the common "tree that breathes" look; it needs per-axis pins (§2) plus a repulsion that ignores the level axis. |
| `avoidOverlap` (0..1) | — | **gap** | Confirms NetChart §3 radius-aware spacing. |
| `adaptiveTimestep` during stabilisation | fixed `Dt` | gap | Small; belongs with `FastForward`. |
| `wind: {x, y}` or a function per node id | — | **gap** | Small: a constant or per-node bias force, and not the soft pin of ADR-0224 §SD16 — a spring's pull falls off as the node arrives, where wind has nowhere to arrive. It is how a force layout is nudged into a left-to-right flow without switching to the hierarchical walk. |
| `stabilization.enabled`, `iterations`, `fit`; `stabilize(n)` | fit latch (SD4), `FastForward(steps)` | ✓ | Hidden pre-first-paint stabilisation is the caller's `FastForward` before the first `Render`. |
| `stabilization.updateInterval` + `stabilizationProgress {iterations, total}`, `stabilizationIterationsDone`, `stabilized {iterations}`, `startStabilizing` | `Metrics.Steps`, `Metrics.Settled`, `IsSettled` polled per frame | ≈ | Progress is readable; a "settled since last frame" transition is a caller diff. Adequate. |
| `startSimulation`, `stopSimulation` | `ForceParams.Paused` | ✓ | |
| `stabilization.onlyDynamicEdges` | n/a | — | Support nodes of `smooth.type: dynamic` do not exist here. |
| `randomSeed`, `getSeed` | placement hashes the node id (SD2) | ≈ | Deterministic without a seed knob; a seed would let a caller pick among placements. Small. |
| `improvedLayout` (Kamada–Kawai initial placement) | hashed random | **gap** | Medium. A distance-based initial placement shortens settle time visibly; pairs with the §3 warm-up row. |
| `hierarchical.levelSeparation`, `nodeSpacing` | `RowDist`, `ColDist` | ✓ | |
| `hierarchical.treeSpacing` | — | ≈ | Confirms NetChart §3 `groupSpacing`. |
| `hierarchical.direction` UD / DU / LR / RL | `OrientationTopDown`, `OrientationLeftRight` | ≈ | Confirms NetChart §3: the two mirrored directions are missing. |
| `hierarchical.sortMethod: directed` | tree walk from roots | ✓ | |
| `hierarchical.sortMethod: hubsize` | — | **gap** | Medium: a second level-assignment rule (most-connected nodes at the top) for undirected or cyclic graphs, where the root walk has nothing to start from. |
| `hierarchical.shakeTowards` roots / leaves | levels by depth from a root | **gap** | Small–medium: aligning leaves on the bottom level is the longest-path levelling; two rules over the same walk. |
| `hierarchical.blockShifting`, `edgeMinimization` | none | **gap** | Medium: whitespace compaction and edge-length reduction over the placed tree. `layeredgraph` is the home for a full crossing minimisation; a sibling-shift pass is graphview-sized. |
| `hierarchical.parentCentralization` | `CenterParent` | ✓ | |
| per-node `level` | — | **gap** | Confirms NetChart §3 `categoryHierarchy`: a caller-assigned level overriding the walk. |
| `dragNodes`, `fixed`, `physics` per node | `NoDragging`, `Pinned` | ✓ | Per-node `draggable` remains the NetChart §3 gap. |
| `length` per edge | `EdgeSpec.Length` | ✓ | Closed by ADR-0224 §SD13, as a multiplier on the ideal length `k`. |

## 5 Navigation and camera

| vis-network | graphview | Status | Note |
|---|---|---|---|
| `fit({nodes, minZoomLevel, maxZoomLevel})` | `FitNodes(ids)`; `Options.ZoomMin` / `ZoomMax` | ✓ | Closed by ADR-0224 §SD12, as two surfaces rather than one call's arguments. |
| `focus(nodeId, {scale, offset, locked})`, `releaseNode` | `SetCamera` from `NodePosition` each frame | ≈ | The `locked` case — the camera follows a node while the layout moves it — is a per-frame `SetCamera` from the caller; a widget-side follow target would be a few lines and jitter-free. Small. |
| `moveTo({position, scale, offset})` | `SetCamera(zoom, panX, panY)` | ≈ | Small: a world-position-centred form of `SetCamera`, so a caller does not derive the pan from the canvas size. |
| `getScale`, `getViewPosition` | `Camera` | ✓ | |
| `dragView`, `zoomView`, `zoomSpeed` | `NoZoomAndPan`, `ZoomSpeed` | ≈ | Pan and zoom cannot be disabled separately. Small. |
| `keyboard` (arrow-key pan, zoom keys, `speed.x/y/zoom`) | — | **gap** | Small: keyboard pan and zoom when the canvas is hovered or focused; the substrate reports keys. `bindToWindow` is the host's. |
| `hideEdgesOnDrag`, `hideEdgesOnZoom`, `hideNodesOnDrag` | — | **gap** | Small: level-of-detail during a gesture. Confirms the NetChart §6 detail-LOD row from the interaction side. |
| `zoom` event `{direction, scale, pointer}` | diff `Camera()` | caller | |

## 6 Selection, picking and events

Every pointer event carries `pointer.DOM`, `pointer.canvas`, the original
event and an `items` list in z-order of everything under the pointer —
nodes, edges and their labels. graphview reports the one picked item.

| vis-network | graphview | Status | Note |
|---|---|---|---|
| `click` on empty canvas with `pointer.canvas` | `EventKindBackgroundClick` under `Options.BackgroundClicking`, carrying the world position | ✓ | Closed by ADR-0224 §SD12. |
| `click` `items` list (node and edge under the pointer, labels as separate items) | one picked id | ≈ | A label as a pick target is new: clicking a label beside a small node is how users hit it. Small: include the label rect in the node pick. |
| `doubleClick` on edge / background | `EventKindEdgeDoubleClick`, `EventKindBackgroundDoubleClick` | ✓ | Closed by ADR-0224 §SD12. |
| `oncontext` | `EventKindNodeSecondaryClick` / `EdgeSecondaryClick` / `BackgroundSecondaryClick` | ✓ | Closed by ADR-0224 §SD12 on all three targets. |
| `hold` | folded into the secondary-click kinds — the right button *or* a long touch | ✓ | Closed by ADR-0224 §SD12. One kind rather than two, since both mean "the other gesture" on their device. |
| `release` | — | — | Raw pointer-up; the host's. |
| `select`, `selectNode`, `selectEdge`, `deselectNode`, `deselectEdge` with `previousSelection` | `NodeSelect/Deselect`, `EdgeSelect/Deselect` | ✓ | Previous selection is a caller diff. |
| `selectable`, `multiselect` (Ctrl/Cmd-click or long-hold adds) | `NodeSelection[Multi]` option | ≈ | Confirms NetChart §5 modifier multi-select: accumulate is a mode, not a held key. Shift is now spent on the rectangle selection, so the accumulating key wants naming rather than assuming. |
| `selectConnectedEdges`, `hoverConnectedEdges` (a node's incident edges highlight with it) | node highlight only | **gap** | Small and new: highlight incident edges on hover or selection. Cheap over the SoA edge list; the most visible default users of any graph library expect. |
| `selectNodes(ids, highlightEdges)`, `selectEdges`, `setSelection({nodes, edges}, {unselectAll, highlightEdges})`, `unselectAll` (no events) | `SelectNode`, `SelectEdge`, `DeselectNode`, `DeselectEdge`, `ClearSelection` | ✓ | Closed by ADR-0224 §SD12, and silent — which is `unselectAll`'s documented no-events behaviour, generalised to every setter. The `highlightEdges` flag is the incident-edge row below, still open. |
| `getSelection`, `getSelectedNodes`, `getSelectedEdges` | `SelectedNodes`, `SelectedEdges` | ✓ | |
| `getNodeAt`, `getEdgeAt` | — | **gap** | See §2. |
| `dragStart`, `dragging`, `dragEnd` (nodes **or** view; `nodes` empty for a view drag) | `NodeDragStart/End`; live position via `NodePosition` | ≈ | A per-frame `dragging` event is readable through `NodePosition`; a view-drag start/end is a camera diff. |
| `hoverNode`, `blurNode` | `NodeHoverEnter/Leave` | ✓ | |
| `hoverEdge`, `blurEdge` | `HoveredEdge` readable, no event | ≈ | Confirms NetChart §5. |
| `beforeDrawing(ctx)`, `afterDrawing(ctx)` in canvas coordinates; `initRedraw` | none | **gap** | Medium and new: a paint hook under and over the graph in world space — grids, annotations, caller-drawn badges, a rubber line for §8. On the painter lane this is a callback receiving the canvas painter and the camera transform, not an opcode. |
| `resize {width, height, oldWidth, oldHeight}` | `RenderFill` / caller passes `w, h` | caller | |
| `startStabilizing`, `stabilized` … | see §4 | ≈ | |

## 7 Styling: the functional subset

| vis-network | graphview | Status | Note |
|---|---|---|---|
| `shape`: `ellipse`, `circle`, `database`, `box`, `text` (label inside; size follows the label), `diamond`, `dot`, `star`, `triangle`, `triangleDown`, `hexagon`, `square`, `icon`, `image`, `circularImage` (label below; `size`) | circle markers | **gap** | Confirms NetChart §6 shapes and image nodes. New here is the **label-inside** family: the node's extent is derived from its text under `widthConstraint` / `heightConstraint`, which turns a node into a labelled box or a text-only node. Medium; it changes picking, layout spacing and aura radius. |
| `shape: custom` + `ctxRenderer({ctx, id, x, y, state, style, label}) → {drawNode, drawExternalLabel, nodeDimensions}` | — | **gap** | Medium: a per-node paint callback with the node's screen geometry, drawn under arrows, with the caller reporting its extent for picking. The same hook as §6 `beforeDrawing`, scoped to a node. |
| `size`, `scaling.min/max`, `value`, `customScalingFunction` | `Radius`; caller maps | ✓ | |
| `scaling.label.enabled/min/max/maxVisible/drawThreshold` | fixed screen size (SD7) | ≈ | `drawThreshold` (no text below N px) confirms the NetChart §6 LOD row; `maxVisible` (a cap on the screen size when zoomed in) is the mirror case. |
| `opacity` per node; edge `color.opacity` | `NodeSpec.Opacity`, `EdgeSpec.Opacity` | ✓ | Closed by ADR-0224 §SD14, with `NoPick` beside it — Ogma's reading of the same feature, which this page's row did not separate. |
| `borderWidth`, `borderWidthSelected`, `color.border`, `color.background`, `color.highlight.*`, `color.hover.*` per node | `Color`; global `NodeStroke[W]`, `Highlight`, `Selected` | ≈ | Per-node stroke confirms NetChart §6. New: per-node **state colours** (a node's own selected or hovered colour) and a wider selected border. Small once per-node stroke exists. |
| `shapeProperties.borderDashes`, `borderRadius` | — | gap | Small; dashed borders mark provisional or external nodes. Confirms the per-item dash rows. |
| `shapeProperties.useImageSize`, `useBorderWithImage`, `imagePadding`, `image.selected` / `unselected` | — | gap | Rides on the image-node gap; `image.selected` is a state image, the image twin of state colours. |
| `icon` (`face`, `code`, `size`, `color`, `weight`) | — | **gap** | Small–medium and new: a glyph from an icon font as the node body. graphview paints text, so a glyph node is a label variant with a font handle; the design question is the font. |
| `label` with `\n`; `font.multi: 'markdown' / 'html'` (bold, italic, mono runs); `font.align` left / center | one line, one face | **gap** | Small: multi-line labels with alignment. Rich runs are polish. |
| `font.background`, `font.strokeWidth` / `strokeColor` (text halo) | — | **gap** | Small and functional: a halo or backing keeps a label legible over edges and auras. Applies to edge labels too. |
| `labelHighlightBold`, `chosen.node` / `chosen.label` / `chosen.edge` functions per state | caller fills the spec from `SelectedNodes` / `HoveredNode` | ≈ | One frame late by SD5; acceptable. |
| `level` | — | gap | See §4. |
| `widthConstraint` (label wrap at spaces; min / max width), `heightConstraint.valign` | — | gap | Small: word-wrap at a maximum label width. |
| edge `arrows.to/from/middle` `{enabled, scaleFactor, type: arrow / bar / circle / image}` | arrow head at `To` | **gap** | Confirms NetChart §6 from-decoration and kinds; new is the **middle** marker, which marks direction on long edges and on undirected edges drawn without heads. |
| `arrowStrikethrough: false` (line ends at the tip), `endPointOffset.from/to` | line drawn to the node edge | ≈ | Small; matters once decorations exist. |
| `dashes` pattern array | — | **gap** | Confirms NetChart §6 per-edge dash; a pattern rather than a flag. |
| `smooth.enabled`, `type` continuous / discrete / diagonalCross / straightCross / horizontal / vertical / curvedCW / curvedCCW / cubicBezier, `roundness`, `forceDirection` | straight; automatic bulge for parallel edges and loops | ≈ | Confirms NetChart §6 per-edge curvature and orthogonal routing; `horizontal` / `vertical` / `cubicBezier` are the hierarchical-layout connectors. A per-edge curvature sign (`curvedCW` / `CCW`) is the small cut. |
| `smooth.type: dynamic` (a physics support node per edge) | — | — | Bundling by simulation; not a fit for the SoA step. |
| `color.inherit` from / to / both | `EdgeSpec.Color` per frame | ≈ | The caller can copy the endpoint colour. A gradient (`both`) is polish. |
| `width`, `value` + `scaling` | `Width` | ✓ | |
| `hoverWidth`, `selectionWidth` (added width by state) | colour change only | gap | Small: width bump on hover / selection, as a `Style` pair. |
| `selfReference.size`, `angle`, `renderBehindTheNode` | `LoopSize` | ≈ | Confirms NetChart §6 self-link angle. |
| edge `label`, `font.align` horizontal / top / middle / bottom (along the edge, above or below it) | horizontal at the midpoint | ≈ | Confirms NetChart §6: not rotated along the edge. |
| edge `widthConstraint.maximum` (label wrap) | — | gap | See node `widthConstraint`. |
| `groups` module | caller | caller | See §2. |

## 8 Manipulation, as gestures

The manipulation module is a toolbar plus five modes with caller handlers
(`addNode`, `addEdge`, `editNode`, `editEdge`, `deleteNode`, `deleteEdge`),
each of which confirms or cancels through a callback. Under SD1 the data
change is the caller's and the toolbar is out of scope; what remains is
the **gesture** each mode needs from the widget.

| vis-network mode | Gesture | graphview | Status | Note |
|---|---|---|---|---|
| add node | click on empty canvas → `nodeData` with the canvas position | `EventKindBackgroundClick`, with the world position | ✓ | The gesture it reduces to landed in ADR-0224 §SD12; creating the node is the caller's under SD1. |
| add edge | drag from a node to a node; a rubber line follows the pointer; `edgeData {from, to}` on release | node drag moves the node | **gap** | Medium and new: a "connect" drag mode in which a node drag draws a line instead of moving the node and reports the node under the pointer at release. Confirms the NetChart §5 "dropped over node" hint. |
| edit edge | control nodes at both endpoints; drag one to another node to reconnect; `controlNodeDragging` / `controlNodeDragEnd` with `controlEdge {from, to}` | — | **gap** | Medium: endpoint handles on the selected edge. Shares the drop-target report with add edge. `editWithoutDrag` is the handler-only variant and is the caller's. |
| edit node, delete node / edge | caller acts on the selection | `SelectedNodes`, `SelectedEdges` | ✓ | |
| `enableEditMode`, `addNodeMode`, `addEdgeMode`, `editEdgeMode`, `deleteSelected` | none — these arm a mode, they are not themselves gestures | — | caller | A mode enum on `Options` would carry the two gesture modes above. |

## 9 Reading

Counting only rows still marked **gap** after the 2026-09-12 re-baseline, and
grouping by the size of the change rather than by the library's module
boundaries.

**Closed since this page was compiled.** Edge identity; programmatic selection;
background click and double-click with world coordinates; edge double-click;
secondary click and long-press; the bulk position read; per-edge length;
fit-to-subset and the zoom clamp; the add-node gesture's background click. All
of it is ADR-0224 §SD12 and §SD13. The navigation helper this page pointed at
twice is ADR-0225's `nav`.

**Small, self-contained, no design question.** Per-edge `physics` opt-out —
narrowed to a spelling problem, since `Strength` arrived but zero means
default; per-node `mass`; per-axis pins; pick-at-point (`getNodeAt` /
`getEdgeAt`) and `WorldToCanvas`; a declare-with-initial-position field on
`NodeSpec`; a wind / bias force; a placement seed; `shakeTowards` levelling;
keyboard pan and zoom; level of detail during pan and zoom gestures; separate
pan and zoom toggles; a camera follow target; incident-edge highlight on hover
and selection; the label rect in the node pick; multi-line labels with
alignment and a text halo or backing; word-wrap at a maximum label width;
per-node state colours and a wider selected stroke; per-node dashed border; a
middle edge marker and `arrowStrikethrough`; width bump by state; per-edge
curvature sign.

**Medium, one design decision each.** A ForceAtlas2-style solver
(degree-weighted mass, linear repulsion); the hierarchical-repulsion posture
(levels fixed, siblings settle); `hubsize` level assignment; block shifting and
edge-length compaction of the placed tree; a Kamada–Kawai or otherwise
distance-based initial placement; label-inside node shapes whose extent follows
the text; a per-node custom paint callback and the before/after paint hook it
generalises; icon-glyph nodes; the connect-drag gesture and edge endpoint
handles.

**Large, each its own SD or ADR.** The coarse-then-refine warm-up that
`improvedLayout` + `clusterThreshold` describe, which is the SD6
very-large-graph deferral seen from another library.

**Still confirmed from the NetChart inventory**, after removing what has since
landed — the cross-library recurrence this document was written to find:
per-node and per-edge opacity; per-edge exclusion from the layout; radius-aware
spacing (`avoidOverlap`); layout-only invisible items; hierarchical mirror
directions, inter-tree spacing and caller-assigned levels; node shapes and
image nodes; per-node stroke; from-decorations and decoration kinds; per-edge
dash; per-edge curvature and orthogonal connectors; rotated edge labels;
self-loop angle; label level of detail by on-screen size; the initial position
on declaration; a "stands for more" badge (the items slot); modifier-key
multi-select; and the drop-over-node hint.

**The dependencies have parted.** Of the two the NetChart reading named, **edge
identity** landed and every edge-level row got cleaner for it; **opacity** did
not, and it is still the one primitive most rows wait on — the Ogma reading has
since sharpened it into a pair, a fade and a non-pickable flag, because a faded
node that still answers the pointer is a bug. The two this library surfaced on
its own both stand: a **paint hook** in world space (custom node bodies, the
connect rubber line, caller adornments and the `getBoundingBox` readback all
want it) and a **gesture mode** on `Options` (connect, reconnect) that
reinterprets a node drag. Clustering itself still adds no widget requirement
beyond the initial-position field and the badge: it is a helper package, and
`nav` is now the precedent for how one is shaped.

As before, nothing here needs IDL, Rust or fetcher work; the paint hook is
a Go callback over the canvas painter the widget already holds.

## 10 References

- [ADR-0224](../adr/0224-graphview-go-graph-widget-painter-lane.md) —
  the widget and its design decisions.
- [ADR-0225](../adr/0225-graphview-navigation-layer-and-radial-layout.md) —
  the navigation helper this page's §3 and §5 point at, and the radial layout.
- [netchart-graphview-gap-analysis.md](./netchart-graphview-gap-analysis.md)
  — the first analysis in this series; its §2–§7 rows are the baseline
  this document confirms against, and its §8 lists the remaining
  candidates.
- [graph-viewer-gap-analysis-cytoscape-ogma.md](./graph-viewer-gap-analysis-cytoscape-ogma.md)
  — the later reading that takes clustering further, and whose §3.10 sharpens
  the opacity row above into a pair.
- vis-network public documentation, read 2026-09-11:
  `https://visjs.github.io/vis-network/docs/network/` (index — methods and
  events), and the module pages `nodes.html`, `edges.html`,
  `layout.html`, `physics.html`, `interaction.html`, `manipulation.html`,
  `groups.html`, `configure.html`. All pages were reachable. The edges
  page lists `arrow`, `bar`, `circle` and `image` as the arrow types; no
  other types appear on the published page.
