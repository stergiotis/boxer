---
type: adr
status: accepted
date: 2026-08-06
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-14
---

# ADR-0171: leeway's SQL read surface — named, versioned, and generated

## Context

Reading leeway data from SQL is, today, three unrelated things that a consumer
must assemble unaided:

- **`chpack`** ([ADR-0162](./0162-leeway-co-ragged-function-pack.md)) — the lane
  algebra, 16 SQL UDFs in `public/semistructured/leeway/chpack`, carrying a
  `Version` constant and a `LW_PACK_VERSION()` marker function.
- **the read-back family** ([ADR-0066](./0066-leeway-dql-clickhouse-readback-generator.md))
  — the leeway-schema-aware layer on top of it, emitted by `HelperUDFsSQL()` in
  `public/semistructured/leeway/marshall/clickhouse/readback/`.
- **column handles** ([ADR-0116](./0116-play-leeway-column-handle-resolution.md))
  — `ResolveColumnNames`, which expands `` `symbol:value` `` to the physical
  column name.

Each is documented where it was built. Nothing names the three together, and
nothing on the path a task-level consumer actually walks points at any of them.

*Function names below are quoted as they were at the time each observation was
made. The vocabulary moved under a single `LW_` namespace on 2026-08-07
(ADR-0162's Update of that date) — `CO_GATHER` is now `LW_CO_GATHER`,
`LEEWAY_VALUE_BY_TAG_EQUAL` is `LW_VALUE_BY_TAG_EQUAL`, and so on. The
findings are about the surface, not the spellings, and rewriting a measured
observation to a name that did not exist when it was measured would falsify
it.*

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

### SD2 — The surface gets one version handshake, one declared set, and a retirement path

The family adopts `chpack`'s existing mechanism: a version constant, a marker
function, and an installer that verifies the marker after installing. Because
`CREATE OR REPLACE` cannot remove a renamed or retired function, the installer
also reconciles — it knows the full roster the build declares and drops
leeway-owned functions on the server that are not in it.

This is the only sub-decision that turns a silent failure into a loud one, and
the trial hit that failure twice.

The 2026-08-07 namespace rename does two things for this sub-decision. It
makes "leeway-owned functions on the server" a single question —
`WHERE name LIKE 'LW\_%'` — where it previously meant enumerating four
prefixes and hoping none was forgotten. And it ships the half of the
reconcile that needed no new decision: `chpack.Install` now drops an
append-only list of names this repository has withdrawn
(`chpack.RetiredNames`), which is what kept the rename from leaving 23 stale
functions behind the way its predecessor left 16.

What the list cannot do is catch a leeway-owned function that *no build ever
declared* — a hand-installed extra, or a spelling from a fork. That needs the
full declared set, which spans `chpack`, the read-back family and `identsql`.
The four choices that turns into are settled as follows.

**Home — a new package, `public/semistructured/leeway/lwsqlsurface`.** It
imports all three rosters and is the single entry point §SD1 names. The two
cheaper homes do not work: `readback` already imports `chpack`, so a registry
inside `chpack` would close an import cycle, escapable only by init-time
self-registration or by moving the read-back roster away from the SQL
`TestRosterMatchesSQL` pins it to; and a registry inside `readback` would put
the whole surface under `marshall/clickhouse/`, where `identsql` — an identity
concern, not a marshalling one — does not belong. Each family keeps declaring
its own roster beside its own SQL; `lwsqlsurface` declares only the union.

**One marker for the surface, `LW_SURFACE_VERSION`.** Not one per family. The
invariant it carries is the reason: *the marker present at revision N means
all three families are installed at revision N.* A per-family scheme answers
three questions a caller then has to combine, and the trial's failure was not
knowing which combination it was looking at.

Three consequences follow, none of them optional once the invariant holds:

- `LW_PACK_VERSION` is **retired** onto the append-only list. Two markers that
  can disagree reintroduce the ambiguity this removes.
- `chpack.Install` **folds into** the surface installer. A pack-only install
  can verify nothing once the marker is the surface's; and were it to stamp
  the marker anyway, the marker would stop meaning what the invariant says.
- A client probing a server provisioned by a **pre-surface build** finds no
  surface marker. It falls back to reading `LW_PACK_VERSION`'s `create_query`
  — the existing technique, for the existing reason: calling either function
  fails with unknown-function on exactly the servers whose revision matters.

**Install provisions all three families**, in dependency order — pack,
read-back helpers, identity UDFs — then stamps and verifies the marker, then
drops retired names. This ends the asymmetry ADR-0066's 2026-08-07 Update
records, where provisioning and reconciling lived in different places and a
host could install one family without the other. It is a behaviour change for
callers that install the pack alone today.

**Undeclared names are reported, not dropped, unless the caller asks.**
`Reconcile` returns the `LW\_%` functions the server carries that the declared
set does not contain; dropping them is an explicit mode. Retired-name drops
stay automatic, because those are spellings this repository itself withdrew.
The asymmetry is deliberate: the repository may delete its own past, but a
name it never declared may be a fork's or a downstream consumer's, and
`play` reconciles endpoints automatically at startup.

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

### SD5 — An exploded companion table, as an explicitly redundant second representation

SD3 materializes *paths* inside the packed table. This sub-decision is the
larger sibling: a whole second representation of the same data — one row per
attribute, `(doc, section, path, value…)` — maintained alongside the packed one,
with the consumer choosing which to read. Storing the same data twice in two
shapes to serve two query classes is an old database trick; what is new here is
that leeway can generate both from one schema, and that the conversion is cheap
enough to be unremarkable.

The [second-substrate trial](../trials/leeway-second-substrate/README.md) priced
it on the JSONBench Bluesky corpus (2026-08-07 logbook entry):

- **Conversion:** 100M documents / 1.2 billion attributes in **50.8 s**, one
  pass, no staging, reading 14.0 GiB at 281 MiB/s. Peak memory **5.41 GB
  against 5.28 GB at 10M** — bounded, not proportional to row count.
- **Footprint:** the exploded form is **0.6978× the packed form** at 100M and
  0.6965× at 10M — a 0.2 % move across a 10× scale-up, with every column
  growing 9.4–10.5× against a 9.906× row increase. Carrying both costs
  **1.70×** the packed footprint, and capacity planning has a stable ratio to
  plan against. Below the 10M tier the ratio is not yet settled and a plan
  extrapolated from it errs on the safe side.
- **What it buys:** the path becomes a sort-key prefix instead of a value
  inside an array, which no packed layout can offer. Measured against the
  packed table: 0.03× on a subtree-prefix census, 0.07× on a corpus-wide leaf
  count, 0.18× on both a group-by-collection count and a discovered
  array-degree query. It is at parity or better on four of the five JSONBench
  queries and worse on one (2.19×).
- **What it costs beyond storage:** memory. The exploded form is 2.7–13.2×
  the packed form's peak on the document-reassembly queries and 69.8× on one
  value-predicate query.

Two things this sub-decision does **not** settle, and they are the work:

- **How the redundancy is maintained.** Batch reconversion is what was
  measured, and at 50.8 s per 100M it is affordable, but it leaves the copy
  stale between runs. A ClickHouse `MATERIALIZED VIEW` carrying the same
  `ARRAY JOIN` would maintain it on insert; that form is untested here.
- **Who chooses which table a query reads.** Nothing routes automatically. The
  trial's queries name their table, and a consumer that picks wrong gets the
  worse of the two — which is the honest cost of redundancy and the reason
  this is a sub-decision rather than a default.

Placing the two representations on different drives — the read-parallelism
argument that makes redundancy pay twice — is expressible via MergeTree's
`storage_policy`, but was **not measured**: the trial's server has a single
disk configured.

**Ordering.** SD1 first: it is the cheapest and it closes the finding the trial
was built to produce. SD2 second, because it protects every number anyone
measures afterwards. SD3, SD4 and SD5 are independent and can be descoped
without touching the others. SD5 is the largest and the least urgent: it is a
deployment option with a measured price, not a gap in the surface.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `lwsqlsurface` (new exported Go package under `public/`) | the declared set, `Version`, `Install`, `Reconcile` | the three rosters it unions; the roster-pinning tests |
| `chpack.Install` (exported Go API under `public/`) | removed — superseded by `lwsqlsurface.Install` | `play`, `jsonbench`, and any out-of-tree caller |
| `LW_PACK_VERSION` (SQL function name, named registry) | retired in favour of `LW_SURFACE_VERSION` | `chpack.RetiredNames`; play's vocabulary probe, which keeps reading it as the pre-surface fallback |
| `readback.HelperUDFsSQL()` (exported Go API under `public/`) | keeps its signature; its family joins the surface's declared set and installer | the read-back generator's golden tests; any server already carrying the family |
| leeway DDL generator (generated-code input) | gains `MATERIALIZED` projection emission | `go generate ./...` output for schemas that opt in |
| keelson subjects and vocab (named registry) | SD4 exposes membership name→id to SQL readers | whatever carries the lookup; no change to how ids are minted |
| leeway DDL generator (generated-code input) | SD5 only: gains exploded-companion DDL and its conversion statement | `go generate ./...` output for schemas that opt in; whatever maintains the copy |
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
  functions, which is a new thing to keep correct — a fourth declaration of
  names that are already declared three times. Getting it wrong under the
  opt-in drop mode deletes a function someone else depends on; getting it
  wrong under the default mode reports drift that is not there.
- One marker for three families means any family's revision bump invalidates
  the surface version, so a server is "stale" for a change that did not touch
  the part it uses. That is the price of the invariant, and it is paid on
  every bump, not only on the ones that matter to a given caller.
- SD3 adds a schema-to-DDL path that must stay in step with physical column
  naming — the same coupling that made the hand-written version fragile, now in
  a generator.

### Neutral

- Nothing here changes the leeway encoding, the facts schema, or any query
  result. The trial's arms return byte-identical results with and without every
  lever discussed above.

## Migration — Tier 1

- **Breaks.** Nothing at rest. Two things break at build time: `chpack.Install`
  is removed, and callers move to `lwsqlsurface.Install`; `LW_PACK_VERSION`
  stops being installed, so anything reading it as *the* marker reads
  `LW_SURFACE_VERSION` instead. A server carrying hand-installed extras under
  an `LW_` name is reported, and only dropped if the caller asks — see SD2.
- **Path.** Call `lwsqlsurface.Install`; it provisions all three families,
  verifies the marker, and drops the retired names — `LW_PACK_VERSION` among
  them — after the verify succeeds. A server on an older build is reconciled on
  next install with no manual drop step; the manual step is what this removes.
  A client that must diagnose a server *before* reconciling it falls back to
  `LW_PACK_VERSION`'s `create_query`.
- **Regeneration.** SD3 changes generator output for schemas that opt in;
  `go generate ./...` must re-run. No FFI boundary is involved.
- **Old shape.** `HelperUDFsSQL()` keeps its signature and meaning. What it
  emits is now also reachable through the surface installer, which is the
  supported path; the string form stays for callers that provision by hand.

## Verification plan — Tier 1

- **Lane.** The `//go:build integration` lane, which already has a live
  ClickHouse and is where `chpack`'s install-and-verify test runs. The parts
  that fit in one session — install, marker verify, `system.functions`
  drift detection — also run hermetically against `clickhouse-local` in the
  unit lane, skipped when the binary is absent, as the read-back UDF tests do.
- **What would fail.** A test that installs the surface against a server
  pre-loaded with a retired function name and asserts the function is gone
  afterwards; a test that plants an undeclared `LW_` function and asserts
  `Reconcile` reports it and leaves it alone until asked; a test that asserts
  the declared set equals the union of the three rosters, so a family growing
  a function without the surface noticing goes red; and a golden over the
  generated `MATERIALIZED` definitions, so a physical-column-naming change
  that silently invalidates them goes red.
- **Gap.** Nothing verifies SD1 — a documentation pointer has no lane. The
  honest check is a re-run of the jsonbench trial by an operator who has not
  read this ADR, which is exactly what
  [§7b](../trials/jsonbench-on-facts/README.md) asks a later run to report.

## Status

Accepted 2026-08-14.

This ADR was written to close the jsonbench-on-facts trial's findings ledger,
not from an implementation. SD1 and SD2 are settled to a shape and carry
milestones below. SD3, SD4 and SD5 record an intent, not a shape: each still
needs the design dialogue
[CODINGSTANDARDS § Design Before Code](../../CODINGSTANDARDS.md#design-before-code)
asks for, and accepting this ADR does not licence building them.

- **M0 — SD1: the read surface as one page; skills pointers.** ✓
- **M1 — SD2: `lwsqlsurface` — declared set, marker, `Install`, `Reconcile`.** ✓
- **M2 — SD2: play's vocabulary probe on the surface marker.** ✓
- **M3 — SD3: `MATERIALIZED` projection emission — dialogue first.**
- **M4 — SD4: membership name→id readable from SQL — dialogue first.**
- **M5 — SD5: the exploded companion table — dialogue first.**

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

## Update 2026-08-14 — M1 and M2 implemented; what the shape turned into

The four choices §SD2 left open, as landed, and the two things that only
became visible while building it.

- **Homes.** `public/semistructured/leeway/lwsqlsurface` holds `Version`,
  `LW_SURFACE_VERSION`, `DeclaredFunctions`, `Statements`, `Install` and
  `Reconcile`. The installer machinery moved there whole from `chpack` —
  feature probe, collision check, retired-name drop — because a pack-only
  installer could no longer verify anything. `chpack` is now declaration and
  rendering only. `RetiredNames` stayed in `chpack`, where it already covered
  more than the pack.
- **`readback.FamilyStatements`.** The installer executes one statement per
  round trip, and `HelperUDFsSQL()` emits the pack ahead of the family, so
  the family needed a way to hand over its own statements alone. The split is
  lexical and pinned against the roster by test, rather than parsed by a SQL
  parser on an install path.
- **The panel's server population is now the declared set** rather than three
  separately-looped rosters. That was not tidying: the marker is declared by
  this package and probed against that list, so a hand-maintained copy would
  have reported `LW_SURFACE_VERSION` as an undeclared extra on every
  correctly provisioned server.
- **The pre-surface fallback is a sentence, not a missing marker.** A server
  carrying the retired `LW_PACK_VERSION` and no surface marker is neither
  broken nor empty — it works, and was provisioned before the families shared
  a marker. The panel says that, because "unknown" would throw away the one
  thing the reading does establish.

**Verification deviation.** The install-and-reconcile lane is hermetic:
`clickhouse-local --path` persists created functions across invocations, so
`Install`, the marker verify, the retired-name drop and both `Reconcile`
modes run for real without a server. The integration lane keeps what
`clickhouse-local` cannot stand in for — a server with its own builtins and
other users' functions — and runs `Reconcile` in report mode only, because
that lane may target a shared server and dropping there would delete work
that is not ours.

**Not done here.** M0 (SD1's page and the skills pointers) is still open, and
SD3–SD5 remain intents awaiting their own dialogue.

## Update 2026-08-14 — M0: the page is a router, and it is linked both ways

`doc/explanation/leeway-sql-read-surface.md` is deliberately **not** a fifth
explanation of the mechanics. Four pages already cover them — physical names
and handles, the array idioms, the three DQL contracts, the query algebra —
and a page that restated any of them would be the thing that goes stale. It
names the five layers, says where each name runs and how each fails, covers
provisioning and the version check, walks one attribute end to end, and lists
the gaps. Everything else is a link.

**Linked from both directions**, because a router nothing points at
reproduces the failure it exists to fix: the four leeway skills, the two
explanation pages' reading lists, the array-idioms how-to (whose opening now
says to read the surface first, and names the truncating idiom), and
AGENTS.md's leeway subsystem note.

Two deviations from §SD1 as written. It says *three* leeway skills; there are
four now, and all four carry a pointer — placed where each skill's reader
first meets the problem rather than in a footer, which is the placement the
trial's evidence argues for. And each pointer carries one sentence of *why*,
not just a link: a bare cross-reference is as unfindable as the vocabulary
was.

**One gap the page surfaced rather than closed.** No CLI installs the
surface. `lwsqlsurface.Install` is Go, play reconciles on startup, and
`jsonbench` has a subcommand — so an operator with neither is left piping SQL
that carries no version marker, which is exactly the state §SD2 exists to
make visible. A `leeway`-family install command is the obvious next step; it
is recorded on the page as a known gap rather than fixed under a
documentation milestone.

## Update 2026-08-14 — `leeway sqlsurface`, and a split in the drift report

M0's page named "no CLI installs the surface" as a known gap. It is closed:
`leeway sqlsurface` has `install`, `status`, `print` and `drop-undeclared`,
mapping one-to-one onto the package API — including its asymmetry, which the
command shapes rather than describes. `install` drops this repository's
withdrawn spellings without being asked; `drop-undeclared` is a separate
command, explicitly named, and additionally requires `--confirm`, so removing
a name that might be someone else's takes three deliberate acts.

`print` is the half that closes the *original* complaint. Provisioning by
hand was already possible — pipe `readback.HelperUDFsSQL()` — but that script
carries no version marker, so it produced a server that works and cannot say
what it carries, which is the failure §SD2 exists to prevent. `print` emits
the marker with the rest.

**`Report` gained `Retired`,** separated from `Undeclared`. Both are
leftovers, but they need different actions: a withdrawn spelling has a known
fix — the next install drops it — while an undeclared name is somebody's.
Conflated, a status tool either nags about the fixable case or offers to
delete the unknown one, and on a pre-surface server the retired
`LW_PACK_VERSION` reported as "undeclared", which is true and useless.
`ReconcileDrop` now removes only genuinely undeclared names, and
`Report.PreSurface()` names the server that carries the old marker and no new
one. Only the namespaced generations can appear in `Retired` — the listing
asks for `LW\_%` — while `Install` drops the pre-namespace spellings anyway,
by unconditional DROP rather than by diffing a listing.

**Verified against a live server**, which is new for this ADR: ClickHouse
26.7 on a scratch instance, driven through the CLI — `status` on an empty
server reporting 39 missing, `install` provisioning and verifying, `status`
reporting in sync, a planted fork helper and a planted retired marker
reported in their separate buckets, `install` clearing only the retired one,
`--confirm` clearing the other, and `--fail-on-drift` exiting non-zero
exactly while drift stood. The `//go:build integration` lane — this ADR's
own test and ADR-0162's correctness matrix, plan-identity, guard-pruning and
differential-oracle suites — passed against the same server.
