---
type: adr
status: accepted
date: 2026-11-10
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-10
---

# ADR-0367: SQL statements leave through one fairlead, and each is placed in a stratum

## Context

Boxer's layers ask different amounts of whatever answers a statement. The
endpoint that serves keelson tables without an engine
([ADR-0290](0290-keelson-named-arguments-and-a-trivial-sql-endpoint.md)
§SD3) hands a table over whole and refuses everything else; a store's
latest-state read needs grouping; a mutation needs the storage engine. That
stratification is real, but it is nowhere stated. A statement finds out
which layer it needed when a layer refuses it.

Two things are missing for it to be stated.

**There is no one place a statement passes.** SQL is sent by a shared
client, by two query-engine adapters, by the local-worker pool, by a
one-shot local executor, and by several functions that build their own
request or start their own query process. A statement composed from
fragments exists as a whole only at the moment one of those sends it, so a
process-wide question — what does this tree send, and of what kind — has no
place to be asked. A scan of string literals answers it for static text
only: most statements in the tree are assembled from pieces.

**The classifications exist one at a time.** Statement kind, the security
class of [ADR-0132](0132-sqlapplet-sql-defined-applets.md) §SD5, and the
passthrough tables of
[ADR-0117](0117-passthrough-table-classifier.md) each parse the statement
themselves and each answers one question. None says how much work a read
is, and the engine-free endpoint's shape is known only to its own refusal.

[ADR-0144](0144-query-engine-adapters.md) abstracts how a run is delivered,
observed and cancelled. It deliberately carries no statement-level
knowledge, and this ADR adds none to it.

## Decision

We will lead every SQL statement through one named opening on its way out
of the process, the **fairlead**, and we will give every statement a
**stratum** computed from its text. The fairlead does nothing unless a pass
is installed; the stratum is a property a pass, a panel or a report may
read. Routing by stratum is not decided here.

### Subsidiary design decisions

- **SD1 — One opening, at the lowest level.** An *exit point* is the
  function that hands statement text to an HTTP request or to a query
  process. Each calls `fairlead.Lead` with the text it is about to send,
  under a stable site name, and sends what comes back. Callers above an
  exit point do not call it again: a statement is led once. A test in the
  package fails when a file matches an exit-point shape and does not lead,
  with an allowlist for files that send no statement, each entry carrying
  its reason.
- **SD2 — A stratum per statement, from one parse.** `analysis.Classify`
  parses once and answers kind, security class, stratum, passthrough tables
  and the functions called. The read strata, from least to most demanding:
  *verbatim* (one source handed over whole, `SELECT *` with at most a row
  limit), *restrict* (stored rows unchanged but filtered, ordered, or with
  columns left out) and *derive* (anything computed). A statement that
  writes, changes a setting, or that the grammar does not read is
  *unplaced*, which is the zero value. Every axis fails closed
  independently.
- **SD3 — A pass may let through, replace or refuse.** Passes are installed
  process-wide by the host and run in installation order, each seeing the
  text the one before returned. A refusal is returned to the exit point's
  caller and the statement is not sent. With no pass installed `Lead`
  returns its argument without allocating. The fairlead package does not
  parse and does not link the parser; a pass that needs a classification
  asks for one.
- **SD4 — A journal answers "what does this tree send".** A pass records
  each distinct statement text once per exit point, as one JSON line. A
  registered environment variable starts it on a process's first
  statement, so a test run or a scene can be recorded without the code
  under test knowing. It records text and never holds a statement back; a
  report command reads journals and counts statements per exit point and
  classification. Folding statements that differ only in a literal is the
  reader's job, done with the parser the journal does not link.
- **SD5 — A facts read is a verb on a reader interface.** The reads of the
  hand-written facts store are declared as role interfaces beside
  `FactsStoreI`, with their filter and row types, so a consumer names the
  capability it needs rather than the ClickHouse-backed store. The log
  read joins the three that were already declared that way. A read with
  no consumer is removed rather than given an interface: four went, with
  the tests that used them to read a write back.

### What the stratum is and is not

It follows ADR-0117's rules with two differences. A table function is a
source like any other, since whether it reaches beyond the endpoint is the
security class's question. And `FINAL`, `SAMPLE` and `LIMIT BY` are
*derive*, because each needs more than a scan with a predicate. ADR-0117's
own answer is unchanged.

*Restrict* admits any expression as predicate or sort key. A consumer that
cares which functions a predicate calls reads them from the same
classification; this ADR fixes no vocabulary.

The engine-free endpoint remains the authority on what it accepts.
*Verbatim* is close to its shape and is not the same set: that endpoint
also answers constants and refuses a stored table.

### Deferred

- **Routing.** No pass that places a statement by stratum is built. Play's
  per-statement dispatch stays in play; lifting it into the runtime as a
  consumer of `Classify` is the natural next step and needs its own
  decision about who installs policy.
- **What a journal line omits.** Settings and bound parameters travel
  beside the statement and are not recorded.
- **The local-worker pool's context.** A worker takes its statement without
  a context, so passes run there under the background one.
- **The facts store's lane arithmetic.** The hand-written store composes
  membership lookups itself, where the readers outside it already use the
  leeway read surface
  ([leeway-sql-read-surface](../explanation/leeway-sql-read-surface.md)).
  Moving it is left to the envelope migration of
  [ADR-0366](0366-the-facts-envelope-says-what-a-row-is-where-it-lives-and-how-long.md),
  which rewrites those statements in any case.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API: `public/db/fairlead` | added | every exit point; the conformance test's allowlist |
| Exported Go API: `analysis.Classify`, `analysis.QueryStratumE`, `analysis.ClassifyQueryStratum` | added | nothing; the three existing classifiers keep their signatures |
| Exported Go API: `factsstore.LogReaderI`, `factsstore.LogRowsFilter`, `factsstore.LogRowRead` | the two types moved from `chstore`; the interface added | the log host, the store's tests |
| Exported Go API: `chstore.Store` | `RecentLogs`, `LifecyclesByRun`, `LastHeartbeatForRun`, `LookupRunStart` and their filter types removed | the store's tests |
| Environment-variable registry | `BOXER_FAIRLEAD_JOURNAL_DIR` added | `doc/env-vars.md` |
| CLI | `boxer fairlead report` added | — |
| House names | `fairlead` added | the README table |

## Alternatives

- **Carry the passes in the context.** Injectable and test-friendly, but a
  statement sent under a context that was never given them goes unseen,
  and totality is the property being bought.
- **Wrap the HTTP transport.** Sees every request without touching a call
  site, but not a statement handed to a query process, and it sees
  statements already encoded into a URL or a body.
- **Decorate `recordstore.ExecutorI`.** Covers the stores and none of the
  functions that send without an executor, which are the ones a choke
  point exists for.
- **Classify inside the fairlead.** Every client would link the parser,
  and every statement would pay a parse whether or not anything reads the
  answer.
- **Capability data on the engine seam.** ADR-0144 rejected it because
  engines differ in lifecycle. A stratum is a property of the statement,
  not of an engine, so that reasoning is left standing.
- **Reuse ADR-0117 as the stratum.** Its question is which stored rows
  surface unchanged, for policy. It reports nothing for a table function
  and does not separate a whole read from a filtered one.

## Consequences

### Positive

- One place sees every statement, so a policy, a recording or a rewrite is
  written once.
- A statement's layer is stated before it runs, by one function, with one
  parse.
- The set of statements the tree sends can be measured from running code
  rather than estimated from source text.

### Negative

- A process-wide install is global state: a test that installs a pass must
  remove it, and two tests installing in parallel see each other.
- Each exit point carries one more call and one more way to fail.
- The conformance check is textual and per file. It catches an exit point
  added without the call; it does not prove every request in a matching
  file is led.
- A journal directory holds statement text with inlined literals, and is
  as sensitive as the queries.

### Neutral

- With nothing installed the behaviour of every exit point is unchanged.
- The stratum of a statement can change when the grammar learns a form it
  did not read; it moves from *unplaced* to a read stratum, never the
  other way.

## Migration — Tier 1

- **Breaks.** `chstore.LogRowsFilter` and `chstore.LogRowRead` no longer
  exist under those names. `Store.RecentLogs`, `Store.LifecyclesByRun`,
  `Store.LastHeartbeatForRun` and `Store.LookupRunStart` are gone, with
  `chstore.LogFilter` and `chstore.LifecycleFilter`.
- **Path.** Import `factsstore` and use the same type names from it; the
  fields are unchanged. For log rows read `LogReaderI.LogRows`; for a
  run's lifecycle and liveness read `RunEventReaderI`. Nothing replaces a
  runtime-start row read back field by field.
- **Regeneration.** None.
- **Old shape.** Removed outright.

Everything else is additive: nothing to migrate.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the fairlead package (identity with nothing
  installed, order, refusal, the journal, the exit-point conformance scan),
  the stratum and `Classify` tables in `analysis`, the report's fold.
- **What would fail.** A new file that sends a statement without leading
  it; a stratum that moves for a listed statement; a zero-valued
  classification that grants anything.
- **Gap.** No pass other than the journal runs anywhere, so replacing and
  refusing are exercised by tests only. The conformance scan knows two
  transports and is blind to a third. With the four reads went the
  round-trip tests that checked the runtime-start, heartbeat and lifecycle
  writes field by field; the remaining reads cover those rows only as far
  as they project them.

## Status

Accepted 2026-11-10.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-11-10 — a first journal, shapes in the report, and what stayed deferred

**A first measurement.** One run of the default test lane with the journal
on, at the commit that added the shape column, folded by statement shape:
662 shapes left the process. 382 are unplaced, nearly all of them writes
and DDL the grammar does not read. Of the 280 reads, 147 are *derive*, 109
*restrict* and 24 *verbatim*. This counts what the tests send, not what a
running host sends: statements to a query process outnumber statements to
the server about six to one in it, and neither the integration lane nor
a scene was recorded. It says the simpler strata are not a rounding error
among reads; it does not say how a workload divides.

**Shapes.** `analysis.StatementShape` folds statements that differ in
literals, on tokens rather than on a parse, and the report counts shapes
beside texts. SD4's division of labour is unchanged: the journal still
records text.

**Routing stays where it is.** Play's dispatch is a seam with one resolver
and one consumer, and [ADR-0141](0141-play-endpoint-dispatch-seam.md) keeps its
vocabulary play-internal until a second consumer needs it. The journal
produced no such consumer, so nothing is lifted into the runtime and no
pass places statements. What would reopen it: a second app that has to
choose between endpoints per statement, or a host that must refuse a
stratum.

**The lane arithmetic, restated.** The hand-written facts store now spells
its columns and lookups in one file. It was not moved onto the leeway read
surface, and the reason is not the envelope migration alone: that surface
expands into functions installed on the server, and whether the runtime's
own store may require them, or installs them itself at setup, is a
decision nobody has taken.

**Write coverage.** A test now checks that every string and number the
three run-session writers are handed is present in the stored row. It does
not check which membership carries a value, which the removed round trips
did.

### 2026-11-11 — the read surface is part of setup, and the facts store reads through it

**Decided.** The question the previous entry left open is answered: the
leeway read surface is mandatory initial setup. The reason is the
statements, not the server — SQL in this tree should be as readable and as
high-level as the surface allows, and a store that may not assume the
surface is a store that must spell lane positions.

**What follows.** `chstore.Store.SetupTable` installs the surface when the
server does not report the build's revision, and fails setup when it cannot;
`NewWithFallback` then falls back to the in-memory store, as it does for a
failed table DDL. Every read of the store is authored with handles and the
`LW_GET` family and expanded before it ships by `factsread.Prepare`, which
the query-run history shares. The store names no physical column in a read,
and the *lane arithmetic* item under Deferred is closed; it did not wait for
[ADR-0366](0366-the-facts-envelope-says-what-a-row-is-where-it-lives-and-how-long.md)
after all, whose migration now has handles to move rather than physical
column names.

**Surfaces.** Added: the package `factsread`; `chstore.Store.EnsureReadSurface`.
Changed: `SetupTable` does more than DDL, and a role that may create the
table but not a function now fails setup. `SetupStatements` and
`ComposeSetupSQL` are unchanged and install nothing — a caller that runs
them through its own client gets a table it can write and not read.

**Costs.** The store links the SQL parser, which the log host and the
browser tab linked already. An expansion parses the statement: a few
milliseconds for the largest read (`BenchmarkPrepareLogRows`), paid once per
distinct authored text and again on every call for a read whose filter
carries a moving timestamp. Two builds at different surface revisions
sharing one server reinstall over each other at every setup.

**Verification.** A test with no server expands every read with every
optional filter set and fails on a physical column name or an unexpanded
call. The store's live-server tests passed unchanged across the rewrite,
which is the evidence that the rows read the same. Gap: the install branch
of `EnsureReadSurface` ran against no server here — the one at hand already
carried the revision — so it is covered only by the surface package's own
install tests.

## References

- [ADR-0117](0117-passthrough-table-classifier.md) — passthrough tables.
- [ADR-0132](0132-sqlapplet-sql-defined-applets.md) §SD5 — the security class.
- [ADR-0144](0144-query-engine-adapters.md) — the engine seam this ADR leaves alone.
- [ADR-0290](0290-keelson-named-arguments-and-a-trivial-sql-endpoint.md) — the engine-free endpoint.
- [ADR-0366](0366-the-facts-envelope-says-what-a-row-is-where-it-lives-and-how-long.md) — the envelope migration the lane-arithmetic deferral waits on.
