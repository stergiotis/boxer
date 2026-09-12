---
type: adr
status: accepted
date: 2026-09-12
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-12
---

# ADR-0225: graphview navigation — a visible subset over a larger graph, and a radial layout

## Context

[ADR-0224](./0224-graphview-go-graph-widget-painter-lane.md) §SD1 makes the
caller declare the full node and edge set every frame and the widget own
only geometry. A gap analysis of the ZoomCharts NetChart instance API
against the widget (recorded in §SD12) found one gap that is not a missing
method but a missing model: the reference chart owns a *visible subset* of
a larger graph. The user starts from a few nodes and grows the picture by
expanding a node's neighbourhood to a depth, shrinks it by collapsing or
hiding, and in the "focus" mode keeps a short list of focus nodes whose
surroundings are shown to a radius, with the oldest focus dropped as new
ones arrive and a relevance that falls off with distance. Nodes may be
known only as neighbours of a loaded node, and asking to expand one is the
request to load it.

§SD12 deferred that model to a helper package above the widget: under SD1
it is a walk over data the widget never sees, and folding it into graphview
would give the widget a second, hidden node set beside the declaration. It
also deferred a radial layout — rings by hop distance around a focus node —
because it is the layout that model wants and none of the four ported
layouts gives it.

Two constraints from the tree carry over. The widget's `placeNear` already
seats a newly declared node beside a placed neighbour, so growing a subset
one hop at a time does not need help from the helper. And every layout in
graphview is deterministic, so the helper and the radial layout must be
too: the same universe, the same gestures, the same picture.

## Design space (QOC)

**Question.** Where does the visible-subset state live, and what is the
shape of the layout that presents it?

**Options.**

- **O1** — A helper package that owns a copy of the caller's graph and
  derives the declaration every frame from a small navigation state.
- **O2** — The same, but the helper keeps the visible set as explicit
  mutable state — expand adds nodes, collapse removes the ones it added.
- **O3** — Fold the model into graphview: a `Hidden` flag per `NodeSpec`
  and expand/collapse methods on the view.
- **O4** — No package; a howto that shows the caller how to run its own
  breadth-first walk.

**Criteria.**

- **C1** — Keeps SD1: one source of topology, the declaration.
- **C2** — Determinism and testability without a client.
- **C3** — Collapse, hide and unfocus compose without bookkeeping the
  caller can get wrong.
- **C4** — Cost per frame at a few thousand visible nodes.
- **C5** — Work for a consumer to adopt.

**Assessment.**

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | ++ | −− | ++ |
| C2 | ++ | +  | +  | −  |
| C3 | ++ | −  | −  | −− |
| C4 | +  | ++ | ++ | +  |
| C5 | ++ | ++ | +  | −− |

O2 is the reference chart's own model and the natural first draft, and it
is where the reference's documented oddities come from — "collapsing hides
the collapsed node and all surrounding nodes, except for the very last
visible node" is the sound of a reveal set being unwound in the wrong
order. Deriving the visible set from a small state (O1) makes collapse the
inverse of expand by construction and costs one bounded walk per change,
cached until the next one. O3 breaks the one rule the widget is built on.
O4 leaves every consumer to rediscover the same walk and the same
tie-breaks.

## Decision

We build **`widgets/graphview/nav`**, a Go package above graphview that
owns a universe of nodes and edges the caller feeds, keeps a small
navigation state, and derives from the two the declaration graphview
renders. And we add a **radial layout** to graphview, rings by hop distance
around a set of centres, as a fifth static layout beside the four of
ADR-0224 §SD2.

**SD1 — The universe is a copy the caller feeds, keyed like the
declaration.** `AddNodes` takes `nav.Node{Spec graphview.NodeSpec, Stub
bool}` and `AddEdges` takes `graphview.EdgeSpec`; `RemoveNodes`,
`RemoveEdges` and `Clear` take them away. The specs are stored as given and
declared as given, so colour, radius, donut, auras and pins are the
caller's to set once rather than per frame. A **stub** is a node known only
as a neighbour: it can be shown and it can end a walk, but expanding it is
a request to load it (SD5). Adjacency is kept undirected with direction
per entry, ids sorted, so a walk visits neighbours in a fixed order.

**SD2 — The visible set is a function of the universe and a small
state, never an edit log.** The state is: the *roots* (the initial nodes
plus every `Show`), the *expanded* set with each entry's depth and
direction, the *hidden* set, and the *focus list* in arrival order with a
relevance each. The mode selects the function:

- *show-all*: every node not hidden.
- *manual*: the roots, plus every node within `depth` hops of an expanded
  node walking edges in that entry's direction — out, in or both — with
  hidden nodes neither shown nor walked through; minus the hidden set.
- *focus*: for each focus node, every node within its radius — the
  configured radius for the newest focus, a tail radius for the older ones
  — walking both directions and not through hidden nodes; minus the hidden
  set. A node's **relevance** is the largest `r_f · decay^distance` over the
  focus nodes that reach it, with `decay` a parameter (default one half)
  and `r_f` the relevance the focus was given (default one).

`Expand(id, depth, dir)` adds or replaces an expanded entry; `Collapse(id)`
removes it, and every node only that entry reached leaves the picture,
which is the inverse the reference could not promise. `Hide` and `Show` add
to the hidden and root sets and remove from the other; `Close` is collapse
plus hide. `Focus(id, relevance)` appends to the focus list and, past
`MaxFocusNodes`, drops the oldest unless auto-unfocus is off; `Unfocus` and
`ClearFocus` remove. `Reset` returns the state to the roots the universe
was opened with. The edges declared are every universe edge with both ends
visible. The result is cached against a universe version and a state
version, so a frame with no change costs a slice copy and a frame with one
costs one walk bounded by what it reveals.

**SD3 — The declaration is materialised by the helper, styled by the
caller.** `Declare` returns node and edge slices the helper owns, in
universe insertion order filtered by visibility, so graphview's batches
stay stable across frames. An optional `Style func(id, relevance, depth,
*NodeSpec)` in the options runs per visible node during `Declare`: the
reference's focus fade-out, a radius by relevance, a badge for a node with
unseen neighbours are all the caller's choice at that one hook, and the
helper draws nothing. Readers give what the hook and the app need:
`Visible`, `Relevance`, `Depth` (hops from the nearest root or focus),
`Expanded`, `FocusNodes`, and `HiddenNeighbours(id)` — the count of a
visible node's neighbours that are known and not shown, the number a "+"
badge carries.

**SD4 — Gestures are the caller's, with one default wired.** The helper
reads no input. `Apply(ev graphview.Event)` is the one convenience: a node
double-click expands a node that is not expanded and collapses one that is,
at the default depth and direction, in manual mode; in focus mode it
focuses. A consumer that wants a context menu or a different gesture
routes graphview's events itself.

**SD5 — Loading is a request, not a callback.** When a walk reaches a stub
that it would expand further — depth left in a manual expansion, radius
left in a focus walk — the stub's id goes on a **pending** list the caller
reads after `Declare`, loads by whatever transport it has, and answers
with `AddNodes` (the same id, `Stub` false) and `AddEdges`. The universe
version moves, the next `Declare` walks again, and the picture grows. The
helper never blocks, never calls out, and never knows where data comes
from; a caller that loads nothing simply shows stubs at the frontier.

**SD6 — The radial layout is graphview's fifth static layout.**
`LayoutRadial` with `RadialParams{Centers []uint64, RingDist float32}`
lays every node on ring `d · RingDist`, with `d` the undirected hop
distance from the centre set. One centre sits at the origin; several sit
on ring one, spread evenly, under a virtual root. The breadth-first tree
from the centres, children in id order, decides angles: a subtree is
allotted an angular sector proportional to its leaf count, children share
their parent's sector in order, and a node sits on its ring at the middle
of its sector — the classic radial tree, so parents and children stay
angularly close and crossings stay few. A component the centres do not
reach gets its own centre — its node of highest degree, smallest id on a
tie — and its own system, packed to the right of the last one like the
hierarchical layout packs its forest. With no centre given, the whole
graph takes the highest-degree node. Like the hierarchical layout it
re-runs on a topology or parameter change, is a function of the topology
alone, and leaves declared pins where they are. The navigation helper
does not know the layout exists; a consumer that wants the reference's
focus picture sets `Centers` to `FocusNodes()`.

**SD7 — Home and provenance.** Package `nav`, a directory beneath the
[graphview package](../../public/thestack/imzero2/egui2/widgets/graphview/),
importing graphview for the spec and event types and nothing else; the
radial layout is a file in graphview. The navigation modes and their parameter names follow the
ZoomCharts NetChart settings reference so a reader of one recognises the
other; the derivation of the visible set from state is this design's own,
and the radial angle allotment is the standard radial tree drawing
(Eades, 1992).

## Alternatives

- **O2 — an explicit visible set edited by expand and collapse.** Killed
  for C3: collapse has to remember what expand revealed and in what order,
  and two expansions that reveal the same node make the bookkeeping a
  reference count the reference chart visibly gets wrong.
- **O3 — a `Hidden` flag on `NodeSpec` and expand/collapse on the view.**
  Killed: the widget would hold topology the declaration does not carry,
  against ADR-0224 §SD1, and the walk needs the universe the widget never
  sees.
- **O4 — a howto instead of a package.** Killed: the walk, the caching and
  the stub protocol are the same in every consumer, and the tie-breaks
  that keep a capture stable are exactly the part a hand-rolled walk
  forgets.
- **A callback for loading (`dataFunction`).** The reference's shape.
  Killed for SD5: a callback from inside `Declare` runs on the render path,
  where nothing may block, and it fixes the transport; a pending list
  leaves both to the caller.
- **Radial layout in the helper.** The helper would then own positions,
  which ADR-0224 gives to the widget. Killed for SD6.
- **A radial ring per relevance band instead of per hop.** Relevance is a
  focus-mode notion; the layout should not need the helper. Killed.
- **`minNumberOfFocusNodes`, `expandDelay`, `focusAutoFadeout`,
  `initialFocusNodeExpansionRadius`.** Deferred: the first is a guard a
  caller can keep, the second is animation, the third is the `Style` hook,
  the fourth a one-line special case of the radius. None changes the model.

## Consequences

### Positive

- A consumer gets the reference's exploration model over any graph it can
  feed, with collapse, hide and unfocus that compose, and no widget change.
- The visible set is a pure function: every gesture sequence is a unit
  test, and a capture of the demo is stable.
- Loading stays the caller's, so the helper works over an in-memory graph,
  a facts query or a remote service alike.

### Negative

- The universe is a second copy of the caller's graph, sized like it. A
  consumer with millions of nodes feeds the helper a neighbourhood, not
  the whole graph — which is what the stub protocol is for.
- A change recomputes the visible set with a walk bounded by what it
  reveals; a focus radius over a dense graph reveals a lot. The radius is
  the knob.
- The radial layout is static: a node dragged away stays until the layout
  re-runs, as in the hierarchical layout.

### Neutral

- graphview gains one layout value and one parameter struct; nothing in
  its declaration or event surface changes.

## Verification plan

- **Helper.** Unit tests over a small universe for every mode: expand and
  collapse as inverses, hide as a wall the walk does not pass, focus
  arrival order and auto-unfocus, relevance by distance with several
  focus nodes, stubs reaching the pending list once and leaving it when
  loaded, declaration order stable across changes, the cache reused when
  nothing changed. A property test that any sequence of gestures gives the
  same visible set as recomputing from the final state.
- **Radial layout.** Unit tests for ring radii by hop, angle sectors by
  leaf count, several centres on ring one, an unreached component packed
  beside, determinism across declaration order; the gallery demo gains a
  focus-mode section under the screenshot tour.
- **Scene.** A headless scene (ADR-0224's 2026-09-12 update) drives a
  double-click through `Apply`, renders the grown declaration and reads
  the pending list.

## Status

Accepted 2026-09-12. Implemented with this record; ADR-0224 carries a dated
update naming it for the radial layout.

## Updates

### 2026-09-12 — review corrections to the surface

Five things the first review of the implementation changed, recorded
against the body rather than by editing it.

- **The style hook is told the facts.** SD3's hook takes a `NodeInfo` —
  relevance, depth, hidden-neighbour count, stub, focused — beside the
  spec, so it has no reason to call back into the navigator; the
  derivation is published before any hook runs, so a hook that does call
  back reads a finished walk instead of re-entering it. The specs are
  copied and styled on every `Declare`, with the walk behind them cached,
  so a hook that reads outside state is current each frame.
- **Depth continues through an expansion.** A node an expansion reaches
  sits at the expanded node's depth plus its hops; the expanded node keeps
  its own. SD2's "hops from the nearest root or focus" now holds through
  chained expansions.
- **A hidden focus entry counts for nothing.** `Hide` keeps a focus entry
  for when the node is shown again, but the bound of SD2 counts the
  entries not hidden and evicts the oldest such; `Reset` focuses the
  initial nodes under the same bound, in order.
- **Several centres are spread by their subtrees.** SD6 said "spread
  evenly"; the layout allots each centre a sector proportional to its
  subtree's leaf count, as it does every node, and the sentence is
  corrected here rather than the code.
- **`FocusNodes` returns a slice**, the navigator's, oldest first, hidden
  entries included — the repo's iterator naming rule reserves the
  iterator form for a type's single canonical iteration, which this is
  not. The universe's edge index is a map, so bulk loads and removals
  stay linear.

### 2026-09-12 — the universe answers graph questions

§SD1 builds an undirected adjacency with the direction per entry and ids
sorted, and every derivation walks it; none of it was published. A consumer
that wanted a node's neighbours, its degree, the component it sits in or a
path to another node therefore kept a second adjacency beside the universe
it had already fed in. `Neighbours`, `Degree`, `Components` and
`ShortestPath` read the one that exists.

They answer over the **universe**, not the picture: a hidden node is still a
neighbour and a node no mode would show still has a degree, because these
are questions about the graph, where `Visible`, `Depth` and `Relevance` are
the questions about the view. Direction is a parameter, the `DirectionE` an
expansion already takes. Every result is ordered — ascending ids, components
by their smallest member, breadth-first ties broken by the adjacency's id
order — so each is a function of the topology alone, as §SD1 requires of
everything here.

Two deliberate departures from the package's conventions. The results are
the **caller's**, where `FocusNodes` and `HiddenNodes` lend the navigator's
slice: a query runs on a gesture rather than every frame, so an owned result
is worth more than the allocation it costs and is one less aliasing rule to
remember. And each query carries its own scratch rather than the walk's, so
one may run inside the `Style` hook while `Declare` is on the stack — which
is exactly where a consumer sizing a node by its degree, or dimming
everything outside a hovered node's neighbourhood, would put it.

What the queries deliberately leave out is the analytics: PageRank,
centralities and the clusterings produce a number per node, and a number per
node is something the caller declares. This tree computes those in the query
lane; putting them here would put an analysis engine under a renderer.

## References

- [ADR-0224](./0224-graphview-go-graph-widget-painter-lane.md) — the
  widget, its declaration contract (§SD1), and the deferral (§SD12).
- ZoomCharts NetChart settings reference, `navigation` and `layout`,
  `https://zoomcharts.com/developers/en/net-chart/api-reference/settings.html`
  — the modes and parameter names.
- Eades, *Drawing Free Trees*, Bulletin of the Institute for Combinatorics
  and its Applications 5, 1992 — the radial tree drawing.
