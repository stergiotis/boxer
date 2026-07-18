---
type: adr
status: proposed
date: 2026-07-18
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not
> implement as if accepted.

# ADR-0129: `play` layered-graph result panel — a node-link view from two CTEs

## Context

`play` renders a query result through a typed panel — Table, Projection,
Timeline, World, Kanban, Detail — each a `PanelI` observer bound to one or more
node results through the channel contract ([ADR-0097](./0097-play-reactive-query-graph.md)
slice 4). The panels answer "show me these rows *as* a table / a map / a board".
There is no panel that shows them *as a graph*.

Yet many relational results already *are* graphs. Foreign-key relations, package
or module dependencies, call graphs, ADR supersession chains, the marking edges
of the topology model ([ADR-0126](./0126-appliance-topology-as-data.md)) — all
are edge lists over a vertex set, and today the only way to see one in `play` is
to read the pairs out of a table.

Two things already in the tree make a graph panel cheap rather than a new
subsystem:

- **The `layeredgraph` widget ([ADR-0069](./0069-imzero2-layeredgraph-widget.md))**
  lays out a directed graph host-side (Graphviz compiled to WebAssembly,
  deterministic and pinned) and draws it through the existing imzero2 painter —
  box/ellipse/circle nodes, Bézier edge splines, arrow heads, per-node
  click/hover, and `NodeFill` / `NodeText` / `EdgeStroke` override hooks. No FFI
  or IDL surface is added by using it.
- **`play` already drives that widget.** The System graph chrome
  (`play_graph_viz.go`) builds a `layeredgraph.GraphModel` per frame, lays it out
  via `goccyengine.Shared()`, caches the `*Layout` on a topology fingerprint so a
  selection click never re-lays-out, and paints it with `view.Render`. That is
  exactly the render loop a result panel needs — the only new part is *where the
  model comes from*: query rows instead of the dataflow.

So the panel is mostly a mapping from two result sets to a `GraphModel`, plus the
now-standard panel plumbing. This ADR settles what that mapping is — which rows
are edges, which are vertices, and by what names.

## Decision

The panel renders a query result as a directed node-link graph, reading its
vertices and edges from two convention-named CTEs of the user's own query. Edges
are the required, primary input; a `vertices` CTE is optional and, when absent,
the vertex set is inferred from the edge endpoints.

### SD1 — Two channels, edges primary

The panel declares two input channels (ADR-0097 slice 4): `chEdges` (required)
and `chVertices` (optional). Both are filled from **top-level CTEs named by
convention** — `edges` and `vertices` — pulled off the user's own split graph,
each demanded on its own lane. This is the kanban `lanes`-CTE mechanism
([ADR-0122](./0122-play-kanban-panel.md) §SD6) applied twice: the split already
makes every top-level CTE a node, so the panel adds no query authoring beyond a
naming convention.

When `chVertices` is absent, the vertex set is inferred from the union of the
edge endpoints. So the smallest working query is an edge list:

```sql
WITH edges AS (SELECT parent AS source, child AS target FROM deps)
SELECT * FROM edges
```

and a richer one decorates those vertices:

```sql
WITH
  vertices AS (SELECT name AS id, label, kind AS group FROM pkg),
  edges    AS (SELECT parent AS source, child AS target FROM deps)
SELECT * FROM edges
```

The sink (`SELECT * FROM edges` above) is conventional — the panel reads the two
CTEs by name, so any sink works; making it `SELECT * FROM edges` just lets the
Table tab show the same edges. The two rejected binding models are recorded under
[Alternatives](#alternatives).

### SD2 — Column contract, by name

Columns are matched **by name, not by type or position** — the kanban precedent
(ADR-0122 §SD1): nothing but intent separates a `source` column from a `target`
column, so detection is refused and the name carries the meaning.

**Edges** (`chEdges`):

- `source`, `target` — **required**; the ordered endpoints of a directed arc.
  Values reference vertex `id`s.
- `label` — optional; drawn on the arc.

**Vertices** (`chVertices`, optional):

- `id` — **required**; the vertex key, and the value `source`/`target`
  reference. Must be unique; the panel de-duplicates and reports a collision in
  the status line rather than tripping the widget's unique-id invariant.
- `label` — optional; the drawn text (the `id` is used when absent).
- `group` — optional; distinct values are assigned a categorical **background**
  fill through the widget's `NodeFill` hook, with paired ink via `NodeText` (a
  light fill carries dark text and vice-versa). This is the *inverse* of a kanban
  dot, which is a foreground mark on a surface — a node body is a background, so
  the palette is background tones, and the `*Subtle`-tone trap ADR-0122 §SD2
  documents for dots does not apply here.
- `shape` — optional; `box` (default), `ellipse`, or `circle`.

**Endpoint reconciliation.** An edge endpoint with no matching `vertices` row
**synthesizes** a vertex (id-labelled, default style). So a partial `vertices`
CTE — one that names only some nodes — still draws every edge, and the
inference-from-endpoints case (SD1) is just the degenerate "no vertices row
matches anything" of the same rule.

### SD3 — Mechanism

A new `play_layeredgraph_panel.go` holds a `PanelI` driver with **two
`nodeLane`s** (one per input CTE) and a cached `*Layout`. The dock tab body:

1. Demands `edges` and `vertices` by name — `findSplitNode(split, "edges")` /
   `fuseNode` / `lane.demand(compiledNode{SQL, Params: resolveSignalNames(...)})`,
   exactly `demandKanbanLanes` done twice, so both CTEs resolve their signal
   reads like any other node and a `SET`-bound name travels inside the fused SQL.
2. Builds a `layeredgraph.GraphModel` from the two records per SD2 (vertices
   de-duplicated; missing endpoints synthesized; parallel `(source,target)` pairs
   collapse under the widget's one-edge-per-ordered-pair contract).
3. Lays out via `goccyengine.Shared()`, **caching the `*Layout` on a topology
   fingerprint** (sorted ids + endpoints + labels + rankdir) so a selection click
   — which changes only the highlight, not the topology — never re-runs the
   layout. This is the `play_graph_viz.go` idiom verbatim.
4. Paints with `view.Render`, wiring the `NodeFill` / `NodeText` (group colour +
   selection highlight) and interaction hooks below.

No new widget, IDL, or FFI surface; the panel is Go over the existing painter
binding.

### SD4 — Interaction

- **Selection (local in v1).** A vertex click (`RenderResult.Clicked`) marks the
  node — the driver holds a `selectedID`, the `NodeFill` hook paints it in the
  accent tone, and clicking it again clears it. The highlight is **local**: the
  panel publishes nothing to the shared `selection` signal. This is a considered
  cut, not an oversight. The graph's vertices come from a private lane (SD3), not
  an observable split node, and the `selection` signal is node-scoped
  (`syncSelectionClamp` sends a cursor whose `selection_node` is neither the
  active node nor a bound view *home* — resetting it to row 0). So emitting
  `selection` for a vertex would be clamped away *and* would jerk Table and
  Detail to their row 0. Cross-panel selection (Detail showing the clicked
  vertex, a `{selection_id}` cross-filter) waits for the graph's CTEs to become
  observable nodes — the SD7 observe/bind direction — at which point the standard
  `selectionStamper` path applies unchanged. *(This is the one place live-driving
  corrected the design: the first cut emitted `selection` and the clamp erased
  it; verified via egui_mcp, then reduced to the local highlight.)*
- **Layout direction.** A control row toggles `RankDir` (top-bottom default,
  left-right), like the Map and Kanban control rows.
- **Pan / zoom.** The panel holds one `view.ViewState` and passes it through
  `RenderOpts.State`, enabling the widget's opt-in pan/zoom over the fitted view.

### SD5 — Scale ceiling

Layered layout of a Graphviz-WASM run is comfortable for tens to low hundreds of
nodes and becomes both slow and unreadable well below kanban's 2000-card list cap
(a board is a list; a graph layout is super-linear and its legibility falls off a
cliff). The panel caps the model at a few hundred vertices and ~1000 edges,
surfaces the cap in the status line (the kanban precedent of naming what was
dropped rather than truncating silently), and defers filtering/clustering.

Two properties are inherited from the widget's v1 contract and stated rather than
worked around: parallel edges between the same ordered pair **collapse** to one,
and the layout is **directed** (undirected data still draws, with arrow heads).
The `Engine` seam (ADR-0069) leaves room for a force-directed or ELK backend
later if a use case needs one.

### SD6 — Tab identity

The tab is titled **"Network"** (slug `network`), DockID **15** (the next
built-in after Kanban's 14), with a `BOXER_PLAY_FOCUS_NETWORK` knob derived like
the other body tabs. The title deliberately differs from the existing **"Graph"**
tab, which is the dataflow chrome (`dockTabGraph`, ADR-0097 slice 3e) — one draws
the query *pipeline*, the other draws the query *result*, and sharing the word
"graph" between them in the tab strip would be a standing confusion.

### SD7 — Deferrals, recorded

Parallel/multi-edges (v1 collapses) · undirected layout · edge-weight→thickness ·
explicit per-vertex colour tokens (v1 is `group`→auto-palette) · above-cap
clustering/filtering · `chEdges` from an observed/bound node (the slice-6c path;
see Alternatives) · cross-panel selection (SD4 — the local highlight is v1; a
`selection`/`selection_id` publish waits on the observable-node work). Each is a
triggered deferral, not a defect — none blocks the first cut, and the seams (the
`Engine` interface, the channel contract) are where they would land.

## Alternatives

**Both CTEs required.** The most literal reading of the request ("a CTE gives the
vertices, another the edges") is to require both. Rejected because it is strictly
less capable for no gain: an edge list is the common case (dependency graphs,
call graphs, foreign-key pairs *are* pairs), and requiring a `vertices` CTE there
is ceremony. The optional form still draws isolated vertices and vertex
attributes the moment a `vertices` CTE is present, so nothing is lost — it is the
both-required contract plus a fallback. This mirrors kanban, where `lanes` is
optional and the board falls back to lanes-off-the-rows.

**Edges follow the observed node.** An alternative binds `chEdges` to whatever
node the tab observes (the sink by default, any node via the ADR-0097 slice 6c
"fill tab" gesture), with `vertices` an optional decorating CTE. It integrates
with `play`'s observe/bind machinery and lets "observe node X" retarget the
graph. Rejected as the *default* because the primary input would then be the sink
*statement*, not a CTE literally named `edges` — a worse match for the request
and for the mental model — and it entangles the panel with 6c binding lifetime
for no first-cut benefit. The observe/bind path stays available as a later
refinement (SD7): a bound node whose result exposes `source`/`target` could fill
`chEdges`.

**`from`/`to`, or a permissive alias set, for endpoints.** Graphviz names its
endpoints `from`/`to` and it reads naturally, but `from` is a SQL keyword and
awkward to produce as a column alias, whereas `source`/`target` is the graph-data
standard (D3, GraphML, Cytoscape) and collides with nothing. A permissive alias
set (`source`/`target` | `from`/`to` | `src`/`dst`) was also rejected for the
first cut: it widens the contract surface for the same reason kanban settled on
exactly `lane`/`title`. `source`/`target` is the one name pair.

## Consequences

The marginal cost is the two-lane demand, the row→`GraphModel` mapping, and the
column contract — the widget, the layout engine, the render loop and the layout
cache are already paid for by the System graph. Unlike ADR-0122 §SD4, the panel
crosses no line: it reads query results, introduces no filesystem-at-query-time
providers, and needs no server it did not already need.

What it buys: any graph-shaped result becomes visible declaratively, by naming
two CTEs, with a clicked node highlighted in place (SD4 — cross-panel selection
is the deferred half). The honest limit is that layered layout suits directed,
DAG-ish data; a dense cyclic graph will read poorly, and that is a property of
Graphviz `dot`, not of this panel — the seam is where a different layout would go.

## Validation

Live-drive via `hmi.sh --launch play` (the ADR-0122 / ADR-0097 screenshot recipe)
against a graph-shaped fixture — candidates already in the tree: the ADR
supersession edges (`keelson('adr')`), foreign-key relations (`tv:foreignKey:*`),
or the ADR-0126 topology marking edges. Confirm: (1) an edges-only query draws
with endpoint-inferred vertices; (2) adding a `vertices` CTE decorates them
(label, `group` colour, `shape`); (3) a vertex click highlights the node locally
and the highlight persists when the pointer leaves (SD4); (4) the RankDir toggle
re-lays-out; (5) the cap shows in the status line on an over-large result. Unit
tests cover the row→`GraphModel` mapping (by-name binding, endpoint synthesis,
vertex de-duplication, parallel-edge collapse, cap) without the widget harness,
as the kanban fold tests do.

**Verified 2026-07-18** (build + `apps/play` suite green; live-driven via
egui_mcp against a small dependency-DAG fixture — a `vertices` CTE of `id`/
`label`/`group`/`shape` and an `edges` CTE of `source`/`target`): the Network tab
rendered "6 nodes · 7 edges" as a directed layered graph, the `group` column
coloured the two categories distinctly (box shapes, id labels), the RankDir
toggle re-laid-out top-down↔left-right, and a node click highlighted in place and
persisted with the pointer away (2 and 3 above). (1) and (5) rest on the unit
tests; the live cross-fixtures remain a follow-up.

## Status

Proposed 2026-07-18. The design dialogue settled the binding model (SD1 — edges
required, vertices inferred when absent) and the column contract (SD2 —
`source`/`target` for edges, `id` for vertices, all by name) before any code.

**Built the same day and live-verified** (see Validation) in the working tree,
not yet committed: `apps/play/play_layeredgraph_panel.go` (+ tests), the Network
dock tab (`dockTabNetwork`, `BOXER_PLAY_FOCUS_NETWORK`), and the driver wiring.
Live-driving corrected SD4 from a shared-signal selection to a local highlight
(recorded there). Awaiting human review — reviewed-by / a status flip to
`accepted` is the reviewer's, per the ADR lifecycle.

## References

- [ADR-0069](./0069-imzero2-layeredgraph-widget.md) — the `layeredgraph` widget
  (engine seam, `goccyengine`, `view.Render`) this panel draws with.
- [ADR-0097](./0097-play-reactive-query-graph.md) — the reactive query graph and
  the channel contract (`PanelI`, `dispatchPanel`, the split/`fuseNode` lane
  mechanism, the `selection` signals) the panel is built on.
- [ADR-0122](./0122-play-kanban-panel.md) — the kanban panel, whose `lanes`-CTE
  channel (§SD6) and by-name column contract (§SD1) this ADR follows.
- [ADR-0114](./0114-play-world-choropleth-panel.md) — the World panel, the sibling
  precedent for a result panel with a bespoke drawing.
- [ADR-0126](./0126-appliance-topology-as-data.md) — topology-as-data, a source of
  graph-shaped results to validate against.
