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
  registers companion keelson tables: `<base>_steps`; `<base>_geometry` and
  `<base>_regularity` (argument `step`); `<base>_window` (`step`, `level`, row
  and column bounds, the turns of a periodic grid); `<base>_summary` (bounds and
  level). Each replies in the shape of the statement it replaces.

- **SD2 — Built once, sliced per request.** At registration the package
  computes, per step, the binned grid of every power-of-two level from the
  native planes — about a third more than the raw data — with the per-bin vector
  mean, scalar mean speed and valid count the window statement returns. A window
  is a slice of one level; a summary reduces the decimated nodes inside the
  bounds. Nothing in a request is computed at a cost proportional to the
  native grid.

- **SD3 — Any planes.** The family takes its input from any source that yields
  two `float32` planes per step on a regular grid
  ([ADR-0249](./0249-vector-fields-on-the-map-particles-over-a-batched-segment-opcode.md)),
  so it is independent of the file format behind them.

- **SD4 — `sqlfield` reads the family when the relation says so.** The pane's
  options relation (`vector_field_opts`) gains a column that names the family
  base. When it is set, `sqlfield` sends its keelson statement set —
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
- The family holds every level of every step in memory, about 1.33× the field.

### Neutral

- The family serves the vector field's purposes only; it is not a general cache
  of query results.

## Migration — Tier 1

None. `sqlfield` without the option behaves as before.

## Verification plan — Tier 1

- M1: the parity integration test of SD5 against a ClickHouse server; unit tests
  of the per-level grids and of a window across the seam.
- M2: the demo scene captured in the tab and natively, compared.

## Status

Proposed 2026-10-07.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.

## References

- [ADR-0249](./0249-vector-fields-on-the-map-particles-over-a-batched-segment-opcode.md) — vector fields; GRIB deferred.
- [ADR-0250](./0250-a-sql-backed-vector-field-source-and-plays-vector-field-pane.md) — the vector field pane over SQL.
- [ADR-0290](./0290-keelson-named-arguments-and-a-trivial-sql-endpoint.md) — named arguments and the trivial endpoint this builds on.
