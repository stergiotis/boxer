---
type: adr
status: proposed
date: 2026-07-17
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

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

We will build **`pipelineview`** (working name), a read-only imzero2 widget
that renders a data-processing pipeline schematically: the main path as a
left-to-right spine of stage boxes, side channels leaving each stage at sides
fixed by port class, endpoint artifacts (files, stores, streams) as
distinctly-shaped leaves. Layout is bespoke, deterministic, host-side Go;
rendering goes through the existing painter (no new IDL expected), following
the `layeredgraph` precedent.

### SD1 — Model: series/parallel stage tree with classed ports

The primary input is the structure the consumers already have, not a flat
edge list:

```go
type PortClass uint8 // Primary | Diagnostic | Artifact | Config
type EndpointKind uint8 // File | Store | Stream | Null

type Stage struct {
    ID, Label string
    Ports     []Port // class + name; declaration order is display order
}
type Group struct { // series or parallel composition of children
    Seq      bool
    Children []Element // Stage or Group
}
type Edge struct {
    From, To PortRef // stage.port, or an Endpoint
    Volume   float64 // optional; 0 = unknown (reserved for the overlay)
}
```

Port class → side is a closed mapping: `Primary` = east/west (the spine),
`Diagnostic` = south, `Config` = north, `Artifact` = south-east leaf.
Extending the class set is a Tier-2 ADR update, not an ad-hoc widget option.

### SD2 — Layout: grid recursion over the tree, not structure recovery

Spine ranks become columns; a parallel group stacks its branches as rows;
side nodes hang off their stage's column at fixed offsets. Port sides and
orders are fixed by SD1 — in the vocabulary of the port-constraint
literature, we implement only the bottom rungs of the constraint ladder
(`FIXED_SIDE` + `FIXED_ORDER`) and none of the free-port optimization.

Determinism is an invariant, not an aspiration: model order is the universal
tiebreak, no randomness, no map-iteration dependence. (The port-constraint
engine this survey drew on retrofitted exactly this as its model-order
options; we get it by construction.)

### SD3 — Degradation path for flat inputs

A consumer with only a flat edge list gets: greedy cycle removal
(Eades–Lin–Smyth), longest-path layering, and per-column ordering computed
exactly — at pipeline scale (columns of ≤ ~8 items) exact beats heuristics
for free. Genuinely arbitrary DAGs are out of scope: the widget documents a
handoff to `layeredgraph`, and the two models stay mechanically convertible.

### SD4 — Edge routing: three tiers, closed-form first

1. Spine edges: straight horizontal segments.
2. Side, skip (`a → c` past `b`), and feedback edges: closed-form rounded
   orthogonal paths (the "smoothstep" shape), with per-channel track
   assignment when several edges share an inter-column gap. Feedback edges
   are reversed for layout and drawn dashed against the flow.
3. Corridor-fitted Béziers (the dot look) — deferred until wanted; the
   painter already has the required primitives.

### SD5 — Overlays: status in v1, quantity and motion deferred

v1 ships per-stage/per-edge color hooks (the `RenderOpts` override pattern
from `layeredgraph/view`) so a host can mark running/failed/selected. The
volume overlay (edge thickness and ribbons ∝ `Edge.Volume`, links at a node
sorted by far-end y) and dash-march animation for live flow are specified by
the sources below but deferred until a live consumer exists.

### SD6 — Clean-room protocol

The implementation is written from the papers and public reference
documentation named in [§References](#references) — never from project source
code. For EPL/LGPL/GPL projects (Graphviz, ELK, libavoid, OGDF) source is
off-limits outright; for permissively-licensed projects (dagre, d3-dag,
d3-sankey, ReactFlow, Jenkins plugins) reading source would be lawful, but
the bar is the same: method-level extraction, all geometry and code
re-derived.

### Milestones

- **M1 — model + layout core.** Pure Go, no UI imports; golden-file layout
  tests asserting determinism.
- **M2 — painter renderer + gallery demo.** Canned shell-style pipeline
  (stderr, files written, one skip edge, one feedback edge) in the demo
  gallery, tour-captured.
- **M3 — first real consumer.** Candidate: extbin-resolved command chains or
  the videopipeline status view; pick at review.
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
- Go-side label sizing via `MeasureText` implies either a one-frame settle or
  uniform stage boxes with truncation; the ADR accepts that trade and leaves
  the choice to M2.
- Consumers without a stage tree get the degraded flat path, which is
  strictly weaker (no nesting, heuristic-free but structure-blind).

### Neutral

- The widget name (`pipelineview` vs `spineview`) and the M3 consumer are
  open at review.
- Tier-3 spline routing may never be needed; the deferral costs nothing.

## Status

Proposed — awaiting review by the repo owner. Open at review: the widget
name, the M3 first consumer, and whether the volume overlay is promoted into
v1.

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
