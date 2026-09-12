---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-09-12 as follow-on
> material for [ADR-0224](../adr/0224-graphview-go-graph-widget-painter-lane.md)
> and [ADR-0225](../adr/0225-graphview-navigation-layer-and-radial-layout.md).
> Nothing here is a decision. Provenance: no library source was read or
> decompiled. The inputs are the two vendors' public documentation as served on
> the compile date — the Cytoscape.js single-page reference, and the Ogma 6.0.9
> API reference and tutorials — read as HTML and quoted. Behaviour not stated in
> those pages is not asserted here; where this page describes what a reference
> does, the sentence it rests on is quoted. Effort figures are estimates, not
> measurements.

# Cytoscape.js and Ogma against graphview and nav

## 1 Question, scope and method

A gap analysis of the ZoomCharts NetChart instance API against the widget was
done first and acted on; its widget-local half is [ADR-0224
§SD12](../adr/0224-graphview-go-graph-widget-painter-lane.md) and §SD13, and
the navigation model it deferred is [ADR-0225](../adr/0225-graphview-navigation-layer-and-radial-layout.md).
This page asks a different question: what do Cytoscape.js and Ogma carry that
NetChart does not, and where would each piece land here.

The tree this is measured against is the [graphview
widget](../../public/thestack/imzero2/egui2/widgets/graphview/) and the
[nav package](../../public/thestack/imzero2/egui2/widgets/graphview/nav/)
beneath it. The classification follows the boundary ADR-0224 §SD1 draws — the
caller owns topology, the widget owns geometry — so every capability sorts into
one of four buckets: **covered** (an existing API is named), a **widget gap**
(a change inside graphview), a **helper gap** (a change in nav or a sibling
package above the widget), or **out of scope** with a reason.

**Skipped as UI bling or web-specific**, one line each:

- Cytoscape's animation API and easing catalogue, and Ogma's `duration` /
  `easing` on every transformation and layout — animation as such.
- Cytoscape's stylesheet syntax (`cy.style()`, classes, CSS-like property
  names) and Ogma's `styleRules` — a text styling language for a DOM-shaped
  host; the property semantics are read below, the syntax is not.
- Image and document export on both sides — a separate lane here.
- Cytoscape's touch-gesture normalisation (the `vmousedown` / `tapstart`
  aliases) — the host normalises pointer input before it reaches a widget.
- `cy.mount()` / `cy.unmount()` / container sizing, and Ogma's Leaflet `Map`
  handle from `ogma.geo.getMap()` — DOM plumbing.
- Cytoscape's extension mechanism (`cytoscape.use`, registered renderers and
  layouts) — an extension point for a library shipped as a package to
  strangers, which is not this widget's situation.
- Ogma's `useWebWorker` and `gpu` layout flags — a substrate choice; the
  equivalent decision here is ADR-0224 §SD6 (goroutine-split rows, Barnes–Hut
  above a threshold).

## 2 Cytoscape.js

### 2.1 Compound nodes

The reference: "Compound nodes are an addition to the traditional graph model.
A compound node contains a number of child nodes, similar to how a HTML DOM
element can contain a number of child elements." Membership is data: "Compound
nodes are specified via the `parent` field in a nodes's `data`", and that field
"is normally immutable … However, you can move child nodes via `eles.move()`."
Geometry is derived: "A compound parent node does not have independent
dimensions (position and size), as those values are automatically inferred by
the positions and dimensions of the descendant nodes." Events follow the
containment: "All events that occur on elements get bubbled up to compound
parents and then to the core."

The reference is candid that the rest of the API does not follow: "traditional
graph theory functions like `eles.dijkstra()` and `eles.neighborhood()` do not
make special allowances for compound nodes, so you may need to make different
calls to the API depending on your usecase."

This splits cleanly along ADR-0224 §SD1. The `parent` field is topology — a
**helper gap**. The parent's inferred extent, its participation in layout, and
its being pickable as a unit are geometry — a **widget gap**, and the largest
one on this page. §4 takes it up with Ogma's version of the same idea.

The nearest thing graphview has is auras (ADR-0224 §SD11): a translucent blob
drawn beneath a set of nodes that share an id, with a legend. Auras are
declared per node (`NodeSpec.Auras`), so the membership half already has a
shape; but the ADR is explicit that "auras are paint only: they take no part in
layout or picking". A compound node is what an aura becomes when it does.

### 2.2 Selectors and collection algebra

Cytoscape's selector language matches on group (`node`, `edge`, `*`), class,
id, data (`[foo]`, `[foo = 'bar']`, `[^foo]`, `[?foo]`, `[!foo]`, comparisons,
and `[[degree > 2]]` for computed properties), and state (`:selected`,
`:locked`, `:grabbed`, `:parent`, `:child`, `:visible`, `:hidden`, `:orphan`),
with compound combinators for child and descendant. It reaches into arrays and
nested objects with dot notation — "get node with nested object property first
= 'Jerry'". Collections compose with `eles.union()`, `eles.difference()`,
`eles.intersection()` and `eles.symmetricDifference()`.

**Out of scope as a language.** A string DSL over `NodeSpec` would be a parser,
an escaping rule ("some characters need to be escaped for IDs, field names, and
so on"), and a reflection surface, to express what a Go predicate over a slice
expresses directly; and the state selectors have direct readers already —
`:selected` is `View.IsNodeSelected`, `:locked` is `View.IsPinned`, `:hidden`
is `Navigator.IsHidden`. Collection algebra over `[]uint64` is the standard
library.

One thing the language buys that a closure does not is a **serialisable** view:
a saved filter, a URL, a row in a table. That is worth wanting, and it is not
this analysis's to design — it belongs with whatever persists a view, not with
the widget.

### 2.3 Traversal and algorithms

Traversal: `eles.neighborhood()` "Get the neighbourhood of the elements",
`eles.connectedEdges()`, `eles.connectedNodes()`, `nodes.incomers()` "Get edges
(and their sources) coming into the nodes in the collection",
`nodes.successors()` "Recursively get edges (and their targets) coming out of
the nodes in the collection", `eles.roots()`, `eles.leaves()`, and
`eles.components()` "Get the connected components, considering only the
elements in the calling collection. An array of collections is returned, with
each collection representing a component."

Algorithms as API calls: `eles.bfs()`, `eles.dfs()`, `eles.dijkstra()` "This
finds the shortest paths to all other nodes in the collection from the root
node", `eles.aStar()`, `eles.bellmanFord()`, `eles.floydWarshall()` "This finds
the shortest path between all pairs of nodes", `eles.kruskal()` "returning the
minimum spanning tree, assuming undirected edges", `eles.pageRank()`, the three
centralities, four clusterings (`markovClustering`, `kMeans`,
`hierarchicalClustering`, `affinityPropagation`), and the connectivity trio
(`hierholzer`, `hopcroftTarjanBiconnected`, `tarjanStronglyConnected`).

**Traversal is a helper gap, small and worth closing.** `nav` already builds an
undirected adjacency with direction per entry and sorted ids (ADR-0225 §SD1)
and walks it every derivation; it publishes none of it. A consumer that wants
"select this node's neighbourhood", "fit the component this node is in", or
"size nodes by degree" has to keep a second adjacency beside the universe it
already fed in. Neighbours, degree, components and an unweighted shortest path
are the useful four; they are reads over a structure that exists.

**The analytics are out of scope.** PageRank, centralities and clustering
produce a number per node, and a number per node is something the caller
declares — a colour, a radius, an aura, a donut slice. This tree computes those
in the query lane and feeds the result to the widget; putting them in a viewer
package would put an analysis engine under a renderer. The one clustering
result that is not just an attribute is a *partition*, because a partition is
what grouping consumes; that dependency is noted in §4 and does not change the
verdict — a partition can arrive as data.

`scctree` already exists in the widget tree for strongly connected components,
which is the shape of answer this repo gives these questions.

### 2.4 Batch updates

"Allow for manipulation of elements without triggering multiple style
calculations or multiple redraws… When the specified callback function is
complete, only elements that require it have their style updated and the
renderer makes at most a single redraw", with `cy.startBatch()` /
`cy.endBatch()` "for asynchronous cases", and caveats: inside a batch "You can
not reliably read element style or dimensions".

**The problem does not arise.** graphview is declarative per frame (ADR-0224
§SD1): there is no retained element to mutate, so there is no per-mutation
invalidation to coalesce. `nav` has the analogous concern and answers it with
versions rather than a bracket — the derived visible set is cached against a
universe version and a state version (ADR-0225 §SD2), so a frame that changed
nothing costs a copy. The caveat Cytoscape documents — stale reads inside the
bracket — is the failure mode that design avoids by construction.

### 2.5 Viewport

`cy.fit()` "Pan and zooms the graph to fit to a collection" with an optional
padding "in rendered pixels" — covered by `View.FitNodes`, `View.FitNow`,
`Options.FitPadding`, and the one-shot fit latch of ADR-0224 §SD4. `cy.pan()`,
`cy.zoom()`, `cy.panBy()` — covered by `View.Camera` / `View.SetCamera`.
`cy.resize()` — not applicable; `View.Render` takes the canvas size each frame.

Two small **widget gaps**:

- `cy.center()` "Pan the graph to the centre of a collection". graphview can
  frame a subset, which changes zoom; it cannot centre on one at the current
  zoom. That is the motion a "go to this search hit" button wants.
- `cy.extent()` "Get the extent of the viewport, a bounding box in model
  co-ordinates that lets you know what model positions are visible in the
  viewport", returning `{ x1, y1, x2, y2, w, h }`. Derivable from
  `View.Camera` and `View.CanvasToWorld`; a reader saves every consumer the
  same four lines, and is what a caller needs to declare only what is on
  screen.

### 2.6 Events

The vocabulary maps onto ADR-0224 §SD12 almost entry for entry: `mouseover` /
`mouseout` to the hover-enter and hover-leave kinds, `grab` / `drag` / `free`
to the drag kinds, `cxttap` "normalised right-click or two-finger tap" and
`taphold` to the secondary-click kinds, `boxstart` / `boxend` / `boxselect` to
`Options.RectSelection`, `pan` / `zoom` / `viewport` to `Metrics.CameraMoved`,
and `layoutstart` / `layoutstop` to `Metrics.Settled` with
`ForceParams.PauseOnSettle`. `add` / `remove` / `position` / `data` have no
counterpart and need none: they report changes to state Cytoscape retains and
graphview does not.

The debounce Cytoscape documents on `onetap` — "triggers after a given debounce
time to first check for dblclick event" — has an equivalent already: graphview
lets the double-click take the second click, so a double-clicked node does not
also toggle its selection.

**Delegation and namespaces are out of scope.** `cy.on(events, [selector],
handler)` takes "A selector to specify elements for which the handler runs",
and a namespace ("`foo`" for "`tap.foo`") exists so one subscriber can
unregister its own handlers. graphview returns `View.Events` as a slice read
after `Render` (ADR-0224 §SD5); the switch over `Event.Kind` is the delegation,
and there are no handlers to unregister.

Bubbling is the exception, and it is not an event-model gap but a consequence
of §2.1: if a container lands, a click on a member must be attributable to the
container, or every consumer re-derives the membership to find out.

### 2.7 Data and scratch

"Only JSON-serialisable data may be put in `ele.data()`. For temporary data or
non-serialisable data, use `ele.scratch()`", the latter namespaced so that
"Extensions — like layouts, renderers, and so on — use `ele.scratch()`
namespaced on their registered name" and app code prefixes with an underscore
to avoid collisions.

**Out of scope, and the reason generalises.** Both exist because Cytoscape owns
the elements, so anything the caller wants to keep per element has to be kept
inside them, and a namespace convention has to keep two owners apart. Here the
caller owns its own data and hands the widget a `NodeSpec` per frame; `nav`
stores the spec as given and copies it per `Declare` (ADR-0225 §SD3), so there
is one owner and nothing to partition.

### 2.8 Layouts on a subgraph, and style mappers

"A layout runs on the subgraph that you specify. All elements in the graph are
used for `cy.layout()`. The specified subset of elements is used for
`eles.layout()`… You may use `eles.layout()` to address complex use-cases, like
running a different layout on each component."

**A widget gap, and it is the same gap as §2.1**: graphview has one
`Options.Layout` for the whole declaration. Laying a subset out on its own is
precisely what an open container needs, and Ogma reaches the same place from
its own direction (§3.2). Ranked with grouping, not separately.

Edge weighting — "there is generally an option to set a weight to each edge to
affect the relative edge lengths" — is covered by `EdgeSpec.Length` and
`EdgeSpec.Strength` (ADR-0224 §SD13). The `preset` layout, which "puts nodes in
the positions you specify manually", is `View.SetNodePosition` and
`View.Positions`.

Style mappers: `data(descr)` maps a property to a data field, and
`mapData(weight, 0, 100, blue, red)` "maps an element's weight to colours
between blue and red for weights between 0 and 100… Elements whose values fall
outside of the specified range are mapped to the extremity values."

**Covered.** `nav`'s `Options.Style` runs per visible node with a `NodeInfo`
and a pointer to the spec, which is the general case of both; the linear colour
ramp itself is next door, in the
[colorscale widget](../../public/thestack/imzero2/egui2/widgets/colorscale/) and the
[colormap widget](../../public/thestack/imzero2/egui2/widgets/colormap/).

## 3 Ogma

### 3.1 The transformation model

"Transformations allow to change the structure of the graph based on rules."
The unit is an object, not a call: "A transformation object is the outcome of
any Ogma transformation operation: the object controls the state of the
transformation, so that it is possible to toggle it, control its execution and
destroy it." The object carries `enable()`, `disable()`, `toggle()`,
`isEnabled()`, `refresh()` and `destroy()` ("After this method is called, the
transformation is not manipulable anymore"), plus `whenApplied()` which
"Returns a Promise that resolves the first time the transformation is applied."

They compose as an ordered list. "Transformations are run sequentially", and
the order is addressable: `transformation.setIndex(index)` — "Set the index of
the transformation in the pipeline. The transformation with the lower index is
applied first, the one with the higher index is applied last" — beside
`getIndex()` and `ogma.transformations.getList()`, which "Returns the list of
transformations applied to the graph."

This is the concept of the two libraries most worth taking, and it is **a
helper gap that generalises what `nav` already is**. ADR-0225 §SD2 chose to
make the visible set "a function of the universe and a small state, never an
edit log", which is the same premise as Ogma's: the data graph is untouched and
the displayed graph is derived, so disabling an operation restores the picture
without unwinding anything. What `nav` does not have is the *plural*: its
derivation is one walk selected by `Options.Mode`, where Ogma's is an ordered
list of named, independently toggleable operations over the same untouched
data.

Generalising the derivation into stages does not cost the property ADR-0225
bought. The stage list is state; the derived declaration stays a pure function
of universe plus state; the version cache still holds. The visible-set walk
becomes one stage among several, and filtering, grouping and collapsing become
stages beside it rather than four more fields on `Options`.

Two details worth carrying over and one worth not. Worth carrying: an operation
has an identity that outlives its effect, so a UI can list what is applied and
toggle each; and the order is explicit rather than implied by call sequence.
Worth not: the `Promise`-shaped lifecycle. Every stage here runs inside
`Declare` on the render path, where ADR-0225 §SD5 already refuses to let
anything block.

### 3.2 Node grouping, closed and open

Closed grouping first: "Nodes can be grouped together to simplify the
visualization of the network: this transformation can group nodes according to
a criteria, calculate compound properties at the group level and create a
hierarchy of nested groups reducing the clutter in the visualization." The
options are a `selector`, a `groupIdFunction` ("If no selector is specified,
all nodes in the graph are selected by default"), a `nodeGenerator` returning
the group node's id and attributes, an `edgeGenerator`, and
`separateEdgesByDirection`. The API reference adds the update contract: "The
groups are automatically updated when original nodes are added, removed or when
their data is updated." Ungrouping is `disable()` or `destroy()`, and "When
ungrouping the nested nodes will get their original position, with an offset
based on the grouped node current position."

Then open grouping, which Ogma calls visual grouping: "Ogma offers a way to
group nodes without hiding them. That can be very useful to work with the
'meta-graph' and arrange it in clusters, while still having access to the
contents of the clusters." It is the same transformation with more fields —
`showContents`, a per-group predicate, and `onCreated`, "which allows you to
apply positioning to the contents of the group, i.e. run a layout or set
pre-calculated positions". The layout rule is stated plainly: "By default, all
the layouts will ignore the nodes that are grouped. But, if you pass them to
the layout explicitly, the layout would be applied on them." Around it sit
`ogma.transformations.layoutGroups`, which runs "layouts on the groups that are
specified in the arguments" and takes a `propagate` flag to "re-layout the
parent nodes of the groups, if false, simply recompute the radius and position
of the parents", and `getXYR`, which "Returns the x, y and radius values of the
nodes during transformations update. This method is useful to create your own
custom layout with multilevel open grouping." `expandGroup` and `collapseGroup`
move between the two states, repositioning neighbours "following the fisheye
algorithm".

The two halves land in different packages, and §4 is the argument. Ogma also
records the cost of the materialised form in the entry for a fourth variant,
`addNodeClustering`: it is "Middleware on `addNode` `addEdge` which allows to
cluster the graph without adding the subNodes and subEdges to it. This is much
faster than Grouping as the graph remains small. Switching between
clustered/unclustered state is though slower than with the Grouping."

### 3.3 Edge grouping

"Group the edges that are parallel and match the same group id together", with
the same automatic-update contract, a `groupIdFunction`, and a `generator`
whose documented example sums the members' widths into the group edge's width.
The constraint is explicit: "Note edge grouping applies only for each set of
parallel edges: it is not possible to group together edges with different
ends."

**A helper gap that needs nothing new from the widget.** For a closed group the
aggregate is an ordinary declared edge between two ordinary declared nodes:
`EdgeSpec.Id` scopes parallel edges to an ordered pair and `EdgeRef{From, To,
Id}` keys hover, click and selection (ADR-0224 §SD13 and its 2026-09-12
update), while `EdgeSpec.Width` and `EdgeSpec.Label` carry the weight and the
count. Ogma's own scope restriction — parallel edges only — is the same scope
`EdgeSpec.Id` already has.

The exception is the open group, and it is a geometry problem, not an edge
problem: an aggregate edge into an open container has to terminate on the
container's boundary rather than on a node centre. §4 states what that implies.

### 3.4 Filters

"Hide all nodes for which the specified function evaluate to false. The filter
is applied as long as it's not removed and is updated when nodes are
added/removed/their data is updated. Also hides the edges adjacent to the
hidden nodes." The edge form is the same with "Also hides the edges adjacent to
the hidden edges"; the tutorial adds the polarity — "Criteria used in the
transformations above must return true for nodes/edges to show and false for
nodes/edges to hide" — and shows a criterion reading both endpoints.

**A helper gap, small.** `nav` hides by id: `Hide`, `Close`, `IsHidden`,
`HiddenNodes`, with hidden nodes acting as a wall the walk does not pass
(ADR-0225 §SD2). It has no predicate that re-evaluates as the universe changes,
which is what a facet control or a search box wants. Ogma's adjacent-edge rule
is already `nav`'s: the edges declared are the universe edges with both ends
visible.

### 3.5 Node collapsing

"Hide the nodes that match the specified selector. For each of the hidden
nodes, create an edge between each pair of adjacent nodes of this node. In
essence, it's transforming the graph such as (A -> B -> C) becomes (A -> C),
with B being a node that matches the selector." The generated edge is styled
through an `edgeGenerator` that receives the hidden node and both sides, and
returning `null` from it suppresses that edge. The reference states its own
limit: "Note that this will throw in the case of multiple collapsed nodes, for
instance a daisy chain of nodes like (A -> B -> C -> D) where B and C are
collapsed."

**A helper gap, and the one with the most obvious use here.** A great deal of
what this repository would draw is bipartite or near-bipartite — a record and
the things it joins through — and collapsing the join node is what turns a
storage model into a picture a reader recognises. The generated edge needs an
id, which `EdgeSpec.Id` supplies; the widget needs nothing.

A derived model can also define the case the reference rejects: with collapse
expressed as reachability through collapsed nodes rather than a one-hop
rewrite, a chain resolves instead of erroring, at the cost of a walk over the
collapsed set. That is a design note for whoever writes it, not a claim about
what exists.

### 3.6 Neighbour generation and merging

Generation: "For each node that match the selector, create one or more neighbor
nodes, each identified by a string. If multiple nodes give the same string
identifier, they will all share the same neighbor." Returning `null` skips one,
an array makes several, and `nodeGenerator` / `edgeGenerator` style what
appears. Merging is the inverse: "Hide the nodes that match the selector, and
add the data properties specified by `dataFunction` to their neighbors. When
the transformation is destroyed or disabled, the data of the affected nodes is
restored." The framing in the tutorial is the useful part — these move a fact
between being an entity and being an attribute.

**Helper gaps, low leverage.** Both are functions of the universe alone: a
caller can compute them once when it feeds `AddNodes` / `AddEdges` and get the
same picture. That is what separates them from grouping and filtering, which
have to recompute as the visible set moves and are therefore worth having
inside the derivation. If the pipeline of §3.1 exists they are cheap stages to
add; without it they are not a reason to build anything.

### 3.7 Drill-down and virtual properties

`addDrillDown` adds "the ability to replace a node by a group of nodes and
edges. For instance, when double click on a node, you can can call
`drilldown.drill(clickedNode)` to replace the clickedNode by its children."

**Covered**, and by the piece of `nav` that was designed against the same
problem: a stub is "a node known only as a neighbour" whose expansion is a
request to load it, the id lands on `Navigator.Pending`, the caller answers
with `AddNodes` / `AddEdges`, and `Navigator.Apply` wires the double-click
(ADR-0225 §SD4, §SD5). ADR-0225 also records why the callback shape was
rejected, and Ogma's `Promise`-returning variant is the shape it rejected.

`addVirtualProperties` — "Add (or overwrite) some data properties to the
specified nodes and edges. When the transformation is disabled/destroyed, the
old data is restored" — is **covered** by `nav`'s `Options.Style`, which styles
a copy per `Declare` (ADR-0225, 2026-09-12 update) so there is nothing to
restore.

### 3.8 Layout lifecycle and options

Ogma's layouts are batch: each returns a `Promise`, takes `nodes` and `edges`
subsets ("If nothing provided, the whole graph will be used"), `onSync`
("Function called every time the graph is updated") and `onEnd`, and `locate`
("Center on the graph bounding box when the layout is complete"). graphview's
model is the opposite and deliberately so — a per-frame simulation with
`ForceParams.PauseOnSettle`, `View.FastForward`, `Metrics.Settled` and the fit
latch of ADR-0224 §SD4 — so the lifecycle itself is **covered by a different
model**, and `autoStop` ("Stop layout earlier if the algorithm decides that it
has converged to a stable configuration") is `PauseOnSettle` by another name,
as `theta` "Theta parameter of the Barnes-Hut optimization" is
`ForceParams.Theta` and `edgeWeight` is `EdgeSpec.Strength`.

Four options inside the layouts are not covered:

- **Node mass.** `nodeMass`, "Use this getter to assign individual node
  masses", with the caution "Avoid very small masses, as it can lead to
  numerical instability". graphview weights edges per edge (ADR-0224 §SD13) but
  every node repels identically. A **widget gap**, small, and the natural
  partner of a radius that already varies per node.
- **Overlap removal.** `elasticity`, "Node collision elasticity. Smaller values
  may result in incomplete node overlap removal. Passing 0 will skip that
  algorithm pass altogether", with `radiusRatio` "used to allow for small gaps
  between the nodes while avoiding the overlapping". graphview has no such
  pass, and nodes of unequal radius — more so with a donut ring around them
  (ADR-0224 §SD9) — can settle on top of one another. A **widget gap**, and the
  one that changes how a dense picture reads.
- **Hierarchy constraints.** A per-node `layer` property — "With the layer
  property we're forcing the algorithm to assign the layering structure" — plus
  `roots` and `sinks`, which "will put the node n3 at the top of the hierarchy,
  while n1 at the bottom". graphview's hierarchical layout takes `RowDist`,
  `ColDist`, `CenterParent` and `Orientation` and derives the layering itself.
  A **widget gap**, small, and useful wherever the layering means something to
  the domain rather than to the algorithm.
- **Incremental placement.** `incremental` and `margin`, which "will apply
  Force layout to the group and place the resulting configuration in the
  closest available position, maintaining a margin" — **covered** by
  `placeNear`, which seats a newly declared node beside a placed neighbour and
  is what makes `nav`'s one-hop growth not re-scatter the picture.

Of the layouts themselves, `concentric` — "This layout takes a base node as
parameter and organizes the graph so the nodes close to the selected node are
close to it spatially", with `circleHopRatio` and a `sortBy` naming "'radius',
'degree' or custom data attributes" — is `LayoutRadial` with `RadialParams`
(ADR-0225 §SD6), which orders children by id for determinism rather than by a
chosen key. `grid` and `sequential` have no counterpart and are not worth one.

### 3.9 Geo mode

"Geographical mode API: allows to display nodes which have geographical
coordinates (latitude and longitude) on a map." Around that: `enable`,
`disable`, `toggle`, `enabled`, `setView(latitude, longitude, zoom)`,
`getUnprojectedCoordinates` "Returns underlying X and Y positions for the nodes
that are currently handled by the geo-mode", `resetCoordinates` "Reset
geographical coordinates of the nodes to the initial values", and `runLayout`,
a "Helper method to get the geo coordinates for the nodes that don't have them,
if you want to position them on the map in a specific way using one of the
layouts". Grouping has a geo-specific sibling, `addGeoClustering`: "Group nodes
in geo mode. Works in a similar way as `addNodeGrouping` but only in geo mode.
The nodes get grouped depending on the distance between each other in Mercator
projection."

Structurally this is three things: a camera whose world-to-screen transform is
a map projection rather than an affine zoom and pan, a layout that places only
the nodes without coordinates, and a grouping keyed on projected distance.

This tree already holds the map. [The portolan widget](../../public/thestack/imzero2/egui2/widgets/portolan/)
is the Leaflet core ported to the painter lane (ADR-0204), with its own
projections, view state, gesture handlers and vector overlay;
[the basemap package](../../public/thestack/imzero2/egui2/widgets/basemap/) resolves the
tile server; [the worldmap widget](../../public/thestack/imzero2/egui2/widgets/worldmap/)
covers the offline case. So the question is composition, not capability, and it
has a light cut and a heavy one.

The light cut needs no change anywhere: project each node's coordinates to the
map's world units, declare it with `NodeSpec.Pinned` and its projected
position, set `Options.NoZoomAndPan`, and drive both widgets from the map's
view. The force step leaves declared pins alone (ADR-0224 §SD10), so the graph
follows the map. What it does not give is one shared canvas, so hover, picking
and gestures belong to whichever widget owns the region.

The heavy cut is a **widget gap**: an injectable camera, so graphview's
world-to-screen transform can be supplied by portolan instead of by
`View.SetCamera`. That is a contract between two widgets and a change to the
piece ADR-0224 §SD4 specified, which makes it its own decision rather than a
line item here. `addGeoClustering` needs nothing of its own — it is the
grouping stage with a distance predicate.

## 4 Where grouping goes

Both references arrive at the same feature from opposite ends, and the two ends
land on opposite sides of the ADR-0224 §SD1 line.

**Closed grouping belongs in `nav`, as derived state, and it is the same kind
of thing as an expansion.** A group definition — a membership function or an
explicit partition, plus a per-group open flag — is a small state; the visible
set is a function of it and the universe; and opening a group is the inverse of
closing it by construction, which is exactly the property ADR-0225 §SD2 was
written to get and the property Ogma's `disable()` / `enable()` pair also has.
What it declares is ordinary: one node per closed group, one aggregate edge per
group pair, and the members simply not declared. The widget does not change.
Nested groups are the same state one level deeper, and `nav` already derives a
depth.

**Open grouping is a different kind of thing and does not belong in `nav`.** It
asks for a container whose extent is derived from where its members happen to
be — Cytoscape: "automatically inferred by the positions and dimensions of the
descendant nodes" — which is geometry, and geometry is the widget's under §SD1.
`nav` cannot compute it without owning positions, which ADR-0225 explicitly
refused when it rejected putting the radial layout in the helper.

Concretely, an open container asks graphview for four things, and the list is
the cost estimate for that gap:

1. A container declared with a member set, whose world extent is derived from
   its members each frame and repainted as they move.
2. Layout in two levels: members laid out within the container, the container
   participating in the outer layout as a body with the derived extent. This is
   Cytoscape's `eles.layout()` on a subgraph and Ogma's `layoutGroups` with its
   `propagate` flag, reached independently.
3. Picking and event attribution: a click inside the container but not on a
   member is the container's, and a click on a member is the member's with the
   container known — Cytoscape's bubbling rule, which exists for this.
4. Edge termination on the container boundary, for the aggregate edges of §3.3.

Auras are the useful starting point for (1): the machinery that turns a set of
member positions into a filled outline each frame is ADR-0224 §SD11, already
built, already cached against camera and drift. What auras deliberately do not
do is (2), (3) and (4). "A compound node is an aura that participates in layout
and picking" is close enough to be worth saying out loud, and it is also the
warning: the field-and-contour shape was chosen for a blob that costs nothing
to be wrong about, and a container that edges terminate on and hit-tests
resolve against may want a plainer shape.

**What edge aggregation over a group needs from the widget: for a closed group,
nothing.** `EdgeSpec.Id`, `Width` and `Label` already carry a distinct
identity, a weight and a count for an aggregate edge between two ordinary
nodes, and `EdgeRef` keys its events. An edge spec field naming a count or a
member list would buy only default styling that the caller can set itself. For
an open group it needs item (4) above and nothing else.

## 5 The gaps, ordered by what they let a consumer build

Effort is a rough estimate in days, including tests; the two large entries
would want an ADR first.

1. **Closed grouping in `nav`** — group definitions as navigation state,
   members undeclared, one aggregate node per group and one aggregate edge per
   group pair, nesting, and open/close as inverses. Turns a graph too dense to
   read into one that can be drilled into, with no widget change. **3–4 days.**
2. **The transformation pipeline in `nav`** — the derivation as an ordered list
   of named, individually toggleable stages over the unchanged universe, the
   present visible-set walk being one of them (§3.1). On its own it changes
   nothing a consumer sees; it is what makes items 1, 4 and 5 compose instead
   of accumulating as fields on `Options`. Worth doing before or with item 1.
   **3 days**, plus roughly a day per stage moved onto it.
3. **Open containers in graphview** — the four-item list in §4. The largest
   item here and the one that unlocks the "meta-graph you can still see into"
   picture both references treat as the payoff of grouping. **6–9 days**, and a
   decision record of its own.
4. **Predicate filters in `nav`** (§3.4) — a facet or search box that keeps
   working as the universe grows. **1 day** on the pipeline, 2 without.
5. **Node collapsing in `nav`** (§3.5) — the join-node-to-edge rewrite, which
   is how a storage model becomes a domain picture. **1.5–2 days.**
6. **Adjacency and graph queries from `nav`** (§2.3) — neighbours, degree,
   components, unweighted shortest path, over the adjacency the package already
   builds. **1.5–2 days.**
7. **Layout readability: overlap removal and per-node mass** (§3.8) — the two
   force-step options that matter once radii vary. **2–2.5 days** together.
8. **Hierarchy constraints: per-node layer, roots, sinks** (§3.8). **1.5 days.**
9. **Viewport readers: centre-on-subset and visible extent** (§2.5).
   **0.5 days.**

Deferred, with the trigger rather than an estimate:

- **Geo mode** (§3.9). The light cut needs nothing; the heavy cut is an
  injectable camera shared with portolan, which is a contract between two
  widgets and wants its own ADR. Trigger: a consumer that needs one canvas to
  own both the map gestures and the graph picking.
- **Neighbour generation and merging** (§3.6). Cheap stages once item 2 exists,
  and computable by the caller before `AddNodes` until then.
- **A serialisable selector expression** (§2.2). Belongs with whatever persists
  a view, not with the widget.

## 6 Not worth doing, with the reason

- **The selector language** (§2.2) — a parser and an escaping rule for what a
  Go predicate expresses directly; the state selectors have readers already.
- **Event delegation and namespaces** (§2.6) — the event slice and a switch on
  `Event.Kind` are the delegation; there are no handlers to unregister.
- **Batch updates** (§2.4) — no retained element to mutate, so no invalidation
  to coalesce; `nav`'s version cache covers the analogous concern.
- **Element data and scratch** (§2.7) — the caller owns its data; there are not
  two owners to keep apart with a namespace convention.
- **Centralities, PageRank and the clusterings** (§2.3) — a number per node is
  something the caller declares, and this tree computes numbers in the query
  lane. A clustering that produces a partition feeds §4 as data.
- **`grid` and `sequential` layouts** (§3.8) — no use here that the five
  existing layouts do not cover.
- **`alignSiblings` and similar post-passes** (§3.8) — readability polish whose
  value is hard to judge without a consumer asking for it.

## 7 References

- [ADR-0224](../adr/0224-graphview-go-graph-widget-painter-lane.md) — the
  widget, the declaration contract (§SD1), the interaction surface (§SD12,
  §SD13), auras (§SD11), pins (§SD10), the camera (§SD4).
- [ADR-0225](../adr/0225-graphview-navigation-layer-and-radial-layout.md) — the
  navigation layer, the derived visible set (§SD2), the style hook (§SD3), the
  stub protocol (§SD5), the radial layout (§SD6).
- [ADR-0204](../adr/0204-leaflet-map-core-port.md) — the map core the geo-mode
  discussion composes against.
- [netchart-aura-analysis.md](./netchart-aura-analysis.md) — the earlier
  black-box analysis of the same problem space.
- Cytoscape.js documentation, `https://js.cytoscape.org/` — the single-page
  reference, read 2026-09-12. Sections quoted: Compound nodes, Selectors,
  Collection (traversing, algorithms), Core (batch, viewport, style), Events,
  Layouts.
- Ogma documentation, `https://doc.linkurious.com/ogma/latest/` — version
  6.0.9, read 2026-09-12. Pages quoted: the Transformations API tutorial, the
  Layouts tutorial, and the API reference for `Ogma.transformations`,
  `Transformation`, `Ogma.layouts` and `Ogma.geo`.
