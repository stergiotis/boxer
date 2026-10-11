---
type: adr
status: accepted
date: 2026-10-10
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-11
---

# ADR-0309: The context projection is SQL over the journal, run in a local worker — passes with declared properties, request identity by canonical forms, laws as queries

## Context

With the journal as the only source of model input
([ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md)), something must turn a journal prefix into a provider-neutral
request. The design discussion of 2026-10-10 took the projection to be SQL,
for the reasons [agent-harness §2.3](../adr-background-work/agent-harness.md)
gives: it is a fold over an ordinal range, which is what MergeTree and the
read surface are good at; it runs the same on the server for analysis and in
a worker for the call; and its properties (determinism, prefix stability)
become tests over stored journals with no model. A spike on `clickhouse local`
26.9 projected a 400-entry run in a median 15 ms warm and 90 ms including
process start, byte-identically on repetition, with exactly the compaction,
edit and section-change entries breaking the prefix, as the CALM theorem
predicts for the `argMax`-shaped passes; the figures are the spike's own and
carry their conditions in that page's Appendix A.

Where the query runs is the constraint. The ClickHouse server is reached over
HTTP one statement per request and its `async_insert` default is on; a
per-call server projection makes the server a per-call dependency, the shape
[ADR-0300](./0300-metered-model-calls-and-rules-a-moderator-writes.md)
(proposed) rejected for admission, and one an appliance without a server
cannot meet. `clickhouse-local` workers
([ADR-0028](./0028-chlocal-low-latency-sql-cap.md)) are pre-spawned,
single-use, hold nothing, take Arrow input tables, and are the one engine
local by identity for confined runs
([ADR-0145](./0145-sealed-app-data.md) §SD5).

A second question is what the request *is* for the purpose of law L4 — the
digest recorded when a request is sent must equal the digest of the request
rebuilt later, by another process and possibly another engine version. The
owner pointed at the canonical record form
([ADR-0201](./0201-leeway-canonical-record-form.md)) and the canonical wire
([ADR-0210](./0210-leeway-canonical-wire-generator.md)), which already give
leeway entities an identity that is independent of serialisation.

## Design space (QOC)

**Question.** Where does the projection run?

**Options.**

- **Q1 — On the server**, a server query per call.
- **Q2 — In a local worker**: the server holds the journal as the record;
  the single writer holds its run's entries and ships them to a
  `clickhouse-local` worker per call.
- **Q3 — SQL as the specification, Go on the hot path**, equivalence tested
  on stored journals.

**Criteria.**

- **C1 — No per-call server dependency**; an appliance without a server
  still chats.
- **C2 — Confined runs are legal**: the projection runs on an engine local
  by identity.
- **C3 — One definition** serves the call and the analysis.
- **C4 — Cost** per call and in code.
- **C5 — Engine coupling** of the request bytes.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | Q1 | Q2 | Q3 |
|----|----|----|----|
| C1 | −− | ++ | ++ |
| C2 | −  | ++ | ++ |
| C3 | ++ | +  | −− |
| C4 | +  | +  | −  |
| C5 | −  | −  | ++ |

Q3 keeps two implementations of one projection in step by hand. Q1 is the
shape the tree has refused for anything on a call path. Q2 is the
placement taken; its cost in C5 is contained by SD3, which takes the
request's identity out of the engine's serialisation. What runs on the
compile date is closer to Q3 with Q2's equivalence lane (SD1): the Go fold
is on the call path and the SQL is held to it by a generated-journal test
and a golden, because the worker's per-call cost was not measured before
the chat needed the projection. Moving the call path onto the worker is a
measured decision left open in SD8, not a change of this record.

## Decision

We will build each model request as a **projection over the run's journal
prefix**, written once as SQL that runs in a `clickhouse-local` worker and
on the server, and once as a Go fold that the coordinator projects with on
the call path, the two held equivalent by the test lanes. The projection is
a pipeline of passes with declared properties checked in the test lanes; a
request's identity is a canonical form of its messages, so law L4 does not
depend on an engine's JSON; and the harness's laws are checks over the
journal and the trail that return the rows that break them.

### SD1 — One definition, two texts

The `projection` package holds the fold and the SQL side by side. The fold
is what the chat projects with on every request; the SQL takes the same
entries as a plain Arrow table in a `clickhouse-local` worker, and the
default lane holds the two to one digest over the corpus golden and over
generated journals whenever `clickhouse` is on the path. The server-side
plain-shaped view over the leeway-laid-out journal tables, through which
the same SQL would serve analysis, is not built (SD8); the introspection
tables of ADR-0308 §SD10 are the server's read surface meanwhile.

### SD2 — Passes with declared properties

π is a pipeline of passes over the prefix. Each declares, and a test over a
`rapid` generator of journals checks:

| Property | Meaning | Check |
| --- | --- | --- |
| `Reads` | the entry kinds the pass consults | a pass reading an undeclared kind fails a corpus test |
| `AppendStable` | appending an entry only appends to the pass's output | `π(J·e)` has `π(J)` as prefix |
| `Rewrites` | the pass may change earlier output | declared; the profile's count of rewriting passes is its prompt-cache cost |
| `Idempotent` | applying twice changes nothing | the nanopass `AssertProperties` shape, over the harness's own state |
| `Shapes` | tool calls and results stay paired, batches whole | a validating pass at the end of π |

Latest-section-wins, latest-edit-wins and the compaction cut are the
`Rewrites` passes; everything else is a monotone query and append-stable.
Markers the projection inserts — `withheld`
([ADR-0313](./0313-agent-schema-generated-relations-an-agent-may-read-in-full.md)),
`recalled`
([ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md)),
visible edits — are passes too, because they change request bytes. As
built, `Rewrites` is declared per entry kind and `AppendStable` is the
property a `rapid` test checks over the fold; `Shapes` is the validation
the chat runs on every projected batch (L2 in SD6); `Reads` and
`Idempotent` are not declared as data.

### SD3 — Request identity by canonical forms

The projection returns **one row per message**, a typed batch (part,
ordinal, role, content, tool calls, the dispatcher's call key, provenance
ordinals, label), not a JSON string. Identity is canonical: each message
row's digest is deterministic CBOR of its content fields under the
canonical form's keyed BLAKE3, and the **request digest** is BLAKE3 over an
item holding the ordered message digests and the profile fingerprint.
Message order is content, so the item is an array, not ADR-0201 §SD1's
sorted multiset. The batch is not a leeway record, so its form is the
projection package's own rather than ADR-0201's record form; what carries
over is the rule — deterministic encoding, keyed digest, serialisation out
of the identity. Go encodes the batch into the route's wire format
([ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md)
§SD4); the wire bytes are not the identity. A lossless byte stream of a
request through ADR-0210's canonical wire is deferred (SD8); the golden is
the batch's JSON. Law L4 therefore compares canonical digests, and a
ClickHouse version whose JSON escaping differs changes nothing it checks.
Who formats the final JSON is settled by this: Go does, once, per route.

### SD4 — Worker preconditions and the fingerprint

Three preconditions, each a test when the worker is on the call path: the
worker enforces a read-only setting on the shipped relation (the M2 broker
ignores `ExecRequest.Settings` on the compile date, so the setting travels
in the statement prelude or a settings profile and a test asserts it holds);
the shipped input is sealed or piped, never a plaintext file another process
can read ([ADR-0145](./0145-sealed-app-data.md)); and the worker's and the
server's engine versions agree or the difference is known, the version
entering the run's fingerprint (ADR-0308 §SD3). None of the three is built
while the fold is the call path (SD1); they go with the move in SD8.
Journal inserts should state `async_insert = 0`; the executor seam has no
per-insert settings on the compile date, and the store records the
deferral.

### SD5 — The host-side half is a small pass shape

What SQL cannot do — encoding the batch to a route's wire format, shaping a
producer's result before it becomes an `outcome`, the sequential replay in
compaction — is Go over harness state, beside nanopass and not a
generalisation of it. As built, that host-side half is composed with the
seam shapes of ADR-0310 §SD1 (a chain with declared order for request
configuration, a first claim for recovery) rather than a `Pass[S]` type of
its own; `Requires`/`Produces` are not declared. Whether π's SQL stages are
themselves spliced by nanopass passes per profile is deferred (SD8).

### SD6 — Laws as queries

Each law is an assertion over the journal and `boxer.facts` returning the
rows that break it: Go checks in the default lane over the preservation
harness's journals, SQL in the integration lane where written. The
projection's own:

- **L1 write-ahead** — every outcome follows an intent with its call key,
  every reply its request (a join on
  [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) §SD6's
  key); the host service refuses the append that would break it (ADR-0308
  §SD9), and the query form is not written;
- **L2 tool pairing** — in a projected request, every tool result answers a
  call made earlier in it, no call goes unanswered at the end and none is
  answered twice; the chat validates every batch before it is sent;
- **L4 reproducibility** — every recorded request digest equals the digest
  of the projection rebuilt (SD3); checked fold against engine in the test
  lanes;
- **L5 append stability** — an entry not declared as rewriting only extends
  the request; a `rapid` property over the fold;
- **L7 required context** and **L28 declared versus observed** — notes the
  profile requires appear in every request; a run's declared profile agrees
  with the passes, guards and tools its journal shows. No checker is
  written: each is a query over stored journals the integration lane does
  not run yet (SD8).
- **L30 placement** — a run's projection never ran on an engine outside
  the declared cell; trivially true while the fold runs in process, and a
  checker goes with the worker's move (SD8).

The label laws L3, L6, L8 are ADR-0311's; L9 is ADR-0315's.

### SD7 — A golden in the default lane

Projected requests over a fixed journal corpus as a golden the fold is
held to in the default lane and the engine's SQL to where `clickhouse` is
installed; `groupArray` order and `argMax` ties pinned by deterministic
tie-breaks. The golden is the batch's deterministic JSON, so the engine's
own JSON is not in it.

### SD8 — Deferred, recorded

- Moving the call path onto the `clickhouse-local` worker (Q2's
  placement), with SD4's preconditions, once the worker's per-call cost is
  measured against the fold's.
- The server's plain-shaped view over the leeway journal tables, and the
  shape-contract test between the two layouts.
- A canonical-wire stream of a request (ADR-0210) for exports.
- Splicing profile-dependent SQL stages by nanopass passes.
- Incremental maintenance of π (`π(J·e)` from `π(J)` and `e`), if the
  per-call cost ever matters at the measured sizes.
- Branch-aware projection over a `parent` ordinal (ADR-0308 §SD1).

## Alternatives

Q1 and Q3 are weighed in the QOC. Three narrower choices:

- **SQL emits the final JSON** and the digest is over it. Rejected: the
  request's identity would depend on the engine's JSON escaping and version,
  so the golden would be per version and L4 would fail on an upgrade with
  nothing wrong.
- **SQL emits neutral JSON and Go re-encodes** per route. Rejected: two
  encodings of the same thing on every call, and the identity still passes
  through an engine serialisation.
- **Generalising nanopass** to carry the host-side passes. Rejected for now:
  a Tier-1 change to a pipeline-stage contract with wide reach, for a
  handful of passes; a small shape beside it costs less (SD5), and the choice
  stays open in SD8.

## Consequences

### Positive

- Any request is reproducible from the journal by anyone with the engine,
  and compared by a digest that does not depend on serialisation.
- Prefix stability becomes a property that fails a test instead of prose
  about cache effects; each rewriting pass is a priced exception.
- One projection text serves the call and the analysis.

### Negative

- A database engine takes part in every model call; a warm local worker
  bounds it to milliseconds at the spiked sizes, a cold start otherwise.
- Two ClickHouse processes whose versions must agree, and a golden per engine
  version.
- Two authoring languages for one pipeline: SQL for the projection, Go for
  the wire and the result shaping.

### Neutral

- The server is not on the call path; it holds the record and serves the
  analysis.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `clickhouse-local` worker path (ADR-0028, ADR-0145) | a consumer in the test lanes; on the call path only with SD8's move | a dated Update on ADR-0028 with that move |
| the journal's plain-shaped view (ADR-0308) | deferred (SD8) | the shape-contract test, with it |
| `openaichat` encoding | takes the projected batch, not a message slice | the preservation harness's golden |
| canonical forms (ADR-0201, ADR-0210) | the batch digests under the canonical form's keyed hash; the canonical-wire stream is deferred; no change to either record | — |
| `keelson('…')` | no law-violation tables yet; the laws are Go checks and lane SQL | — |

## Migration — Tier 1

- **Breaks.** Nothing until ADR-0308 M3; then the chat's wire requests are
  built by the projection, byte-identical on the preservation corpus by
  construction of the harness that precedes it.
- **Path.** None.
- **Old shape.** None kept.

## Verification plan — Tier 1

- **Lane.** Default: pass-property tests over generated journals; the golden
  of SD7; the fold against the engine over generated journals where
  `clickhouse` is installed. Integration: the store against
  `clickhouse-local`.
- **What would fail.** A request whose canonical digest differs from the
  recorded one; an entry declared append-only that rewrites; a tool result
  without its call in a projected request; a fold and an engine that
  disagree on one journal.
- **Gap.** Quality of what the projection chooses to show is not checked
  here; that is ADR-0316's and the trials'.

## Status

Accepted 2026-10-11.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0028](./0028-chlocal-low-latency-sql-cap.md) — the worker pool.
- [ADR-0145](./0145-sealed-app-data.md) — sealed input; local by identity.
- [ADR-0201](./0201-leeway-canonical-record-form.md), [ADR-0210](./0210-leeway-canonical-wire-generator.md), [ADR-0289](./0289-leeway-rows-for-readers-canonical-forms-a-read-model-and-kinds-in-projection.md) — canonical forms for identity and for a lossless stream.
- [ADR-0108](./0108-keelson-sql-pass-registry.md) — the pass registry whose shape SD5 copies.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) — the trail; call keys.
- [ADR-0300](./0300-metered-model-calls-and-rules-a-moderator-writes.md) (proposed) — the per-call dependency refused.
- [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) — the journal.
- [agent-harness](../adr-background-work/agent-harness.md) §2.3 and Appendix A — the spike and its conditions.
