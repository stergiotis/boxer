---
type: adr
status: proposed
date: 2026-10-15
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0331: entity diagrams on `layeredgraph` — edge identity, parallel edges, edge notation, row nodes

## Context

`layeredgraph` ([ADR-0069](./0069-imzero2-layeredgraph-widget.md)) draws
directed flow graphs: state machines, DAGs, pipelines. Its vocabulary is sized
for them — a node is one label in a box, circle or ellipse; an edge is one
directed, solid, labelled arrow, at most one per ordered pair of nodes.

An **entity diagram** — an ER diagram, or a UML class diagram of a domain
model — is the next kind of graph a downstream library asks it to draw: a box
per thing that has records, the fields it holds as rows inside the box,
relations between things as a line (two ends) or a hub (three or more ends),
references as plain arrows, and contracts as boxes that implementers point to
with a dashed, open-headed arrow. Drawn in the flow vocabulary, each
declaration becomes a vertex and each member a labelled arrow, and five
different relationships share one edge kind. The notation it needs, against
what the widget offers (2026-10-15):

| The diagram needs | `layeredgraph` |
| --- | --- |
| a box with a title and one row per field, rows optionally banded | one label string; line breaks are the only structure |
| a diamond for an n-ary relation | box, circle, ellipse |
| two edges between the same two nodes (two members pointing to one target; a relation whose ends are the same kind) | at most one edge per ordered pair; the second is dropped |
| undirected edges, and dashed edges with an open head | every edge directed and solid |
| a role label at each end of a relation | one label, at the middle |
| the edge under the pointer | hover and click reported per node only |
| labels kept readable on a large diagram | fit-to-canvas, so labels shrink with the graph |

Graphviz lays out all of it — `shape=record` and `shape=diamond`, named
parallel edges, `dir`, `arrowhead`, `style`, `headlabel`/`taillabel` — and
reports the geometry back in the laid-out DOT the engine already parses. The
gap is in the seam ([`layout.go`](../../public/thestack/imzero2/egui2/widgets/layeredgraph/layout.go)),
the parse, and the painter ([`layeredgraph/view`](../../public/thestack/imzero2/egui2/widgets/layeredgraph/view/)).

One part of the gap is not additive. Every per-edge property the diagram needs
— a head kind, a dash, end labels, the hovered edge — is ambiguous once two
edges share their endpoints, and the view's per-edge hooks identify an edge by
`(from, to)` only. [ADR-0167](./0167-layeredgraph-magnitude.md) §SD2 chose that
signature deliberately, to match `pipelineview`'s hooks exactly, and listed the
match as a consequence. Giving edges an identity reopens that choice.

## Design space (QOC)

**Question.** How do the view's per-edge hooks (`EdgeStroke`, `EdgeWidth`)
identify an edge once parallel edges exist?

**Options.**

- **O1** — the hooks take the edge's `*layeredgraph.EdgeLayout` (identity,
  endpoints, weight, notation) in place of their current arguments.
- **O2** — keep the `(from, to)` hooks and add twins keyed by edge identity;
  the twin wins when both are set.
- **O3** — add the edge's identity as one more argument to the current hooks.

**Criteria.**

- **C1** — no ambiguity: two parallel edges can be styled apart.
- **C2** — one way to say it: a reader meets one hook per channel, not two
  with a precedence rule.
- **C3** — sibling convergence: the hooks keep `pipelineview`'s signatures
  (ADR-0167 §SD2).
- **C4** — stability: a later per-edge field does not change the signature
  again.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | ++ | ++ | ++ |
| C2 | ++ | −− | ++ |
| C3 | −− | +  | −  |
| C4 | ++ | +  | −− |

O1 is chosen. C3 is the one it loses, and the loss is bounded: `pipelineview`'s
model has no parallel edges, so its pair-keyed hooks stay unambiguous for it,
and the two widgets share an idiom rather than code (ADR-0167 §SD2 already
declined a shared package for the same reason). O2 keeps the match by keeping
the ambiguous hooks alive beside the unambiguous ones. O3 breaks the signature
now and again at the next field.

## Decision

We extend `layeredgraph` so an entity diagram can be drawn in its own
notation. Edges gain an identity and may be parallel; edges carry a head kind,
a dash and labels at their ends; nodes may be diamonds or row nodes; the view
reports the edge under the pointer and can hold labels at a minimum size.
Every addition is a field or option whose zero value is the existing
behaviour, except the two edge hooks, whose signature changes (§SD1).

### SD1 — Edges carry an identity, and the edge hooks take the edge

`Edge.ID` is caller-assigned and unique among a model's edges; empty means the
edge's index in `GraphModel.Edges`, written in decimal, the way an empty
`Node.Label` means the node's ID. A duplicate is a layout error.
`EdgeLayout` carries the ID and the model index.

`view.RenderOpts.EdgeStroke` and `EdgeWidth` take the `*EdgeLayout` they are
asked about, and `view.WeightWidth` returns that shape (§Design space).

Caller IDs never reach Graphviz: the engine names each Graphviz edge by its
model index and maps the laid-out edge back through it, so an ID may hold any
string.

*Kill clause:* if callers commonly share one hook function between
`layeredgraph` and `pipelineview`, the divergence costs more than it saves,
and O2 returns.

### SD2 — Parallel edges are kept

The engine keeps every edge of the model. A laid-out edge is matched to its
model edge by its Graphviz name, not by its endpoints, so two edges between the
same nodes — including two self-loops on one node — come back as two
`EdgeLayout`s with their own splines and labels. This retires ADR-0069's "at
most one edge per ordered pair" contract.

The engine already names edges uniquely, and Graphviz routes named parallel
edges apart and writes each name back as its `key` (observed 2026-10-15 with
the embedded engine). The collapse was a property of the parse, not of the
layout.

### SD3 — Edge notation: a head kind and a dash

`Edge.Head` is one of `EdgeHeadSolid` (the zero value, the existing arrow),
`EdgeHeadNone` (an undirected edge: Graphviz `dir=none`, which changes how the
edge is drawn, not how the graph is ranked) and `EdgeHeadOpen` (an unfilled
triangle, Graphviz `arrowhead=empty`). `Edge.Dashed` draws the edge dashed.
Both pass through the layout onto `EdgeLayout`.

The dash and gap lengths are a `view.Style` concern, in pixels like
`Style.EdgeStrokeW`, so a dashed edge stays readable when the drawing is scaled
to fit. A dashed edge's dashes are sent as one batch, following
[ADR-0329](./0329-painter-lane-vertex-budget-and-batched-graph-geometry.md)
§SD5.

The widget provides the mechanism; what a dash *means* is the caller's.
ADR-0167 §SD7 deferred a dashed-edge *vocabulary* for play's Network panel, and
that deferral is unaffected.

### SD4 — Labels at an edge's ends

`Edge.HeadLabel` and `Edge.TailLabel` are laid out by Graphviz
(`headlabel`, `taillabel`) and returned as anchors on `EdgeLayout`, read from
`head_lp` and `tail_lp`. The view paints them like the middle label.

`dot` places end labels after layout. It reserves no room for them and does not
move them out of the way, so on a dense diagram they can overlap nodes and
other labels. This is accepted, and the fields' doc comments say so.

*Kill clause:* if end labels are unreadable on the diagrams that use them,
their placement moves into Go with collision avoidance, or the roles go back
into the middle label.

### SD5 — Row nodes, laid out as Graphviz records

`Node.Rows` (each `Row` an ID and one line of text) and `Node.Subtitle` make a
node a **row node**: a title line (the node's label, with the subtitle beside
it) above one row per field. Rows require `NodeShapeBox`. `NodeLayout` returns
the title's rectangle and each row's.

The engine lays a row node out as a Graphviz record with one field for the
title and one per row, and reads each field's rectangle back from the laid-out
`rects` attribute. The rows' geometry therefore comes from the same layout that
sized the box, with no second measurement. A record's top level runs across the
rank direction — under `LR` a flat field list stacks vertically, under `TB` it
has to be wrapped in braces to do so (observed 2026-10-15) — so the engine
builds the label from `LayoutOpts.RankDir`. The title field carries the title
and the subtitle together, so Graphviz reserves the width of both.

All of a row node's text is laid out and painted at the node's one font size,
which is what a record supports. The title is set apart by a title band and the
strong text colour, not by weight: the painter has no bold face (`PaintText`
offers a monospace variant only, and egui's strong text is a colour). Row bands
come from a view hook, `RenderOpts.RowFill(nodeID, rowID)`, beside `NodeFill`,
so colour stays in the view and the model stays UI-free.

Each row's field gets a port named by its index. Nothing uses the ports yet;
they let a later decision anchor edges at rows without changing the label
scheme.

*Kill clause:* if rows need different fonts or inline styling, records cannot
carry them, and the engine moves to HTML-like labels with draw-op replay
(ADR-0069's deferred Q3-R2).

### SD6 — A diamond node shape

`NodeShapeDiamond` is appended to `NodeShape`, so existing values keep their
numbers. It is laid out, painted and picked as a rhombus, not by its bounding
box. Graphviz sizes a diamond about twice as wide as a box around the same
label (166 pt against 85 pt for a nine-letter label at 14 pt, 2026-10-15), so a
long name makes a large hub.

### SD7 — The edge under the pointer

`view.RenderResult` reports the hovered and the clicked edge by ID, beside the
node it already reports. An edge is hit within its stroke width, or a few
pixels where the stroke is thinner, of the curve as painted. A node under the
pointer takes priority over an edge, and of two edges the nearer wins. The
curve-distance helpers are copied from `graphview`, as `WeightWidth` copies
`pipelineview`'s curve; ADR-0228 §SD6 keeps a shared painter core deferred, and
this does not change that.

### SD8 — A minimum label size

`view.RenderOpts.MinLabelPx` puts a floor under the fit scale: the drawing is
not fitted smaller than the scale at which a label reaches that size. When the
floor binds, the drawing is anchored at its top-left rather than centred, and
the rest is reached by panning, so the option is meant to be used with
`RenderOpts.State`. Zero leaves fit-to-canvas as it is.

### SD9 — Deferred

- **Edges anchored at rows.** The ports exist (§SD5); the model has no way to
  name one yet.
- **A bold face.** It needs a face loaded by the client and a `PaintText`
  option in the IDL, for an emphasis the title band already gives.
- **Hover per row.** A row node is picked as a box.
- **Tail arrowheads** (`dir=back`, `dir=both`).
- **An edge font for layout.** The engine sets no edge font, so Graphviz
  reserves room for edge labels at its own default size while the view paints
  them smaller (observed 2026-10-15); labels get more room than they use, end
  labels included.

## Milestones

| # | Deliverable |
| --- | --- |
| M1 — Identity and parallel edges ✓ | §SD1, §SD2, with the hook callers migrated in the same change |
| M2 — Edge notation and end labels ✓ | §SD3, §SD4 |
| M3 — Diamond ✓ | §SD6 |
| M4 — Row nodes ✓ | §SD5 |
| M5 — Edge pick | §SD7 |
| M6 — Minimum label size | §SD8 |

M1 comes first because M2 and M5 hang per-edge data on its identity; the others
are independent of each other.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `widgets/layeredgraph` (exported Go API under `public/`) | added — edge ID, head kind, dash and end labels on `Edge`; ID, model index, notation and end-label anchors on `EdgeLayout`; rows and subtitle on `Node`; title and row rectangles on `NodeLayout`; `NodeShapeDiamond` | additive; the zero value of each is the existing behaviour |
| `widgets/layeredgraph` edge contract (ADR-0069) | reshaped — parallel edges are kept (§SD2) | ADR-0069 gains a dated Update with M1 |
| `widgets/layeredgraph/view` (exported Go API under `public/`) | **reshaped** — `RenderOpts.EdgeStroke`, `RenderOpts.EdgeWidth` and `WeightWidth` take the `*EdgeLayout`; added — `RowFill`, `MinLabelPx`, the hovered and clicked edge on `RenderResult`, dash and title styling on `Style` | every caller of the two hooks, in the same change; ADR-0167 gains a dated Update with M1 |
| `goccyengine` DOT emission | added — edge attributes and record nodes, only for edges and nodes that use them | a graph using none of the additions emits the same DOT as before |
| egui2 IDL | **unchanged** | the painter already fills polygons, strokes polylines, draws segment batches and anchors text |

## Alternatives

The hook-shape options are weighed in [Design space (QOC)](#design-space-qoc).
For the rest:

- **HTML-like labels for row nodes.** They carry bold text, per-cell fills and
  mixed fonts, but the laid-out DOT returns no cell geometry, so the rows could
  only be placed by replaying xdot draw operations — ADR-0069's deferred Q3-R2,
  a larger change than this decision needs.
- **Rows measured in Go, boxes given to Graphviz as fixed sizes.** Needs the
  render font's metrics in the Go host, which ADR-0069's 2026-06-07 Update
  costed and deferred.
- **Rows drawn by dividing a plain box's height.** Leaves the row geometry to
  arithmetic over a box sized for a multi-line label, which is the estimate the
  record's `rects` replaces.
- **Keep one edge per pair and let the caller join labels.** Two facts read as
  one, and every per-edge property in this decision becomes impossible to
  attribute.
- **A bold face now.** An IDL change and a font registration for an emphasis
  the title band provides.

## Consequences

### Positive

- An entity or class diagram can be drawn in its usual notation: row nodes,
  relation hubs, undirected and dashed open-headed edges, role labels at the
  ends.
- Parallel edges and parallel self-loops are drawn, in every consumer. A
  consumer that passed duplicate pairs and saw one edge now sees all of them.
- Edge hover reaches parity with `graphview`.
- Row geometry comes from Graphviz, so rows cannot drift from the box they sit
  in.

### Negative

- The edge hooks no longer match `pipelineview`'s, which retracts the
  convergence ADR-0167 counted as a consequence. A reader who knows one
  widget's edge hooks no longer knows the other's.
- Every caller of `EdgeStroke` or `EdgeWidth` changes, in the same change as
  the widget.
- End labels can overlap (§SD4), and the widget cannot prevent it.
- A row node has one font size, and its title is distinguished by colour and
  band rather than weight.
- A dashed edge sends more painter vertices than a solid one.

### Neutral

- A graph that uses none of the additions lays out and renders as before; the
  hook change is a signature change, not a behaviour change.
- Nothing here touches the IDL, so no regeneration and no Rust rebuild.

## Migration — Tier 1

- **Breaks.** `view.RenderOpts.EdgeStroke` and `view.RenderOpts.EdgeWidth`
  change from `(from, to string, …)` to `(e *layeredgraph.EdgeLayout)`;
  `view.WeightWidth` returns the new shape. Callers relying on parallel edges
  collapsing to one see every edge.
- **Path.** Read `e.From`, `e.To` and `e.Weight` where the arguments were
  read. Callers in this repository migrate in the M1 change.
- **Regeneration.** None.
- **Old shape.** Removed outright.

## Verification plan — Tier 1

- **Lane.** Default `go test`. The engine is pinned on: parallel edges and
  parallel self-loops returning separate layouts with their IDs; an empty ID
  defaulting to the index and a duplicate failing; head kind, dash and end-label
  anchors round-tripping; a row node's title and row rectangles stacking inside
  its box under both `LR` and `TB`; record metacharacters in row text
  round-tripping; and a model using none of the additions emitting the same DOT
  as before. The view is pinned on the diamond and edge picks, the dash walk,
  and the fit floor, all pure.
- **What would fail.** A parse that matches by endpoints again drops a parallel
  edge; a record built for the wrong rank direction lays its rows side by side;
  an emission change for unused fields diffs the DOT.
- **Gap.** What is painted — whether the bands, dashes and end labels read
  well — rests on the gallery capture being looked at. End-label overlap is
  accepted, not tested.

## Status

Proposed — awaiting review by p@stergiotis.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

| Source | What it settles |
| --- | --- |
| Chen, *The Entity-Relationship Model — Toward a Unified View of Data*, ACM TODS 1(1), 1976 | the diamond as the relationship symbol (§SD6) |
| OMG, *Unified Modeling Language 2.5.1*, 2017 | a realization drawn dashed with an open triangle; a classifier drawn with compartments (§SD3, §SD5) |
| Graphviz, [node shapes — record-based nodes](https://graphviz.org/doc/info/shapes.html#record) | field syntax, ports, and the record's flip with rank direction (§SD5) |
| Graphviz attributes [`rects`](https://graphviz.org/docs/attrs/rects/), [`head_lp`](https://graphviz.org/docs/attrs/head_lp/), [`tail_lp`](https://graphviz.org/docs/attrs/tail_lp/), [`dir`](https://graphviz.org/docs/attrs/dir/), [`arrowhead`](https://graphviz.org/docs/attrs/arrowhead/) | the output the parse reads and the input the emission sets |

### Related ADRs

- [ADR-0069](./0069-imzero2-layeredgraph-widget.md) — the widget this extends;
  its one-edge-per-pair contract is retired by §SD2.
- [ADR-0167](./0167-layeredgraph-magnitude.md) — the edge hooks' signature
  (§SD2 there) that §SD1 here reshapes, and the dashed-edge vocabulary (§SD7
  there) that stays deferred.
- [ADR-0228](./0228-hosted-canvas-rendering-and-graph-widget-sharing.md) — the
  Go-side pick §SD7 extends to edges, and the painter-core deferral it keeps.
- [ADR-0329](./0329-painter-lane-vertex-budget-and-batched-graph-geometry.md) —
  the batching §SD3 follows for dashes.
