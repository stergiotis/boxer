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
> and the public README of vasturiano's force-graph (its API reference)
> plus the d3-force documentation pages for the forces it exposes through
> `d3Force`, and **re-baselined 2026-09-12** against the package as it stands.
> ADR-0224 §SD12 and §SD13 and
> [ADR-0225](../adr/0225-graphview-navigation-layer-and-radial-layout.md)
> landed between the two dates; rows they closed name them, and §9's baseline
> finding is restated on the current surface. Nothing here is a decision; it is
> the inventory a later ADR update or SD would pick from. Provenance: the two
> projects' documentation only — no library code was read.

# force-graph → graphview: baseline check

## 1 Question and scope

force-graph is the library closest in shape to graphview: a canvas, a
force simulation, a flat list of options, and a caller who owns the data.
So this third analysis in the series the
[NetChart analysis](./netchart-graphview-gap-analysis.md) opened reads
less as a gap hunt than as a **baseline check**: for every option, method
and event on force-graph's README, does graphview have an equivalent? What
a minimal widget has and graphview lacks is the finding; what both have is
recorded too, because the earlier analyses could not say which of their
rows a minimal widget also leaves out.

The [vis-network analysis](./vis-network-graphview-gap-analysis.md)
confirmed most NetChart rows a second time; rows below that overlap with
either say so and add only what force-graph's shape contributes. The
comparison is against the widget's **contract** — what a Go caller can
declare, read back or trigger.

Out of scope by the same framing as before: `nodeLabel` / `linkLabel` as
HTML content, `showPointerCursor`, the `ms` animation argument on
`centerAt`, `zoom` and `zoomToFit`, `nodeId` / `linkSource` / `linkTarget`
accessor names (graphview's specs are typed), `width` / `height` as DOM
getters, and the React and 3D siblings. Two items the brief named are not
on the 2D README and are recorded here so nobody looks for them again:
`dagMode` `zout` / `zin` belong to the 3D sibling, and
`enableZoomPanInteraction` has been split into `enableZoomInteraction` and
`enablePanInteraction`.

Legend: **✓** covered, **≈** covered with a different shape or partially,
**caller** the caller does it under SD1, **gap** not available.

## 2 Data input and container

| force-graph | graphview | Status | Note |
|---|---|---|---|
| `graphData({nodes, links})`, incremental updates by re-setting the data; node objects keep `x`, `y`, `vx`, `vy` across updates | `Render(nodes, edges, w, h)` every frame; positions retained per id (SD1) | ✓ | graphview is stricter: the declaration is the whole set each frame and the reconciliation is by id, not by object identity. |
| link objects are identity-referenced: events return the link object, `emitParticle(link)` takes one | `EdgeSpec.Id`; `Event` carries `From`, `To` and `Edge`, selection and hover key on `EdgeRef` | ✓ | Closed by ADR-0224 §SD13 — the edge-identity row of NetChart §2 and vis-network §2, which this page confirmed for the third time. |
| node `fx`, `fy` (fixed position, either axis) | `Pinned` + `PinX`, `PinY`; `PinNode` | ✓ | Per-axis fixing (`fx` alone) is the vis-network §2 per-axis-pin gap. |
| `backgroundColor` | `Style.Background` | ✓ | |
| `width`, `height` | `Render(…, w, h)`, `RenderFill` | ✓ | |

## 3 Node styling

| force-graph | graphview | Status | Note |
|---|---|---|---|
| `nodeVal` → circle area, `nodeRelSize` (area per value unit); node size scales with zoom | `NodeSpec.Radius`, `Style.NodeRadius` in world units, scaled by the camera | ✓ | Same posture — nodes are world-sized, edges screen-sized (§4). The value-to-area mapping is the caller's, as in NetChart §6. |
| `nodeLabel` (tooltip on hover) | `Label` painted above the hovered, selected or dragged node; `LabelsAlways` | ≈ | A painted label rather than a tooltip; functionally the same. |
| `nodeVisibility` false (not drawn, still simulated) | omit from the declaration | ≈ | Confirms the layout-only invisible node (NetChart §6, vis-network §2). |
| `nodeColor` | `NodeSpec.Color` | ✓ | |
| `nodeAutoColorBy` (a colour cycle over a category, for nodes without a colour) | caller fills `Color`; the aura cycle in `AuraParams.Styles` is the widget-side analogue for groups | caller | vis-network §2 records that nodes have no widget-side cycle deliberately. |
| `nodeCanvasObject(node, ctx, globalScale)` + `nodeCanvasObjectMode` `replace` / `before` / `after` | — | **gap** | Confirms vis-network §7 `ctxRenderer`. New here is the **mode**: the same callback can paint under (`before`) or over (`after`) the default marker instead of replacing it, which covers badges and rings without giving up the marker batch for ordinary nodes. On graphview it is an opt-in per-node callback over the canvas painter with the node's screen position, radius and zoom; a node that uses it leaves its batch. |
| `nodePointerAreaPaint(node, color, ctx, globalScale)` (the pick shape follows the custom paint) | pick by screen disc, floor `pickMinPx` | ≈ | Only meaningful beside a custom paint; the cheaper form is the vis-network §7 `nodeDimensions` readback, where the callback reports its extent. |

## 4 Link styling

| force-graph | graphview | Status | Note |
|---|---|---|---|
| `linkLabel` (tooltip on hover) | `EdgeSpec.Label` painted at the midpoint | ≈ | Always painted rather than on hover; a hover-only mode is a flag. |
| `linkVisibility` false (not drawn, force kept) | omit from the declaration | ≈ | Confirms the layout-only edge (vis-network §2 `hidden`). |
| `linkColor` | `EdgeSpec.Color` | ✓ | |
| `linkAutoColorBy` | caller | caller | |
| `linkLineDash([5, 15])` | — | **gap** | Confirms NetChart §6 / vis-network §7. The lane's dashed-line opcode exists; a pattern on `EdgeSpec` and a second call in the edge loop. |
| `linkWidth` in px, constant through zoom | `EdgeSpec.Width`, `Style.EdgeWidth` in screen pixels | ✓ | Exact match, including the constant-through-zoom semantics. |
| `linkCurvature` signed per link (0 straight, 1 half-length radius, negative flips); self-loop size proportional to the value | automatic bulge by parallel-edge order (`CurveSize`); loop radius `LoopSize` × radius, plus order | ≈ | Confirms NetChart §6 per-edge curvature and vis-network §7 curvature sign. Small: an `EdgeSpec` curvature that overrides the order-derived bulge. |
| `linkCanvasObject` + `linkCanvasObjectMode` | — | **gap** | The edge twin of the node callback, same modes. Together they are the per-item form of the paint hook (§5). |
| `linkDirectionalArrowLength` (0 hides; default 0) | arrow head always painted, `Style.TipSize` | ≈ | graphview draws arrows by default and cannot turn them off; force-graph defaults to none. An arrows-off switch is the Obsidian §2 row; a per-edge length is small. |
| `linkDirectionalArrowColor` | arrow takes the edge colour | ≈ | |
| `linkDirectionalArrowRelPos` 0..1 along the link (default 0.5) | head at the target disc | **gap** | Confirms vis-network §7 middle arrow. Small: a position parameter; the geometry already samples the curve. |
| `linkDirectionalParticles`, `…ParticleSpeed`, `…ParticleOffset`, `…ParticleWidth`, `…ParticleColor`, `…ParticleCanvasObject`, `emitParticle(link)` | — | **gap** | New in the series: animated markers travelling source → target along the edge, per edge, with a one-shot emit. It is the direction and flow cue that arrows are not, and fits the dataflow and SQL-flow consumers in this repository. Small–medium: a per-edge count and speed on `EdgeSpec`, a widget-owned phase advanced per frame, one marker batch over the sampled curve — the sampler `pickEdge` uses. The deterministic-screenshot mode would need a fixed phase. |

## 5 Render control and camera

| force-graph | graphview | Status | Note |
|---|---|---|---|
| `autoPauseRedraw` (stop redrawing when the engine halts) | `ForceParams.PauseOnSettle` holds a settled simulation; the host still owns frame pacing | ✓ | Closed by ADR-0224 §SD12: the widget-side freeze-on-settle this row called small. A graph at rest costs no step per frame, and a drag, a topology or parameter change, a setter or a fast-forward lifts the hold. |
| `pauseAnimation` / `resumeAnimation` | do not call `Render` | ✓ | |
| `minZoom` (0.01), `maxZoom` (1000) | `Options.ZoomMin`, `Options.ZoomMax`; zero takes 0.01 and 100 | ✓ | Closed by ADR-0224 §SD12. |
| `centerAt(x, y)` | `SetCamera(zoom, panX, panY)`; pan derived by the caller from the canvas size | ≈ | Confirms vis-network §5 `moveTo`: a world-centred form is small. |
| `zoom(k)` | `SetCamera` | ≈ | Zooming about the current centre needs the caller to recompute the pan; a `SetZoom(k)` that keeps the centre is a few lines. |
| `zoomToFit(ms, px, nodeFilter)` | `FitNodes(ids)`, `FitNow`, `FitPadding` (fraction), `FitToScreen` | ✓ / ≈ | Fit-to-subset closed by ADR-0224 §SD12. Padding stays a fraction of the canvas here and pixels there, which is the SD2 departure, not a gap. |
| `onRenderFramePre(ctx, globalScale)`, `onRenderFramePost` | — | **gap** | Confirms vis-network §6 `beforeDrawing` / `afterDrawing`. Both libraries pass the world transform; on graphview the hook receives the canvas painter and `Camera()`. One design point graphview adds: whether "pre" runs before or after the auras. |

## 6 Force engine and DAG mode

force-graph runs d3-force: a velocity Verlet integrator with an **alpha**
that cools from 1 toward `alphaTarget` by `alphaDecay` and stops the timer
at `alphaMin`; forces are named objects on the simulation. graphview runs
Fruchterman–Reingold as a displacement step scaled by `Dt · Damping` and
clamped by `MaxStep`, with no cooling schedule: it converges when the
forces balance and keeps stepping, by less than `Epsilon`, afterwards.
The two vocabularies map onto each other loosely; the rows say where the
mapping is exact.

| force-graph / d3-force | graphview | Status | Note |
|---|---|---|---|
| `d3Force('link')` — `distance` per link (default 30), `strength` per link (default `1 / min(degree(source), degree(target))`), `iterations` | `EdgeSpec.Length`, `EdgeSpec.Strength`, both multipliers; one pass | ✓ / ≈ | Closed by ADR-0224 §SD13. What did not carry over is d3's *default*: its strength is degree-normalised, so hub edges pull less, where graphview's multiplier defaults to 1 and leaves the normalisation to the caller. Worth knowing when porting tuned values. |
| `d3Force('charge')` — `strength` per node (default −30), `theta` (0.9), `distanceMin` (1), `distanceMax` (∞) | `CRepulse` global, `Theta` (default 0.9), `Epsilon` doubles as the minimum distance in the repulsion clamp | ≈ | Theta matches exactly. Per-node strength is the vis-network §2 `mass` row. `distanceMax` — a locality cutoff that also bounds the exact pair sum — is small. Worth a look on its own: the repulsion's minimum distance is the settle threshold `Epsilon` (1e-3), so two coincident nodes see a force `k²/1e-6` that only `MaxStep` tames; d3 keeps the two knobs apart. |
| `d3Force('center')` — `x`, `y`, `strength` (a translation of the mean, default 1) | `CenterGravity` pulls every node toward the canvas centre (`LayoutForceDirectedCG`) | ≈ | d3's centre force shifts the whole layout so its mean sits at the centre; graphview's is a per-node spring, closer to `forceX` + `forceY` at one point. Same job for a connected graph; for disconnected components graphview's also packs them, which is the NetChart §3 gravity row. |
| `forceCollide` — `radius` per node, `strength`, `iterations` | — | **gap** | Confirms NetChart §3 radius-aware spacing and vis-network §4 `avoidOverlap`. d3's shape — a separate pass after the forces, with its own iteration count — is the one to copy: it leaves the FR step alone. |
| `forceX` / `forceY` — target per node, `strength` per node (default 0.1) | `NodeSpec.Pull{X, Y, StrengthX, StrengthY}` | ✓ | Closed by ADR-0224 §SD16, and it turned out to be the term `CenterGravity` already applied to every node at the canvas centre, made per node and per axis. Two of the four rows it was said to carry did not follow: wind is a constant bias rather than a spring, and an exact per-axis pin needs a per-axis `fixed`. |
| `forceRadial` — `radius` per node, centre `x`, `y`, `strength` | `Pull` toward the nearest point on the ring, recomputed from `NodePosition` each frame | ≈ | A ring is a different target shape — a distance from a centre, not a point — so the soft pin approximates it one frame late rather than carrying it. `LayoutRadial` (ADR-0225 §SD6) is the exact, static answer. |
| `alpha`, `alphaMin` (force-graph sets 0), `alphaDecay` (0.0228), `alphaTarget` | none — no annealing | ≈ | graphview has no temperature; `Dt · Damping` is a constant scale. FR's classic cooling schedule was left out with the crate's parameters (SD2). Whether a cooling factor would settle faster is a measurement, not a decision, and belongs in a trial. |
| `velocityDecay` (0.4, friction) | `Damping` (0.3) | ≈ | Both are friction-like, but on different integrators (velocity vs displacement); the numbers do not transfer. |
| `d3ReheatSimulation` (alpha ← 1) | `ResetLayout` is the strong form; the `PauseOnSettle` hold lifts on a drag, a topology or parameter change, a setter or `FastForward` | ✓ | The freeze-on-settle of §5 landed with its reheat built in as a wake list rather than a method, so there is nothing to call. |
| `warmupTicks` (dry-run before the first render) | `FastForward(n)` before the first `Render` | ✓ | |
| `cooldownTicks` (∞), `cooldownTime` (15 000 ms) — then freeze | `ForceParams.PauseOnSettle`; `Metrics.Steps`, `IsSettled`, `Paused` | ≈ | The convergence-triggered freeze landed (§5). A *budget* — stop after N steps or N milliseconds whether or not it settled — did not, and force-graph's own default is the wall-clock form. Still the NetChart §3 time-budget row, still two lines of caller code. |
| `onEngineTick` | every `Render` is one tick; `Metrics.Steps`, `LastDisplacement` | ✓ | |
| `onEngineStop` | `IsSettled` transition, diffed by the caller | ≈ | vis-network §4 judged this adequate. |
| **`dagMode`** `td` / `bu` / `lr` / `rl` — depth per node from the DAG walk, then a `forceX` / `forceY` toward `depth × dagLevelDistance` while the other axis stays free | `LayoutHierarchical`: a static tree walk, `OrientationTopDown` / `OrientationLeftRight` | ≈ | Two different postures for the same picture. force-graph's is the vis-network §4 `hierarchicalRepulsion` posture — levels held, siblings settle by repulsion — and needs nothing beyond the soft pin above plus the depth walk graphview already has. `bu` / `rl` are the NetChart §3 mirror gap. |
| `dagMode` `radialout` / `radialin` | `LayoutRadial` places rings by hop distance, statically | ✓ / **gap** | The picture landed with ADR-0225 §SD6; the *posture* did not. `LayoutRadial` computes positions and holds them, where `forceRadial` is a per-node radius target the simulation settles against, so nodes still respond to repulsion. That is the soft pin below, in polar form. |
| `dagLevelDistance` (default derived from node count) | `HierParams.RowDist` (50) | ✓ | |
| `dagNodeFilter` (excluded nodes are unconstrained) | — | **gap** | Small on a force-DAG: a per-node "no level" flag leaves the node to the plain forces. Confirms the vis-network §4 caller-assigned `level` from the other side. |
| `onDagError(loopNodeIds)` (default: throw) | cycles start their own tree silently | ≈ | Small: report the nodes the root walk could not reach — a `Metrics` count and an iterator — so a caller can mark or break the cycle. |

## 7 Interaction

| force-graph | graphview | Status | Note |
|---|---|---|---|
| `onNodeClick(node, event)` | `EventKindNodeClick` under `NodeClicking` | ✓ | |
| `onNodeRightClick` | `EventKindNodeSecondaryClick` | ✓ | Closed by ADR-0224 §SD12 — the row four surfaces asked for. |
| `onNodeHover(node, prevNode)` | `NodeHoverEnter` / `NodeHoverLeave` pair, `HoveredNode` | ✓ | The leave event carries the previous node. |
| `onNodeDrag(node, translate)` per move | `NodeDragStart` then `NodePosition` per frame | ≈ | Confirms vis-network §6 `dragging`; the per-frame delta is a caller diff. |
| `onNodeDragEnd(node, translate)` | `NodeDragEnd` with the final world position | ✓ | The translation from the start is the difference against the `NodeDragStart` event. |
| dragging a node reheats the simulation so neighbours react | the simulation is always running; the dragged node is fixed for the frame | ✓ | |
| `onLinkClick(link, event)` | `EventKindEdgeClick` under `EdgeClicking` | ✓ | |
| `onLinkRightClick` | `EventKindEdgeSecondaryClick` | ✓ | As for nodes. |
| `onLinkHover(link, prevLink)` | `EventKindEdgeHoverEnter` / `EdgeHoverLeave`, beside `HoveredEdge` | ✓ | Closed by ADR-0224 §SD12. |
| `linkHoverPrecision` (default 4) | `pickEdge` tolerance `max(width, 4)` px | ≈ | Same default; not configurable. |
| `onBackgroundClick(event)`, `onBackgroundRightClick(event)` | `EventKindBackgroundClick`, `BackgroundDoubleClick`, `BackgroundSecondaryClick` under `Options.BackgroundClicking`, each carrying the world position | ✓ | Closed by ADR-0224 §SD12. Being the fourth surface to ask for it is most of why. |
| `onZoom({k, x, y})`, `onZoomEnd` | `Camera()` diffed per frame | caller | |
| `enablePointerInteraction` | `NoHover`, `NoDragging`, `NoZoomAndPan`, the clicking flags | ≈ | No single switch; the pick still runs when the pointer is inside. Minor. |
| `enableNodeDrag` | `NoDragging` | ✓ | |
| `enableZoomInteraction`, `enablePanInteraction` — separately, each also as a predicate over the pointer event | `NoZoomAndPan` | **gap** | Confirms vis-network §5 separate toggles. New is the **predicate form**: zoom only while a modifier is held, so a wheel over an embedded graph scrolls the page. The substrate reports modifiers, so this is an `Options` field. |

## 8 Utility

| force-graph | graphview | Status | Note |
|---|---|---|---|
| `getGraphBbox(nodeFilter)` | `Bounds()`, `BoundsOf(ids)` | ✓ | Closed by ADR-0224 §SD14, filter included: `FitNodes` is now expressed through `BoundsOf`, so the fit and the reader cannot drift. Auras and labels are outside the box, as they are outside force-graph's. |
| `screen2GraphCoords(x, y)` | `CanvasToWorld` | ✓ | |
| `graph2ScreenCoords(x, y)` | `NodeCanvasPosition` for nodes only | ≈ | Confirms vis-network §2 `WorldToCanvas`. |
| `simulation.find(x, y, radius)` (d3) | no pick entry point | **gap** | Confirms vis-network §2 `getNodeAt` / `getEdgeAt`. |
| `simulation.randomSource` (d3; fixed-seed LCG by default) | placement hashes the node id (SD2) | ≈ | Deterministic in both; a seed knob is the vis-network §4 row. |

## 9 Reading

**The baseline finding, restated on the current surface.** Against the minimal
widget, graphview matches on the data model (caller-owned nodes and links with
incremental updates, fixed positions, background colour), the size posture
(world-unit nodes, screen-pixel edges, both scaling nodes with zoom and holding
edge width constant), node and edge click, hover and right-click, background
click and right-click, edge identity, node drag with the simulation reacting,
pan and wheel zoom, a zoom clamp on options at the same lower bound, fit and
fit-to-subset, the edge-hover tolerance (4 px in both), warm-up before the
first paint, tick and stop readback, a freeze once the engine settles, per-edge
length and strength, and the Barnes–Hut `theta` default. After ADR-0224 §SD12
and §SD13 the two widgets differ in almost nothing a caller of the minimal one
would reach for.

**What a minimal widget still has and graphview lacks** — the remaining small
tier, an `Options` field, a spec field or a method each: per-edge dash; an
arrows-off switch, a per-edge arrow length and a head position along the edge;
separate pan and zoom toggles, with force-graph's predicate form so a wheel over
an embedded graph can scroll the page; pixel padding on the fit beside the
fractional one; an exported bounding box; `WorldToCanvas`; pick-at-point; a
world-centred `SetCamera` and a centre-preserving `SetZoom`; a *budgeted* freeze
(after N steps or N milliseconds, settled or not) beside the convergence one
that landed; a cycle report from the hierarchical walk; and a
`dagNodeFilter`-style per-node opt-out from the level assignment.

**What force-graph adds to the series.** The three findings that were not
confirmations stand unchanged, and none has landed:

- **Directional particles** — still the only flow cue in any library examined,
  and the one a dataflow consumer would use. Small–medium: a per-edge count and
  speed, a widget-owned phase advanced per frame, one marker batch over the
  curve the pick code already samples. A deterministic capture needs a fixed
  phase.
- **The paint-callback modes** (`before` / `after` beside `replace`), which make
  the per-node and per-edge callbacks useful without giving up the marker batch
  for the nodes that do not use them.
- **The soft pin**: d3's `forceX` / `forceY` / `forceRadial` as per-node targets
  with a strength. **Landed as ADR-0224 §SD16**, `NodeSpec.Pull`, and worth
  recording against the claim made here. It does carry the force-DAG posture —
  hold the level axis, let siblings settle on the other — given a depth the
  caller supplies, and it subsumes `CenterGravity`, which is the same term over
  every node at one point. It does *not* carry wind, a constant bias rather
  than a spring, nor an exact per-axis pin, which needs the step to fix one
  axis where `fixed` is per node; and it only approximates a ring target, which
  is a distance and not a point. Two rows of the four, then: the reading was
  right that the primitive was cheap and wrong about its reach.

**Two integrator notes**, for a trial rather than a decision, both unchanged:
graphview has no cooling schedule, where d3 anneals and halts — `PauseOnSettle`
freezes on convergence but does not cool on the way there; and graphview's
repulsion clamps its minimum distance at the settle `Epsilon`, coupling two
knobs d3 keeps apart, so two coincident nodes see a force only `MaxStep` tames.
Neither is a contract gap. Both were worth measuring before the per-edge force
fields landed, and those fields have since landed, which makes the measurement
more useful rather than less.

**Confirmed, and still open** — of the rows this page confirmed for a third
time, edge identity, per-edge length and strength, the zoom clamp, the
fit-to-subset and edge hover as an event have closed; radius-aware spacing
(`forceCollide`), layout-only invisible items, the per-edge curvature override,
the paint hook, the middle arrow marker, the per-frame drag event, the mirror
hierarchical directions and the caller-assigned or excluded level have not.
Nothing in what remains requires IDL, Rust or fetcher work: dashed lines, the
pointer-button flags and the modifiers are on the lane, and the particles are a
marker batch over geometry the pick code already samples.

## 10 References

- [ADR-0224](../adr/0224-graphview-go-graph-widget-painter-lane.md) —
  the widget and its design decisions; §SD12 and §SD13 closed the rows above
  that name them.
- [ADR-0225](../adr/0225-graphview-navigation-layer-and-radial-layout.md) —
  the radial layout, which is the static half of the `radialout` row.
- [graph-viewer-gap-analysis-cytoscape-ogma.md](./graph-viewer-gap-analysis-cytoscape-ogma.md)
  — the fourth and fifth surfaces in the series, read after this page.
- [netchart-graphview-gap-analysis.md](./netchart-graphview-gap-analysis.md)
  — the first analysis in this series and the model for this page; its
  §8 lists force-graph as the minimal-widget baseline check this document
  carries out.
- [vis-network-graphview-gap-analysis.md](./vis-network-graphview-gap-analysis.md)
  — the second analysis; the rows above that overlap with it are marked.
- [obsidian-graph-view-graphview-gap-analysis.md](./obsidian-graph-view-graphview-gap-analysis.md)
  — the consumer-expectation page; its arrows-off and right-click rows
  recur here.
- force-graph README, `https://github.com/vasturiano/force-graph`, read
  2026-09-11 — the sections Data input, Container layout, Node styling,
  Link styling, Render control, Force engine (d3-force) configuration,
  Interaction, Utility and Input JSON syntax. The README is the API
  reference; the 2D page lists `dagMode` values `td`, `bu`, `lr`, `rl`,
  `radialout`, `radialin` only.
- d3-force documentation, `https://d3js.org/d3-force` (the repository's
  README now points there), pages Force simulations, Link force, Many-body
  force, Collide force, Position forces and Center force, read 2026-09-11.
