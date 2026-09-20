---
type: adr
status: accepted
date: 2026-09-20
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-20
---

# ADR-0250: a SQL-backed vector-field source, and play's Vector field pane over a named CTE

## Context

ADR-0249 put gridded vector fields on the portolan map: a data contract
([the vectorfield package](../../public/science/geo/vectorfield/)), an
in-memory pyramid behind it, and a particle layer
([the flowoverlay package](../../public/thestack/imzero2/egui2/widgets/portolan/flowoverlay/))
that asks its source for windows bounded by the request. It deferred "a
ClickHouse-backed source for play, on the ADR-0096 pattern of a viewport-keyed
query", and shaped the window so that one could exist. This ADR takes that
deferral up.

What makes it a decision:

- **The field is larger than a result.** play's panes read a node's result. A
  global quarter-degree wind is about a million rows per step and a forecast
  is tens of steps; handing that to a pane as a result moves the whole field
  to the client to show a few thousand samples of it. The reduction belongs
  where the data is.
- **play has two ways of binding SQL to a pane, and they disagree.** The Map
  (ADR-0096) splices a table source typed into a panel field into a
  panel-authored template; the later panes (ADR-0122, ADR-0129, ADR-0231) read
  CTEs the buffer names, so the buffer stays the whole artifact (ADR-0097) and
  everything upstream of the CTE composes. A field pane has to choose.
- **The layer asks from goroutines of its own**, for the two steps around the
  display time and one ahead, and cancels what a pan supersedes. play's lanes
  are frame-driven single slots keyed by the signal snapshot. The two do not
  fit without losing one side's behaviour.
- **A wrong field draws a plausible map.** ADR-0249 SD1 lists what a source
  must get right, each clause a defect some tool shipped. A SQL source moves
  that responsibility into a query someone types.

Measured before deciding, on one machine with a local server and a synthetic
six-step quarter-degree global table ordered by time, latitude, longitude: a
reduction query wrapped around a CTE that only renames the table's columns
still prunes on the primary key (a regional view read 23 of 764 granules), and
the whole-world reduction of one step to about fifteen thousand samples
returned in under a tenth of a second. That is one observation, not a trial;
it says the shape is viable, not what it costs elsewhere.

## Design space (QOC)

**Question.** By what route does a field held in ClickHouse become the windows
the flow layer draws?

**Options.**

- **O1** — the pane reads the field as a result and builds the in-memory
  pyramid from it.
- **O2** — one query per step loads that step's native grid into the
  in-memory pyramid through its step loader.
- **O3** — a source whose every window request is one reduction query, keyed
  by parameters.
- **O4** — the query is written by the user against published viewport
  signals, and its result *is* the window.

**Criteria.**

- **C1** — cost independent of the field's size (rows crossing the wire per
  view).
- **C2** — the ADR-0249 SD1 promises hold without the user having to know
  them.
- **C3** — the layer keeps its request behaviour: bracketing steps, prefetch,
  supersession, hysteresis.
- **C4** — queries per interaction.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | −  | ++ | ++ |
| C2 | ++ | ++ | +  | −− |
| C3 | ++ | ++ | ++ | −− |
| C4 | ++ | +  | −  | −  |

## Decision

We will add a `vectorfield.SourceI` whose windows are reduction queries over a
**field relation**, in a package of its own beside the contract, and bind it in
play to a CTE named `vector_field` shown by a new **Vector field** pane whose
drawing core is a portolan guest (O3).

### Subsidiary design decisions

#### SD1 — the field relation: five reserved column names on a regular grid

A field relation yields `lat`, `lon`, `u`, `v` and optionally `t`: degrees,
earth-relative east and north components, one row per grid node per step, on a
grid regular in latitude and longitude. `t` is a `Date`, `DateTime` or
`DateTime64`; without it the field has one step. Longitudes may run −180…180
or 0…360.

Names, not positions or detection — the ADR-0122 §SD2 rule — so a misspelt
column is a stated reason and not a guess. Level, run, member and variable are
not columns of the contract: the query filters to one (ADR-0249 SD1), and a
picker is a parameter the CTE reads.

What ADR-0249's how-to makes the loader's job is the query's job here, and SQL
can say each of it: the rotation is arithmetic, the row order is irrelevant,
a sentinel is `nullIf`. `NULL` and non-finite values are both missing.

#### SD2 — a window is one aggregation, aligned to the native grid

The source describes a relation once — the steps, then the geometry of the
first step — and answers each request with one query that bins native nodes by
integer division of their grid index, and returns per bin the vector mean, the
scalar mean of the magnitude, and the count of valid nodes. The client lays
the rows into planes filled with `NaN`, and keeps a bin only where the valid
count reaches the contract's valid fraction of the nodes the bin covers.

- **Bins are aligned to the grid's origin, not to the request**, so a pan
  re-reads the same samples and the picture does not shimmer. The reduction
  factor is a power of two, the same along both axes: a window is never finer
  than asked and less than twice as coarse, which is the band the flow
  layer's hysteresis is built around, and where the in-memory pyramid serves
  a level it built by halving the two sources return the same samples.
- **Native columns are addressed in an unwrapped frame** — column *c* of turn
  *n* is *c + n·cols* — so a window across the seam of a periodic grid is
  contiguous, and one at world zoom may hold a column twice. The turns a
  window spans are a parameter the query joins each node against; two
  longitude ranges on the relation's own `lon` keep the scan to the nodes
  that can land in it. Periodicity is the field's property, read off the
  geometry as the pyramid reads it, a repeated seam column included.
- **A regional field may be stored in the other longitude convention than the
  request uses** (0…360 against −180…180); the request is moved onto the
  field by whole turns and the window handed back in the request's frame.
- **The describe step refuses what the binning would hide**: more rows in a
  step than the grid has nodes (an unfiltered level or run, which a mean would
  merge silently), nodes off the regular grid (a Gaussian grid), a step count
  past a bound.
- **A reply is bounded twice**: the query carries a `LIMIT` derived from the
  request, and the client refuses a row outside the window it asked for.
- The source caches nothing. The layer already holds the windows it shows; a
  second cache would need an invalidation rule and the server has one.

`vectorfieldtest.Run` is the check that the promises hold, run against a
server in the integration lane.

#### SD3 — values ride parameters; the text is fixed per relation

Bounds, factors, the step and the limits are `{name:Type}` slots sent as
`param_*`. The statement text for a relation never changes with the view, so a
server-side query cache or a log reader sees one query. The slots carry a
prefix the package reserves.

Two things are text. The relation — the caller's own SQL, which play takes
from the buffer — and the type name of `t`, which the server reports and the
package checks against a closed character set before it becomes the step
slot's type. Typing the slot as the column's own type is what lets equality on
`t` reach a primary key, and the step's value is the server's own rendering of
it, so no literal is encoded client-side.

#### SD4 — the package runs queries through a seam, and speaks canonical SQL

The source takes a `QueryerI` — statement and parameters in, Arrow record out
— and imports no client and no UI, as ADR-0249 SD1 asks of the contract. play
adapts its `Client`, so the pre-execute passes (ADR-0108), the dispatch seam
(ADR-0141) and the `log_comment` stamp (ADR-0115) apply to a window query as
to any other; a test adapts a local server.

The generated text stays inside what the canonicaliser round-trips: function
calls, no `DIV` operator (the constraint ADR-0096's raster template records).
A test sends the templates through play's statement builder.

The source does not go through a node lane. `SampleE` is called concurrently
and synchronously from the layer's goroutines and is cancelled through its
context; a lane is one frame-driven slot with last-good semantics. The pane
reports the source's last statement, parameters, duration and error itself.

#### SD5 — play binds the CTE `vector_field`, and an optional `vector_field_opts`

The pane is a `PanelI` whose required channel is fed by name from the split,
like the Network's `edges`. What the channel carries is the relation's
**schema** — the fused node under `LIMIT 0` — so `AcceptForChannel` judges
column names and types with a reason per failure and the dock strip shows the
verdict, and no row of the field reaches the pane.

The relation handed to the source is the fused node (its `SET` prelude and
upstream CTEs) with the node as the last `WITH` item. Its identity is that
text plus the resolved values of the signals it reads; a new identity is a new
source and a new layer.

`vector_field_opts` is one row of optional, schema-visible columns (the
ADR-0231 §SD5 form): `name`, `unit`, `speed_max`. Without `speed_max` the
describe step takes a high quantile of the first step's magnitude. What is a
matter of taste — density, trail length, opacity, basemap — is a pane control.

#### SD6 — the pane writes its time and its viewport as reserved signals

`vf_t` is the valid time of the step nearest the display time; `vf_min_lat`,
`vf_max_lat`, `vf_min_lon`, `vf_max_lon` are the settled view. They are rows
of `reservedSignals`, written when they change and the control rests, never
per animation frame. Another node — stations at that hour, a table of the
strongest gusts in view — follows the map by referencing them. `vf_t` is also
read: a value the pane did not write (a `SET`, the Signals editor, a history
restore) moves the display time.

#### SD7 — the drawing core is a guest

Source management, the layer and its options live in a type with a
`Draw(portolan.Projector)` and no map of its own. The pane hosts it in a map
with the configured basemap or the land outlines. Hosting it under the Map's
raster or a located graphview is then call order (ADR-0249 SD6), and is
deferred.

### Milestones

- **M1 — the source.** ✓ The describe and window statements, factor selection,
  decoding and validation, against a fake `QueryerI`.
- **M2 — conformance.** ✓ `vectorfieldtest.Run` and a primary-key pruning check
  against a local server, in the integration lane.
- **M3 — the pane.** ✓ The channel and its verdict, the guest, the signals, the
  options row, the status line with the served statement, the pointer readout
  through `Layer.At`.
- **M4 — a field to look at.** ✓ A snippet that computes an analytic field from
  `numbers()` so the pane works against any server, a scene, the help page and
  the how-to.

### Deferred

- **Hosting the guest in the Map tab and under a located graphview** — when a
  second layer on one camera is asked for.
- **A per-step loader into the in-memory pyramid (O2)** — trigger: an endpoint
  whose per-query latency makes two queries per settled view worse than one
  large transfer per step.
- **Precomputed level tables** — a relation that carries its own reductions —
  when a field's native window scan is measured to be the cost.
- **A non-temporal `t`** (a member or a step index) and **interval steps**.
- **A trial under `doc/trials/`** for window latency by field size and
  endpoint distance; until then no figure from the Context travels.
- **A step ahead.** The source implements no prefetch, so playback waits on
  each step's window; the layer's `PrefetchE` seam is there when a field is
  played often enough to want it.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API under `public/` | new package `science/geo/vectorfield/sqlfield` | its `package_props.go` and the proptable harvest |
| play's reserved signals (`reservedSignals`) | `vf_t` and the four viewport names added | the declaration test, the tab's `Writes` |
| play's reserved CTE names | `vector_field`, `vector_field_opts` | `splitFedChannel`, the tab registry and its frozen dock id, the help book |

## Alternatives

- **O1 — the field as a result.** Every row of every step crosses the wire
  before the first particle moves, and a pane's result has no notion of being
  asked again for a different window.
- **O2 — a step at a time into the pyramid.** Pans and zooms cost no query and
  the contract work is already done and tested. It moves a whole native step
  per step — tens of megabytes for the grids ADR-0249 names — and leaves
  ClickHouse serving rows it could have reduced. Kept as a deferral with a
  trigger, because for a distant endpoint it may be the better trade.
- **O4 — the user writes the window query against viewport signals.** The most
  SQL-native shape and the Map's original one. The ADR-0249 SD1 promises —
  gutter, node registration, the scalar mean, the valid fraction — would be
  the user's to restate in every query, and requests keyed through the signal
  store are one per frame snapshot: no second bracketing step in flight, no
  prefetch.
- **A table-source field on the pane, as the Map has.** Text outside the
  buffer is not in the artifact, cannot read a parameter, and cannot sit
  downstream of a join. ADR-0096's 2026-08-15 Update records what the spliced
  source already costs.
- **Window queries through a node lane.** See SD4.
- **Inlining values into the statement.** A different text per view defeats
  any per-statement cache and log grouping, and needs a literal encoder the
  parameter channel makes unnecessary.
- **`toString(t) = {…:String}` for the step.** No type name in the text, and
  no primary-key use on `t` either.

## Consequences

### Positive

- A field of any size is drawable from a table without an ingestion step in
  Go, and everything upstream of the CTE — a join, a level picker, a unit
  conversion — is ordinary SQL.
- The source is usable outside play by anything that can run a query.
- Other nodes can follow the field's time and view.

### Negative

- A settled view costs queries — one per bracketing step — where the
  in-memory pyramid costs none. On a slow endpoint the last good window stays
  on screen for as long as they take.
- The relation must be a regular lat/lon grid. A reduced Gaussian or a
  projected grid has to be resampled in SQL first, or is refused.
- The describe step scans the relation's `t` column once per identity, which
  for a relation without a usable key is a full scan.
- A second implementation of the window contract exists beside the pyramid;
  the conformance suite is what keeps them the same contract, and the
  integration test that compares the two sample for sample.

### Neutral

- Pruning depends on the user's table and on what the CTE does to its
  columns; a CTE that computes `lat` from something else reads every row of
  the step. The pane shows the served statement so that `EXPLAIN` is a paste
  away.
- The Map keeps its spliced table source; this ADR does not revisit it.

## Migration — Tier 1

Nothing to migrate: the package, the pane, the CTE names and the signals are
additive. A buffer that already names a CTE `vector_field` with other columns
gets a pane that states why it does not draw, and is otherwise unaffected.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the statements, factor selection, decoding
  and validation against a fake `QueryerI`, and for the pane's channel
  verdicts and signal declaration. The integration lane for
  `vectorfieldtest.Run` against a local server and the pruning check. A scene
  for the pane drawing trails from the analytic snippet.
- **What would fail.** A broken contract clause fails the conformance suite,
  which the integration lane also runs over the statements in canonical form;
  a template the canonicaliser cannot round-trip fails
  `TestVectorFieldStatementsSurviveCanonicalization`; a value that reaches
  the text instead of a parameter fails `TestEveryRequestSendsTheSameText`.
- **Gap.** Whether a user's relation is earth-relative cannot be checked, as
  in ADR-0249. Latency on a distant endpoint is unmeasured (see *Deferred*).

## Status

Accepted 2026-09-20.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-09-20 — the pane's signals are seeded, and a window query opens run

Two defects found while writing the tour's scenes, both in what §SD6 and §SD5
shipped.

**§SD6's names blocked a Run until written, and could then never be
written.** They were declared like the Map's viewport. The Map queries on its
own and writes its viewport whatever the buffer does; this pane learns of its
CTE from a Run. A buffer whose sink read `vf_t` was refused for the unfilled
name, so the split never reached the pane, so the name stayed unfilled. They
are seeded now, as the Graphview's are for the same reason: `vf_t` with the
epoch, the four bounds with zero. A query filtering on the seeds selects
nothing; the pane overwrites them once its field is described, and a Live
sink follows from there. `TestVectorFieldSignalsAreDeclared` holds the seeds,
and the tour's `36_vector_field_follow` scene runs the case end to end.

**The served statement opened unrun showed its first line only.** The
playground it opens in is now launched to run, which is also the more useful
state: the result is the window's bins and the parameters are pinned in the
pane. The `36_vector_field_window_query` scene covers it.

### 2026-09-20 — the pane reports the queries it runs

Between a Run and the first particle the pane sends five statements one after
another — §SD5's shape probe, the three §SD2 describes, and the first window —
and said so with one sentence on the status line. Where the describe is two
full scans, that reads as a pane that has stopped.

It now carries the readout a lane-owning panel already has (ADR-0115 plane A,
estimated by ADR-0247's estimator): a spinner, a bar and the statement's
counters beside the controls, following one phase at a time — the describe
first, since nothing is on screen for it, then a window, then the per-step
summary of ADR-0251 §SD4. The status line keeps the words, and no longer
prints a window clause before there is a window to describe.

Two restrictions are the decision here:

- **Only the describe and the summary ask the server for in-band progress.**
  The transport that surfaces a progress line mid-run takes one request per
  connection, and a window is short and fired several at a time by the
  look-ahead. Windows stay on the pooled client and read as running without
  numbers.
- **Only the describe and the summary can be cancelled.** A window is asked
  for from the view on screen, so the next frame would ask for it again. The
  two that can be cancelled latch — the describe keeps its identity claimed
  and the summary its view key — and a Run is what asks again.

## References

- ADR-0249 — the field contract, the flow layer, and the deferral taken up here.
- ADR-0096 — the Map's viewport-keyed raster and the canonical-form constraint on generated SQL.
- ADR-0097 — the buffer as the artifact, lanes, signals, channels.
- ADR-0122, ADR-0129, ADR-0231 — panes fed by named CTEs; the options row.
- ADR-0108, ADR-0115, ADR-0141 — what play's client applies to every statement.
- [How to draw a vector field on a map](../howto/vector-field-on-a-map.md).
