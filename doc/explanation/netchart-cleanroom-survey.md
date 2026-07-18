---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Outside-in survey of a third-party
> commercial product, compiled 2026-07-17 from the vendor's public developer
> documentation and announcements, plus the *published sample listing* of one
> example page examined from a saved copy (see §7 Sources for the exact
> boundary). No product source code — shipped, minified, or otherwise — was
> read, executed, or decompiled; the library bundles present in the saved copy
> were catalogued by name and size only. Internal mechanisms are
> *reconstructions* from documented behavior and are marked as such. IP
> observations are engineering-grade context, not legal advice.

# A clean-room survey of ZoomCharts NetChart

NetChart is the network-graph widget in ZoomCharts' commercial JavaScript
chart library (siblings: TimeChart, PieChart, FacetChart, GeoChart). This
survey reconstructs its architecture, algorithms, and feature depth/breadth
from public documentation, as an outside reference point for graph-widget
design in this repository (cf.
[ADR-0119](../adr/0119-imzero2-pipelineview-widget.md) and
[`github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph`](../../public/thestack/imzero2/egui2/widgets/layeredgraph)).

Version coverage: the stable v1 line (reference pages document 1.21.17;
feature vintages are noted where the docs state them — auras 1.15.0, marquee
selection 1.16.0, configurable gravity 1.17.0) plus the v2.0.2 BETA, which the
vendor positions as a WebGL + WebWorkers re-architecture behind the same
settings surface.

## 1. Product shape

- A browser widget: one class per chart type (`NetChart`), constructed over a
  DOM container with a single nested settings object. No framework
  dependency; themes (`flat`, `dark`) are settings presets, and the root
  `theme` setting is itself just a `NetChartSettings` fragment.
- Config-driven to an unusual degree: nearly all behavior sits in one
  ~21-group settings tree (`advanced`, `area`, `auras`, `breadcrumb`,
  `callbacks`, `credits`, `data`, `events`, `filters`, `info`, `interaction`,
  `layout`, `legend`, `linkMenu`, `localization`, `navigation`, `nodeMenu`,
  `style`, `title`, `toolbar`, plus root-level `container`/`assetsUrlBase`/
  `parentChart`). Imperative API exists for what config cannot express:
  navigation verbs, data deltas, viewport, export, state save/restore.
- Licensing is commercial (free trial, paid per-product licenses); the
  library is closed-source. That is what motivates the clean-room posture of
  this document.

## 2. Architecture (functional reconstruction)

### 2.1 Three-tier class layering

The public type names expose a three-tier layering (tier names are the
vendor's; the assignment of responsibilities is inference from where settings
and types live):

```text
BaseApi / BaseSettings*          shared chart chassis: lifecycle, event bus,
                                 export, fullscreen, toolbar/menu/title/legend
                                 chrome, save/restoreState, resize
ItemsChart* types                the node-link scene layer (shared machinery
                                 for graph-shaped charts): ItemsChartNode,
                                 ItemsChartLink, node/link style settings,
                                 menus, info popups, auras layer
NetChart* types                  net-graph specifics: layout modes, navigation
                                 modes, gravity, net-specific settings/events
```

### 2.2 Retained scene graph over user data

Two object populations are visible in the API:

- **Data objects** — plain JSON the caller supplies (`nodes[]`, `links[]`
  with `from`/`to`), via URL, embedded `preloaded` object, or a
  `dataFunction` callback.
- **Scene objects** — runtime wrappers (`ItemsChartNode`, `ItemsChartLink`)
  holding: a `data` back-pointer; *flattened, computed* style fields (shape,
  colors, gradients, shadows, label(s), image, badge items, aura membership);
  state flags (`focused`, `selected`, `hovered`, `expanded`, `background`,
  `invisible`, `userLock`, `removed`); and layout state (`hierarchyLevel`,
  anchor coordinates). `removed` doubles as removal-animation state
  (fade-out over `fadeTime` 600 ms), i.e. the scene object outlives its datum.

Two graphs coexist: the loaded data graph and the *visible* subgraph
(`node.links` vs `node.dataLinks`; `getHiddenNodes()`). "Expanded" is defined
observationally — a node counts as expanded when all its links are visible.
Visibility is decided by the navigation policy (§3.5), not by data presence.
`invisible` short-circuits both drawing and hit-testing.

A noteworthy detail: nodes carry an `anchorMode` of *Scene* or *Display* —
individual nodes can be pinned in screen space while the rest live in world
space.

### 2.3 Data subsystem

- Multiple named data sources; deltas per source: `addData` / `removeData` /
  `replaceData(data, sourceId)`, `reloadData`, and `exportData(visibleOnly,
  exportCoordinates)` back out.
- **Incremental expansion protocol**: when navigation needs hidden neighbors,
  the chart batches node ids into requests (`requestMaxUnits`, default 2, per
  request; `numberOfParallelRequests` 3; `requestTimeout` 40 s) against `url`
  or `dataFunction`, with an element cache (`cacheSize` 10 000 nodes+links).
  The graph is thus explorable without ever holding the full dataset
  client-side.
- A built-in random-graph generator (grid / tree / uniform, with density
  knobs) for demos and testing.

### 2.4 Style engine

A precedence pipeline recomputes the flattened style of each scene object
(re-entry via `updateStyle()`, or automatically on state change):

```text
defaults → class cascade → data-driven auto-scaling → state overlays → style functions
```

- **Class cascade**: `nodeClasses`/`linkClasses` are named style fragments;
  data objects reference them (multi-class strings split on
  `classSplitChar`). CSS-like, but flat.
- **Auto-scaling**: numeric data values map to node radius / link width
  through `linear | logarithmic | square` distributions bounded by
  `nodeRadiusExtent` [10, 150] / `linkRadiusExtent` [0.6, 30]; link *length*
  and *strength* (spring parameters, §3.1) are separately auto-scalable.
- **State overlays**: `nodeHovered`, `nodeSelected`, `nodeFocused`,
  `nodeBackground`, `nodeExpanded`, `nodeLocked`, `nodeNotLoaded` (+ link
  equivalents) layer on top.
- **Style functions**: per-object callbacks (`nodeStyleFunction`,
  `linkStyleFunction`) and a batch `allObjectsStyleFunction` mutate computed
  style imperatively — the primary extension point. A declarative rule
  system (`nodeRules`/`linkRules`) existed and was deprecated in favor of
  these; the vendor evidently concluded imperative hooks age better than a
  rule DSL.
- Two merge/data details visible in the published auras sample: arbitrary
  extra fields on data objects survive ingestion and are reachable from
  style functions (`node.data.<anything>` — the sample routes a custom data
  field into `node.aura`, and builds `node.image` URLs from `node.id`); and
  setting a style property to `null` in an overlay *disables* the inherited
  value (the sample nulls `selection.fillColor`/`lineColor` to switch the
  default selection blob off) — null-means-remove merge semantics.

### 2.5 Rendering

- v1 renders to Canvas 2D; v2 is advertised as WebGL-rendered with layout in
  WebWorkers ("thousands upon thousands of nodes" — vendor claim, not
  independently verified).
- Paint order (reconstructed from feature semantics): aura field layer
  beneath everything (§3.6) → selection blob (a merged soft polygon behind
  all selected items) → links (with decorations) → nodes (shape/image/icon) →
  labels and badge items → DOM chrome (menus, popups, toolbar).
- Ten node shape primitives (`circle`, `text`, `rectText`,
  `verticalRectText`, `roundtext`, `droplet`, `rectangle`, `rhombus`,
  `diamond`) plus `customShape` with caller-supplied geometry; images with
  crop/fit/letterbox policies; per-node badge `items` and `extraLabels`.
- **Level-of-detail gates** rather than a declutter solver: details (labels,
  images, items) draw only above per-kind pixel/zoom floors
  (`nodeDetailMinSize` 5 px, `nodeDetailMinZoom` 0.2, `linkDetailMinSize`
  12 px, `linkDetailMinZoom` 0.5, `linkDecorationMinSize` 4 px), and object
  sizing can be either world-scaled or screen-constant
  (`scaleObjectsWithZoom`). No evidence of label collision resolution.
- Export renders to `png | jpg | pdf | csv | xlsx`, to file or data-URI.

### 2.6 Interaction, chrome, events

- Gestures: wheel/pinch zoom, drag pan, node drag (dragging sets a
  `userLock` pin), marquee `dragSelection` (v1.16+), lasso selection (v2),
  optional two-finger rotation. Viewport verbs: `zoom("auto" | "overview" |
  "in" | "out" | factor)`, `scrollIntoView(nodes, margins)`.
- Chrome is built in, not composed by the host app: toolbar (zoom slider,
  re-layout, freeze, undo, fullscreen), configurable node/link context menus
  with stock verbs (e.g. `btn:unfocus`), info popups, a legend fed by style
  classes and (opt-in) auras, breadcrumbs, title, localization table for
  every UI string.
- Flat event bus (`.on`/`.off`): `click`, `doubleClick`, `tripleClick`,
  `rightClick`, `hoverChange`, `pointerDown`, `selectionChange`,
  `positionChange`, `chartUpdate`, `dataUpdated`, `settingsChange`, `error`.
- Session semantics on the base chassis: `saveState()`/`restoreState()`,
  `back()`/`home()` history, `suspendPaint`/`resumePaint`,
  `freezeLayout`/`unfreezeLayout`, `updateSettings` (merge) vs
  `replaceSettings`, and a `parentChart` slot for chart-in-chart embedding.

## 3. Algorithms

Each subsection separates the **documented contract** (facts, with the
vendor's setting names and defaults) from the **reconstruction** (how one
would reproduce it; the vendor's actual implementation is unknown).

### 3.1 Dynamic layout (default): budgeted force simulation

**Contract.** Three forces are documented: node repulsion (strength derived
from node radius), link attraction as springs with per-link target `length`
and stiffness `strength` (both data-scalable, §2.4), and gravity. Simulation
advances in discrete steps, several per screen frame when the framerate
allows. Two scheduling phases exist: a *global layout* burst on initial
display or on significant change (budget `initialLayoutMaxTime` 2 000 ms) and
*incremental* relayout on small changes (`incrementalLayoutMaxTime` 300 ms;
`globalLayoutOnChanges` picks between them). The sim then *freezes*: after
`layoutFreezeMinTimeout` 1 500 ms without significant movement (significance
threshold `advanced.adaptiveFreezeTreshold`, sic) or `layoutFreezeTimeout`
10 000 ms without user interaction. Desired separation is `nodeSpacing` 16
(explicitly not guaranteed in crowded graphs); `aspectRatio` optionally biases
the layout toward the viewport's shape.

Gravity (since 1.17.0) is unusually structured: force applies `from` nodes or
whole *clusters* (disconnected components), `to` a target — the cluster
center, the whole graph center, the nearest *locked* node, or the set of
locked nodes — with centers computed either `weighted` (radius as mass) or
`geometric` (circumscribed circle). `strength` 0 disables; negative values
are documented as unstable. Locked nodes — pinned by drag (`userLock`) or by
`lockNode(id, x, y)` — both anchor the sim and can *be* gravity targets, so a
user can pin two hubs and have their neighborhoods condense around them.

**Reconstruction.** A spring-embedder of the Eades / Fruchterman–Reingold
family with velocity damping, per-component simulation, position constraints
for locked nodes, and component packing — the spring-length/stiffness pair is
plain Hooke; radius-derived repulsion is standard area-aware charge. None of
that is distinctive. What *is* distinctive, and worth copying, is the
scheduling envelope: wall-clock budgets per phase, an adaptive
movement-significance threshold, and inactivity freeze — i.e. the sim is
treated as an interruptible service with a latency contract, not as a
run-to-convergence batch. Repulsion pairing strategy is undocumented; at the
scales implied by v1, naive O(n²) with the time budget as backstop is
plausible, and a Barnes–Hut quadtree is the published upgrade path
(cf. d3-force's `alpha` cooling and ForceAtlas2's per-component gravity for
the closest open analogues).

### 3.2 Radial layout: BFS rings around a chosen center

**Contract.** Concentric circles from a center chosen by priority: most
recently focused node → any `navigation.initialNodes` member → any visible
node. Link direction is ignored; cycles are broken by demoting links from the
layout tree (demoted links get `link.background = true`, a queryable flag).
Ring-to-ring distance honors `nodeSpacing`; within a ring, neighbors may sit
closer. A `twoRingRadialLayout` option splits an overfull first ring into a
two-ring zig-zag. Disconnected components lay out as separate radial groups
placed adjacent. Documented as the natural companion of focusnodes
navigation (§3.5) — the walk's current focus stays central.

**Reconstruction.** BFS spanning tree from the center; ring index = BFS
depth; angular allocation per subtree. Matches the classic radial/concentric
tree drawings (Bachmaier–Brandes-style radial layering; Cytoscape.js
"concentric" is the ubiquitous open implementation).

### 3.3 Hierarchy layout: tree-ify, then tidy-tree

**Contract.** Top-down org-chart layout. Roots are *implicit*: link
direction determines layering ("links always flow 'away' from the root");
there is no explicit root designation. Multi-parent and cycle-closing links
are demoted to `background` — the layout is computed on a spanning forest.
`nodeSpacing` is strictly honored horizontally, `rowSpacing` vertically;
sibling groups separate by `groupSpacing` (default 2×`nodeSpacing`); trees of
a forest pack side by side. Post-transforms: whole-forest `rotation` in
degrees (90° yields left-right trees; components rotate as a unit, so the
packing direction rotates too), `scaleX`/`scaleY`, optional `centerNodes`
(center parents over children), `sortNodes`, `sortForestBySize`. A
`categoryHierarchy` variant assigns nodes to named category bands
(`hierarchyCategory` on the scene node) and reports cycle-creating links via
an `onCycleDetect` callback; a `swimlane` mode exists but the fetched pages
do not detail it.

**Reconstruction.** The important observation is what they *avoided*: this
is not Sugiyama. By demoting non-tree links and laying out a forest, the
whole layered-graph problem (crossing minimization, dummy nodes) collapses to
tidy-tree drawing — Reingold–Tilford / Buchheim–Walker in the literature —
plus rotation/scale as an affine afterstep. Cheap, stable, predictable; DAGs
render with their extra edges drawn "through" the tree rather than routed.

### 3.4 Static layout

Coordinates come from data; the engine does no positioning at all, and only
a dragged node moves. The degenerate mode that turns the widget into a pure
renderer + interaction shell (server-side or precomputed layouts).

### 3.5 Focusnodes navigation: an LRU focus set with BFS balls

This is the product's signature exploration model, and it is a *visibility*
policy layered on any layout mode.

**Contract.** The visible subgraph is the union of BFS balls around a small
focus set: at most `numberOfFocusNodes` 3 (min `minNumberOfFocusNodes` 1)
focus nodes, each expanded to `focusNodeExpansionRadius` 2 levels — except
the *least recently* focused node, which can get a smaller
`focusNodeTailExpansionRadius`; an initial-load override exists. Focusing one
more node than the cap evicts the least recently focused (`autoUnfocus`) —
the focus set is an LRU queue, so a walk through the graph drags a window of
context behind it. Expansion can be directional (`defaultExpandMode`:
`both | from | to`), staged level-by-level on a timer (`expandDelay`), and
click-driven (`expandOnClick`; `addFocusNode(id, relevance)` / menu verb
`btn:unfocus` on the API side). Collapsing preserves links to focus nodes.
Each node carries a `relevance` number — "a rough measure of how
'interesting' a node is" — and with `focusAutoFadeout`, nodes with
relevance < 1 draw smaller and faded, producing a visual gradient from focus
to periphery. The other two modes bracket this one: `manual` (only
`initialNodes` shown; every change via API verbs) and `showall` (the default;
the docs warn that on a large network showall "will bring down the chart!").

**Reconstruction.** Multi-source BFS with per-source radius, an LRU eviction
policy over sources, and a distance-decayed relevance scalar driving both
visibility and rendering emphasis. In the literature this is a
degree-of-interest exploration in the Furnas DOI tradition — closest
published analogue: van Ham & Perer, "Search, Show Context, Expand on
Demand" (IEEE InfoVis 2009). The separable insight: exploration is a policy
over the data graph that *feeds* layout, not a layout feature.

### 3.6 Auras: a thresholded influence field

The feature the linked example showcases: named soft-colored regions drawn
beneath node groups (set membership, communities).

**Contract.** Nodes join auras by id (`node.aura`: one id or several; per-aura
styles keyed by id, 10 default colors as fallback). The vendor documents the
mechanism as a grid: computation happens on cells of `cellSize` 3 display
pixels (smaller = more accurate, more time/memory); each member node emanates
a field over an area `intensity` 6 × its radius, with values near 1.0 at the
node center falling toward 0.0 at the periphery; a cell belongs to the aura
when its accumulated value exceeds `drawLimit` 0.8; and `overlap` false
assigns contested cells to exactly one aura (partition) while true lets
auras overlap.

The published sample listing of the "auras on dark theme" example adds usage
facts: membership is assigned *inside the style pipeline* — the sample's
`nodeStyleFunction` copies a custom data field onto `node.aura` per pass, so
group membership is recomputable like any other style output; per-aura style
carries `fillColor` plus `shadowColor`/`shadowBlur` (soft halo edges via
shadow compositing rather than a hard threshold boundary); auras can enroll
in the chart legend (`defaultStyle.showInLegend`); and the demo coarsens
`cellSize` to 10 px with `overlap` true — the documented
precision-for-performance trade, exercised.

Per-aura settings (`ItemsChartSettingsAuraStyle`) are visual only: fill,
line (`lineColor`/`lineWidth`/`lineDash`/`lineDashOffset`), shadow, legend
enrollment, and a `zIndex` draw order. The geometry parameters (`cellSize`,
`intensity`, `drawLimit`, `overlap`) exist only at layer level — one field
resolution and one threshold for all auras. The presence of line styling
implies the region boundary is strokeable — evidence that the implementation
produces an outline path (or strokes the mask edge), not merely filled cells.

**Reconstruction.** This is a metaball / influence map: accumulate radial
falloff kernels into a per-aura scalar grid, threshold, and rasterize the
winning cells as translucent fill (optionally smoothing the boundary —
marching squares would give a vector outline). Cost is
O(members × footprint area / cellSize²) per aura, linear in screen area —
which is exactly the accuracy/performance trade the `cellSize` knob exposes.
Among published set-visualization techniques (Bubble Sets, LineSets,
KelpFusion), this sits nearest Bubble Sets but drops edge-following "virtual
edges" for a pure node-kernel field — cheaper, blobbier, good enough when
groups are already spatially coherent (which the force layout tends to
produce).

### 3.7 Edge geometry

Parallel links between the same node pair fan out by auto-curvature
(`multilinkAutoCurve`, `multilinkSpacing` 10); self-links draw as
`quadratic | parabolic` loops with angle/height/width factors; arrowheads and
other decorations scale with link width above a minimum-size gate. Curvature
is also directly settable per link.

### 3.8 Undocumented internals (inference only)

Hit-testing (the `invisible` flag "skips drawing and hit testing" — so
per-object tests exist, spatial indexing unknown), label placement (no
collision solver in evidence, only LOD gates), and the v2 WebGL scene
organization are not documented and are not reconstructed here.

## 4. Depth/breadth map

| Area | Surface (evidence) | Assessment |
|---|---|---|
| Data ingestion | URL / preloaded / callback sources; per-source deltas; batched incremental expand-loading with cache; random generators | Deep, and shaped for *partial* graphs — the expand protocol is a first-class citizen |
| Layout | 6 modes; heavy tuning on dynamic (forces, budgets, freeze) and hierarchy (spacing, rotation, sorting) | Deep on the two workhorses; swimlane/categoryHierarchy thinner; no Sugiyama, orthogonal routing, or edge bundling |
| Exploration | manual / showall / focusnodes; LRU focus set, relevance fade, staged + directional expansion; breadcrumbs, history, save/restore | Deep and distinctive — the differentiating subsystem |
| Styling | Precedence pipeline; classes; 3 auto-scaling distributions; 10 shapes + custom; images/icons/badges/multi-labels; gradients/shadows; state overlays | Deep; extension via imperative hooks (a deprecated rule DSL is the scar of the alternative) |
| Auras | Grid field with documented cellSize/intensity/threshold/partition semantics | Narrow surface, distinctive feature |
| Interaction | Pan/zoom/pinch, node drag→pin, marquee + lasso (v2), optional rotation gesture, context menus, info popups | Broad, product-grade; touch-first claims prominent |
| Chrome | Toolbar, legend, breadcrumbs, title, localization, credits, resizer | Broad — the widget ships its own UI shell |
| Export | png/jpg/pdf/csv/xlsx, file or data-URI | Broad |
| Events/extensibility | Flat event bus; style functions; custom shapes; menu customization | Hook-rich but closed: no plugin API, no custom layout/renderer injection |
| Graph analytics | — | Absent: no centrality, clustering, community detection, pathfinding surfaced |
| Performance posture | LOD gates, paint suspension, budgeted layout (v1 Canvas); v2 WebGL + WebWorkers, vendor scale claims | Engineering visible in the contract; scale claims unverified |
| Accessibility | — | No evidence in the fetched documentation |

**Overall shape.** Breadth goes to product completeness (chrome, export,
touch, localization) rather than graph science (no analytics, no exotic
layouts). Depth concentrates in exactly the loop the product is sold for:
*load partially → explore by focus → keep the picture stable and readable
while it changes* — the expand protocol, the focusnodes policy, the budgeted
incremental layout, and the LOD/fade machinery are all facets of that one
loop.

## 5. What is distinctive vs commodity

Commodity (published, many open implementations): force-directed layout,
radial/tidy-tree layouts, canvas LOD, pan/zoom/drag interaction, class-based
styling.

Distinctive (worth studying, still reproducible from public knowledge):

1. **The focus-set exploration model** — LRU focus queue + per-focus BFS
   radius + relevance fade as one coherent policy (§3.5).
2. **Layout as an interruptible service** — wall-clock budgets, adaptive
   settle detection, inactivity freeze (§3.1).
3. **Cluster-aware gravity with locked-node targets** — user pins become
   layout attractors (§3.1).
4. **The aura field layer** — set visualization cheap enough to run per
   frame under a moving layout (§3.6).
5. **The incremental expand-loading protocol** — visibility policy, data
   fetching, and layout cooperating on a partial graph (§2.3).

## 6. Notes for an independent implementation

Observations, not a plan — if this repository grows a net-graph widget, the
design goes through its own ADR.

- The four subsystems are separable: *visibility policy* (focus sets) →
  *layout* (consumes the visible subgraph) → *field overlays* (auras, consume
  positions) → *style resolution* (orthogonal). NetChart's API is evidence
  the separation holds in a shipping product.
- The immediate-mode frame loop of
  [`github.com/stergiotis/boxer/public/thestack/imzero2/egui2`](../../public/thestack/imzero2/egui2)
  is a natural fit for the budgeted-simulation contract (§3.1): a per-frame
  step budget replaces their multi-step-per-frame scheduler; freeze
  thresholds map to skipping the step.
- Aura fields are a coarse offscreen raster + threshold; at `cellSize` ≈ 3 px
  this is bandwidth-light and needs no GPU support to start. The coarse grid
  can *be* the texture: GPU magnification filtering of a grid-resolution
  image supplies the soft boundary for free, and a smoothstep alpha ramp
  around the threshold reproduces the halo look without any blur pass.
- Everything in §3 maps to published algorithms (Fruchterman–Reingold /
  ForceAtlas2 / d3-force; Buchheim–Walker; Bachmaier–Brandes; Furnas DOI /
  van Ham–Perer; Bubble-Sets-family fields). An implementation from those
  sources plus this functional description owes nothing to the vendor's code.
- Clean-room hygiene for implementers: do not read the vendor's shipped
  JavaScript (including via devtools), do not copy documentation sample code
  or assets, and keep this document — behavior and setting *semantics*, not
  expression — as the only NetChart-derived input. Setting/API *names* here
  are functional facts reported for identification, not a naming scheme to
  clone.

## 7. Sources

All accessed 2026-07-17. Vendor documentation:
[settings root](https://zoomcharts.com/developers/en/net-chart/api-reference/settings.html),
[NetChart full reference](https://zoomcharts.com/developers/en/full-reference/NetChart.html),
[layout settings](https://zoomcharts.com/developers/en/net-chart/api-reference/settings/layout.html),
[advanced layout topic](https://zoomcharts.com/developers/en/net-chart/advanced-topics/layout.html),
[navigation settings](https://zoomcharts.com/developers/en/net-chart/api-reference/settings/navigation.html),
[auras settings](https://zoomcharts.com/developers/en/net-chart/api-reference/settings/auras.html),
[style settings](https://zoomcharts.com/developers/en/net-chart/api-reference/settings/style.html),
[data settings](https://zoomcharts.com/developers/en/net-chart/api-reference/settings/data.html),
[interaction settings](https://zoomcharts.com/developers/en/net-chart/api-reference/settings/interaction.html),
[gravity settings](https://zoomcharts.com/developers/en/full-reference/NetChartGravitySettings.html),
[ItemsChartNode reference](https://zoomcharts.com/developers/en/full-reference/ItemsChartNode.html),
[ItemsChartSettingsAuraStyle reference](https://zoomcharts.com/developers/en/full-reference/ItemsChartSettingsAuraStyle.html),
[components topic](https://zoomcharts.com/developers/en/net-chart/introductory-topics/components.html),
[the auras example that prompted this survey](https://zoomcharts.com/developers/v2/en/net-chart/examples/style/auras-on-dark-theme.html).
v2 claims (WebGL, WebWorkers, lasso, scale) are from the vendor's
[2022 feature announcement](https://zoomcharts.com/en/blog/zoomcharts-javascript-charts-best-new-features-of-2022)
and product pages, and are marked as vendor claims where cited.

Additionally, a browser-saved copy of the auras example page (user-supplied)
was examined under this boundary: **read** — the page's markup and its
server-rendered sample code listing (the code pane shown to every visitor,
identical to the page's JSFiddle export); **catalogued by filename/size only,
never opened** — the chart library bundle (`zoomcharts.js`, ~1.6 MB) and the
example iframe's compiled `frame`/`vendor` bundles; **skipped** — the
library's theme CSS and all docs-site machinery (editor, site framework,
analytics). Sample-derived facts above are described functionally; no sample
code, data, or asset was copied into this repository.
