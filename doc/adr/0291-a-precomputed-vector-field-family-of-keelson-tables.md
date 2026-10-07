---
type: adr
status: proposed
date: 2026-10-07
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0291: a precomputed vector field as a family of keelson tables, read by the vector field pane through named arguments

## Context

The vector field pane
([ADR-0250](./0250-a-sql-backed-vector-field-source-and-plays-vector-field-pane.md))
reads a field from SQL. It does not send the statement a person types: given a
relation, `sqlfield` sends six statements of its own, one per purpose — probe,
steps, geometry, regularity, window, summary — and they use subqueries, typed
query parameters, `arrayJoin`, `quantileIf`, `uniqExact` and `avgWeightedIf`.
They need ClickHouse, so the pane cannot run where ClickHouse is absent: in a
browser tab served as static files, which is where a demo a reader can open
would live.

[ADR-0290](./0290-keelson-named-arguments-and-a-trivial-sql-endpoint.md) lets a
keelson table take named arguments bound from query parameters, and answers
`SELECT * FROM keelson(…)` without ClickHouse. Two properties of `sqlfield` let
such tables stand in for its six statements:

- Four of them — probe, steps, geometry, regularity — depend on the step alone,
  so their answers can be computed once per step.
- The window's bins are aligned to the grid's origin, not to the request, and
  its factor is a power of two. Any window the pane can ask for is therefore a
  sub-rectangle of one per-level binned grid; what a request adds is a range on
  step, level, row and column. The summary reduces the decimated nodes inside
  the bounds, again a range.

The window and the summary cannot be precomputed outright: they depend on the
view's bounds and on the pane's size in pixels, which cannot be enumerated.

## Decision

We will register a precomputed field as a **family of keelson tables** with
named arguments, built once from the field's planes, and give `sqlfield` a
**keelson statement set** that reads the family in place of its six aggregating
statements when the relation names a family. An integration test holds the two
statement sets to the same answers.

### Subsidiary design decisions

- **SD1 — The family.** For a field registered under a base name, a Go package
  registers keelson tables: `<base>`, the relation itself (`t, lat, lon, u, v`),
  which a probe and a person's `SELECT *` read; `<base>_opts`, what
  `vector_field_opts` carries (`name, unit, speed_max`) and the base under
  `family`; and one table per purpose — `<base>_steps`; `<base>_geometry` and
  `<base>_regularity` (argument `t`); `<base>_window` (`t`, row and column
  bounds, the turns of a periodic grid, the plan's node filter); and
  `<base>_summary` (the node filter). A purpose table's arguments are named
  after the parameters its statement binds, and it replies in that
  statement's shape. With `<base>_opts` in place of a literal options CTE, a
  statement over a family is `SELECT *` over keelson calls throughout, which the
  trivial endpoint of ADR-0290 answers.

- **SD2 — Computed from the planes, per request.** Each purpose table computes
  what its statement computes — the same filters, the same grouping, means in
  float64 rounded to float32, non-finite values excluded as `isFinite`
  excludes them. A window reads the nodes of one step once; for a grid of a
  degree or coarser that is tens of thousands of nodes, cheap enough per
  request in a tab. A binned grid per power-of-two level, which would make a
  window a slice, is deferred until a finer grid needs it (Q3).

- **SD3 — Any planes.** The family takes its input from any source that yields
  two `float32` planes per step on a regular grid
  ([ADR-0249](./0249-vector-fields-on-the-map-particles-over-a-batched-segment-opcode.md)),
  so it is independent of the file format behind them.

- **SD4 — `sqlfield` reads the family when the relation says so.** A
  `sqlfield.Relation` names a family base; a host sets it from the `family`
  column of `vector_field_opts`. When it is set, `sqlfield` sends its keelson
  statement set, without the relation's head, which the family does not need —
  `SELECT * FROM keelson('<base>_window', step = {ff_step:UInt32}, level =
  {ff_level:UInt8}, …)` and its siblings — with the parameters it sends today,
  and reads replies of the same shape. When it is not, nothing changes. The
  option is explicit, so a relation is never switched by a guess about the
  endpoint, and the keelson set is valid natively as well (ADR-0290 §SD2).

- **SD5 — Parity is tested, not assumed.** An integration test builds one field
  relation, answers every purpose both ways — the six aggregating statements in
  ClickHouse, the family through the keelson set — and holds the replies equal
  within float tolerance, including a window across the seam of a periodic grid
  and a view coarser than the grid.

### Deferred and open

- **Q1 — Which demo data.** A real forecast needs a GRIB reader, which
  ADR-0249 deferred to an ADR of its own; until then the family is filled from a
  synthetic field such as the tour's storm.
- **Q3 — Per-level grids.** SD2 computes each reply from the native planes.
  A 0.25° global grid is a million nodes a step, where a binned grid per level
  would answer a coarse window from a few thousand; it is built when such a
  grid is served in a tab.
- **Q4 — Array arguments.** ADR-0290's arguments are scalars, so the window's
  turns travel as the text of an `Array(Int64)` literal and the table parses it.
- **Q2 — Families of other fields.** Only the vector field's purposes are
  served; whether another pane gains a family is that pane's decision.

### Milestones

- **M1 — The family and the statement set.** SD1–SD5: the family package, the
  keelson statement set in `sqlfield`, the parity test.
- **M2 — The demo.** A tab demo of play over a family filled from a synthetic
  field, then from real data once Q1 is decided.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Vector field family | added: a package registering `<base>_*` tables | the parity test |
| `sqlfield` statements | added: the keelson statement set, chosen by an option | `vector_field_opts` gains a column |

## Alternatives

- **Evaluate the pane's six statements without ClickHouse.** Rejected: it is a
  query engine with ClickHouse's semantics, widened by every statement
  `sqlfield` adds (ADR-0290, O1).
- **A Go source behind the pane, bypassing SQL.** Rejected: the tab would show a
  field that no SQL produced, which is the opposite of what the demo is for,
  and the pane would carry a second data path.
- **Precompute every result.** Rejected: window and summary depend on the
  view's bounds and the pane's size in pixels. SD2 precomputes what does not
  depend on them and slices the rest.

## Consequences

### Positive

- The vector field pane runs in a static tab over SQL, with no ClickHouse.
- A family is one more set of keelson tables, so it is readable from any SQL
  surface, natively as well.
- The parity test keeps the family honest against the statements it replaces.

### Negative

- `sqlfield` carries two statement sets, held together by the parity test.
- The family holds the field's planes in memory, and the relation table
  materialises every node of every step when it is read whole.

### Neutral

- The family serves the vector field's purposes only; it is not a general cache
  of query results.

## Migration — Tier 1

None. `sqlfield` without the option behaves as before.

## Verification plan — Tier 1

- M1: the parity test of SD5 against clickhouse-local, with windows across the
  seam, coarser and finer than the grid, and over masked nodes.
- M2: the demo scene captured in the tab and natively, compared.

## Status

Proposed 2026-10-07.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.

## Updates

### 2026-10-07 — M1 built

`keelsonfield` registers the family of SD1 from a `vectorfield.Grid` per step;
`sqlfield.Relation.Family` switches a source to the keelson statement set. The
parity test builds a masked 2° global swirl over three steps and reads it three
ways — the six reduction statements over `keelson('<base>')` in
clickhouse-local, the keelson set through the same engine (named arguments
resolved natively), and the keelson set through the trivial evaluator with no
ClickHouse — and finds the descriptions equal, the windows and summaries equal
within float32 summation order, for a global, a regional, a seam-crossing, a
masked and a finer-than-native request; the palette's high end agrees within
5 %, since ClickHouse's quantile samples. The body above was revised as built:
the relation and options tables joined the family, and windows are computed
per request rather than sliced from per-level grids.

### 2026-10-08 — M2 built: the demo

The Pages demo binary registers the GFS forecast of ADR-0292 as `gfs_wind`
with `keelsonfield.RegisterLazy`, which decodes it on the first read rather
than at start, beside the static tables, and serves them through the trivial
endpoint (ADR-0290 §SD4). A landing-page card opens play with the family as its
`vector_field` and `vector_field_opts` and the Vector field pane in front; the
tab draws the forecast with no ClickHouse. Two settings keep the page's claim
that it loads nothing from elsewhere: `basemap.SetOffline` starts every map on
its offline outlines, and `tabhost.Services.NoEgress` refuses any request that
would leave the tab, whatever a visitor switches on. With play the first visit
downloads about 27 MB.

The outlines show a defect in the tab that predates this work: long straight
grey lines across the map, the same in the gallery's flow-on-a-map demo and
whatever the field's longitude convention. A README image of the demo waits
for it.

## References

- [ADR-0249](./0249-vector-fields-on-the-map-particles-over-a-batched-segment-opcode.md) — vector fields; GRIB deferred.
- [ADR-0250](./0250-a-sql-backed-vector-field-source-and-plays-vector-field-pane.md) — the vector field pane over SQL.
- [ADR-0290](./0290-keelson-named-arguments-and-a-trivial-sql-endpoint.md) — named arguments and the trivial endpoint this builds on.
