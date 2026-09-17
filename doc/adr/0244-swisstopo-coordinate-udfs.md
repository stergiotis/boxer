---
type: adr
status: accepted
date: 2026-09-17
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-17
---

# ADR-0244: Swiss coordinate transforms as ClickHouse SQL UDFs

## Context

`science/geo/swisstopo` converts between the Swiss national frames and WGS84 in
Go, at two accuracy tiers: swisstopo's truncated series (~1 m, sub-pixel on a
2 m raster) and the closed-form Swiss Oblique Mercator with the CH1903+ datum
shift (~1.5 cm against REFRAME's reference points). Both are used by Go
callers that already hold the coordinates in memory.

Rows, though, mostly sit in ClickHouse. A transform that exists only in Go
means a query that needs one has to leave the server: pull the rows out,
convert, and either push them back or finish the work outside SQL. That costs
the join, the filter and the aggregate that would otherwise have run beside the
data, and it rules out the ordinary things — bucketing LV95 points by WGS84
tile, joining a Swiss dataset to a global one, feeding a map layer straight
from a `SELECT`.

ClickHouse has no Swiss transform of its own. Its geo functions (`geoDistance`,
`geoToH3`, the polygon family) all assume WGS84 input, which is exactly the
input a Swiss dataset does not have.

## Decision

We will ship the transforms a second time, as a family of ClickHouse SQL UDFs
under the owned namespace `SWISSTOPO_`, declared in one commented SQL file that
is the source of truth for them. Both tiers, both directions, plus the LV03
frame offset and the compositions across it. Go reaches the file through
`ScriptSQL` / `StatementsSQL` / `UDFNames`; nothing in the package installs it,
because a package that computes coordinates has no business owning a database
connection.

The plain name is the series and `_EXACT` is the closed form, after ClickHouse's
own `quantile` / `quantileExact` and `uniq` / `uniqExact`: the cheap answer is
what an unqualified call gets, and paying for the precise one is something a
caller asks for by name. It is also the split the Go side already makes —
`LV95ToWGS84` against `LV95ToWGS84Rigorous` — so the two surfaces default the
same way and a reader moving between them is not surprised by which tier they
just called.

Three spellings in that file are not the published formulas, and each is forced
by how ClickHouse evaluates a SQL UDF rather than chosen:

- **`atanh`/`sinh` in place of `ln∘tan` and `2·atan∘exp`.** ClickHouse's `exp()`
  and `log()` are vectorised approximations carrying ~1e-9 relative error — its
  `atanh`, `sinh`, `asin`, `atan`, `atan2` and `hypot` are not — and 1e-9 in
  isometric latitude is ~5 mm on the ground. The identities are exact; only the
  spelling changes.
- **`arrayMap((x) -> …, [v])[1]` as a let-binding.** A UDF parameter is
  substituted textually at every mention, so an intermediate used twice
  duplicates its whole subtree and the stages compound multiplicatively. The
  direct spelling of the inverse chain overflowed `max_expanded_ast_elements`
  (500 000 by default) outright; bound, the same call expands to under 4 000. A
  UDF that takes the lambda itself cannot serve as the binder — ClickHouse
  refuses to nest one inside another as a recursive lambda.
- **A closing `CAST` to a named tuple.** It is what makes a call read as
  `SWISSTOPO_LV95_TO_WGS84(e, n).lat` instead of `.1`, on a surface where the
  classic defect is a swapped pair.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `SWISSTOPO_` SQL function namespace | added | nothing — no other family claims the prefix |
| Exported Go API of `public/science/geo/swisstopo` | added (`ScriptSQL`, `StatementsSQL`, `UDFNames`) | nothing; no existing symbol changes shape |

## Alternatives

- **Leave it in Go and move the rows.** The status quo, and the thing this ADR
  is about: it forfeits every predicate, join and aggregate the server could
  have applied to the converted coordinate.
- **An executable UDF wrapping PROJ or `pyproj`.** Correct, and it brings
  FINELTRA with it, but it puts a binary and its environment on every server
  the family must work on and pays a process boundary per block. "Pure SQL" is
  what makes this installable by a single script on a server nobody administers.
- **Ship only the approximate tier.** One statement, no iteration, no datum
  stage — and ~1 m of error, which is fine for display and not fine for
  anything stored, published, or compared against another implementation.
- **Default to the exact tier and suffix the cheap one.** Safer-looking, and
  wrong twice over: it inverts the convention ClickHouse users already read a
  suffix through, and it charges every caller an order of magnitude per row for
  a precision most of them are about to round away. The accuracy a caller needs
  is a property of what they are doing with the coordinate, which is knowledge
  the call site has and the family does not.
- **Generate the SQL from the Go constants at build time.** One source of truth
  for the constants, at the price of the artifact no longer being a file a
  reader can open or a DBA can install. The constants are derived in SQL the
  same way instead, and a test pins the two derivations against each other.
- **Two scalar functions per direction (`…_LAT`, `…_LON`).** Avoids the tuple
  and its NULL restriction, at twice the call sites and with the lat/lon swap
  put back on the caller.

## Consequences

### Positive

- A Swiss coordinate can be converted where it is stored, so the predicate and
  the aggregate around it stay server-side.
- Both accuracy tiers travel, so a caller picks the same trade-off in SQL as in
  Go rather than getting whichever one happened to be ported — and picks it the
  way ClickHouse already asks such a choice to be made.
- The namespace makes the family enumerable — `system.functions WHERE name LIKE
  'SWISSTOPO\_%'` — which is how a provisioned server is told from a partly
  provisioned one.

### Negative

- A few dozen names in a server-global namespace, installed for the whole
  server rather than a database. Two boxer versions sharing a server share one copy of
  the family, so a member's meaning has to stay append-only: changed behaviour
  needs a new name.
- The `_EXACT` inverse is about an order of magnitude dearer per row than the
  default tier, most of it the six-step fixed-point iteration. The naming makes
  that the opt-in, but it also means a caller who needs centimetres and does
  not know the convention silently gets metres. The tolerance is documented at
  both names; nothing enforces it.
- A `Nullable` argument fails the call: ClickHouse has no `Nullable(Tuple)`, so
  the named-tuple result type cannot hold one. Callers route nullable columns
  through `ifNull(col, nan)`, which travels the chain and returns `(nan, nan)`.
- Formulas now exist twice. The integration lane below is what keeps the copies
  from drifting.

### Neutral

- No installer ships with the family. Every caller that would provision it
  already holds a client, and the two accessors cover both transports (one
  multi-statement script, or one statement per request).
- FINELTRA and the LN02/LHN95 height transformation are out, in SQL as they are
  in Go: both need data a UDF cannot carry. `SWISSTOPO_LV03_TO_LV95` is the
  nominal frame offset, and the two realizations differ by up to ~1.6 m beyond
  it.

## Verification plan — Tier 1

- **Lane.** The `//go:build integration` lane in the package, against a live
  server. It installs the family, then differential-tests both tiers and both
  directions against the Go functions over a lattice covering the national
  extent, re-runs the REFRAME reference points through the server at the
  tolerances the Go tests demand, and checks the derived constants where a
  drift is cheapest to localise. The default lane pins the file's statement
  split and its dependency order, which is load-bearing because ClickHouse
  resolves a called function at CREATE time.
- **What would fail.** A coefficient, a constant or an iteration count that
  drifts from the Go twin moves the result by millimetres at least, four orders
  above the agreement the lane demands. A reordered file fails before it
  reaches a server.
- **Gap.** Nothing here measures the family against REFRAME itself beyond the
  reference points the Go tests already carry, and nothing measures the ~1.5 cm
  datum residual, which is a property of the three-parameter shift and not of
  either implementation.

## Status

Accepted — 2026-09-17, following review of the family, its two-tier naming, and
the three ClickHouse-imposed spellings.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0162](./0162-leeway-co-ragged-function-pack.md) — the repository's other
  SQL function pack, and where the namespace-and-family naming convention this
  one follows comes from.
- swisstopo, *Formulas and constants for the calculation of the Swiss conformal
  cylindrical projection and for the transformation between coordinate systems*
  — the source of both tiers.
- EPSG:1676, *CH1903+ to WGS 84 (1)* — the three-parameter datum shift.
