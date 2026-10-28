---
type: adr
status: proposed
date: 2026-10-28
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0343: Coverage on the trail — the code an agent's call executed, as unit sets over the build, opt-in

## Context

A coverage build ([ADR-0169](./0169-continuous-coverage-keelson.md)) knows
which of its code has run: the sampler folds the process's counters into
one cumulative covered set and serves it live. What it cannot say is *who*
ran it. The audit trail ([ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md))
knows who asked for what: every agent call the dispatcher decides leaves an
action record with its task, call and operation. What it cannot say is what
happened inside the app once the command applied — ADR-0269 §SD9 states it:
the audit "records requests only and cannot say what happened inside one".

The two are one join away from each other, and neither is persisted in a
form the other can reach. ADR-0169 §SD6 recorded the facts contract for
coverage and deferred building it; nothing in `boxer.facts` carries a
coverage row today, and nothing ties a build's coverage meta hash to the
run row that names the binary. The questions that want the join:

- **Which UI paths have no operation equivalent.** Code a person reaches
  through gestures that no agent command ever executes is a gap in the
  operations catalog ([ADR-0270](./0270-play-operations-catalog-and-agent-limits.md)).
- **Whether an agent's command reached code it should not.** A confirmation
  handler or a gesture-only path showing up under an agent's call is a
  trust-boundary observation ([ADR-0207](./0207-keelson-trust-boundaries-compartments.md))
  the request-level audit cannot make.
- **What real use exercises that no scene covers**
  ([ADR-0248](./0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md)),
  and, joined with blame at identifier grain
  (codescope, a downstream module's ADR), the quadrant *written by an agent,
  exercised only by agents*.

Three properties of the Go runtime bound what is possible (claims checked
against the pinned toolchain; re-check on bumps):

- **Counters are process-global.** `runtime/coverage.WriteCounters` emits
  every instrumented package's counters at once; nothing tags an increment
  with a goroutine or a context. Attribution is only possible by reading
  the counters twice around a stretch of code that runs on one goroutine
  and belongs to one actor.
- **The sampler's signal is monotone.** ADR-0169 §SD3 stores the 0→1
  transition only, so a unit the person reached first can never later show
  up as the agent's. Attribution has to come from *counter deltas*, reduced
  to the set of units whose counter moved.
- **Granularity is the block.** A unit is a basic block with a line and
  column span. Lines are derived from units, not the other way round.

What makes the bracket possible is the frame contract of
[ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) §SD4:
queued agent commands apply at one point on the render goroutine, after the
person's write-back and before the app's `Frame`, each through the one
handler call that runs app code. Queries are answered off the render
goroutine from a snapshot, external reads and jobs run on their own
goroutines, and the command's rendering lands in the next frame mixed with
the person's input. A bracket around the handler call attributes the
command's synchronous path exactly and nothing else.

The owner's direction (2026-10-28): an opt-in that makes the coverage data
available in the audit trail, with the representation of large sets of
source locations decided deliberately rather than inherited — a numeric id
per source line per commit was the candidate, with the consequence that a
dirty working tree is not representable.

## Terms

- **Unit** — one coverable basic block of the build; the grain Go
  instruments.
- **Meta hash** — `MetaFileHash`, the 16-byte hash of the build's coverage
  meta-data; the namespace of the unit index (ADR-0169 §SD3).
- **Build id** — the BLAKE3 digest of the running executable on the run row
  (ADR-0277 §SD8). It names a binary whether or not its tree was clean.
- **Anchored** — a build whose run row carries a VCS revision and
  `vcs_modified = false`, so its units resolve to lines of a commit.
- **Bracket** — two counter reads around one agent command's handler call;
  its delta is the set of units whose counter moved between them.

## Design space (QOC)

**Question.** What is the stored identity of a source location in a
coverage row — the key a set of "what ran" is a set *of*?

**Options.**

- **O1** — The build's unit index (uint32) under its meta hash, anchored to
  a commit through the run row's build id and VCS stamp.
- **O2** — A uint64 per (commit, file, line), minted at build or write time
  from a line table of the module at that commit.
- **O3** — Spelled locations: (file, line) strings per unit on each row.

**Criteria.**

- **C1** — No build step and no toolchain at run time: the id must come
  from what the binary already carries.
- **C2** — A build from a modified tree is representable.
- **C3** — Joins: to source lines of a commit, to codescope's
  (import path, base name, line) keys, across builds of one commit.
- **C4** — Volume per row under continuous sampling, and fit with the
  modelled set sections `boxer.facts` has.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | ++ | −− | +  |
| C2 | ++ | −− | +  |
| C3 | +  | ++ | +  |
| C4 | ++ | −  | −− |

O1 wins. The meta blob is self-describing and free: the binary carries its
own unit enumeration, its own file names (import path plus base name, the
spelling codescope matches coverage on) and its own line spans, and the run
row already names the binary (build id) and its commit (VCS revision,
modified flag). A unit set is a set of uint32, which is the one set shape
the marshall front-ends read and write natively. The commit is a property
of the *build*, so it lives once on the build's row, not on every unit.

O2 answers C3 best and nothing else. Go has no line table; minting one
means a build step that walks the module at the commit, or git access at
run time, and the id has no meaning for a dirty tree — the very case a
development agent driving a scene is usually in. A line unchanged across
commits gets a new id per commit, so cross-commit history needs blame
anyway. And a line id is derivable: for an anchored build,
`(vcs_revision, src_file, line)` is on the joined rows and a 64-bit hash of
it is a query-time expression, not a stored column.

O3 stores strings per unit per row at continuous-sampling volume, which
ADR-0169 already rejected for the wire; it has no advantage over the unit
index once the function table (SD3) exists.

## Decision

Coverage becomes trail data behind one opt-in. The build's function table,
the run's covered-set growth and, per agent command, the set of units the
command's handler executed are written to `boxer.facts` through the trail
recorder, as sets of the build's unit index anchored to the run row.

### SD1 — One opt-in, idle without its inputs

`IMZERO2_COVERAGE_TRAIL` (ADR-0009 registry, boolean, default off) turns the
feature on. It needs the sampler of ADR-0169 (an atomic-mode cover build and
a sampling interval) and a trail recorder with a store; missing either, the
host logs one line and the feature idles. The opt-in adds nothing to a
non-instrumented build and nothing to the bus.

The cover lane (`scripts/dev/cover-build.sh`) sources the one build
environment every shipped binary uses ([ADR-0215](./0215-retire-mimalloc-reproducible-builds.md)),
so an instrumented binary carries the VCS stamp the anchor needs. Without
`-buildvcs` every coverage build is unanchored by construction.

### SD2 — Unit sets under the meta hash, anchored by a build row

A `coverageBuild` row is written once per run: the meta hash, the counting
mode and granularity, unit, statement, function and package totals, and —
denormalised from the run row so the join from hash to binary is one
lookup — the build id, the VCS revision and the modified flag. The hash is
the key every other coverage row carries; the run is the row's Origin.

A build from a modified tree is representable: its units resolve against
its own function table and its sets are comparable with any run of the same
binary (same build id, same hash). What it cannot do is resolve to lines of
a commit, and a reader sees that on the row rather than inferring it. This
is the deliberate departure from the candidate design, which would have
made such a build unrepresentable.

Unit sets are stored on the `u32Set` section of `boxer.facts`, the modelled
set of uint32 ADR-0169 §SD6 called for; never as serialized bitmap bytes.

### SD3 — The function table, once per meta hash

`coverageFunc` rows resolve a unit id to source: one row per instrumented
function with its package path, name, source file (import path plus base
name, as the meta spells it), literal flag, unit base, unit and statement
counts, and the per-unit start line, end line and statement count as
parallel arrays indexed by the unit's offset from the base. Two meta hashes
that share a function write it twice; the rows are append-shaped and read
with `any()`.

The host writes the table when the opt-in starts and the store holds no row
for the hash yet; a second run of the same binary writes none. The table is
the stored form of the per-line lens ADR-0169 §SD5 left live-only.

### SD4 — The run's growth, from the sampler in process

`coverageSample` rows carry what the sampler's tick found newly covered:
the hash, the sample sequence, the newly covered units and the cumulative
totals after the fold. The producer hands each update to an in-process sink
beside publishing it; the sink is the trail, not a bus subscriber. ADR-0169
§SD6 sketched a `coverage.>` tee, and that shape is right for a consumer in
another process. In the same process it adds a fire-and-forget hop to a
durable record for nothing, so the periodic full re-statements that heal
the bus are not persisted: the union of a run's sample rows *is* its
covered set, and nothing is lost on the way.

### SD5 — The bracket: one agent command, two counter reads

The operations engine exposes a bracket seam the window host fills: before
each queued command's handler call and after it, on the render goroutine.
The coverage bracket reads the process counters through
`runtime/coverage.WriteCounters` into its own buffer — never through the
sampler, whose `Sample` is single-caller and whose fold would consume the
delta from the published stream — decodes them, and takes the set of units
whose counter changed between the two reads. A read is reused as the
"before" of the next command in the same frame, so a frame applying *n*
commands costs *n* + 1 reads, and a frame with an empty queue costs none.

The row is `coverageAgentApply`: Origin is the app and window, Delegation
is the task, epoch and call the dispatcher stamped on the command (so the
row joins the action record on `(task, call)`, as ADR-0277 §SD7 has every
caused row do), and the body is the hash, the frame, the changed units, the
counts of changed units and functions, the operation, the outcome phase and
the bracket's own cost in microseconds. A command whose apply ended before
the handler ran — a conflict, an unavailable operation, a paused instance —
writes no row: no app code ran.

What the bracket says, and does not:

- It attributes the command's **synchronous handler path**. Work the
  handler starts on other goroutines, the queries the task answered from a
  snapshot, its external reads, the next frame's drawing of the command's
  effects, and the person's gestures and undos are outside it, and the
  row makes no claim about them.
- Counters other goroutines move during the bracket are inside it. The
  bracket is short and the sampler's own packages are the usual noise; a
  reader filtering by package removes most of it, and the row's cost field
  says how long the window was open.
- The run's covered set minus the union of agent sets is "the frame": the
  person's input and the app's own work together, not the person alone.

### SD6 — Trail views

`trailviews` gains one data mart per kind — builds, functions, samples and
agent applies — on the shared context columns, with the unit sets read as
lists, and bumps its views version. Attribution is a query over them:
agent units are the union of a run's apply rows, run units the union of its
sample rows, and both resolve to source through the function table on
`(meta_hash, unit_base ≤ unit < unit_base + num_units)`. No derived view is
installed with this ADR; the first durable query belongs in an applet book
([ADR-0132](./0132-sqlapplet-sql-defined-applets.md)) once the questions
settle.

### SD7 — Cost posture

The bracket pays two serializations of the executed functions' counters
per command and the trail pays one row. Both are measured, not assumed:
the coverage package gains a benchmark for one read-and-decode, M0 records
its figure, and every apply row carries its bracket's cost so a session's
overhead is a `max()` away. The feature is off by default, runs only in a
cover build, and touches no frame without a queued command, so a release
binary and a cover build without the opt-in pay nothing.

### Deferred

- **A driver stamp on the run row** — person, scene, headless driver or
  coordinator — so a whole run driven by a development agent is
  attributable at run grain without a bracket. Small, separate, and the
  right unit for scene runs; not this ADR's.
- **A live keelson table** of agent-attributed units, and publishing apply
  sets on the bus. The trail is the record; the live view can follow.
- **The derived attribution and line-id views** (SD6).
- **Retention** for the sample kind; travels with ADR-0169's deferral.
- **Gesture attribution.** A person's gesture applies inline inside the
  app's frame through the same engine; bracketing it would cost a read per
  gesture per frame and is not needed to separate agent from frame.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Runtime vocabulary (`keelson/runtime/vocab`) | added: four kind memberships and their attributes at the next free ordinals | `AllMembs`, the assignments golden, the trail store regenerated |
| `boxer.facts` kinds | added: `coverageBuild`, `coverageFunc`, `coverageSample`, `coverageAgentApply`; first writers of the `u32Set` section through a generated store | `trailviews` (new marts, version bump) |
| Env-var registry (ADR-0009) | added: `IMZERO2_COVERAGE_TRAIL` | `doc/env-vars.md` |
| Exported Go API under `public/` | added: a counter-diff function and a snapshot benchmark in `observability/coverage`; an update sink on `coveragebus.ProducerOptions`; an apply-bracket seam on `opengine.Engine` and on the window host; a `covtrail` package owning the sink, the bracket and the writers. All additive | hostboot wiring behind the opt-in |
| `scripts/dev/cover-build.sh` | reshaped: sources the shared build environment | none |

## Alternatives

- **A line id per commit (O2).** Rejected in the QOC: needs a build step or
  git at run time, cannot name a dirty build, and is a query-time
  expression over the chosen rows for an anchored one.
- **Bracket the whole `ApplyQueued` once per frame.** Rejected: the only
  saving is one read per extra command in a frame, which is rare (one task
  holds an instance and awaits status between commands), and the row would
  have to carry a list of calls instead of joining the action record on
  its call.
- **Attribute by time window while a task holds the instance.** Rejected:
  the person keeps every other window and may edit this one; the sets would
  be a mixture with no row saying so.
- **Context-propagated coverage.** Rejected: Go's instrumentation has no
  hook for it; a custom pass is a toolchain to maintain for a gain over the
  bracket that the deferred work (queries, jobs) does not need.
- **A bus tee for the samples (ADR-0169 §SD6 as sketched).** Rejected for
  the in-process case for the reason in SD4; still the shape for a
  consumer in another process.
- **Persisting the sampler's full re-statements.** Rejected: they exist to
  heal a lossy channel the in-process sink does not have.

## Consequences

### Positive

- The audit trail can say what an agent's command executed, not only that
  it was asked, and the answer joins the action record on the keys it
  already carries.
- ADR-0169 §SD6's deferred persistence lands, as the generated store it
  said the next kind should be, and the per-line lens gains a stored form.
- A dirty build is representable against its own binary, and a reader can
  tell an anchored build from an unanchored one on the row.

### Negative

- A cover build with the opt-in pays two counter serializations per agent
  command on the render goroutine; the figure is M0's to record and the
  row's to carry.
- The function table is tens of thousands of rows per new build hash; a
  developer rebuilding often writes it often. Each hash writes it once.
- The bracket's claim is narrow by design; a reader who wants "everything
  the task caused" has the deferred work ahead of them, and the row text
  says so.

### Neutral

- Four kinds join the trail store; its archetype grows, its DDL does not.
- The sampler, the bus codec and the live tables are unchanged.

## Migration — Tier 1

Nothing to migrate. Every surface change is additive: new memberships at
new ordinals, new kinds, new views beside the old, new optional fields on
option structs, a new env var that defaults off. The cover lane script
gains flags; binaries built with the old script keep working and are
unanchored.

## Verification plan — Tier 1

- **Lane.** The default `go test` lane: the counter diff over two decoded
  snapshots (new function, changed counter, unchanged function); the
  bracket's reuse of a read within a frame and its silence on a conflict;
  the trail store's regeneration and membership ids; the views' SQL. The
  coverage package's decode probe already runs an instrumented binary with
  the live toolchain, and the integration lane's schema check covers the
  new kinds against the live table.
- **What would fail.** A DTO or vocabulary change that does not regenerate
  the store; a view whose column is not on the section the DTO names; a
  bracket that writes a row for a command whose handler never ran.
- **Gap.** The cost figure is a benchmark, not a gate; the frame-time
  effect of the opt-in in a real session is M0's observation, recorded in
  an Update, not re-run by CI.

## Milestones

- **M0 — Measure.** The read-and-decode benchmark; its figure and the apply
  row's cost field observed in one session, recorded in an Update.
- **M1 — Kinds and store.** Vocabulary, DTOs, regenerated trail store, the
  function table and build row writers, the sampler sink.
- **M2 — The bracket.** Engine seam, window host wiring, counter diff, the
  apply row.
- **M3 — Views.** The four marts and the version bump.

## Update (2026-10-28) — the driver stamp and the jobs gap move to ADR-0344

[ADR-0344](./0344-agent-caused-jobs-in-a-process-of-their-own-and-the-entry-frame-every-stack-carries.md) (proposed) takes two items from this ADR's Deferred list
and the gap §SD5 names. The **driver stamp on the run row** lands there as
§SD5, first used for a job child. **Jobs** are attributed by running an
agent-caused job in a process of its own, so the samples this ADR writes
under that run are the job's set; no bracket row is added for jobs, for the
reason §SD5 gives. Independently of coverage, ADR-0344 §SD1 puts a named
**entry frame** on every handler call — `entrypoint.Agentic` or
`entrypoint.Human` — so a stack captured inside app code says which kind of
caller it serves; the bracket of §SD5 now encloses that frame, with `Begin`
and `End` outside it and the handler inside.

## Status

Proposed — awaiting review by the code owner. Built alongside the proposal.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0169](./0169-continuous-coverage-keelson.md) — live coverage as keelson data; §SD3 the unit index, §SD6 the deferred facts contract this builds.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) — the trail, its context components, the build id on the run row.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — §SD4 the frame the bracket sits in, §SD9 what the audit cannot say.
- [ADR-0191](./0191-runtime-instance-attribution.md) — run and instance on every runtime-written row.
- [ADR-0184](./0184-sysmetrics-persistence-tee.md) — the first facts-bound store; chstore stays the DDL author.
- [ADR-0105](./0105-keelson-adopts-generated-record-stores.md) — the next kind lands as a generated store.
- [ADR-0148](./0148-app-workingsets.md) — data as facts, not bytes in a table.
- [ADR-0215](./0215-retire-mimalloc-reproducible-builds.md) — the one build environment the cover lane now sources.
- [ADR-0009](./0009-environment-variable-registry.md) — the env-var registry the opt-in is declared in.
- [ADR-0248](./0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md) — scenes, the run-grain case deferred here.
