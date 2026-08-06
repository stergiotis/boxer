---
type: adr
status: proposed
date: 2026-08-06
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0171: leeway's SQL read surface — named, versioned, and generated

## Context

Reading leeway data from SQL is, today, three unrelated things that a consumer
must assemble unaided:

- **`chpack`** ([ADR-0162](./0162-leeway-co-ragged-function-pack.md)) — the lane
  algebra, 16 SQL UDFs in `public/semistructured/leeway/chpack`, carrying a
  `Version` constant and a `LEEWAY_PACK_VERSION()` marker function.
- **the read-back family** ([ADR-0066](./0066-leeway-dql-clickhouse-readback-generator.md))
  — the leeway-schema-aware layer on top of it, emitted by `HelperUDFsSQL()` in
  `public/semistructured/leeway/marshall/clickhouse/readback/`.
- **column handles** ([ADR-0116](./0116-play-leeway-column-handle-resolution.md))
  — `ResolveColumnNames`, which expands `` `symbol:value` `` to the physical
  column name.

Each is documented where it was built. Nothing names the three together, and
nothing on the path a task-level consumer actually walks points at any of them.

The [jsonbench-on-facts trial](../trials/jsonbench-on-facts/README.md) is the
evidence. It recreated a published benchmark on `boxer.facts` using native
idioms only, and its headline number moved four times under review — never
because the data model was slow, and every time because the trial had
hand-written something the repository already provided:

| What the trial wrote by hand | What existed | Cost of not finding it |
| --- | --- | --- |
| Open-coded lane arithmetic | the query vocabulary | ~3× |
| A per-row path reconstruction | a `MATERIALIZED` column | 7× time, 8× memory |
| `CO_GATHER(vals, RAGGED_STARTS(len))` | `LEEWAY_LIST_BY_TAG_EQUAL` | up to 2.4×, and it silently truncated |
| Physical column names, spelled out | `ResolveColumnNames` | repetition, no measured cost |

Three further observations from the same trial, each verified against the tree
at `b33bab3a`:

- **The read-back family carries no version marker.** `chpack` has one;
  `HelperUDFsSQL()` does not. Every statement in both is `CREATE OR REPLACE`,
  which cannot remove a function that has been retired or renamed. The trial
  server was found carrying `LEEWAY_LU_MEMB_IDX_TO_VAL_IDX` — retired in the
  repository — while three functions that exist in the repository were absent,
  and nothing detected the skew. Renaming the pack to UPPER_SNAKE reproduced
  this deliberately: 16 stale camelCase functions had to be dropped by hand.
- **Nothing emits `MATERIALIZED` column definitions from a leeway schema.**
  There is no occurrence of `MATERIALIZED` anywhere under
  `public/semistructured/leeway/`. Materializing five backbone paths was worth
  **3.8–13.8×** on the trial's queries, at +18.8 % storage — the single largest
  lever it found — and it is hand-written per path, with the physical column
  names inlined and no check that they still match the schema.
- **A Ref-membership table cannot be read by anyone who does not hold the
  registry.** Memberships are identified by a uint64 from a vcs-registered
  vocabulary; there is no server-side name→id lookup, so ids ride SQL text as
  literals like `6917529027641081861`. The trial shipped a CLI subcommand whose
  only purpose is to print them.

The three leeway skills under `doc/skills/` mention none of this: zero
occurrences of `chpack`, `CO_GATHER`, `RAGGED_`, `LEEWAY_VALUE_BY_TAG_EQUAL` or
`LEEWAY_LIST_BY_TAG_EQUAL` across all three files.

The trial's verdict states the conclusion plainly: the toolbelt is not slow, it
is *undiscoverable from where a task-level consumer stands*. This ADR is about
that, not about performance — the performance numbers are only how the
discoverability gap was found and priced.

## Design space (QOC)

**Question.** What is the supported way to read leeway data from SQL, and how
does a consumer find it and know it is installed?

**Options.**

- **O1 — Document it.** One page naming the three pieces and how they compose;
  the skills gain a section pointing at it. No code.
- **O2 — Generate per-schema artifacts.** Extend the DDL generator to emit, from
  a leeway schema, the `MATERIALIZED` projections and the vocabulary lookup a
  SQL consumer needs, so the surface is a build product rather than a recipe.
- **O3 — Provision and introspect at runtime.** One installer that puts the pack
  and the read-back family on a server, reconciles what is there against what
  the build declares (including removing retired functions), and exposes the
  vocabulary as a queryable table.

**Criteria.**

- **C1 — Closes the discoverability finding.** Would a consumer standing where
  this trial stood have found the vocabulary? Assessed against the trial's four
  misses.
- **C2 — Detects drift.** Does a stale or partial installation become visible
  rather than silently returning wrong or slow answers?
- **C3 — Cost to build.** Against the existing generators and installers.
- **C4 — Keeps the hand-written escape hatch cheap.** A consumer must still be
  able to write the SQL directly.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | +  | ++ | +  |
| C2 | −− | −  | ++ |
| C3 | ++ | −  | +  |
| C4 | ++ | +  | ++ |

No option dominates: O1 is the only cheap one and detects nothing, O2 is the
only one that removes the hand-written step, O3 is the only one that makes a
stale server visible. They are also not exclusive — they address different
failure modes of the same surface.

## Decision

We will treat leeway's SQL read path as **one named surface** rather than three
packages, and close the four gaps the trial priced, in the order they were
priced. Concretely, and each independently descope-able:

### SD1 — The surface gets a name and a single entry point

`chpack`, the read-back family, and column handles are documented together as
the SQL read surface, with the layering stated once: the pack is the lane
algebra, the read-back family is the leeway-schema-aware layer on top of it, and
handles are how a query names columns. The three leeway skills gain a pointer to
it. This is O1, and it is the part that would have prevented all four of the
trial's misses.

The layering is not a new claim — it is already true and already recorded in
ADR-0162's 2026-08-02 Update. What is missing is a reader arriving from the
consumer side finding it.

### SD2 — The read-back family gets a version handshake and a retirement path

The family adopts `chpack`'s existing mechanism: a version constant, a marker
function, and an installer that verifies the marker after installing. Because
`CREATE OR REPLACE` cannot remove a renamed or retired function, the installer
also reconciles — it knows the full roster the build declares and drops
leeway-owned functions on the server that are not in it.

This is the only sub-decision that turns a silent failure into a loud one, and
the trial hit that failure twice.

### SD3 — Materialized projections are emitted from the schema, not hand-written

The DDL generator learns to emit `MATERIALIZED` column definitions for a named
set of paths, resolving each path against the schema so the physical column
names are a build product. The trial's arm D becomes a generated artifact rather
than five hand-written expressions.

**Deliberately not in scope:** choosing *which* paths to materialize. That is a
workload question, and the trial is explicit that its five came from the
benchmark's own queries. The generator takes the set as input.

### SD4 — The vocabulary is readable from SQL

A membership name → uint64 lookup is reachable from a query, so a SQL page does
not carry `6917529027641081861` as a literal. The shape — a small table, a
dictionary, or a UDF — is left open here; the trial only establishes that the
absence is felt by anyone reading a Ref-membership table.

**Ordering.** SD1 first: it is the cheapest and it closes the finding the trial
was built to produce. SD2 second, because it protects every number anyone
measures afterwards. SD3 and SD4 are independent and can be descoped without
touching the others.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `readback.HelperUDFsSQL()` (exported Go API under `public/`) | gains a version constant, a marker function, and a reconciling installer | the read-back generator's golden tests; any server already carrying the family |
| leeway DDL generator (generated-code input) | gains `MATERIALIZED` projection emission | `go generate ./...` output for schemas that opt in |
| keelson subjects and vocab (named registry) | SD4 exposes membership name→id to SQL readers | whatever carries the lookup; no change to how ids are minted |
| `doc/skills/leeway-*` (documentation surface) | gains a pointer to the read surface | none |

## Alternatives

- **Fold this into ADR-0162.** Rejected: 0162 is accepted, and its subject is
  the pack's design. Provisioning the *other* family, generating projections,
  and vocabulary introspection are new scope, not amendments — a dated Update
  would bury three decisions inside a fourth's ADR.
- **Do nothing but document (O1 alone).** Tempting, and it is genuinely the
  highest-value single step. Rejected as the whole answer because it leaves the
  drift finding entirely unaddressed, and a stale server produces wrong answers
  quietly — the trial has a measured instance of `CO_LOOKUP` returning the wrong
  value on a table whose lanes are not 1:1.
- **Generate the whole query, not just the projections.** Out of scope and
  probably wrong here: the trial's queries were translations of someone else's
  benchmark, and the finding is that the *primitives* were unfindable, not that
  writing SQL is the problem.
- **Wait for the finding-fact family to land and prioritize from facts.** The
  ledger this would draw on is one trial deep; waiting adds ceremony, not
  information.

## Consequences

### Positive

- A consumer arriving from the task side finds the read vocabulary — which, on
  the trial's own evidence, is worth between 3× and 26× on individual queries
  before anyone tunes anything.
- A stale or partial UDF installation becomes detectable instead of producing
  quietly wrong results.
- The largest measured lever (materialization, 3.8–13.8×) stops being a
  hand-written artifact that drifts from the schema.

### Negative

- Four sub-decisions across three packages and the skills — more surface than
  any single finding justifies on its own. The ordering exists so the tail can
  be dropped.
- SD2's reconciling installer must know the full roster of leeway-owned
  functions on a server, which is a new thing to keep correct; getting it wrong
  drops a function someone else depends on.
- SD3 adds a schema-to-DDL path that must stay in step with physical column
  naming — the same coupling that made the hand-written version fragile, now in
  a generator.

### Neutral

- Nothing here changes the leeway encoding, the facts schema, or any query
  result. The trial's arms return byte-identical results with and without every
  lever discussed above.

## Migration — Tier 1

- **Breaks.** Nothing at rest. SD2's reconciling installer will drop
  leeway-owned functions on a server that the build no longer declares — which
  is its purpose, and is a behaviour change for any server carrying
  hand-installed extras under a `LEEWAY_`/`CO_`/`RAGGED_` name.
- **Path.** Install the versioned family; the installer reports what it removed.
  Servers on the current family are reconciled on next install with no manual
  drop step — the manual step is what this removes.
- **Regeneration.** SD3 changes generator output for schemas that opt in;
  `go generate ./...` must re-run. No FFI boundary is involved.
- **Old shape.** `HelperUDFsSQL()` keeps its signature and meaning; the version
  marker is additive.

## Verification plan — Tier 1

- **Lane.** The `//go:build integration` lane, which already has a live
  ClickHouse and is where `chpack`'s install-and-verify test runs.
- **What would fail.** A test that installs the read-back family against a
  server pre-loaded with a retired function name and asserts the function is
  gone afterwards; and a golden over the generated `MATERIALIZED` definitions,
  so a physical-column-naming change that silently invalidates them goes red.
- **Gap.** Nothing verifies SD1 — a documentation pointer has no lane. The
  honest check is a re-run of the jsonbench trial by an operator who has not
  read this ADR, which is exactly what
  [§7b](../trials/jsonbench-on-facts/README.md) asks a later run to report.

## Status

Proposed — awaiting review by p@stergiotis.

This ADR was written to close the jsonbench-on-facts trial's findings ledger,
not from an implementation. SD3 and SD4 in particular describe an intent, not a
shape: both would need the design dialogue
[CODINGSTANDARDS § Design Before Code](../../CODINGSTANDARDS.md#design-before-code)
asks for before anything is built.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [jsonbench-on-facts trial](../trials/jsonbench-on-facts/README.md) — §7b is the
  findings ledger this ADR carries rows 5–8 of.
- [ADR-0162](./0162-leeway-co-ragged-function-pack.md) — the pack, its version
  marker, and the 2026-08-02 Update recording the layering.
- [ADR-0066](./0066-leeway-dql-clickhouse-readback-generator.md) — the read-back
  generator.
- [ADR-0116](./0116-play-leeway-column-handle-resolution.md) — column handles.
- [ADR-0168](./0168-capmap-business-capability-corpus.md) — the competence vault
  the findings' slugs come from.
