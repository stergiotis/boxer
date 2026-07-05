---
type: adr
status: proposed
date: 2026-07-05
---

> **Status: proposed — pre-human-review.** The v1 slice is implemented
> alongside this record; treat the decision as open until reviewed.

# ADR-0108: keelson SQL pass registry — a pre-execute seam with an introspectable catalog

## Status

Proposed — 2026-07-05. v1 implemented: `public/keelson/data/passreg` (+
`defaults`), the `sql_passes` provider, and both consumers.

## Context

Nanopass passes are first-class values (`nanopass.Pass`, ADR-0006), but their
wiring is point-to-point: every executor of user-authored SQL imports every
pass producer it wants applied. Two live pressures show the cost:

1. **`identsql.ExpandPass`** (ADR-0106 §SD5) rewrites `LW_ID_*` macro calls
   into bit arithmetic. It must run wherever user-authored SQL is executed.
   The planned wiring into `apps/play` would couple play to
   `public/identity/identsql` directly — and every future executor would
   repeat that import, one wiring site per producer × consumer pair.
2. **The introspection `/query` endpoint** (ADR-0094 §SD4,
   `introspecthttp.handleQuery`) executes user SQL via `clickhouse-local`,
   which does not have the `LW_ID_*` UDFs installed (those are emitted for
   real servers). An unexpanded macro reaching that path fails at query time;
   nothing today gives that seam the expansion.

Separately, there is no inventory of passes: which passes exist, with which
declared `PassProperties`, is discoverable only by reading source. ADR-0094
gave keelson runtime state a queryable-table surface; a pass catalog is a
natural fit.

Both play and the `/query` endpoint funnel through exactly one choke point
each (`play.Client.ExecuteArrowStream`; `introspecthttp.handleQuery`), so a
shared seam covers both with one application site apiece.

## Decision

A new package `public/keelson/data/passreg`: a registry of `nanopass.Pass`
values keyed by *stage* (a semantic execution point), consumed by the
executors of user-authored SQL and exposed as an introspection table. It
lives under `keelson/data/` because it is ClickHouse glue serving both app
and runtime code, the same rationale as `chclient`/`chlocalbroker`
(ADR-0035). The dependency direction is safe: keelson already imports
nanopass; nanopass never imports keelson.

### SD1 — Entry and registry semantics

```go
type Entry struct {
    Pass        nanopass.Pass
    Stage       StageE
    Order       int    // composition order within a stage; ties break by Pass.Name
    Description string // one line, shown in the catalog
    Provenance  string // import path of the package providing the pass
}
```

- The registry key is `(Stage, Pass.Name)`. A duplicate registration is an
  error and the first registration stays — mirroring `introspect.Registry`.
- `Register` validates: known stage, non-empty pass name, non-nil `Apply`.
- `Entries(stage)` returns a copy, sorted by `(Order, Name)`. Registration
  order is deliberately not trusted: it varies with host wiring order, and
  composition must be deterministic across processes.
- A package-level `Default` registry plus `Register`/`Entries` wrappers
  mirror the introspect idiom. Everything also works on an explicit
  `*Registry` instance so tests never touch process-global state.

### SD2 — Stages are semantic execution points, not consumer names

A stage names *where in the life of a statement* the passes apply, so any
consumer sitting at that point applies the same set — that is the point of
the seam: a pass registered once behaves identically in play and in
`/query`. v1 defines exactly one stage:

- `StagePreExecute` — rewrites applied to user-authored SQL immediately
  before it is shipped to an executor (remote ClickHouse server or
  chlocal). Body-only rewrites; the user's editor/preview text is never
  touched.

The enum is extensible; new stages arrive with their first consumer, not
speculatively.

### SD3 — Consumption: the registry hands out entries, strictness is the consumer's

Two helpers cover the known consumption modes:

- `Compose(name, stage)` — a strict `nanopass.Sequence` over the stage's
  entries (shared env, first error aborts). For pipeline builders that own
  their input.
- `ApplyBestEffort(stage, sql, logger)` — applies each entry's `Pass.Run`
  in order; an entry that errors is logged and skipped, keeping the prior
  SQL, and later entries still run. This is the existing play degradation
  idiom (`SetFormat` falls back to a textual append): user SQL may
  legitimately exceed Grammar1, and a rewrite failure must never block
  executing otherwise-valid SQL. Entries at `StagePreExecute` are
  independent body rewrites, so skipping one does not invalidate the next.

`ApplyBestEffort` round-trips each pass independently (`Run`, not a shared
env). A `SET` prelude survives — `Extract`/`Integrate` re-emit it — but is
normalised; consumers needing shared-env semantics use `Compose`.

### SD4 — Registration is explicit aggregation, not init() side effects

`passreg/defaults` registers the standard set; hosts call
`defaults.RegisterDefaults()` once at wiring time (the carousel host next to
`introspecthost.Start`, the standalone play CLI in its action). Producers
stay keelson-free: `identsql` does not learn about the registry; the
aggregator imports both sides.

init()-time self-registration was rejected: it would make every importer of
a producer package (its tests, CLI tools) mutate process-global keelson
state as an import side effect, and the active set per process would be
implicit in the import graph rather than reviewable at a wiring site. (Apps
self-register via init(), but apps are import leaves; library packages are
not.)

The v1 standard set is exactly one entry: `identsql.ExpandPass` at
`StagePreExecute`, order 100 (leaving room on both sides), provenance
`github.com/stergiotis/boxer/public/identity/identsql`.

### SD5 — Catalog: `keelson('sql_passes')`

An introspect provider in `runtime/introspect/providers` snapshots
`passreg.Default` as table `sql_passes`: stage, name, order, idempotent,
needs_fixed_point, reads, writes (region names as `Array(String)`),
provenance, description. `FreshnessLive` — registration happens at boot in
practice, but Live keeps late registration honest and the table is tiny.
This is the inventory half of the decision: which rewrites a given process
applies becomes a query, not a source dive.

### SD6 — v1 consumers

- **play** (`Client.ExecuteArrowStream`): `ApplyBestEffort(StagePreExecute)`
  on the residual SQL, between `ExtractParams` and `SetFormat`. The editor
  and preview show the user's text unexpanded; only the shipped statement is
  rewritten. This lands the pending identsql→play wiring in registry form.
- **introspection `/query`** (`introspecthttp.handleQuery`):
  `ApplyBestEffort(StagePreExecute)` before `keelsonsql.RewriteToURL`.
  `introspecthttp.Config` gains a `Passes *passreg.Registry` field, nil
  defaulting to `passreg.Default`, so tests inject their own. Rationale:
  chlocal has no `LW_ID_*` UDFs, and non-play clients (curl during
  development) hit `/query` directly.

Double application — play expands client-side, then `/query` expands again
server-side — is harmless: a fixpointed macro expansion leaves no expandable
calls behind, and best-effort application keeps even a pathological second
run from blocking execution.

`introspectengine.Query` is the same semantic seam but currently has no live
consumer; it adopts the stage when it gains one (deferred, not gated).

### SD7 — Scope cull

Descoped, deliberately:

- **FormTag dependency solving.** `Requires`/`Produces` stay documentation
  (ADR-0006 v1); explicit `Order` is the composition mechanism until a real
  ordering conflict between independent registrants appears.
- **Play UI toggles / per-user enable-disable.** The entry metadata is the
  read model a future toggle UI needs; nothing in v1 renders one.
- **Registry-fying the canonicalization pipeline.** `CanonicalizeFull` is a
  fixed internal `Sequence` with a proof obligation (Grammar2 closure), not
  an extension point.
- **Pass factories as entries** (`func(...) nanopass.Pass`). Needed only for
  passes parameterised per call site (`QualifyTables(db)`); the pre-execute
  registrants are self-contained values. A second entry kind can arrive with
  its first real registrant.
- **Dynamic loading** of passes from outside the binary. Out of scope
  entirely.

## Alternatives considered

- **Direct import wiring (status quo, plus play importing identsql).** Cheap
  for one producer and one consumer; already two executors exist, so every
  new pass costs one wiring site per executor and couples apps to producer
  packages (play → identity). No inventory falls out. Rejected as the thing
  this ADR replaces.
- **Registry inside `dsl/nanopass`.** The dsl tree is a leaf library; a
  process-wide registry with a `Default` instance is platform wiring, which
  is keelson's job (ADR-0035). Keeping nanopass free of platform semantics
  also keeps it importable everywhere (including from keelson) without
  cycles.
- **init() self-registration by producers.** Rejected in SD4 — import side
  effects, implicit active set.
- **Reusing `MacroExpander`/`FunctionEvaluator` registries as the
  mechanism.** Those register macros/functions *inside one pass instance*;
  this registry registers *passes across the process*. Orthogonal
  granularity — a future macro pack would register its `expander.Pass()` as
  an entry here.

## Consequences

- One seam: both executors of user SQL behave identically with respect to
  registered rewrites, and the identsql→play handoff lands without an
  apps→identity import.
- The active rewrite set is queryable (`SELECT * FROM keelson('sql_passes')`)
  and reviewable at the wiring sites.
- A process-global `Default` registry is shared state; tests use their own
  `*Registry` instances (all helpers accept one).
- Best-effort application can silently skip a broken pass for a given input;
  the skip is logged at warn level and the catalog says what should have
  applied. That trade — availability of execution over guaranteed rewrite —
  matches how play already treats its own rewrites.
- Relative ordering across independent registrants rests on explicit `Order`
  values; a wrong relative order is a wiring bug caught by consumer tests,
  not detectable by the registry itself.

## Validation

- passreg unit tests: duplicate rejection, unknown-stage rejection,
  deterministic `(Order, Name)` ordering, best-effort skip-and-continue
  semantics, `Compose` strictness.
- defaults test: the standard set registers cleanly into a fresh registry
  and contains `ExpandLwIdMacros` at `StagePreExecute`.
- provider test: `sql_passes` snapshot rows match the registry contents.
- play client test (httptest): a registered rewrite is applied to the SQL on
  the wire; with an empty registry the wire SQL is unchanged.
- introspecthttp test: a `/query` request carrying an `LW_ID_*` call reaches
  the runner expanded, without UDFs being involved.
