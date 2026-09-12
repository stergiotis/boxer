---
type: adr
status: proposed
date: 2026-09-11
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** The panel ships with this record;
> it has not been reviewed.

# ADR-0227: a Graphview panel for `play` — the live reading of the Network contract

## Context

`play` draws a result as a node-link graph in the Network tab
([ADR-0129](./0129-play-layered-graph-panel.md)): two convention-named CTEs of
the user's own query — `edges` (required) and `vertices` (optional) — each
pulled off the split on its own lane, mapped to a `layeredgraph.GraphModel`,
laid out by Graphviz in WebAssembly and painted through `layeredgraph/view`.
The layout is cached on a topology fingerprint, so a click never re-lays-out,
and the picture is static: ranks, spline routing, no animation.

[ADR-0224](./0224-graphview-go-graph-widget-painter-lane.md) added
`widgets/graphview`, the live node-and-edge widget — force-directed and
hierarchical layouts owned in Go, a camera with the one-shot fit latch, node
drag and pins, donut rings, screen-space auras with the shared legend, and
events, selection and metrics as Go values. It has no consumer in this tree
beyond the gallery demo, which is the gap its own verification plan names: *a
scene follows once the widget has consumers in this tree*.

The two answer different questions over the same rows. A layered drawing reads
*what comes before what*; a force drawing reads *what clumps with what*. The
input is the same either way: `source`/`target`/`label`/`tone`/`weight` on the
edges, `id`/`label`/`group`/`shape`/`tone`/`weight` on the vertices is as good
a declaration for a force graph as for a ranked one.

This repository has answered the same shape once already. The Treemap
([ADR-0166](./0166-play-treemap-panel.md) §SD1) reads the hierarchy contract
the Icicle defines, hoisted into a file both panels resolve against, on the
reasoning that *a query that draws as a flamegraph draws here unchanged, and
the choice between the two is about the question, not about the SQL* — with
duplicating the resolver killed because `color` would have been the first
divergence, on the first commit.

## Design space (QOC)

**Question.** How does `play` expose graphview's feature set, given that the
Network tab already owns the contract a graph result declares?

**Options.**

- **O1** — Merge: the Network tab's `layout` control becomes an *engine*
  switch (layered / force / tree), one panel driving two renderers.
- **O2** — A second dock tab over the same contract, hoisted into a file both
  panels resolve against.
- **O3** — Replace `layeredgraph` inside the Network tab with graphview.
- **O4** — Leave graphview to the gallery until the downstream consumers of
  the `Graph` binding migrate.

**Criteria.**

- **C1** — One place to look for "my rows as a graph".
- **C2** — The two readings can be compared.
- **C3** — A home for the channels only one renderer honours (auras, donuts,
  pins).
- **C4** — Chrome: how much of the panel's controls are conditional on which
  renderer is live.
- **C5** — Caps: 400 vertices is a Graphviz-WASM number, not a force-layout
  one.
- **C6** — Closes ADR-0224's named verification gap.

**Assessment.**

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | −  | ++ | n/a |
| C2 | −− | ++ | −− | −− |
| C3 | −  | ++ | ++ | n/a |
| C4 | −  | ++ | +  | n/a |
| C5 | −  | ++ | −  | n/a |
| C6 | +  | +  | +  | −− |

C2 decides it. A dock holds one tab per dock id, so a merged panel can only
ever show one renderer: the reader who wants to see that the hub Graphviz
ranks at the top is also the node the force layout parks in the centre has to
toggle and remember. Two tabs put both readings on screen at once, which is
why the Icicle and the Treemap are two tabs and not one tab with a mode.

## Decision

`play` gains a **Graphview** dock tab: the live reading of the graph contract
the Network tab already defines. The contract is hoisted so one file owns it,
the two lanes that feed it are shared, and the columns only the live renderer
can honour are additions to that one contract rather than a second one.

**SD1 — One contract, one owner, caps as a parameter.** The edge and vertex
resolvers and the record-to-model build move out of the Network panel into a
file both panels resolve against, producing a **renderer-neutral model**:
vertices and edges carrying the contract's cells as declared — `group`,
`shape` and `tone` as the strings the query wrote, not as resolved colours —
plus the first-seen `group` ordering, the two weight maxima the magnitude
channels normalise against ([ADR-0167](./0167-layeredgraph-magnitude.md) §SD5)
and the capped flag. Each panel adapts that model to its own widget: the
Network panel to a `GraphModel` with its `NodeFill` / `EdgeStroke` hooks, this
one to node and edge specs. The dedup, endpoint synthesis, parallel-edge
collapse and cap rules are the build's and are stated once. The vertex and
edge **caps become an argument** rather than a constant, because the two
panels have different reasons for having one (§SD9).

Resolving the contract twice is killed for ADR-0166 §SD1's reason: the first
column added to one resolver is the divergence.

One rule changes in the move, and it is recorded as a dated Update on ADR-0129:
a `group` claims its palette position whether or not the vertex that named it
also carried a tone that wins the fill. The old rule assigned a position only
where the group actually coloured something, which left a group named solely by
toned vertices without one — no colour for its aura here, and a palette that
shifted under the groups after it there.

**SD2 — One source, shared lanes.** The `edges` and `vertices` lanes, their
loading and error mirrors, and the demand calls that compile each CTE off the
split move off the Network driver onto a source both tabs demand from. With
both tabs open the CTEs execute **once** — the second demand is a memo hit —
and a forced re-fetch clears one memo, not two. The panel channel ids are
unchanged, so the strip marks that report an unfed contract
(`splitFedChannel`) cover the new tab without knowing it exists.

**SD3 — The tab is named for the widget.** "Graph" is the reactive-dataflow
chrome and "Network" is the ranked reading, both taken; ADR-0129 §SD4 already
chose its title against the same collision. The tab is **Graphview**, a new
frozen dock id, listed beside Network. It is `lazy` like its neighbours, which
also means a hidden tab steps no simulation.

**SD4 — `group` is the aura id.** A vertex's `group` already means *these
belong together*; the live panel draws that grouping as an aura (ADR-0224
§SD11) as well as colouring the node from the palette position the shared
model assigns. Auras are **off by default and overlapping when
switched on**, which the panel's first capture argued for rather than the other
way round: a force layout interleaves the members of a group that is not also a
cluster — every operator sits among its own aircraft types — so the blob over
such a group is a mass, not a reading, and only the reader can see whether the
grouping is spatial. Overlapping, because without it a cell goes to the aura
with the most members reaching it and the larger group swallows the smaller:
two groups, one blob. The aura colours are the widget's own
cycle rather than the contract's group palette: that palette is the Subtle
background tones, chosen dark so one light ink reads on a node body, and a
translucent blob of one over the same dark panel would be a blob nobody can
see. The legend names the group, so the reader pairs them by label. No new column, and the graph query shipped in the help corpus
grows its two blobs with no edit. Multi-membership — a vertex in several auras
— needs a list-typed column of its own and is **deferred**; so is a dedicated
`aura` column that would let a query group differently from how it colours.

**SD5 — `weight` sizes the node, `donut` breaks it down.** A vertex `weight`
scales the node's radius by the square root of its share of the maximum, the
same root the Network panel's width and ramp already use, so the two panels
encode one magnitude the same way. A new optional `donut` column — a list of
numbers — draws the ring of ADR-0224 §SD9 around the node, with an optional
`donut_total` leaving the remainder as a muted track. `donut` is read by this
panel alone; the Network panel ignores it, which is the point of a second tab
rather than a contract that half-applies.

**SD6 — Selection is the widget's; `selection_key` mirrors it.** Node
selection is on, so the widget owns the highlight and reports select and
deselect; the panel publishes the selected vertex's **string** id as
`selection_key` and the empty string when it is cleared. A selection the next
declaration no longer carries is cleared and the clear is published: the widget
drops such a selection silently, having no id left to report it against, and a
query reading the signal would otherwise keep pointing at a vertex that is not
in the graph. The row-index
`selection` stays unpublished for ADR-0129 §SD4's reason, unchanged here: the
vertices come from a private lane rather than an observable split node, so a
cursor emit would be clamped away and would jerk the other panels to row 0.

**SD7 — Ids are xxh3 of the declared id, probed on collision.** The contract
keys vertices by string; graphview declares `uint64`. The panel interns one to
the other with `xxh3` and keeps the reverse map for the frame, so a click
publishes the id the query wrote. A collision — two distinct strings landing
on one hash — is resolved by incrementing until the slot is free, which is
deterministic given the model and therefore stable frame to frame, and the
reverse map is what keeps the published id honest rather than the hash.

**SD8 — The declaration is rebuilt on the lanes' fingerprint, not per frame.**
A force layout re-declares every node and edge each frame, and building that
declaration means formatting every cell of both records. The lane already
carries the early-cutoff hook the other observers use
([ADR-0097](./0097-play-reactive-query-graph.md) §SD4): the model is cached on
the two lanes' fingerprints and the resolved claims, so a running simulation
re-formats nothing and a result that re-fetches to identical bytes costs no
rebuild. The specs handed to the widget are derived from the cached model and
reused.

A rebuild also re-arms the camera's one-shot fit. The widget arms it on its
first nodes and not again (ADR-0224 §SD4), which is right for a graph that
grows and wrong for one that is replaced: without the re-arm a re-Run against a
different graph would be framed by the camera the previous one was left at.
Positions of surviving ids are kept — only the framing is re-armed, so a
partial refresh does not throw away a layout the user has been reading.

**SD9 — The caps are this panel's own.** The Network panel caps at 400
vertices because a Graphviz-WASM layered layout is a tens-to-low-hundreds
instrument. A force step is not: measured on one laptop (an i7-10510U, not a
trial), the package's parallel Barnes–Hut repulsion takes about 0.5 ms at 1 000
nodes and about 3.4 ms at 5 000. The step is one item in a 60 Hz frame that
also paints the graph, so this panel caps at **2 000 vertices and 6 000
edges** — a frame-budget guard, with legibility giving out well before it —
and says so in the status line when it bites, as the Network panel does.

**SD10 — The panel freezes a simulation that will not settle.** The widget
offers `Paused` and `IsSettled` and leaves the timing to its caller. A result
graph does not always reach the epsilon — a heavy hub trades small
displacements indefinitely — and a dock tab that steps forever is a query tool
burning a core on a picture nobody is watching change, so the panel stops the
simulation past a step budget, says so in the status line, and starts it again
on `settle` or `re-lay-out`. A fresh declaration is also given a **budget of
steps before its first paint**, expressed in node-steps rather than steps so it
is imperceptible on a small graph and not a stall on a large one: what the
reader would otherwise watch for the first second is the algorithm, not the
graph.

**SD11 — Labels follow a budget.** Every node carries its label up to a
vertex count past which the text would be both illegible and the frame's
largest cost; above it labels are drawn for the hovered, selected and dragged
node only, and the status line says which rule is in force. The budget is a
property of the model, not a remembered toggle, so the same result always
reads the same way — and it is low, because these labels are screen-sized
(ADR-0224 §SD7) and a result's are database values rather than the short ids a
hand-built demo carries.

**SD12 — The layout is spaced for those labels.** The ideal edge length is
widened over the crate's `k = sqrt(area/n)`, which was sized for labels drawn
at the node radius: a screen-sized label does not shrink as the graph grows, so
the stock `k` packs a result graph into a knot. The aura reach, by contrast,
stays at the widget's default — shortening it looks like a way to tighten the
blobs and instead fragments a group into an island per node, each its own
concave fill.

## Alternatives

- **O1 — an engine switch inside the Network tab.** Killed on C2: one tab
  shows one renderer, and the two readings are most useful side by side. It
  also makes the rank-direction control conditional on the engine and gives
  the contract columns (`donut`) that half of the panel ignores.
- **O3 — replace `layeredgraph` in the Network tab.** Killed: ranks and
  spline routing are the better reading of a DAG, and the System-graph chrome
  keeps the widget either way — the retirement would buy nothing and lose a
  reading.
- **O4 — gallery only.** Killed: it leaves ADR-0224 unexercised by anything
  with real data, which is the gap that record names.
- **A second pair of CTE names** (`nodes` / `links`) for the live panel.
  Killed for ADR-0166 §SD1's reason and for the user's: the same query should
  draw in both tabs without an edit.
- **Pins from the query** (`pin_x` / `pin_y` columns) and holding a dropped
  node. Deferred, not killed: it is the first thing to add once the panel has
  been used, and it raises a question this record does not have to answer —
  whether a drag-end position is published anywhere.
- **Per-edge force weight** (a heavy edge pulling shorter). The contract
  already carries `weight` on an edge and this panel spends it on width alone.
  The force step has no per-edge term; that is a graphview gap recorded in
  `doc/adr-background-work/netchart-graphview-gap-analysis.md`, and it is
  where this panel's `weight` will land when it grows one.
- **Node shapes.** The contract's `shape` is a layered-drawing vocabulary
  (box, ellipse, circle); graphview draws circles only, which is a gap in the
  same analysis. The column is ignored here rather than approximated.

## Consequences

### Positive

- graphview gains an in-tree consumer with real data, which is what ADR-0224's
  verification plan is waiting on.
- One contract owner: a column added to the graph contract reaches both
  panels, and the resolvers cannot drift.
- Both readings of one result can be open at once, in one dock layout.
- With both tabs open the two CTEs still execute once.

### Negative

- A 30th dock tab, and a second entry in every place that lists the graph
  panes.
- The live panel animates while it settles, so a `play` window with the tab
  open costs a force step per frame until the simulation converges or is
  paused.
- Two panels read one contract and honour different subsets of it: `donut` is
  inert in the Network tab, `shape` is inert here.

### Neutral

- The Network tab is unchanged in what it draws and what it accepts; what
  moved out from under it is where the contract is resolved.
- The `Graph` binding and its retirement are untouched by this record
  (ADR-0224 §SD8).

## Verification plan

- **Lane.** Default `go test`: the hoisted contract's resolvers and build
  (dedup, endpoint synthesis, parallel collapse, caps, tone-over-group,
  weights) keep the tests they had, now against the neutral model; new tests
  for the id intern and its collision probe, the group-to-aura mapping, the
  donut column, the label budget and the model cache's fingerprint key.
- **Painted result.** A scene in the play screenshot tour, over the Network
  scene's query unedited — the comparison the second tab exists for — with a
  second capture that switches the auras on. The defaults of §SD4, §SD10,
  §SD11 and §SD12 were read off that capture rather than chosen ahead of it.
- **What would fail.** A contract regression shows in the shared build's
  tests and in both panels at once; an id-mapping regression shows as a
  published `selection_key` that is not a vertex id; a render or gesture
  regression shows in the tour capture and in driving the tab with egui-mcp.
- **Gap.** Gesture handling (drag, pan, wheel) is verified interactively, not
  headlessly — ADR-0224's own gap, inherited.

## Status

Proposed — awaiting review by p@stergiotis.

## References

- [ADR-0129](./0129-play-layered-graph-panel.md) — the contract this panel
  reads and the tab it sits beside.
- [ADR-0166](./0166-play-treemap-panel.md) — one contract, two panels; §SD1 is
  the move SD1 repeats.
- [ADR-0224](./0224-graphview-go-graph-widget-painter-lane.md) — the widget.
- [ADR-0097](./0097-play-reactive-query-graph.md) — channels, lanes and the
  early-cutoff fingerprint SD8 uses.
- [ADR-0167](./0167-layeredgraph-magnitude.md) — the `weight` channel SD5
  spends on the node radius.
- `doc/adr-background-work/netchart-graphview-gap-analysis.md` — where the
  deferred rows (per-edge force weight, node shapes) are inventoried.
