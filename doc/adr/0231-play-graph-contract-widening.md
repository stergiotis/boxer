---
type: adr
status: accepted
date: 2026-09-13
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-13
---

# ADR-0231: widening the graph contract — placement, emphasis, computed metrics and a read-back seam

## Context

`play` draws a result as a graph through one contract: the `edges` and
`vertices` CTEs of [ADR-0129](./0129-play-layered-graph-panel.md) §SD2, hoisted
by [ADR-0227](./0227-play-graphview-panel.md) §SD1 into a renderer-neutral
model that the Network tab and the Graphview tab both resolve against.

The contract carries identity, category and magnitude: `id`, `label`, `group`,
`shape`, `tone`, `weight`, and the Graphview-only `donut` pair. Measured
against `widgets/graphview`'s own per-row declaration, that is most of identity
and category, part of magnitude, and none of placement, emphasis or state. The
widget has since grown past the contract in five directions, none of which a
query can reach:

- the fade-and-inert pair, `Opacity` and `NoPick`
  ([ADR-0224](./0224-graphview-go-graph-widget-painter-lane.md) §SD14), which
  is what a caller spends a relevance or a search result on;
- placement — declared pins (§SD10), the soft pull (§SD16), an initial
  position (`SetNodePosition`), and the per-edge `Length` and `Strength` the
  force step applies (§SD13). ADR-0227 leaves the last of these unmapped
  because the mapping wants a scale decision — a weight is a magnitude, a
  strength a spring — that it declines to take;
- aura membership as a *set*. `NodeSpec.Auras` is a slice and §SD4 spends it
  on one value of `group`, so a node in two groups — the overlapping case
  auras exist for — cannot be declared, and the per-aura `AuraStyle` is
  unreachable;
- state the caller may set silently — `SelectNode`, `SelectEdge`, `FitNodes`
  (§SD12) — which is how a selection made elsewhere is shown here;
- the radial layout's centres ([ADR-0225](./0225-graphview-navigation-layer-and-radial-layout.md)
  §SD6), centre gravity, and `HideEdges`
  ([ADR-0230](./0230-neighbour-graph-and-neighbour-embedding-force-model.md)),
  each a property of the data with no spelling.

Three further gaps are not about columns:

- **The record behind a node is unreachable.** Detail consumes the row-index
  `selection`, and `selection_id` / `selection_key` are sidecars stamped onto a
  `selection` write. The graph panels deliberately never write `selection`
  (ADR-0129 §SD4) because their vertices come from a private lane rather than
  an observable split node, so they emit `selection_key` directly and
  `selection_id` never moves. Clicking a node reaches no leeway record; the
  only path today narrows a query and relies on the selection clamp landing on
  row 0, which is wrong whenever the narrowed result has more than one row.
- **The graph is write-only.** One outbound signal against the Map's six
  `vp_*`. A gesture's result — hover, multi-selection, a dropped node's
  position, the camera, which auras are hidden, a right-click — cannot be
  read back by the query that drew it. The signal store's value encoder
  accepts scalars only, so even a multi-selection has no wire form today.
- **[ADR-0229](./0229-graph-analytics-engine.md) §SD6 names this panel as the
  analytics engine's first consumer** — "a metric column source for node
  radius and tone" — and the engine is unwired. Degree, BFS distance,
  components, PageRank, cores, triangles, betweenness and cliques have no
  spelling, so a query that wants to size by PageRank or dim everything more
  than two hops from the selection computes it in SQL or does without.

The pressure is to widen without turning the contract into a settings dump.
The contract is read by two panels with different capabilities, and its columns
are the thing users write by hand.

## Design space (QOC)

**Question.** How does the graph contract widen to cover the widget's and the
engine's feature set without becoming a place where every knob is spelled
twice?

**Options.**

- **O1** — Per-row columns only: widen `vertices` and `edges`, leave everything
  else to chrome.
- **O2** — Three seams: per-row columns, a one-row `graph_opts` CTE for what is
  not per-row, and reserved `gv_*` signals for read-back.
- **O3** — One key/value `graph_opts` CTE carrying everything, per-row settings
  included, addressed by vertex id.
- **O4** — Chrome only: no contract change; every new capability is a control.

**Criteria.**

- **C1** — A knob lands where the knowledge is: the query knows what the reader
  cannot see, the reader knows their own preference.
- **C2** — Names are schema-visible, so completion and type-checking reach them.
- **C3** — What a new widget feature costs to expose.
- **C4** — The Network tab can ignore what it cannot draw (ADR-0227 §SD1).
- **C5** — A gesture's result can be read back by the query.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | +  | ++ | −  | −− |
| C2 | ++ | ++ | −− | n/a |
| C3 | −  | +  | ++ | −  |
| C4 | ++ | +  | −  | ++ |
| C5 | −− | ++ | −− | −− |

C1 and C5 decide it. O1 has no home for a knob that is a property of the whole
drawing rather than of one row — the layout a DAG wants, the metric a size
should carry — and pushes them to chrome, where the query cannot reach them.
O3 buys extensibility by giving up the thing that makes the contract usable:
a stringly-typed key/value table is invisible to completion, carries no type
checking, and makes a per-vertex setting a join the user writes by hand. O4
leaves the query unable to say anything the chrome does not already offer.

## Decision

The graph contract widens along three seams, under one rule about where a knob
belongs and one rule about what closes each seam, and the graph panels gain a
read-back seam and a leeway-id publish. It is implemented over
[ADR-0232](./0232-play-first-class-consumer-columnar-graphview-engine-vocabulary.md):
the columns below are handed to the widget as columns, the
metric names below are the engine's own vocabulary, and a result's NULL has
a widget spelling.

**SD1 — The rule, and who wins when both speak.** A knob belongs in SQL when
**the query knows something the reader cannot see**; it belongs in chrome when
it is **the reader's preference**. Placement, emphasis, relevance, category,
magnitude, selection, per-edge physics, the layout family and what the
picture is *of* (directed or not, edges as reading or as input) are the
query's. Damping, the Barnes–Hut angle, the integrator, zoom bounds, fit
padding, aura field shape, label mode, gesture modes (multi-select, rectangle
select) and style colours are the reader's, and are not spelled in SQL by
this decision or a later one — that is what the rule is for.

Where both can speak, the precedent is the Sankey panel's `stage`: **the query
sets the default and an explicit chrome setting overrides it**, with the
control's *auto* position meaning "whatever the query said". A control that has
been moved says so, so a reader can tell a drawing they changed from one the
query specified. Auras, `PinOnDrag` and the layout are the three controls this
gives an *auto* position to.

**The seams are closed by the widget's and the engine's vocabulary, not by
taste.** A per-row column exists where `NodeSpec` or `EdgeSpec` has the field;
a `graph_opts` column where `Options` has the knob and §SD1 gives it to the
query; a signal where the widget reports the gesture; a metric where `algo`
computes it. A feature the widget lacks — arrow decorations, dashes, a
caller-assigned hierarchy level — gets no column until the widget has it
(§Alternatives), because a column the panel cannot honour is a column that
lies. Where the review found the field missing and small, ADR-0232 adds it
first and this record spells it second.

**SD2 — Seam A: the per-row vocabulary widens.** New optional columns on the
existing CTEs, each claimed only when the name *and* the type fit — the
existing guard that keeps a column which merely shares a name from being spent
as a channel. An unclaimed column stays an ordinary result column. Two rules
the guard did not have to state before: a **`Nullable` column is claimed and a
NULL cell means "not declared for this row"** — which is how a query pins some
vertices and leaves the rest to the layout, and which reaches the widget as
the NaN of ADR-0232 §SD4 — and a **Bool column is claimed as Arrow's boolean
or as a `UInt8` 0/1**, whichever the server's Arrow output sends. Because
NULL is the unset value, **a declared zero is a zero**: `strength = 0` is an
edge that is drawn and does not pull, `opacity = 0` a node that is not
painted, which the row-shaped spec could not say.

On `vertices`:

| column | type | claim |
| --- | --- | --- |
| `opacity` | numeric 0..1 | fades the node's own paint; state rings stay full |
| `pick` | Bool | `false` takes it out of the pointer's reach; absent is pickable |
| `selected` | Bool | declared selection, applied silently on rebuild (§SD8) |
| `fit` | Bool | frame these vertices on rebuild instead of the whole graph |
| `radius` | numeric, world units | absolute size, winning over `weight`'s share (§SD12) |
| `label_always` | Bool | the label paints under the label budget, whatever the hover and selection |
| `pin_x` / `pin_y` | numeric, world units, both required | fixes the node; a drag moves it for the gesture only |
| `lat` / `lon` | numeric, both required | fixes it geographically (§SD3) |
| `start_x` / `start_y` | numeric, world units, both required | initial position, then free — what a stored layout is restored from |
| `pull_x` / `pull_y` | numeric, world units | a soft target per axis; naming an axis turns the pull on |
| `pull_strength`, `pull_strength_x` / `pull_strength_y` | numeric | the pull's strength, on the `CenterGravity` scale; per-axis wins over the shared one |
| `groups` | `Array(String)` | aura membership as a set (§SD4) |
| `donut_tones` | `Array(String)` | one tone family per ring slice, paired by index |
| `center` | Bool | a radial-layout centre (§SD5) |

On `edges`:

| column | type | claim |
| --- | --- | --- |
| `id` | any, read as text | tells parallel edges apart in hover, selection and events |
| `opacity`, `pick`, `selected` | as on `vertices` | the edge forms of the same three |
| `length` | numeric multiplier | the force step's ideal length for that edge |
| `strength` | numeric multiplier | its attraction; the affinity under the neighbour-embedding model |

`weight` keeps its meaning and continues to size — an edge's magnitude and its
physics are different claims, and a query that wants a heavy edge to also pull
shorter says so twice. `pull_x` alone holds a node near a column and leaves the
layout free to settle it vertically, which is how a level is expressed without
making it a placement; an exact per-axis pin has no widget form (ADR-0224
§SD16) and no column.

**SD3 — `lat` / `lon` are pins, not a map.** A located vertex is projected to
world units in Web Mercator at a fixed reference zoom, measured from a local
origin at the located set's centroid, and declared `Pinned`; unlocated vertices
are declared with no pin and the force step lays them out among the located
ones, which ADR-0224 §SD10 gives for free. This is a geographic *arrangement*,
not a basemap: hosting the graph inside `portolan`
([ADR-0228](./0228-hosted-canvas-rendering-and-graph-widget-sharing.md)) is a
different panel with different rules — the aura legend must go external,
`FitNow` and `SetCamera` are overruled, and the pointer claim has to be
arbitrated — and is deliberately **not** in scope here. The column names are
chosen so that panel can read the same contract when it is written.

The projection choice is the panel's and not the reader's: the identity-camera
alternative rescales the geometry a layout solves against, which the
graph-on-a-map recipe records as the reason unlocated nodes are flung about on
zoom. **A located graph reads back in its own units**: while any vertex is
located, the drag-end and camera signals of §SD8 are published in lat/lon
beside the world-unit forms, so the query that wrote `lat` / `lon` gets
`lat` / `lon` back.

**SD4 — Aura membership separates from the node fill.** `group` keeps both
jobs — it is the fill and, absent `groups`, the single aura — so no existing
query changes. `groups` declares membership as a set, and once it exists the
two questions are distinct: a fill wants one category per node, an aura wants a
set. `aura_by` (§SD6) is what says "colour by `group`, blob by `owner`".

Declaring `groups`, `aura_by` or an `aura_style` row is the query asking for
auras, so it switches them on as the default under §SD1; ADR-0227 §SD4's
off-by-default stays the rule for a query that declares only `group`, where
the reader still has to judge whether the grouping is spatial. The auras
control gains an *auto* position accordingly.

The aura palette stays the widget's own cycle rather than this contract's group
palette, for the reason ADR-0227 §SD4 records: the group palette is the
`*Subtle` background tones, chosen dark so one light ink reads on them, and a
translucent blob of one over the same dark panel is a blob nobody can see.
`aura_style` (§SD7) is the override for a query that means something specific.

**SD5 — Seam B: a one-row `graph_opts` CTE, columns rather than keys.** What is
a property of the whole drawing and still the query's under §SD1. All optional;
the first row is read and further rows are ignored with a note in the status
line, because a settings CTE that returned many rows is a query bug worth
saying out loud rather than a set of settings. The CTE is an ordinary split
node, so its cells may read signals — `{gv_zoom:Float64} < 0.3 AS hide_edges`
is a level of detail the query owns.

| column | type | meaning |
| --- | --- | --- |
| `layout` | String | `force` (default), `force_gravity`, `hierarchical`, `radial`, `random` |
| `orientation` | String | `top_down` (default) or `left_right`, for `hierarchical` |
| `ring_dist`, `row_dist`, `col_dist` | numeric, world units | the radial ring pitch; the hierarchical level and sibling pitches |
| `k_scale` | numeric | scales the ideal edge length |
| `gravity` | numeric | the centre pull's strength under `force_gravity` |
| `force_model` | String | `fr` (default) or `neighbour_embedding` (ADR-0230 §SD2) |
| `exaggeration` | numeric | the neighbour-embedding model's one knob |
| `hide_edges` | Bool | edges drive the layout and are neither painted nor picked |
| `undirected` | Bool | the picture paints no arrow heads and the metrics read the graph symmetrised (§SD6) |
| `pin_on_drag` | Bool | the default for §SD11's control |
| `size_by`, `tone_by`, `opacity_by`, `aura_by` | String | the encoding selectors of §SD6 |
| `distance_from` | String | the seed set of the seeded metrics: `selection` (default: the selected vertices plus `gv_focus`) or `hover` |

Columns rather than keys is the C2 kill-reason for O3: a column name is
schema-visible, so completion offers it and the claim can type-check it, and a
misspelling is an unclaimed column rather than a silently ignored key. A value
outside a column's vocabulary is refused with a reason in the status line and
the column falls back to its default.

`k_scale` and `gravity` are in rather than in chrome because they have to reach
the spread of whatever the query placed — a located graph needs a different
ideal length from a free one, a disconnected graph wants gravity and a
connected one does not — and the query is what knows which it wrote. The
integrator (`dt`, damping, epsilon, the step clamp) and the Barnes–Hut angle
stay out: they change how fast the same picture settles, not the picture.

**SD6 — Encoding selectors, and the analytics engine behind them.**
`size_by`, `tone_by`, `opacity_by` and `aura_by` each name either a column of
`vertices` or a computed metric. Naming a column spends that column on the
channel *instead of* the conventional one — `size_by = 'revenue'` sizes by
`revenue` rather than by `weight` — so a query encodes any of its columns on
any channel without renaming it. A leading `-` inverts an ordinal ramp
(`opacity_by = '-distance'`: the nearer, the brighter), the spelling `ORDER BY
-x` already has.

The metric vocabulary **is** the engine's: every name below is the string of
an `algo.MetricE` (ADR-0232 §SD6), its kind is that value's `Kind`, and its
undirected alias is its `Symmetric`. Play parses the selector and refuses
what the parser refuses; it keeps no table of its own.

| metric | kind | source |
| --- | --- | --- |
| `degree`, `in_degree`, `out_degree` | ordinal | `Degrees` |
| `pagerank` | ordinal | `PageRank`, iterated to tolerance under the iteration budget |
| `betweenness` | ordinal | `Betweenness`; exact at this panel's caps (§SD9 of ADR-0227), so the sampled path is never taken here |
| `kcore` | ordinal | `KCore` coreness |
| `triangles`, `clustering` | ordinal | `Triangles` per vertex; the local clustering coefficient derived from it and the degree |
| `clique` | ordinal | the largest maximal clique containing the vertex, from `MaximalCliques` under its output cap |
| `component`, `scc` | categorical | `ConnectedComponents`, `StronglyConnectedComponents` |
| `component_size` | ordinal | derived from `component` |
| `distance`, `distance_in`, `distance_out` | ordinal | `BFS` hops from the seed set of `distance_from` in each direction; unreached rows are absent |
| `relevance` | ordinal | `PageRank` with the seed set as its teleport set (ADR-0232 §SD7): the smooth form of `distance` |

Categorical metrics are labels, not quantities, so they are refused in
`size_by` and `opacity_by` with a reason in the status line; in `tone_by` they
take the group palette by distinct value and in `aura_by` an aura per value.
Every ordinal metric works in all four; in `aura_by` an ordinal groups by
distinct value, which is what makes `aura_by = 'kcore'` blob the k-shells. A
selector naming neither a column nor a metric is refused the same way, and the
channel falls back to what the row declared. Normalisation is the contract's
existing one — size by the square root of the share of the maximum, tone by
the ADR-0167 ramp — and opacity maps the share onto a fixed floor and 1,
because the widget has no spelling for fully transparent.

Under `undirected` the CSR is built symmetrised: `in_degree` and `out_degree`
read as `degree`, `scc` as `component`, `distance_in` and `distance_out` as
`distance`, and PageRank and betweenness run on the symmetric graph, while
the widget paints no arrow heads (ADR-0232 §SD5), so the picture is the
graph the numbers describe. Metrics are **unweighted** in this cut, as the
engine's are (ADR-0229 §SD7);
`weight` sizes and never enters a metric, which is also why the CSR
fingerprint — which excludes weights — is a sufficient cache key.

The engine ([ADR-0229](./0229-graph-analytics-engine.md)) is wired as that
record's §SD6 anticipated. The CSR is built from the model's edge list and
cached on its content fingerprint, and a metric is computed once per
(fingerprint, `undirected`, metric) rather than per frame — the declaration is
already rebuilt only when the lanes' fingerprints move, and the metric joins
that cache. The seeded metrics are keyed on their seed set as well and
recomputed when it moves: a BFS the engine runs in milliseconds at the cap,
and a seeded PageRank that is a few sweeps more. Because the model is sorted
by id (ADR-0232 §SD9), a metric column indexes the model's rows and the
widget's slots directly; no join is written. Budgets follow ADR-0229 §SD4: PageRank runs its
iteration budget or converges first, cliques stop at the output cap, and **a
truncated result is reported in the status line**, because a clique size
under a cap is a lower bound and a reader sizing nodes by it should know.

Computing synchronously on rebuild is the first cut. The draw side is capped
three orders of magnitude below the engine's operating point (ADR-0227 §SD9),
so the expensive metric on the largest graph the panel will draw is within the
rebuild it already pays for. If a measured graph exceeds that, the move is the
Projection panel's shape — a background goroutine owning the engine calls while
the render thread owns the widget — and not a smaller vocabulary.

**SD7 — Seam B, second CTE: `aura_style`, one row per aura.** `aura` names the
group; `tone`, `line_width`, `z_index`, `label`, `no_legend` and `hidden` give
that aura's look and its initial legend state. Per-group rather than
per-vertex, because that is what it is: a query that colours an aura says it
once, and saying it on every member is where the two disagree. An aura the
CTE does not name keeps the cycle colour. `hidden` is the query's default
for the legend toggle under §SD1: a reader's click overrides it and is read
back through `gv_aura_hidden`.
`tone` names a design-system family, as the contract's `tone` does, resolved
at the aura's alpha; a literal colour is not accepted, for
[ADR-0156](./0156-qualitative-palette-dark-surface.md)'s reason that one
owner decides what a colour looks like on this surface (§Alternatives).

**SD8 — Seam C: the graph publishes what a gesture did.** Reserved signals, the
Map's `vp_*` precedent, one family per gesture the widget reports:

| gesture | signals | type |
| --- | --- | --- |
| hover, on a dwell | `gv_hover` | String, the vertex id or empty |
| click, selection | `gv_selection`; `selection_key` keeps its meaning as the last selected | `Array(String)` |
| double-click | `gv_focus` | String |
| secondary click on a node | `gv_context` | String |
| edge click | `gv_edge_source`, `gv_edge_target`, `gv_edge_id` | String |
| drag end | `gv_pin_id`, `gv_pin_x`, `gv_pin_y`; `gv_pin_lat`, `gv_pin_lon` while located (§SD3) | String, Float64 |
| background click | `gv_bg_x`, `gv_bg_y` | Float64, world units |
| camera, on settle | `gv_min_x`, `gv_max_x`, `gv_min_y`, `gv_max_y`, `gv_zoom`; the lat/lon bounds while located | Float64 |
| legend toggle | `gv_aura_hidden` | `Array(String)` |

The double and secondary forms of edge and background clicks, and edge hover,
are not published: nothing distinguishes them in a query that is worth a name,
and the set should be short enough to remember. Edge clicking and edge
selection are switched on, without which the edge signals have nothing to
report and the contract's `edges.label` and `edges.tone` are drawn but
unselectable.

Four rules make the seam usable:

- **Arrays reach the store.** The signal store's encoder gains the array
  case, written as a ClickHouse array literal on the same `param_*` wire the
  scalars ride, so `{gv_selection:Array(String)}` substitutes like any typed
  param. This is the store's first array-typed reserved signal.
- **Every `gv_*` signal is seeded when the tab first renders** — strings
  empty, arrays empty, numbers zero, the camera from its first fit — so a
  query referencing any of them runs from the first frame. The Map's numeric
  `vp_*` gate the Run until written because a viewport of zero would draw a
  raster of nowhere; a pin of zero beside an empty `gv_pin_id` is a valid "no
  drop yet".
- **The high-rate signals debounce.** The camera publishes on settle, as the
  Map's does, and hover on a dwell rather than on every crossing. Publishing
  per frame would re-run a Live query on every frame of a pan or every node
  the pointer crosses; play's runaway breaker would then switch Live off,
  which is the right behaviour for a loop and the wrong one for a pan. The
  breaker stays the backstop for a query that feeds its own input.
- **A rebuild the panel caused keeps the camera.** ADR-0227 §SD8 re-arms the
  one-shot fit on every rebuild, which is right for a re-Run and wrong for a
  re-run the panel's own signal triggered: an expansion on `gv_focus` would
  refit on every hop. When the run diverged only on signals this panel wrote —
  the store records the writer — positions and camera are kept; a `fit`
  column (§SD2) still frames what it names, since the query asked.

`gv_focus` with Live is what makes the navigation layer's expansion a query
rather than a widget: double-click a node, the signal moves, the query
re-runs with a wider neighbourhood, the new edges arrive beside the old ones,
which keep their positions. The universe is the database. ADR-0225's `nav`
package is not wired by this decision and the two are alternatives, not
layers — which one a panel should use is a question for whoever has both
working.

`gv_pin_*` closes the question ADR-0227 left open, whether a drag-end position
is published anywhere. With it and §SD2's `pin_x` / `pin_y`, a dropped node
round-trips: the query reads back where the user put it and declares it there
next time. `selected` closes the other direction: a Table click writes
`selection_key`, the vertices CTE marks `id = {selection_key:String} AS
selected`, and the graph highlights it — master/detail with the graph as the
detail, through columns the query already has. A declared selection is
applied on rebuild through the widget's silent setters and publishes nothing,
so the round trip cannot loop: `selection_key` and `gv_selection` report
gestures only.

**SD9 — A node click publishes `selection_id`.** The `vertices` CTE may carry a
leeway id column alongside its `id` — the same `id:id:…` column the Table's
stamper reads — and the two are different names and do not collide. When it
does, a node click publishes `selection_id` beside `selection_key`, read off
the clicked vertex's row by the same extraction the row-click path already
uses.

The panel still does **not** emit `selection`. ADR-0129 §SD4's reason is
unchanged — the vertices come from a private lane, so a row cursor would be
clamped away and would jerk the other panels — and this decision does not
reopen it. What it fixes is that the *value* channels were collateral damage of
that choice: `selection_id` is not node-scoped, so publishing it directly costs
nothing and is what lets Detail follow a node through a query the user writes
once. Teaching Detail to accept a value rather than a row ordinal would be the
fuller answer and reshapes ADR-0097's selection contract; it is not attempted
here.

**SD10 — What the Network tab does with each new column.** ADR-0227 §SD1's
obligation: one contract, two panels, and the split is stated rather than
discovered. The layered panel honours `opacity`, `pick` — it gained Go
hit-testing in ADR-0228 — `selected` and the edge `id`. It ignores `fit`,
`radius`, `label_always` (it labels every node), `pin_x` / `pin_y`, `lat` /
`lon`, `start_*`, `pull_*`, `center`, `length` and `strength`, because
Graphviz owns positions and sizes there, and
`groups` and `donut_tones`, because it draws neither auras nor rings.
`graph_opts` and `aura_style` are read by the Graphview tab alone, and so is
every `gv_*` signal written; the Network tab keeps `selection_key` and gains
`selection_id`.

A column one renderer ignores is the contract's existing shape, not a new
compromise: `donut` is already inert in the Network tab and `shape` is already
inert in the Graphview tab.

**SD11 — `PinOnDrag` becomes a control with an *auto* position.** The help
corpus promises that a dropped node is pinned; nothing in `play` sets the
option, so the promise is currently false. *Auto* reads `graph_opts.pin_on_drag`
and is off when the query says nothing, because with `gv_pin_*` and `pin_x` /
`pin_y` the query is now the better place to decide whether a drop sticks; a
reader who wants nodes to hold moves the control. The help text is corrected
to match.

**SD12 — Precedence, when more than one column speaks to a channel.** Stated
once so the resolvers cannot disagree:

| channel | wins → loses |
| --- | --- |
| position | `pin_x`/`pin_y` → `lat`/`lon` → `start_x`/`start_y` (first frame only) → `pull_*` → the layout |
| node size | `radius` → `size_by` → `weight` → the style default |
| node fill | `tone_by` → `tone` → `group` → the style default |
| aura set | `aura_by` → `groups` → `group` → none |
| opacity | `opacity_by` → `opacity` → 1 |
| aura look | `aura_style` row → the widget's cycle |
| any of the above | an explicit chrome setting → the query's value (§SD1) |

A selector that names a column the row also spends conventionally — `size_by
= 'weight'` — is the identity and is accepted.

**SD13 — What stays unreachable, and the trigger for each.** Three things a
query still cannot do, named so they are not discovered as omissions:

- **Read a metric as a value.** The selectors *encode* PageRank; nothing lets
  a query `SELECT` it, join it, or show it in Detail. The route is the
  persistence ADR-0229 §SD5 already defers — metric fact kinds keyed by the
  graph fingerprint, which the query then joins — and its trigger is the first
  query that wants the number rather than the picture. A lighter route, the
  panel handing the metric columns to the server as an external table the
  query joins, was weighed and deferred with it: it is a second transport for
  the same columns and would be retired the day the facts land.
- **Read the whole layout back.** `gv_pin_*` carries one drop; a query that
  wants every position — to store a layout — has no signal wide enough, and
  a signal that carried two thousand pairs would be the wrong shape. The
  widget half exists: `PositionColumns` (ADR-0232 §SD3) reads every
  position as two columns in the model's order. The store half is the same
  persistence record, with a layout fact keyed the same way; `start_x` /
  `start_y` is the restore half, in place now so the store half has a
  consumer.
- **Weighted metrics.** `weight` sizes and never enters a metric until the
  engine's weighted shortest paths land (ADR-0229 §SD7); the cache key gains
  the weight column then.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| The graph contract (`edges` / `vertices` CTE columns) | added: the §SD2 columns; the claim gains the NULL-is-absent and Bool rules | both graph panels' resolvers, in the shared owner; the help corpus's contract description; the type guard, which gains Bool and list-of-string |
| Panel channels | added: `graph_opts`, `aura_style` as optional channels with their own lanes | the shared graph source; the channel registry |
| Reserved signal names | added: the `gv_*` set; `selection_id` gains a second writer | the Signals chrome's declared-type table, its empty-default rule (arrays default to `[]`), and the tab-marks writer list; a `graphview` writer identity |
| The signal store's value encoder | added: string arrays as a ClickHouse array literal | the emit-drop notice, which no longer fires for a `[]string` |
| Exported Go API under `public/` | unchanged by this record; the columnar declaration, `Undirected`, `LabelAlways`, `MetricE`, `Compute` and the seeded PageRank it relies on are ADR-0232's | ADR-0232 lands first |
| `boxer.facts` schema | unchanged | the metric and layout fact kinds stay ADR-0229 §SD5's follow-up (§SD13) |

## Alternatives

- **A key/value `graph_opts`.** Killed on C2: invisible to completion, no type
  checking, and a misspelled key is silently ignored where a misspelled column
  is merely unclaimed.
- **`graph_opts` as `SET`-bound params.** Killed: a `SET` name is substituted
  into SQL by the server and read by no panel, and a panel reading the param
  prelude would give the buffer a second reader with different rules from the
  store's. A CTE is a split node like any other, which is also what lets it
  read signals.
- **Per-vertex settings addressed by id in a settings CTE** (O3's per-row half).
  Killed: it makes every per-row channel a join the user writes by hand, when
  the row is already in front of them in `vertices`.
- **A second contract for the live panel.** Killed for ADR-0227 §SD1's reason:
  the same query should draw in both tabs without an edit.
- **Spelling the chrome knobs in SQL too** — damping, θ, zoom bounds, style
  colours, gesture modes. Killed by §SD1: they are the reader's, and spelling
  them in both places is where the two disagree.
- **Literal colours** on `aura_style`, `donut_tones` or the vertices. Killed:
  ADR-0156 makes one owner of what a colour looks like on this surface, the
  contract already names families rather than colours in `tone`, and a hex
  string that reads on one theme is a blob on the other. If a family's aura
  variant is wanted, it is added to the palette, not to the contract.
- **Columns that wait on the widget.** `arrow` per edge (`none` / `both`,
  beyond the graph-wide `undirected`), `dash`, `level` (a caller-assigned
  hierarchy level), `shape` for the live panel. Each is a small `NodeSpec` or
  `EdgeSpec` field the gap analyses under `doc/adr-background-work/` already
  list, and each gets its column the day the field exists, under §SD1's
  closure rule — not before. `label_always` and the undirected picture were
  on this list until ADR-0232 added the fields.
- **Metrics as a joinable table now** (an external table per run). Deferred
  with the reason in §SD13.
- **Hover published on every crossing.** Killed: a Live query per pointer
  movement trips the breaker on any query that reads it; a dwell is what a
  reader means by "hovering".
- **Hosting the graph in `portolan` as part of this change.** Descoped, not
  killed (§SD3). It changes the legend mode, the fit gesture and the pointer
  arbitration, which is a panel-shaped decision rather than a contract-shaped
  one. The `lat` / `lon` columns are named so it can read this contract
  unchanged.
- **Wiring `nav` instead of `gv_focus`.** Deferred (§SD8). Expansion as a query
  needs no new subsystem and reuses the Live path; whether a client-side
  universe beats a re-query is a question with no measurement behind it yet.
- **Publishing `selection` from the graph panels.** Killed by ADR-0129 §SD4,
  unchanged.
- **Approximating `shape`.** Still killed (ADR-0227): graphview draws circles,
  and a shape vocabulary rendered as circles is worse than an ignored column.

## Consequences

### Positive

- The per-row contract reaches the widget's declaration rather than half of it,
  including the channels the widget grew after ADR-0227 was written and the
  silent setters that show a selection made elsewhere.
- Detail can follow a node click, through a column the query already has, and
  the graph can follow a Table click, through a column the query writes.
- The graph stops being write-only: every gesture the widget reports is
  readable by the query that drew it, which is what makes master/detail,
  expansion-by-query and geographic round-tripping possible without new
  panels.
- ADR-0229's engine gains the consumer its §SD6 names, and a reader can size,
  tint, fade or group by any of its first-cut metrics — or by any column of
  their own — without computing one in SQL.
- The SQL-versus-chrome split and the closure rule are stated, so the next
  knob has an answer before it is argued about.

### Negative

- The `vertices` CTE can now carry a great many optional columns. They are
  grouped by what they claim — identity, category, magnitude, placement,
  emphasis, state — and §SD12 states their precedence, but the contract is
  undeniably larger to learn.
- Two more lanes and two more channels on a panel that already had two, and
  twenty-odd reserved signal names beside the Map's six.
- A metric computed on rebuild adds work to the frame a new result first draws,
  bounded but not zero, and a capped clique count gives a lower bound the
  reader must notice is truncated.
- `graph_opts`, `aura_style` and the `gv_*` set are Graphview-only, which
  widens the asymmetry ADR-0227 already recorded between the two tabs'
  honoured subsets.
- The signal store grows a second value shape. One array case is small; it is
  still the first.

### Neutral

- No existing query changes meaning: every new column and both new CTEs are
  optional, `group` keeps both of its jobs, and a query that declares nothing
  new draws exactly as before, auras off.
- The chrome knobs stay chrome knobs, which is a decision and not an omission.
- Map hosting, `nav`, and persisting metrics and layouts as facts are each
  named and left where they are, with a trigger.

## Migration — Tier 1

- **Breaks.** Nothing. Every addition is an optional column, an optional
  channel or a seeded signal, and the existing columns keep their meaning and
  precedence.
- **Path.** Nothing to migrate. A query that wants a new channel adds a column.
- **Regeneration.** None; no IDL or generated artifact is touched.
- **Old shape.** Kept indefinitely — `group` as both fill and single aura is
  the shorthand, not a deprecated spelling.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the shared contract's resolvers and build gain
  cases for each new column — claimed on the right type, unclaimed on the
  wrong one, absent, NULL — plus the §SD12 precedence table row by row, the
  `groups`-over-`group` rule, the selector vocabulary including the
  categorical refusals, the column-as-selector case and the `-` inversion, the
  one-row `graph_opts` rule and its vocabulary refusals, the `undirected`
  aliasing, the `distance_from` seed sets, and the `aura_style` join
  including `hidden` as the legend's default. The metric cache is tested on
  its key: one computation per (fingerprint, `undirected`, metric), none per
  frame, and a seeded recomputation per seed-set change. The `gv_*` publishes are
  tested against synthesised widget events, the array encoder against values
  containing quotes and brackets, the seeding against a first render, the
  own-signal rebuild against the fit latch, the declared `selected` against
  the emitter (nothing published), and `selection_id` against a vertices
  record carrying a leeway id column.
- **What would fail.** A contract regression shows in the shared build's tests
  and in both panels at once. A selector regression shows as a channel that
  silently falls back. A debounce regression shows as a camera or hover signal
  written every frame, which the signal-writer provenance in the Signals
  chrome makes visible. A metric-cache regression shows as a recomputation
  count. A selection loop shows as the Live breaker tripping on a query that
  reads `selection_key` and declares `selected`.
- **Painted result.** A scene in the play screenshot tour over a query
  exercising placement, emphasis, a declared selection and a computed
  selector, beside the existing Graphview scene so the widened contract is
  compared against the plain one.
- **Gap.** Gesture handling — drag, pan, wheel, double-click, dwell — stays
  verified interactively rather than headlessly, which is ADR-0224's gap
  inherited twice. The `gv_*` publishes are tested from synthesised events, so
  what is not covered is that a real gesture produces the event, not that the
  event produces the signal.

## Status

Accepted 2026-09-13.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.

## Updates

### 2026-09-14 — SD2–SD5, SD8 and SD11 shipped; SD3's descoping is reversed

`play` reads Seam A's columns on both CTEs, the `graph_opts` and `aura_style`
seams' first half — `graph_opts` with every column §SD5 names — the four
encoding selectors over a metric or a column of the query's own, the `gv_*`
signal set with its located forms, the `PinOnDrag` control with its *auto*
position, and the layered tab's honoured subset of §SD10. `aura_style` (§SD7)
and the `selection_id` publish (§SD9) are still open.

**The basemap.** §SD3 descoped hosting the located graph inside `portolan` as
"a different panel with different rules". The first located queries wanted the
map under them, and each of the three rules the descoping named has an answer
that already existed: the legend goes external (ADR-0224 §SD15), the host owns
the camera and `FitNow` is replaced by the map's own framing of the located
set, and the pointer is arbitrated by the hosted claim (ADR-0228 §SD2). So a
located declaration is drawn **inside a map in the Graphview tab itself**, not
in a new panel: the map owns the canvas, pan and wheel, the graph paints and
picks inside it at the world fixed at the reference zoom the pins were
projected at, unlocated vertices are laid out among the pinned ones, and the
camera signals publish from the map's settled view in both units. A configured
tile server draws tiles; without one the offline atlas draws country outlines,
so the arrangement reads as a map either way. A `basemap` control with an
*auto* position lets the reader draw the located graph on the plain canvas
instead. The model's Web Mercator projection is asserted equal to the host's
CRS at the reference zoom, which is what lets one projection serve both.

**Deviations recorded, with the reason each was resolved as it was.**

- §SD12's position order stands: `pin_x`/`pin_y` wins over `lat`/`lon` on a
  row that declares both. The first implementation had it the other way; the
  world-unit pin is the more deliberate statement and a query that wants
  geography does not write one.
- The Bool claim is Arrow's boolean or an 8-bit integer, as §SD2 says. A
  wider numeric sharing a flag's name is a collision: a `pick` column holding
  a score would otherwise take every zero-scored node out of the pointer's
  reach.
- `start_x`/`start_y` is spent on a vertex that was not in the previous
  declaration. Re-applying it to a survivor on every Live re-run undid what
  the reader had dragged, which is the opposite of "place it once and then
  leave it free".
- `pull_x` alone pulls at the centre-gravity default strength, which is what
  "naming an axis turns the pull on" needed a number for.
- §SD10's honoured subset for the Network tab is `opacity` and `selected`;
  `pick` and the edge `id` wait on the layered view, which has no per-node
  hit exclusion and one edge per ordered pair.

**Three rules the implementation needed stated.** Only the settings that are
built into the declaration — the selectors and `undirected` — re-key the
model; the layout, spacing and `hide_edges` reach the widget's options per
frame, so a settings row that reads `{gv_zoom:Float64}` re-executes on every
settle without rebuilding or re-framing. The seeded metrics re-derive the
channels that spend them when the seed moves, without a rebuild, which is what
makes `distance_from = 'hover'` follow the pointer. And §SD8's own-signal rule
is implemented over the lanes' served inputs: a rebuild whose SQL is unchanged
and whose signal values differ only in names this panel writes keeps the
camera.

## References

- [ADR-0129](./0129-play-layered-graph-panel.md) — the graph contract and the
  §SD4 reason the graph panels do not write `selection`.
- [ADR-0232](./0232-play-first-class-consumer-columnar-graphview-engine-vocabulary.md)
  — the columnar declaration, the engine's metric vocabulary, the seeded
  PageRank and the two widget fields this record is implemented over.
- [ADR-0227](./0227-play-graphview-panel.md) — the contract's single owner,
  the Graphview tab, and the deferrals this record takes up.
- [ADR-0224](./0224-graphview-go-graph-widget-painter-lane.md) — the widget:
  pins (§SD10), auras (§SD11), the silent setters (§SD12), the fade pair
  (§SD14), the legend (§SD15), the soft pull (§SD16).
- [ADR-0225](./0225-graphview-navigation-layer-and-radial-layout.md) — the
  radial centres of §SD2's `center`, and the `nav` layer §SD8 leaves aside.
- [ADR-0228](./0228-hosted-canvas-rendering-and-graph-widget-sharing.md) — the
  hosted seam §SD3 descopes, and the layered hit-testing §SD10 relies on.
- [ADR-0229](./0229-graph-analytics-engine.md) — the engine, its budgets and
  truncation, the §SD5 persistence §SD13 defers to, and the §SD6 consumer this
  record supplies.
- [ADR-0230](./0230-neighbour-graph-and-neighbour-embedding-force-model.md) —
  the force model and exaggeration of §SD5, and `HideEdges`.
- [ADR-0097](./0097-play-reactive-query-graph.md) — channels, signals, the
  store's wire channel and breaker, and the selection contract §SD9 declines
  to reshape.
- [ADR-0096](./0096-play-geo-raster-map-panel.md) — the `vp_*` precedent
  §SD8 follows and departs from on seeding.
- [ADR-0156](./0156-qualitative-palette-dark-surface.md) — why the contract
  names tone families rather than colours.
- [ADR-0167](./0167-layeredgraph-magnitude.md) — the normalisation the
  selectors reuse.
- [graph on a map](../howto/graph-on-a-map.md) — the projection and world
  choices §SD3 adopts.
