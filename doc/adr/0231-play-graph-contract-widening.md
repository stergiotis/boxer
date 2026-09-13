---
type: adr
status: proposed
date: 2026-09-13
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not
> implement as if accepted.

# ADR-0231: widening the graph contract — placement, emphasis, computed metrics and a read-back seam

## Context

`play` draws a result as a graph through one contract: the `edges` and
`vertices` CTEs of [ADR-0129](./0129-play-layered-graph-panel.md) §SD2, hoisted
by [ADR-0227](./0227-play-graphview-panel.md) (proposed) §SD1 into a
renderer-neutral model that the Network tab and the Graphview tab both resolve
against.

The contract carries identity, category and magnitude: `id`, `label`, `group`,
`shape`, `tone`, `weight`, and the Graphview-only `donut` pair. Measured
against `widgets/graphview`'s own per-row declaration, that is most of identity
and category, part of magnitude, and none of placement or emphasis. The widget
has since grown past the contract in four directions, none of which a query can
reach:

- the fade-and-inert pair, `Opacity` and `NoPick`
  ([ADR-0224](./0224-graphview-go-graph-widget-painter-lane.md) §SD14), which
  is what a caller spends a relevance or a search result on;
- placement — declared pins (§SD10), the soft pull (§SD16), and the per-edge
  `Length` and `Strength` the force step applies (§SD13). ADR-0227 leaves the
  last of these unmapped because the mapping wants a scale decision — a weight
  is a magnitude, a strength a spring — that it declines to take;
- aura membership as a *set*. `NodeSpec.Auras` is a slice and §SD4 spends it
  on one value of `group`, so a node in two groups — the overlapping case
  auras exist for — cannot be declared, and the per-aura `AuraStyle` is
  unreachable;
- the radial layout's centres ([ADR-0225](./0225-graphview-navigation-layer-and-radial-layout.md)
  §SD6), which are a property of the data and have no spelling.

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
  position, the camera, which auras are hidden — cannot be read back by the
  query that drew it.
- **[ADR-0229](./0229-graph-analytics-engine.md) §SD6 names this panel as the
  analytics engine's first consumer** — "a metric column source for node
  radius and tone" — and the engine is unwired. Centrality, cores and
  components have no spelling, so a query that wants to size by PageRank
  computes it in SQL or does without.

The pressure is to widen without turning the contract into a settings dump.
The contract is read by two panels with different capabilities, and its columns
are the thing users write by hand.

## Design space (QOC)

**Question.** How does the graph contract widen to cover the widget's feature
set without becoming a place where every knob is spelled twice?

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
belongs, and the graph panels gain a read-back seam and a leeway-id publish.

**SD1 — The rule, and who wins when both speak.** A knob belongs in SQL when
**the query knows something the reader cannot see**; it belongs in chrome when
it is **the reader's preference**. Placement, emphasis, relevance, category,
magnitude and per-edge physics are the query's. Damping, the Barnes–Hut angle,
zoom bounds, fit padding, aura field shape, label mode and style colours are
the reader's, and are not spelled in SQL by this decision or a later one — that
is what the rule is for.

Where both can speak, the precedent is the Sankey panel's `stage`: **the query
sets the default and an explicit chrome setting overrides it**, with the
control's *auto* position meaning "whatever the query said". A control that has
been moved says so, so a reader can tell a drawing they changed from one the
query specified.

**SD2 — Seam A: the per-row vocabulary widens.** New optional columns on the
existing CTEs, each claimed only when the name *and* the type fit — the
existing guard that keeps a column which merely shares a name from being spent
as a channel. An unclaimed column stays an ordinary result column.

On `vertices`: `opacity` fades the node (numeric, 0..1); `pick` (Bool) takes it
out of the pointer's reach when false, absent meaning pickable; `pin_x` /
`pin_y` fix it, both required; `lat` / `lon` fix it geographically (§SD3);
`pull_x` / `pull_y` name a soft target per axis, with `pull_strength` and the
per-axis `pull_strength_x` / `pull_strength_y` tuning it; `groups`
(`Array(String)`) declares aura membership as a set (§SD4); `donut_colors`
(`Array(String)`) colours the ring slices; `center` (Bool) marks a radial
centre.

On `edges`: `id` tells parallel edges apart in hover, selection and events;
`opacity` and `pick` as above; `length` and `strength` multiply the force
step's ideal length and attraction for that edge. `weight` keeps its meaning
and continues to size — an edge's magnitude and its physics are different
claims, and a query that wants a heavy edge to also pull shorter says so twice.

Naming an axis's target is what turns the pull on: `pull_x` alone holds a node
near a column and leaves the layout free to settle it vertically, which is how
a level is expressed without making it a placement.

**SD3 — `lat` / `lon` are pins, not a map.** A located vertex is projected to
world units at a fixed reference zoom measured from a local origin, and
declared `Pinned`; unlocated vertices are declared with no pin and the force
step lays them out among the located ones, which ADR-0224 §SD10 gives for free.
This is a geographic *arrangement*, not a basemap: hosting the graph inside
`portolan` ([ADR-0228](./0228-hosted-canvas-rendering-and-graph-widget-sharing.md))
is a different panel with different rules — the aura legend must go external,
`FitNow` and `SetCamera` are overruled, and the pointer claim has to be
arbitrated — and is deliberately **not** in scope here. The column names are
chosen so that panel can read the same contract when it is written.

The projection choice is the panel's and not the reader's: the identity-camera
alternative rescales the geometry a layout solves against, which the
graph-on-a-map recipe records as the reason unlocated nodes are flung about on
zoom.

**SD4 — Aura membership separates from the node fill.** `group` keeps both
jobs — it is the fill and, absent `groups`, the single aura — so no existing
query changes. `groups` declares membership as a set, and once it exists the
two questions are distinct: a fill wants one category per node, an aura wants a
set. `aura_by` (§SD6) is what says "colour by `group`, blob by `owner`".

The aura palette stays the widget's own cycle rather than this contract's group
palette, for the reason ADR-0227 §SD4 records: the group palette is the
`*Subtle` background tones, chosen dark so one light ink reads on them, and a
translucent blob of one over the same dark panel is a blob nobody can see.
`aura_style` (§SD7) is the override for a query that means something specific.

**SD5 — Seam B: a one-row `graph_opts` CTE, columns rather than keys.** What is
a property of the whole drawing and still the query's under §SD1: `layout`,
`orientation`, `ring_dist`, `row_dist`, `col_dist`, `k_scale`, `force_model`,
`exaggeration`, and the three encoding selectors of §SD6. All optional; the
first row is read and further rows are ignored with a note in the status line,
because a settings CTE that returned many rows is a query bug worth saying out
loud rather than a set of settings.

Columns rather than keys is the C2 kill-reason for O3: a column name is
schema-visible, so completion offers it and the claim can type-check it, and a
misspelling is an unclaimed column rather than a silently ignored key.

`k_scale` is in rather than in chrome because the ideal edge length has to
reach the spread of whatever the query placed — the graph-on-a-map recipe needs
a different one from a free layout, and the query is what knows which it wrote.

**SD6 — Encoding selectors, and the analytics engine behind them.**
`size_by`, `tone_by` and `aura_by` each name either a column of `vertices` or a
computed metric. The metric vocabulary is the engine's first cut:
`degree`, `in_degree`, `out_degree`, `pagerank`, `betweenness`, `kcore`,
`triangles`, `component`, `scc`.

`component` and `scc` are categorical — labels, not quantities — so they are
refused in `size_by` with a reason in the status line. Every other metric is
ordinal and works in all three; in `aura_by` an ordinal metric groups by
distinct value, which is what makes `aura_by = 'kcore'` blob the k-shells.
A selector naming neither a column nor a metric is refused the same way, and
the channel falls back to what the row declared.

The engine ([ADR-0229](./0229-graph-analytics-engine.md)) is wired as that
record's §SD6 anticipated. The CSR is built from the model's edge list and
cached on its content fingerprint, and a metric is computed once per
(fingerprint, metric) rather than per frame — the declaration is already
rebuilt only when the lanes' fingerprints move, and the metric joins that
cache. Budgets follow ADR-0229 §SD4: betweenness is exact up to a vertex count
and sampled by pivots above it, PageRank runs its iteration budget or converges
first, and **a truncated or sampled metric is reported in the status line**,
because a sampled centrality is an estimate and a reader sizing nodes by it
should know.

Computing synchronously on rebuild is the first cut. The draw side is capped
three orders of magnitude below the engine's operating point (ADR-0227 §SD9),
so the expensive metric on the largest graph the panel will draw is within the
rebuild it already pays for. If a measured graph exceeds that, the move is the
Projection panel's shape — a background goroutine owning the engine calls while
the render thread owns the widget — and not a smaller vocabulary.

**SD7 — Seam B, second CTE: `aura_style`, one row per aura.** `aura` names the
group; `fill`, `line`, `line_width`, `z_index`, `label` and `no_legend` give
that aura's look. Per-group rather than per-vertex, because that is what it is:
a query that colours an aura says it once, and saying it on every member is
where the two disagree. An aura the CTE does not name keeps the cycle colour.

**SD8 — Seam C: the graph publishes what a gesture did.** Reserved signals, the
Map's `vp_*` precedent: `gv_hover`; `gv_selection` (`Array(String)`, the
multi-selection `selection_key` cannot carry); `gv_focus` on a node
double-click; `gv_edge_source` / `gv_edge_target` on an edge click;
`gv_pin_id` / `gv_pin_x` / `gv_pin_y` on drag end; `gv_min_x` / `gv_max_x` /
`gv_min_y` / `gv_max_y` / `gv_zoom` for the camera; `gv_aura_hidden`
(`Array(String)`) on a legend toggle. `selection_key` keeps its current
meaning, since it is the cross-panel value other panels also write.

Edge clicking and edge selection are switched on, without which the edge
signals have nothing to report and the contract's `edges.label` and
`edges.tone` are drawn but unselectable.

**The camera signals debounce on settle**, as the Map's do. Publishing per
frame would re-run a Live query on every frame of a pan; play's own runaway
guard would then switch Live off, which is the right behaviour for a loop and
the wrong one for a pan.

`gv_focus` with Live is what makes the navigation layer's expansion a query
rather than a widget: double-click a node, the signal moves, the query
re-runs with a wider neighbourhood, the new edges arrive. The universe is the
database. ADR-0225's `nav` package is not wired by this decision and the two
are alternatives, not layers — which one a panel should use is a question for
whoever has both working.

`gv_pin_*` closes the question ADR-0227 left open, whether a drag-end position
is published anywhere. With it and §SD2's `pin_x` / `pin_y`, a dropped node
round-trips: the query reads back where the user put it and declares it there
next time.

**SD9 — A node click publishes `selection_id`.** The `vertices` CTE may carry a
leeway id column alongside its `id`; the two are different names and do not
collide. When it does, a node click publishes `selection_id` beside
`selection_key`, read off the clicked vertex's row by the same extraction the
row-click path already uses.

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
hit-testing in ADR-0228 — and the edge `id`. It ignores `pin_x` / `pin_y`,
`lat` / `lon`, `pull_*`, `center`, `length` and `strength`, because Graphviz
owns positions there, and `groups` and `donut_colors`, because it draws neither
auras nor rings. `graph_opts` and `aura_style` are read by the Graphview tab
alone.

A column one renderer ignores is the contract's existing shape, not a new
compromise: `donut` is already inert in the Network tab and `shape` is already
inert in the Graphview tab.

**SD11 — `PinOnDrag` becomes a control, defaulting off.** The help corpus
promises that a dropped node is pinned; nothing in `play` sets the option, so
the promise is currently false. Off stays the default because with `gv_pin_*`
and `pin_x` / `pin_y` the query is now the better place to decide whether a
drop sticks, and a reader who wants nodes to hold gets a checkbox. The help
text is corrected to match.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| The graph contract (`edges` / `vertices` CTE columns) | added: the §SD2 columns | both graph panels' resolvers, in the shared owner; the help corpus's contract description |
| Panel channels | added: `graph_opts`, `aura_style` as optional channels with their own lanes | the shared graph source; the channel registry |
| Reserved signal names | added: the `gv_*` set; `selection_id` gains a second writer | the Signals chrome's declared-type table and the tab-marks writer list |
| Exported Go API under `public/` | unchanged | — |
| `boxer.facts` schema | unchanged | the metric fact kinds stay ADR-0229 §SD5's follow-up |

## Alternatives

- **A key/value `graph_opts`.** Killed on C2: invisible to completion, no type
  checking, and a misspelled key is silently ignored where a misspelled column
  is merely unclaimed.
- **Per-vertex settings addressed by id in a settings CTE** (O3's per-row half).
  Killed: it makes every per-row channel a join the user writes by hand, when
  the row is already in front of them in `vertices`.
- **A second contract for the live panel.** Killed for ADR-0227 §SD1's reason:
  the same query should draw in both tabs without an edit.
- **Spelling the chrome knobs in SQL too** — damping, θ, zoom bounds, style
  colours. Killed by §SD1: they are the reader's, and spelling them in both
  places is where the two disagree.
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
  including the three channels the widget grew after ADR-0227 was written.
- Detail can follow a node click, through a column the query already has.
- The graph stops being write-only: a gesture's result is readable by the query
  that drew it, which is what makes master/detail, expansion-by-query and
  geographic round-tripping possible without new panels.
- ADR-0229's engine gains the consumer its §SD6 names, and a reader can size by
  a centrality without computing one in SQL.
- The SQL-versus-chrome split is a stated rule rather than an accident, so the
  next knob has an answer before it is argued about.

### Negative

- The `vertices` CTE can now carry a great many optional columns. They are
  grouped by what they claim — identity, category, magnitude, placement,
  emphasis — but the contract is undeniably larger to learn.
- Two more lanes and two more channels on a panel that already had two.
- A metric computed on rebuild adds work to the frame a new result first draws,
  bounded but not zero, and betweenness above the exact threshold gives an
  estimate that the reader must notice is sampled.
- `graph_opts` and `aura_style` are Graphview-only, which widens the asymmetry
  ADR-0227 already recorded between the two tabs' honoured subsets.

### Neutral

- No existing query changes meaning: every new column and both new CTEs are
  optional, and `group` keeps both of its jobs.
- The chrome knobs stay chrome knobs, which is a decision and not an omission.
- Map hosting, `nav`, and persisting metrics as facts are each named and left
  where they are.

## Migration — Tier 1

- **Breaks.** Nothing. Every addition is an optional column or an optional
  channel, and the existing columns keep their meaning and precedence.
- **Path.** Nothing to migrate. A query that wants a new channel adds a column.
- **Regeneration.** None; no IDL or generated artifact is touched.
- **Old shape.** Kept indefinitely — `group` as both fill and single aura is
  the shorthand, not a deprecated spelling.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the shared contract's resolvers and build gain
  cases for each new column — claimed on the right type, unclaimed on the
  wrong one, absent — plus the pin/pull resolution, the `groups`-over-`group`
  precedence, the selector vocabulary including the categorical refusals, the
  one-row `graph_opts` rule, and the `aura_style` join. The metric cache is
  tested on its key: one computation per (fingerprint, metric), none per frame.
  The `gv_*` publishes are tested against synthesised widget events, and
  `selection_id` against a vertices record carrying a leeway id column.
- **What would fail.** A contract regression shows in the shared build's tests
  and in both panels at once. A selector regression shows as a channel that
  silently falls back. A debounce regression shows as a camera signal written
  every frame, which the signal-writer provenance in the Signals chrome makes
  visible. A metric-cache regression shows as a recomputation count.
- **Painted result.** A scene in the play screenshot tour over a query
  exercising placement, emphasis and a computed selector, beside the existing
  Graphview scene so the widened contract is compared against the plain one.
- **Gap.** Gesture handling — drag, pan, wheel, double-click — stays verified
  interactively rather than headlessly, which is ADR-0224's gap inherited
  twice. The `gv_*` publishes are tested from synthesised events, so what is
  not covered is that a real gesture produces the event, not that the event
  produces the signal.

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.

## References

- [ADR-0129](./0129-play-layered-graph-panel.md) — the graph contract and the
  §SD4 reason the graph panels do not write `selection`.
- [ADR-0227](./0227-play-graphview-panel.md) (proposed) — the contract's single
  owner, the Graphview tab, and the deferrals this record takes up.
- [ADR-0224](./0224-graphview-go-graph-widget-painter-lane.md) — the widget:
  pins (§SD10), auras (§SD11), the fade pair (§SD14), the legend (§SD15), the
  soft pull (§SD16).
- [ADR-0225](./0225-graphview-navigation-layer-and-radial-layout.md) — the
  radial centres of §SD2's `center`, and the `nav` layer §SD8 leaves aside.
- [ADR-0228](./0228-hosted-canvas-rendering-and-graph-widget-sharing.md) — the
  hosted seam §SD3 descopes, and the layered hit-testing §SD10 relies on.
- [ADR-0229](./0229-graph-analytics-engine.md) — the engine, its budgets and
  truncation, and the §SD6 consumer this record supplies.
- [ADR-0230](./0230-neighbour-graph-and-neighbour-embedding-force-model.md) —
  the force model and exaggeration of §SD5.
- [ADR-0097](./0097-play-reactive-query-graph.md) — channels, signals, and the
  selection contract §SD9 declines to reshape.
- [graph on a map](../howto/graph-on-a-map.md) — the projection and world
  choices §SD3 adopts.
