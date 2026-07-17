---
type: adr
status: accepted
date: 2026-07-17
reviewed-by: "@spx"
reviewed-date: 2026-07-17
---

# ADR-0119: imzero2 pipelineview widget — schematic pipelines with classed side ports

## Context

Several boxer subsystems are pipelines in the shell sense — a dominant main
path (`a | b | c | d`) plus subordinate side channels: a stage's stderr, files
it writes, configuration it reads. Candidate consumers that already know their
own stage structure: command chains resolved through extbin
([ADR-0118](./0118-extbin-external-process-chokepoint.md)), the video codec
chain ([ADR-0088](./0088-imzero2-runtime-codec-pipeline-and-viewer-capabilities.md)),
the leeway six-stage pipeline (describe → IR → map → DDL → marshal → query),
and nanopass pass sequences ([ADR-0108](./0108-keelson-sql-pass-registry.md)).

The existing rendering options don't fit this idiom:

- `layeredgraph` ([ADR-0069](./0069-imzero2-layeredgraph-widget.md)) draws
  arbitrary layered DAGs well (play's system graph,
  [ADR-0097](./0097-play-reactive-query-graph.md), is its biggest consumer),
  but it treats every edge as an equal citizen. A pipeline schematic must not:
  the spine should be a straight line, and side channels should leave a stage
  at *semantically meaningful* positions (diagnostics below, config above,
  artifacts hanging as leaves). Its model has no ports, and its Graphviz
  engine will bend the spine unless fought with constraints the seam does not
  expose.
- The `graph` widget (egui_graphs, force-directed) is for live animation
  showcases; directional flow is the wrong model for it.

A survey of the layered-layout literature and of production pipeline UIs
(sources in [§References](#references)) shaped this proposal. The load-bearing
observation: the Sugiyama machinery exists to *recover* rank/order structure
from an arbitrary DAG. Production pipeline UIs whose input structure is
already known — Jenkins' pipeline-graph-view (its 2026 nested layout), GitLab's
stage columns — lay out by recursion over the stage tree instead, and reach
for a general engine (elkjs, dagre) only when the graph is genuinely
arbitrary (Airflow's asset/task graphs). Boxer's candidate consumers all know
their series/parallel structure.

Substrate facts that bound the design: the imzero2 painter already provides
rect/circle/ellipse (filled + stroke), line, arrow, cubic Bézier, filled
convex polygon, sized text, and per-region sense areas; `MeasureText` /
`MeasureTextSize` bindings exist for Go-side label sizing; `layeredgraph`
proved a widget of this kind needs no new IDL. Layout must be deterministic —
the screenshot tour ([ADR-0057](./0057-demo-registry-and-drivers.md)) captures
it.

## Design space (QOC)

**Question.** Which visualization idiom should the widget's core rendering
commit to?

**Options.**

- **O1** — spine schematic: grid recursion over a series/parallel stage tree;
  ports at class-fixed sides (the CI-pipeline idiom).
- **O2** — general layered DAG: reuse `layeredgraph` (Graphviz dot) with
  spine/port constraints bolted on.
- **O3** — ports-and-wires node canvas: a node-editor-style free canvas
  (à la egui-snarl, NiFi, Node-RED).
- **O4** — flow-quantity diagram: Sankey-style value-proportional bands as the
  primary form.

**Criteria.**

- **C1** — side-channel legibility: stderr/artifact/config land at fixed,
  semantically meaningful positions.
- **C2** — layout predictability and determinism: straight spine, stable
  output across runs (tour requirement).
- **C3** — implementation weight: new IDL, new dependencies, algorithmic
  machinery.
- **C4** — consumer fit: known series/parallel structure, tens of nodes,
  read-only display.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | −  | +  | −  |
| C2 | ++ | +  | −  | +  |
| C3 | +  | ++ | −− | +  |
| C4 | ++ | +  | −− | −  |

O2 scores C3 `++` because it already exists — but C1 kills it: ports would
need record shapes and compass attributes the layout seam doesn't expose, and
the spine still wanders. O3 is an *editor* interaction model (retained
Rust-side state, new IDL) for a display problem. O4 encodes volume, not
structure; volumes are secondary here.

## Decision

We will build **`pipelineview`**, a read-only imzero2 widget that renders a
data-processing pipeline schematically: the main path as a left-to-right
spine of stage boxes, side channels leaving each stage at sides fixed by
port class, endpoint artifacts (files, stores, streams) as distinctly-shaped
leaves. Layout is bespoke, deterministic, host-side Go; rendering goes
through the existing painter (no new IDL — confirmed by the implementation),
following the `layeredgraph` precedent.

Implemented (M1+M2, 2026-07-17): `widgets/pipelineview` (model + layout,
UI-free), `widgets/pipelineview/view` (the only UI-importing half), and the
gallery demo `egui2_hl_pipelineview_demo.go` (tour-captured). The SD texts
below were adopted to what the implementation settled.

### SD1 — Model: series/parallel stage tree with classed ports ✓

The primary input is the structure the consumers already have, not a flat
edge list. As implemented:

```go
type PortClass uint8    // Primary | Diagnostic | Config | Artifact (closed set)
type EndpointKind uint8 // File | Store | Stream | Null (glyph vocabulary)

type Stage struct {
    ID, Label string
    Ports     []Port // named SIDE ports only; primary west/east anchors are implicit
}
type Group struct { // Par composes children as stacked branches; the zero
    Par      bool   // value is a series segment
    Children []Element // Stage or Group
}
type Endpoint struct{ ID, Label, Sublabel string; Kind EndpointKind }
// Sublabel: optional detail line (a URL, a database, a path), drawn smaller
// under Label; the box grows to fit. Store cylinders add cap padding so the
// text block sits in the body, not under the ellipse lids.
type Ref struct{ Stage, Port, Endpoint string } // names one of stage/endpoint;
                                                // Port "" = the primary anchor
type Edge struct {
    From, To Ref
    Label    string
    Volume   float64 // reserved for the SD5 overlay; 0 = unknown
}
```

Port class → side is a closed mapping: `Primary` = east/west (the spine),
`Diagnostic` = south, `Config` = north, `Artifact` = south, east of the
diagnostics. Extending the class set is a Tier-2 ADR update, not an ad-hoc
widget option. Decisions the implementation settled:

- **Spine edges are implied by the tree** (exits of each series element →
  entries of the next); explicit edges cover side channels, axial endpoints
  (a `> file` sink east, a `< file` source west), skips and feedback. An
  explicit forward-adjacent primary edge *replaces* its implied twin, so a
  caller can label a pipe without drawing it twice.
- **Endpoints are homed by their first referencing edge** (model order):
  south for diagnostic/artifact sources, north for config targets,
  east/west for primary-anchor edges.
- **Validation is directional**: a named source port must be an output class
  (diagnostic, artifact), a named target port an input class (config);
  stages and endpoints share one id namespace (the sense-region key).
- **Stage-to-stage edges on named ports are rejected in v1** (an artifact
  feeding a later stage's config directly): model the file, route both edges
  through it. Deferred, not designed away — the error message says so.

### SD2 — Layout: grid recursion over the tree, not structure recovery ✓

Spine ranks become columns; a parallel group stacks its branches as rows;
side nodes hang off their stage's column at fixed offsets. Port sides and
orders are fixed by SD1 — in the vocabulary of the port-constraint
literature, we implement only the bottom rungs of the constraint ladder
(`FIXED_SIDE` + `FIXED_ORDER`) and none of the free-port optimization.

Determinism is an invariant, not an aspiration: model order is the universal
tiebreak, no randomness, no map-iteration dependence. (The port-constraint
engine this survey drew on retrofitted exactly this as its model-order
options; we get it by construction.)

What the implementation added to the plan:

- **The shelf rule.** A stage's north/south endpoints form a
  non-overlapping row (a *shelf*) centred on the stage, and the shelf's
  width participates in the **column** width. Both naive placements fail:
  centring each endpoint on its own pin overlaps neighbouring endpoints
  (pins are closer together than label boxes are wide), and letting a shelf
  spill into the inter-column gaps collides with the gap tracks that route
  vertical edge segments there. Column-owns-shelf removes both, at the cost
  of wider columns. Edges elbow from pin to shelf box when the two drift
  apart (straight drop into the near edge when aligned, entry via the
  facing side otherwise).
- **Label sizing** is an injectable `MeasureText` with a deterministic
  monospace-flavoured estimate as the default (0.60×size per rune,
  1.45×size line height). The v1 renderer keeps the estimator; a host
  wanting tighter boxes injects real measurements (`MeasureTextBind`) —
  the "uniform boxes vs one-frame settle" trade from the Consequences
  resolved into this seam instead.
- Layout runs as two small recursions (column span/assignment, vertical
  extents/line placement) plus flat passes — no virtual nodes anywhere.

### SD3 — Degradation path for flat inputs

A consumer with only a flat edge list gets: greedy cycle removal
(Eades–Lin–Smyth), longest-path layering, and per-column ordering computed
exactly — at pipeline scale (columns of ≤ ~8 items) exact beats heuristics
for free. Genuinely arbitrary DAGs are out of scope: the widget documents a
handoff to `layeredgraph`, and the two models stay mechanically convertible.

**v1 ships only the convertibility half**: `Pipeline.ToGraphModel()`
flattens stages, endpoints and (implied + explicit) edges into the
`layeredgraph` model. The flat-input *builder* (edge list → stage tree) is
deferred until a consumer without a tree exists — every consumer named in
the Context knows its structure, and descoping beats gating (house rule).
The algorithm choices above stand as the recorded plan for when it lands.

### SD4 — Edge routing: three tiers, closed-form first ✓ (tiers 1–2)

1. Spine edges: straight horizontal segments.
2. Side, skip (`a → c` past `b`), and feedback edges: closed-form rounded
   orthogonal paths (the "smoothstep" shape), with per-channel track
   assignment when several edges share an inter-column gap. Feedback edges
   are reversed for layout and drawn dashed against the flow.
3. Corridor-fitted Béziers (the dot look) — deferred until wanted; the
   painter already has the required primitives.

As implemented: fan-out/fan-in verticals ride per-gap tracks (offsets 0,
+1, −1, … × 10 pt around the gap centre, assigned in model order); skip
edges run through stacked lanes above the content, feedback through lanes
below (dashed via the dashed-line primitive — dashes keep sharp corners,
since a dash pattern cannot follow a Bézier); virtual gaps beyond the first
and last column let feedback exit the far end and re-enter column 0; solid
edges round interior corners with one quarter cubic Bézier each and end in
a filled-triangle head. Side-edge labels render beside the wire (schematic
net-label style) — centred-above collides in the tight pin-to-shelf band.
Accepted v1 cosmetics: a skip edge shares its source anchor with the spine
edge (collinear first run), and multiple arrowheads overlap at a shared
target anchor (fan-in), as in the surveyed CI UIs.

### SD5 — Overlays: status in v1, quantity and motion deferred ✓ (v1 scope)

v1 ships per-stage/per-edge color hooks (the `RenderOpts` override pattern
from `layeredgraph/view`: `NodeFill`/`NodeText`/`EdgeStroke`) so a host can
mark running/failed/selected; the demo's click-to-select uses exactly this
seam. The volume overlay (edge thickness and ribbons ∝ `Edge.Volume`, links
at a node sorted by far-end y) and dash-march animation for live flow are
specified by the sources below but deferred until a live consumer exists —
`Volume` already flows through `EdgeLayout` untouched. The view is
fit-to-canvas only (no pan/zoom): a spine schematic is expected to fit its
host; `layeredgraph`'s `ViewState` pattern is the ready-made recipe if that
expectation ever breaks.

### SD6 — Clean-room protocol ✓

The implementation is written from the papers and public reference
documentation named in [§References](#references) — never from project source
code. For EPL/LGPL/GPL projects (Graphviz, ELK, libavoid, OGDF) source is
off-limits outright; for permissively-licensed projects (dagre, d3-dag,
d3-sankey, ReactFlow, Jenkins plugins) reading source would be lawful, but
the bar is the same: method-level extraction, all geometry and code
re-derived. The protocol held for M1+M2: no external layout/routing source
was consulted; the shipped code needs nothing beyond the constraint-ladder
vocabulary, the lane/track idea, and the closed-form corner geometry.

### Milestones

- **M1 — model + layout core.** ✓ Pure Go, no UI imports
  (`widgets/pipelineview`); golden-file layout test
  (`testdata/shell_pipeline.golden`) plus determinism (two runs deep-equal)
  and invariant tests (spine straightness, semantic sides, shelf
  non-overlap, lanes outside content, track separation, validation
  rejects).
- **M2 — painter renderer + gallery demo.** ✓ `widgets/pipelineview/view` +
  `egui2_hl_pipelineview_demo.go`: the canned shell-style pipeline (stderr,
  config, written files, store sink, one skip edge, one feedback edge, one
  parallel group), tour-captured and visually verified.
- **M3 — first real consumer: the nanopass pass pipeline in play** ✓
  (picked at review 2026-07-17, over the earlier extbin/videopipeline
  candidates; implemented same day as the Passes dock tab,
  `apps/play/play_passes_tab.go`) — the structure behind a play query is
  already known in-process (passreg, ADR-0108), so the tab draws the
  registry's pre-execute catalog with no discovery step: passes on the
  spine in (Order, Name) apply order, the editor entering west
  (stream glyph), the executor east (store glyph, sublabelled with the
  client's live endpoint URL), a `NeedsFixedPoint`
  pass carrying a dashed self-feedback loop, late-bound factory
  descriptors recessed (they apply only where the client binding accepts
  them, ADR-0116 §SD6). Clicking a stage shows its catalog row
  (description, order, kind, properties, provenance). The canvas sizes to
  the pane via a width probe (a full-width separator then the seq-keyed
  `captureUiRect` snapshot — play's single-slot `CaptureAvailableSize`
  register is owned by the editor tab). The layout caches on
  a catalog fingerprint (which includes the executor URL, so switching
  endpoints relayouts); the drawing is the catalog, not a per-run trace —
  per-run outcomes (pass failed-and-skipped, factory declined) need an
  observed apply seam in passreg and are the tab's natural next slice.
  Live-verified via the scripted play capture (`BOXER_PLAY_FOCUS_PASSES`,
  a derived focus knob). Caveat for future captures: the SVG-export →
  cairosvg path drops the glyph after an `fi` pair (a capture-path trait,
  not a rendering bug — the pane avoids `fixpoint` for `fixed point` all
  the same).
- **M4 — overlays.** Volume thickness/ribbons and live animation, once M3's
  consumer emits volumes.

## Alternatives

- **Extend `layeredgraph` instead of a sibling widget.** Killed by the QOC
  C1 row: ports and spine-straightness would mean exposing record shapes,
  compass ports, `rank=same` and edge weights through the engine seam, then
  fighting dot's freedom — more machinery than the bespoke layout it would
  replace, and the WASM engine stays in the loop for graphs that don't need
  it. `layeredgraph` remains the right home for arbitrary DAGs.
- **Adopt a Rust node-graph crate** ([egui-snarl](https://github.com/zakarumych/egui-snarl),
  [egui_node_graph2](https://github.com/trevyn/egui_node_graph2),
  [egui-graph-edit](https://github.com/kamirr/egui-graph-edit)). These are
  *editors*: retained Rust-side graph state, their own interaction model, a
  new IDL surface — against the ADR-0069 house direction (Go-side model,
  painter rendering), for editing capability no consumer asked for.
- **Reimplement a full port-constrained layered engine** (the ELK approach).
  Its hard parts — free-port optimization, north/south dummy-node
  infrastructure, hierarchical crossing sweeps — exist to serve arbitrary
  diagrams. Our port sides and orders are fixed by semantics; the general
  machinery would be dead weight.
- **Sankey as the primary idiom.** Encodes where the bytes went, not what the
  pipeline is; adopted only as the SD5 overlay mechanics.
- **Metro map.** Multiple pipelines sharing stages is attractive but the
  layout is MIP-grade machinery (Nöllenburg–Wolff) for marginal value here.
- **Railroad / syntax diagrams.** Compact linear alternation, but no port
  vocabulary; the compactness idea survives in the spine grid.
- **Timeline/Gantt hybrid.** Temporal rendering belongs to the existing
  `timeline` widget; the schematic links to it rather than absorbing it.
- **Orthogonal topology-shape-metrics** (OGDF-style planarization + bend
  minimization). Circuit-schematic quality, but planarization machinery for
  near-linear graphs is unjustified weight.

## Consequences

### Positive

- Layout is O(n), allocation-light, and deterministic by construction —
  tour-safe, goldens-testable, no WASM in the loop.
- No new IDL or dependencies expected; rendering reuses the proven painter +
  sense-region + `RenderOpts` patterns from ADR-0069.
- Side channels become first-class and legible: the class → side mapping is
  the widget's contract, not a styling accident.
- Consumers with known structure (extbin, videopipeline, leeway, nanopass)
  map to the model 1:1, without graph-recovery preprocessing.

### Negative

- A second graph-shaped widget lives beside `layeredgraph`; the boundary
  ("known pipeline structure → pipelineview; arbitrary DAG → layeredgraph")
  must be documented and the models kept convertible, or the two drift into
  overlap.
- The closed port-class set will eventually pinch (e.g. metrics taps,
  control-plane ports); extensions require an ADR update by design.
- Go-side label sizing defaults to a deterministic estimate (SD2), so box
  widths are approximate — generous padding hides it; a host wanting exact
  boxes injects `MeasureText` and accepts the measurement round-trip.
- Consumers without a stage tree have no path yet (the SD3 builder is
  deferred); their interim option is `ToGraphModel` + `layeredgraph`.

### Neutral

- The name settled on `pipelineview` at implementation (the `*view` family:
  schemaview, mappingplanview); the M3 consumer is open at review.
- Tier-3 spline routing may never be needed; the deferral costs nothing.

## Status

Accepted 2026-07-17, after M1+M2+M3 were implemented and this text was
adopted to the implementation in place (pre-acceptance editing policy).
M4 (the volume overlay and live animation) stays a deferred milestone until
a consumer emits volumes; the per-run trace slice for the play Passes tab
(an observed apply seam in passreg) is recorded under M3 as its natural
next step. Post-acceptance changes follow the Tier-2 dated-update policy.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.

## References

### Clean-room sources — papers (the implement-from set)

- Sugiyama, K., Tagawa, S., Toda, M. (1981). *Methods for Visual
  Understanding of Hierarchical System Structures.* IEEE Trans. Systems,
  Man, and Cybernetics 11(2). — The layered framework the field builds on.
- Gansner, E., Koutsofios, E., North, S., Vo, K.-P. (1993). *A Technique for
  Drawing Directed Graphs.* IEEE Trans. Software Engineering 19(3). — The
  dot recipe: network-simplex ranking, weighted-median ordering + transpose,
  auxiliary-graph coordinates, spline corridors. Surveyed baseline; SD3
  borrows its phase structure.
- Eades, P., Lin, X., Smyth, W.F. (1993). *A Fast and Effective Heuristic
  for the Feedback Arc Set Problem.* Information Processing Letters 47(6).
  — Greedy cycle removal (SD3).
- Coffman, E.G., Graham, R.L. (1972). *Optimal Scheduling for Two-Processor
  Systems.* Acta Informatica 1. — Bounded-width layering (surveyed, not
  adopted).
- Brandes, U., Köpf, B. (2001). *Fast and Simple Horizontal Coordinate
  Assignment.* Graph Drawing 2001, LNCS 2265. — Linear-time coordinate
  assignment; adopted only if real DAG merges are admitted later.
- Barth, W., Mutzel, P., Jünger, M. (2004). *Simple and Efficient Bilayer
  Cross Counting.* J. Graph Algorithms and Applications 8(2). — Crossing
  counting (surveyed).
- Sander, G. (1995). *A Fast Heuristic for Hierarchical Manhattan Layout.*
  Graph Drawing 1995, LNCS 1027. — Channel/track assignment for orthogonal
  edges (SD4 tier 2).
- Dobkin, D., Gansner, E., Koutsofios, E., North, S. (1997). *Implementing a
  General-Purpose Edge Router.* Graph Drawing 1997, LNCS 1353. — Corridor
  spline fitting (SD4 tier 3, deferred).
- Schneider, P. (1990). *An Algorithm for Automatically Fitting Digitized
  Curves.* Graphics Gems, Academic Press. — Bézier fitting primitive
  (tier 3).
- Spönemann, M., Fuhrmann, H., von Hanxleden, R., Mutzel, P. (2010). *Port
  Constraints in Hierarchical Layout of Data Flow Diagrams.* Graph Drawing
  2009, LNCS 5849. — The port-constraint model for dataflow diagrams
  (SD1/SD2).
- Schulze, C.D., Spönemann, M., von Hanxleden, R. (2014). *Drawing Layered
  Graphs with Port Constraints.* J. Visual Languages and Computing 25(2). —
  The constraint ladder and north/south port handling; SD2 adopts the fixed
  rungs only.
- Wybrow, M., Marriott, K., Stuckey, P. (2006). *Incremental Connector
  Routing.* Graph Drawing 2005, LNCS 3843; and (2010) *Orthogonal Connector
  Routing.* Graph Drawing 2009, LNCS 5849. — Obstacle-avoiding routing
  (surveyed; adopted only if obstacles ever appear).
- Nöllenburg, M., Wolff, A. (2011). *Drawing and Labeling High-Quality Metro
  Maps by Mixed-Integer Programming.* IEEE TVCG 17(5). — Metro-map layout
  (kill-reason for the metro alternative).

### Clean-room sources — implementations surveyed (docs only, per SD6)

| Project | License | Consulted | Taken |
|---|---|---|---|
| [Graphviz](https://graphviz.org/) dot | EPL-1.0 | papers above only | phase recipe (baseline) |
| [ELK Layered](https://eclipse.dev/elk/reference/algorithms/org-eclipse-elk-layered.html) | EPL-2.0 | reference docs + papers | port-constraint ladder (bottom rungs); model-order determinism; phases-plus-processors architecture |
| [dagre](https://github.com/dagrejs/dagre) | MIT | docs | evidence the dot recipe reimplements small; polyline-through-dummies simplification |
| [d3-dag](https://github.com/erikbrinkman/d3-dag) | MIT | docs | exact ordering is affordable at small scale (SD3) |
| [d3-sankey](https://github.com/d3/d3-sankey) | BSD-3-Clause | docs | thickness scale, ribbon geometry, far-end-y link sorting (SD5, deferred) |
| [ReactFlow](https://reactflow.dev/learn/layouting/layouting) | MIT | docs | closed-form rounded-orthogonal edge shape, re-derived (SD4) |
| [libavoid / Adaptagrams](https://www.adaptagrams.org/) | LGPL-2.1 | papers only | surveyed |
| [OGDF](https://ogdf.uos.de/) | GPL | papers only | surveyed |
| [Jenkins pipeline-graph-view](https://www.jenkins.io/blog/2026/06/01/nested-layout-for-pipeline-graph-view/) | MIT | blog + docs | grid recursion over the stage tree; layout-engine/renderer separation (SD2) |
| GitLab pipeline graph | MIT (GitLab FOSS) | public descriptions | stage-columns-by-construction; links drawn in an overlay layer |

### Related ADRs

- [ADR-0069](./0069-imzero2-layeredgraph-widget.md) — layeredgraph: the
  sibling for arbitrary DAGs; rendering patterns reused here.
- [ADR-0057](./0057-demo-registry-and-drivers.md) — screenshot tour; the
  determinism requirement.
- [ADR-0046](./0046-imzero2-value-inspector-infrastructure.md) — inspector
  affordances for click-to-inspect stages.
- [ADR-0088](./0088-imzero2-runtime-codec-pipeline-and-viewer-capabilities.md),
  [ADR-0108](./0108-keelson-sql-pass-registry.md),
  [ADR-0118](./0118-extbin-external-process-chokepoint.md) — candidate
  consumers.
- [ADR-0097](./0097-play-reactive-query-graph.md) — play's system graph, the
  in-repo precedent for a layered dataflow drawing.
