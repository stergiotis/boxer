---
type: adr
status: accepted
date: 2026-09-13
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-13
---

# ADR-0232: the application as a first-class consumer — a columnar declaration for graphview, a metric vocabulary owned by the engine, and one slot order between them

## Context

[ADR-0231](./0231-play-graph-contract-widening.md) widens
`play`'s SQL surface over `widgets/graphview` and `public/analytics/graph`
and finds that nearly every feature of both reaches SQL as a column, a
one-row settings CTE or a signal, with no DSL in between. That is the
encouraging half. The other half is what the widening costs on the way from
the query to the picture, which the review that produced it counted:

- **One dataset changes shape four times per rebuild.** A result arrives as
  Arrow columns; play formats them into a row-shaped `netModel`; the panel
  builds one `NodeSpec` per row; the widget reconciles the rows back into its
  own struct-of-arrays. Every column ADR-0231 adds is a field on each of the
  three intermediate shapes.
- **A metric arrives in the wrong order.** The engine returns columns aligned
  to `csr.Graph`'s slots, which are ascending id; the widget's slots are
  first-seen order; the panel's rows are query order. A PageRank column
  reaches a node radius through a map lookup per row.
- **The metric vocabulary would live in the wrong package.** ADR-0231 §SD6
  spells eleven metric names, their ordinal-or-categorical kind, their
  aliasing under an undirected reading and three derivations
  (`clustering`, `clique`, `component_size`) — all in play. The facts
  follow-up ADR-0229 §SD5 defers, and the navigation layer ADR-0229 §SD6
  names as the second consumer, would each spell the same table again.
- **The zero-as-default idiom leaks into SQL.** `NodeSpec.Opacity`,
  `EdgeSpec.Length`, `Strength` and `Pull` read zero as *unset*, so a query
  that writes `strength = 0` — vis-network's "drawn, no spring", which the
  gap analyses list as a gap — is heard as "default". A column with a NULL
  has an "unset" the widget's spec cannot carry.
- **`undirected` is honest for the metrics and not for the picture**: the
  engine symmetrises, the widget draws an arrow head on every edge.
- **Three hashes name one topology**: the widget's topology hash, the
  panel's model fingerprint and the CSR's fingerprint.

None of this is a defect of either package. Both were shaped for their first
callers — a gallery demo declaring a handful of hand-written specs, a test
comparing an algorithm against an oracle — and both are correct for them.
The question is what they should look like now that an application whose
data is columns is their consumer, and the answer should be one that a
second such application inherits.

**Where this sits against P5.** [why-boxer](../explanation/why-boxer.md)
P5 commits to structure-of-arrays shapes end to end, and on both sides of
this path the commitment already holds: the result arrives as Arrow columns,
the engine's container and results are slot-aligned columns, and below the
widget the painter lane batches markers and the force step runs over SoA
`float32`. What broke the chain was the row-shaped hop between them, which
this record removes for one widget. It is a seam-level decision that
connects two columnar regions, not a new enactment of the bet, and it leaves
four things row-shaped on purpose: the ids, which play still formats and
interns because ADR-0129 §SD2 makes a numeric and a string spelling of one
id the same vertex; the other graph widgets — sankey, layered graph,
kanban — whose declarations are small and not per frame, so a column shape
would buy nothing; the paint commands, which cross the frame boundary per
item as before; and the engine-to-server direction, where a metric would
return to ClickHouse as a table a query can join, which is ADR-0229 §SD5's
results-as-facts and the deeper decision this one only prepares a key for.

The constraints carry over from the two packages' own records. Every layout
and walk is a pure function of the declaration (ADR-0224 §SD2, ADR-0229
§SD2), so nothing here may make a picture depend on the order a caller
happened to append in without saying so. The row-shaped declaration has
consumers — `nav`, the gallery demos, a downstream module — and stays
unchanged to the bit for them. And the dependency direction holds: a
package under `public/` does not learn an app's types.

## Design space (QOC)

**Question.** In what shape do a widget and an analytics engine exchange a
graph with an application whose data is columnar?

**Options.**

- **O1** — Status quo: row-shaped specs in, slot-aligned columns out, the
  application joins by id and owns every vocabulary it needs.
- **O2** — A columnar declaration on graphview beside the row form, with the
  widget's slot order following the declaration; the engine owns the metric
  vocabulary and its derivations; the application aligns the two by
  declaring in id order.
- **O3** — graphview consumes `csr.Graph` as its topology, so one container
  serves the widget's walks and the engine's algorithms.
- **O4** — A shared graph-frame type under `public/` that both packages take
  and the application fills once.

**Criteria.**

- **C1** — Shape changes between the application's columns and the widget's
  paint or the engine's input, per rebuild.
- **C2** — A metric reaches the picture without a join.
- **C3** — Each vocabulary — metrics, sentinels, layouts — is owned once.
- **C4** — Blast radius on the row-form consumers.
- **C5** — What becomes expressible: explicit zero, an undirected picture, a
  per-node label.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | ++ | +  | +  |
| C2 | −− | ++ | ++ | +  |
| C3 | −− | ++ | +  | −  |
| C4 | ++ | +  | −− | −  |
| C5 | −− | ++ | −  | +  |

O3 reads as the obvious unification and is not one. The widget's adjacency
is one entry per edge *end* with the edge's index beside it, because
parallel edges are distinct items with their own strength, label and
selection; the engine's CSR collapses parallel edges into one arc and sums
their weights, because an algorithm walks arcs. Making one carry the other's
bookkeeping costs both what they were built for, and the double build is a
counting sort each at a size where neither shows in a profile. ADR-0229 §SD6
listed the widget's adjacency as a migration candidate; this record answers
that it stays. O4 puts a third type between two that already agree on
`uint64` ids and parallel slices, and its natural home would be the app.
O1 is what ADR-0231 would be implemented against without this record. O2
loses only on C4, and there only by the slot-order rule, whose effect on a
row-form consumer is confined to paint order after a removal (§SD3).

## Decision

We shape the two packages for a columnar consumer: graphview gains a
columnar declaration whose slot order is the declaration's, the engine gains
a metric vocabulary as a type with a dispatcher and its derived metrics, and
the application aligns the two by declaring in id order. ADR-0231 is
implemented against this record.

**SD1 — The principle.** A package under `public/` whose consumer's data is
columnar **accepts columns, returns columns, owns its vocabularies as types
with a spelling and a parser, and gives every "unset" an explicit form**. An
application is then a mapping between column names and column names, and a
second application inherits the same mapping. The principle is stated here
for two packages; promoting it to [CODINGSTANDARDS](../../CODINGSTANDARDS.md)
is a follow-up for when a second pair of packages confirms it, not this
record's.

**SD2 — graphview gains a columnar declaration beside the row form.**
`NodeColumns` carries `Ids` and, each optional and parallel to it, the
columns of `NodeSpec`: label, colour, radius, opacity, no-pick, pinned with
its coordinates, the four pull columns, and the ragged ones — auras, donut
values and colours — in **Arrow's list layout**, one offsets slice and one
values slice, so a list column from a result is handed over without
re-nesting. `EdgeColumns` does the same for `EdgeSpec`. A nil column is
absent for every row. `RenderColumns` takes the pair where `Render` takes
the slices; both feed one reconcile, and a declaration made through either
paints the same picture to the bit, which is the property the verification
lane holds.

The row form stays. It is the right shape for a hand-written demo and for
`nav`, whose `Style` hook edits one spec at a time, and its zero-as-default
reading is unchanged so that no existing caller's picture moves.

**SD3 — Slots are the declaration's order.** Today a slot is assigned when
an id is first seen and a vanished id is swap-removed, so the widget's slot
order is an accident of history. Under this record the slot order **is the
declaration's row order**, on both paths. A declaration that adds, drops or
reorders ids is a topology change, and a topology change permutes the
retained per-id state — positions, widget-side pins, the drag — by id, a
linear pass with the map the reconcile already keeps. A declaration in the
same order as last frame costs what it costs today.

Two things follow. A caller that declares in ascending id order has the
same slot order as `csr.Graph`, so a column the engine returns indexes the
widget's arrays with no join — the application's half of C2 is one sort at
model build. And the widget can publish its state as columns:
`PositionColumns` returns `x` and `y` in declaration order, which is the
bulk layout read that a caller persists a layout with and that `Positions`,
an iterator over pairs, was the wrong shape for.

What a row-form consumer sees change: after a removal, the batches paint in
declaration order rather than with the last slot moved into the hole, which
is more stable, not less; nothing else, since every reading of the widget
is by id.

**SD4 — An explicit "unset", and therefore an explicit zero.** In a column
of `float32`, **NaN is "not declared for this row"** and every other value
is the value. That is where a result's NULL lands, and it is what lets a
declared zero mean zero: `Strength` 0 is an edge that is drawn and does not
pull; `Opacity` 0 paints nothing; `Radius` 0 is a node that is only its
label and its pick radius. ADR-0224 §SD14's rule that fully transparent has
no spelling holds for the row form, where zero is the unset value; in the
columnar form its reason — a faded item that swallows the pointer — is
answered by the `NoPick` column beside it, which the caller declares with
it. A bool column absent is false for every row, which is the spec's
reading; a string column absent is empty. No validity bitmap: NaN is free
for floats, and the other column kinds have no zero worth distinguishing
from absent.

**SD5 — Two small widget fields the consumer needs to be honest.**
`Options.Undirected` paints no arrow heads and changes nothing else — edge
refs stay the ordered pair the caller declared, hover and pick are
unchanged — so a query that declares `undirected` gets the picture its
metrics were computed on. `LabelAlways`, on the spec and as a column, paints
that node's label whatever the hover and selection state, the per-node form
of `Options.LabelsAlways`; it is what a query spends on the ten hubs that
should stay named when the label budget silences the rest.

**SD6 — The engine owns the metric vocabulary.** `algo.MetricE` names every
per-vertex metric the package computes, with `String`, a parser, and two
readers: `Kind` — ordinal or categorical, which is what decides whether a
metric can size a node or only colour it — and `Symmetric`, the metric it
reads as on an undirected graph (`in_degree` → `degree`, `scc` →
`component`, `distance_in` → `distance`). `Compute` takes a graph, a metric
and one options struct — a source set for the seeded metrics, the budgets —
and returns a `MetricColumn`: the slot-aligned values, the kind, the
truncation. It dispatches to the functions that exist and is bit-identical
to calling them; the functions stay, since a caller that wants
`BFSResult.Parent` or `CliqueResult.Members` wants the whole result.

The derived metrics move in with it, as thin functions over the results
they derive from: `LocalClustering` from the per-vertex triangle count and
the degree, `CliqueSizes` — the largest maximal clique each vertex is in —
from a clique listing, `ComponentSizes` from a labelling. They are
one-liners wherever they are written; the point is that they are written
here, once, so the facts follow-up keys a fact kind on the same `MetricE`
and the navigation layer's relevance takes the same column.

**SD7 — A seeded PageRank.** `PageRankOptions` gains a teleport set: the
random surfer restarts at those slots rather than uniformly, and dangling
mass returns there. It is one line in the sweep and it is the metric the
selection wanted: `distance` is a step function from the selected set,
personalised PageRank is the smooth relevance the navigation layer's decay
approximates by hand. It enters the vocabulary as `relevance`, seeded like
the distance family.

**SD8 — One topology key.** `csr.Graph.Fingerprint` — directedness, ids,
out-adjacency, weights excluded — is the key a consumer caches a metric on
and, with an attribute fingerprint beside it, the key it caches its model on.
The widget keeps its own topology hash, which is per-frame and internal and
was never a key anything else could use. Three hashes become one that is
shared and one that is private.

**SD9 — The consumer half, so ADR-0231 can point here.** What `play` does
with the above, stated once:

- The network model becomes columns rather than rows, **sorted by interned
  id at build**, so its row order is the CSR's slot order and the widget's.
- The declaration is the model's columns plus the computed ones — a radius
  column from `weight` or a metric, an opacity column, an aura offsets pair
  from `groups` — handed to `RenderColumns`, with a result's NULL arriving
  as NaN. No `NodeSpec` is built.
- A selector names a `MetricE` by its string; the refusals ADR-0231 §SD6
  describes are `Kind` and the parser saying no.
- The state-shaped signals — hover, the selection set, the camera bounds,
  the hidden auras — are read from the widget's readers each frame and
  written through the store, whose dedup makes a still frame write-free;
  the event-shaped ones — focus, context, an edge click, a drop, a
  background click — come from the event queue. Replaying events to
  reconstruct state the widget already holds was the panel's own
  complexity.
- A panel declares its reserved signals once — name, type, seed — and the
  four places that register them today (the declared-type table, the
  empty-default rule, the tab's writes list, the tab-marks expansion) read
  that declaration. The Map's six move onto it in the same change, which is
  what proves the shape.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API under `public/` — `widgets/graphview` | added: `NodeColumns`, `EdgeColumns`, `RenderColumns`, `PositionColumns`, `Options.Undirected`, `NodeSpec.LabelAlways`; reshaped in behaviour: slot order follows the declaration (§SD3) | the gallery demo's scene tests, which pin paint order; the package doc's declaration contract |
| Exported Go API under `public/` — `analytics/graph/algo` | added: `MetricE` with its readers and parser, `Compute`, `MetricColumn`, `LocalClustering`, `CliqueSizes`, `ComponentSizes`, `PageRankOptions.Teleport` | the property tests, which gain the dispatcher and the seeded rank against the oracle |
| The play network model and graphview panel | reshaped: columnar, id-sorted, no per-row spec | ADR-0231's implementation; the model's tests |
| Reserved signal declaration in play | added: a per-panel declaration replacing four registration points | the Map driver, the Signals chrome, the tab marks |
| Wire and on-disk encodings, IDL, `boxer.facts` | unchanged | — |

## Alternatives

- **One container (O3).** Killed above: two adjacencies with two jobs, and
  a double counting sort that costs nothing measurable. ADR-0229 §SD6's
  migration candidate is answered, not deferred.
- **A shared graph-frame type (O4).** Killed: a third type between two that
  agree on ids and parallel slices, and one whose shape the app would
  dictate to `public/`.
- **A validity bitmap per column instead of NaN.** Weighed, because it is
  Arrow's own answer and covers every type. Killed for now: the only columns
  where "absent" and "zero" both mean something are the float ones, NaN
  costs nothing there, and a bitmap is a second slice per column for the
  caller to keep aligned. If an integer or string column ever needs the
  distinction, the bitmap is the answer and this entry is where it was
  weighed.
- **Keeping the row form only and converting inside play.** Killed on C1:
  it is the status quo with the conversion moved, and the join stays.
- **Slot order by ascending id inside the widget** rather than by the
  declaration. Killed: it would sort the caller's rows for them and make
  the picture's paint order depend on id values, where declaration order is
  something the caller chose. A caller that wants id order declares in it.
- **The metric vocabulary in play, mirrored later by facts and nav.**
  Killed on C3; three tables of one list is how they drift.
- **Personalised PageRank as a play-side post-process.** Killed: the
  teleport set changes the sweep, not the output, and there is nothing to
  post-process.
- **Arrow types on the graphview surface** (`arrow.Array` in, out). Killed:
  the widget would import Arrow for a slice, and a caller with plain slices
  would wrap them. The list *layout* is adopted, the library is not.

## Consequences

### Positive

- One shape change between a result and a picture, where there were four,
  and none between a metric and the node it sizes.
- A query's NULL and a query's zero both have a widget meaning, and
  "drawn, no spring" is expressible.
- The metric list exists once, as a type; play, the facts follow-up and the
  navigation layer read it rather than restate it.
- The whole layout can be read back as two columns, which is the widget
  half of the layout persistence ADR-0231 §SD13 defers.
- The two graph tabs and the Map declare their signals the same way.

### Negative

- graphview carries two declaration forms, and the invariant that they
  paint identically is a test to keep, not a fact of the structure.
- The slot-order rule permutes retained state on every topology change; a
  caller that reorders its declaration every frame pays a linear pass every
  frame. No known caller does; the package doc says not to.
- The engine gains a dispatcher and an enum that must be extended together
  with every new algorithm, which is a rule for the next contributor.
- `RenderColumns` is a second entry point a downstream consumer may choose
  wrongly; the doc comment says which shape suits which caller.

### Neutral

- The row form is unchanged to the bit; `nav` and the gallery are untouched
  except for paint order after a removal.
- The engine's algorithms keep their signatures; `Compute` is beside them.
- The double topology build stays, by decision.

## Migration — Tier 1

- **Breaks.** Nothing compiles differently. A scene test that pinned the
  paint order after a node removal will see the new order.
- **Path.** None for a row-form caller. A caller that wants slot-aligned
  columns declares in ascending id order and reads `PositionColumns`.
- **Regeneration.** None; no IDL or generated artifact is touched.
- **Old shape.** `Render` and the specs are kept indefinitely as the
  row form; `Positions` stays beside `PositionColumns`.

## Verification plan — Tier 1

- **Lane.** Default `go test`. graphview: the same declaration through
  `Render` and `RenderColumns` reconciles to identical retained state and
  paints identical commands; NaN-unset against zero-set for every float
  column, including strength zero pulling nothing and opacity zero painting
  nothing; a reordered, an added and a dropped declaration keeping every
  surviving position and pin by id; `PositionColumns` in declaration order;
  `Undirected` painting no head and picking as before; `LabelAlways`
  painting under the label budget; the headless scene for the paint order
  after a removal. Engine: `MetricE` round-trips its string; `Kind` and
  `Symmetric` against a table; `Compute` bit-identical to the direct
  function for every metric on the R-MAT graph at one worker and many; the
  derived metrics against brute force; the seeded rank against a power
  iteration oracle and equal to the unseeded one when the teleport set is
  every vertex. Play: the model's sort, and the signal declaration feeding
  all four registration points.
- **What would fail.** A divergence between the two declaration forms shows
  as a paint-command diff. A slot permutation losing a position shows as a
  node jumping on a topology change, which the scene test pins. A metric
  added to `algo` without its `MetricE` entry fails the vocabulary
  completeness test, which lists the package's exported per-vertex
  functions against the enum.
- **Gap.** The claim that a second application inherits the mapping is not
  testable until there is one; the Projection tab, already a graphview
  consumer with columnar data, is the candidate and is not migrated here.

## Status

Accepted 2026-09-13.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.

## References

- [ADR-0231](./0231-play-graph-contract-widening.md) — the SQL surface this
  record is the substrate for, and the review that counted the hops.
- [ADR-0224](./0224-graphview-go-graph-widget-painter-lane.md) — the
  widget: the declaration contract (§SD1), the sentinels (§SD13, §SD14),
  and the readers.
- [ADR-0225](./0225-graphview-navigation-layer-and-radial-layout.md) —
  `nav`, the row form's main consumer, and the relevance §SD7 replaces with
  a metric.
- [ADR-0229](./0229-graph-analytics-engine.md) — the engine: the CSR and
  its fingerprint (§SD1), determinism (§SD2), budgets (§SD4), the facts
  follow-up (§SD5) and the consumers and migration candidates (§SD6).
- [ADR-0230](./0230-neighbour-graph-and-neighbour-embedding-force-model.md)
  — the Projection tab, the other columnar consumer, named in the gap.
- [ADR-0227](./0227-play-graphview-panel.md) — the network model and the
  id intern the sort of §SD9 applies to.
- [ADR-0096](./0096-play-geo-raster-map-panel.md) — the Map's reserved
  signals, which move onto §SD9's declaration.
- [why-boxer P1](../explanation/why-boxer.md) — the dependency rule that
  kills O4 and the Arrow-typed surface.
