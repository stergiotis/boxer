---
type: adr
status: proposed
date: 2026-10-08
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0296: Audit events as a trail component — a generic `AuditEvent`, loss that is visible, a forwarder seam, and what keeps `boxer.facts` dispatchable

## Context

A consumer that keeps a records-of-processing trail (GDPR Art 30, Swiss FADP
Art 12 as [ADR-0027](./0027-pushout-forget-swiss-fadp.md) reads it) needs
rows that say *what was done to whose data, by whom, and why*. The
mechanics of such a row exist here already:

- the trail ([ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md))
  writes component rows to `boxer.facts` through one `Recorder`, with a
  host-stamped `Origin`, a bounded backlog of held batches, a separate read
  store and a `TrailComponentSQL` set of per-kind `Filter`s;
- recordstore ([ADR-0295](./0295-recordstore-call-identity-observed-executors-and-write-observers.md))
  carries a `callident.CallIdentity` on the context, gives generated stores
  `BeginCtx` and a `WriteObserverI` that reports which keys a batch made
  durable, and mints a batch id per insert attempt.

What a consumer cannot do with them: the recorder's verbs take no context,
so a principal on the context reaches no trail row; the recorder builds its
stores from a fixed, empty configuration, so no write observer can be
attached; a dropped batch is one Error log line, invisible in the table;
and there is no component whose vocabulary is the consumer's rather than
the host's. The first consumer is a downstream repository whose own ADR
decides that its audit events are rows on `boxer.facts` written through
the trail, and that it adds only values — domain and action names,
reference types, retention classes — never memberships or generated code.
That ADR places six requirements on this one: the component's fields, who
vouches for each, the failure semantics, a forwarder fed two ways, the
constraints under which `boxer.facts` can later be made a Null table that
routes rows to storage tables, and whether the host attaches a call
identity to the contexts it hands apps.

The dispatch question was spiked on ClickHouse 26.9 (clickhouse-local, the
unchanged recorder writing into a Null `boxer.facts` with two materialized
views). Routing on a component `Filter` and its complement put every row in
exactly one table; a generated store read each storage table and a `Merge`
over both unchanged; `MATERIALIZED` sort and partition columns derived from
scalar slots with built-in functions agreed with the leeway UDFs and stayed
out of `SELECT *`. The partial-failure arms only logged what they saw: a
failed view insert left the other target committed, and a plain retry
duplicated. Nothing about deduplication on the recorder's Arrow insert path
was asserted, and no executor sends a deduplication setting today.

Two things in the tree constrain names. `boxer.facts` already holds an
`audit` kind — the bus-request rows of ADR-0026, written by `chstore.WriteAudit`
— and an `event` kind for bus events. Vocabulary ordinals never retire.

## Decision

Add one generic component, `AuditEvent`, to the trail, with a recorder verb
that reads the call identity from its context, a write observer and a read
table as recorder options, loss that is counted and written back into the
trail, read helpers by principal and by subject, and a forwarder seam fed
by the observer and by a lagged re-scan. Record the constraints that keep
`boxer.facts` dispatchable as binding, and build of them only the
read/write split on generated stores. Host wiring of the call identity is
decided against, for now.

### SD1 — The `AuditEvent` component

One domain component, `AuditEvent`, kind marker `runtimeKindAuditEvent`, on
the generated trail store. Every field is a plain scalar or a list under one
membership; nothing is a tuple or a carrier channel, so the row is readable
as a component on every path ([facts-bound record stores](../explanation/facts-bound-record-stores.md)).

| Field | Section | Meaning | Who vouches |
| --- | --- | --- | --- |
| `Kind` | `symbol` | the marker; the component's `Filter` alone identifies the row | the recorder |
| `Domain`, `Action` | `symbol` | the event type: two `naming.StylableName` values in `DefaultNamingStyle`, validated | the emitter |
| `Outcome` | `symbol` | `ok`, `denied` or `failed` | the emitter |
| `Principal` | `stringArray`, unit, optional | a pseudonymous reference to who acted; per-person, so never `symbol` (ADR-0277 §SD9) | the claims on the context |
| `PrincipalBy` | `symbol` | `system`, `env`, `os` or `none`: how much a reader may trust `Principal` | the emitter; forced to `none` when there is no principal |
| `Purpose` | `symbol`, optional | an opaque purpose code | the claims on the context |
| `Node` | `symbol`, optional | the cell or node that wrote the row; absent on a single-node host | the emitter |
| `Subject` | `u64Array`, unit | the primary data subject; 0 when there is none | the emitter |
| `Retention` | `symbol` | the retention class label, a `StylableName` | the emitter |
| `RefTypes`, `RefValues` | `stringArray`, two parallel lists | typed object references: ids or hashes, never values | the emitter |
| `AttrKeys`, `AttrValues` | `stringArray`, two parallel lists | bounded short codes | the emitter |

`Subject` and `Retention` are single scalars so that a storage table can
derive its sort and partition columns from them (SD9). References and
attributes are parallel lists rather than a tuple because a tuple field is
not a component slot.

Bounds are exported constants of the package. Domain, action, retention,
reference types and attribute keys are lower-spinal names of at most 64
runes; a principal, a reference value or an attribute value is at most 256
runes and not empty; at most 32 references and 16 attributes. A row outside
them is invalid (SD4).

The row carries the context components the other verbs carry: `Origin`
always, `Conversation` and `Delegation` when the caller's `Context` has
them. `Origin` on an audit event is **stated by the writing process**: it
comes from the vouched part of the context's `CallIdentity` when one is
there, else from the caller's `Context`, else it is the run alone. Nothing
in this repository attaches an identity to a context, and the downstream
process builds its own, so a reader must weigh an audit event's `Origin` as
a claim. ADR-0277 §SD1's "an app cannot claim `Origin`" holds for the verbs
whose origin is a bus envelope, and its Update says so.

### SD2 — `Recorder.Event` takes a context; the other verbs do not

`Event(ctx, at, c Context, row AuditEvent) error` is the first recorder verb
with a context. It reads `callident.CallIdentityFrom(ctx)`: the origin as
SD1 says, `Principal` and `Purpose` from the claims, overwriting whatever the
row carried. Without a principal the row's `PrincipalBy` becomes `none`.
It opens the row with `BeginCtx`, so the generated store's `WrittenKey`
carries the identity the row was committed under. Its natural key is the
domain, the action, the subject and the run's unique part; its id is the
hash of that key, like every trail row's.

The other verbs keep their signatures. Their rows are host or dispatcher
records whose identity is the bus envelope's; threading a context through
the model, egress and agent services would give them a field nothing sets.
A later ADR that wires an identity into those services revisits this.

A new action is a new value of `Action`. No vocabulary entry, no
regeneration.

### SD3 — Recorder options: a write observer and a read table

`NewRecorder` takes functional options. `WithWriteObserver` attaches a
`recordstore.WriteObserverI` to every store the recorder builds — the
current one and each held batch — so a consumer learns which row ids a
batch made durable, under which batch id, and which were discarded, with
ADR-0295 §SD7's once-per-key guarantee. `WithReadTable` names the table the
recorder's read store scans, so reads can go to a storage table or a `Merge`
while writes still go to `boxer.facts` (SD9 d).

### SD4 — Loss is counted, and written back

The recorder keeps four counters — buffered, written, dropped, invalid —
readable through `Counts()`. They cover every verb.

- **A gap is a row.** When the backlog cap drops batches, the recorder
  remembers how many rows and the earliest and latest timestamps among
  them. The first flush that lands everything held afterwards writes one
  `AuditEvent` in domain `trail`, action `audit-gap`, outcome `failed`,
  with the count and the window as attributes. Until then a gap is only a
  log line, and after it a reader of the table sees that rows are missing
  and roughly when.
- **An invalid event is a row.** A row that fails validation is replaced by
  an `AuditEvent` in domain `trail`, action `audit-invalid`, naming the
  attempted domain and action as attributes and the reason, logged at
  Error. The caller's call does not fail. The recorder's own rows are built
  from constants and bounded values, so they are valid by construction and
  skip validation.
- **Close counts.** `Close` keeps its bound — the final flush's timeout —
  and afterwards counts what the backlog and the current buffer still hold
  as dropped, logged at Error. Those rows are gone; there is no gap row for
  them, because nothing runs after `Close`.

Recording never blocks on the server and never fails the caller for a
server reason; `BOXER_TRAIL_REQUIRED` does not apply, because an audit
event is flushed behind the work it records, as fetches are, not ahead of
it.

### SD5 — Reads: by principal and by subject

Two helpers over the read store: events of one principal, and events of one
subject, each within a time window and under a limit. The SQL narrows by the
`ts` column and a `has()` over the section's value column, with the physical
names read out of the generated DDL as the watchbill store does; the exact
match on the slot is made in Go after decoding. A `dm_trail_audit_events`
view joins the per-kind views in trailviews, and the timeline gains the
kind.

No index is added. ADR-0277 §SD10 asked for a measurement before one; a
bloom filter over the shared u64 value column would index every u64 slot of
every kind, and the facts DDL is `CREATE TABLE IF NOT EXISTS`, so a deployed
table would not get it without an `ALTER … ADD INDEX` path that does not
exist. Both facts go with the open item.

### SD6 — The forwarder seam

`ForwarderI` has one method: forward a batch of trail entities, returning
an error. Delivery is at least once; the receiver deduplicates by row id.
Two paths feed it, and neither reads the in-memory buffer:

- **Prompt.** The recorder keeps each buffered audit event's entity until
  the store reports its key durable or discarded. On `Durable`, the audit
  events among the batch's keys are handed to the forwarder on a goroutine
  of their own behind a bounded queue; a full queue drops the hand-off and
  counts it, and the backstop covers it. The flush is never held up.
- **Backstop.** `ForwardWindow(ctx, from, to)` scans the read store for
  audit events in a time window and forwards them, skipping ids the prompt
  path forwarded recently. The caller chooses and persists its windows. It
  is a lagged re-scan, not a cursor, because `ts` is the event's own time:
  a batch held through an outage lands later with old timestamps, and a
  cursor that had passed them would never see them — the case a backstop
  exists for. A cursor over an insertion-order column comes with the
  storage tables (SD9 c).

### SD7 — Host wiring: no

`MountContextI` hands an app a cancel channel, not a `context.Context`, so
there is no carrier for a host-attached `CallIdentity`. Each process sets
its own identity with `callident.WithCallIdentity`, and adds claims with
`WithClaims`. Deciding where a context reaches an app is a host design
question that this ADR does not open. ADR-0295's deferred item is recorded
as decided this way, not closed.

### SD8 — The read/write split on generated stores

Every `<Store>StoreConfig` gains `ReadTable`. `SELECT` and `DESCRIBE` go to
it when set; `INSERT` and provisioning go to `Table`. Empty, it is `Table`.
Every in-tree store is regenerated; none sets it.

### SD9 — What keeps `boxer.facts` dispatchable — binding, mostly not built

The owner's direction is that `boxer.facts` may later become a Null table
whose materialized views route rows to storage tables — a main one, and an
audit one partitioned by retention class and sorted by subject and time.
The spike showed this works for an unchanged writer under these
constraints. They bind every change to the trail from now on; of them only
(d) is built here.

- **(a) Component-only routing.** A row is routed by a component's `Filter`
  and by nothing else: never a natural-key prefix, never knowledge only Go
  has. The main table's view is the complement of every other route.
- **(b) Timeless ids.** A `Filter` and a derived-column expression carry
  membership ids as literals, so the ids come from the runtime vocabulary
  and its committed golden, and an ordinal is never reused.
- **(c) Derived columns in the table.** A storage table computes its sort
  and partition columns as `MATERIALIZED` columns from scalar slots, with
  built-in functions only. The writer sets no extra column and `SELECT *`
  does not return them, so generated stores read the table unchanged. For
  a symbol scalar under membership `m`:
  `if(has(lr, m), values[arrayFirstIndex(cum -> cum >= indexOf(lr, m), arrayCumSum(lrcard))], '')`;
  for the first value of a list slot, the same index taken into
  `arraySlice` over the `len` column. The generator will emit them next to
  `Filter` for every unit scalar slot when the switch is built; an
  insertion-order column (`MATERIALIZED now64()`) is added then for SD6's
  cursor.
- **(d) Reads and writes name different tables.** Built: SD3's read table
  on the recorder and SD8 on every generated store. Deferred to the switch:
  chstore's hand-written readers, trailviews and keelsonddl, which name the
  fixed facts table.
- **(e) Duplicates on retry are suppressed.** Every view target carries a
  deduplication window (or a Replicated engine). The order in which views
  commit is not deterministic, so a failed insert may have committed into
  some targets. The recorder re-sends a held batch unchanged. What the
  spike did not test, and the switch must: that the recorder's Arrow
  insert, sent with `insert_deduplicate` and
  `deduplicate_blocks_in_dependent_materialized_views`, lands once under a
  window — no executor sends either setting today. If a token is sent it is
  derived from the batch's row ids, never from ADR-0295's batch id, which
  is new on every retry.
- **(f) One DDL path.** `SetupTable` and `keelsonddl` create the storage
  tables, the views and the `Merge` table in one step, with
  `allow_suspicious_low_cardinality_types` at query level for the `Merge`.
  Views are recreated when a vocabulary change renames physical columns, as
  the generated DDL already does. A test proves every component kind lands
  in exactly one table.

The switch is a whole-table decision, not a trail one: once `boxer.facts`
is Null, every writer — chstore's lanes, persist, sysmfacts, watchbill,
queryrunsd — inherits materialized-view semantics: an insert fails when any
view's insert fails, and commits across targets are partial. The ADR that
makes the switch states that for each writer.

### Deferred

- The Null switch, `boxer.audit`, the storage tables and their views; the
  integration test over the spike; the generator's derived-column emission;
  the deduplication settings on Arrow inserts; the insertion-order column
  and a cursor over it; read tables on chstore, trailviews and keelsonddl.
- A skip index for reads by subject (SD5).
- Host wiring of the call identity (SD7).
- Migrating the bus-request `audit` kind onto `AuditEvent`.
- Hash chains, signatures, retention periods, write-ahead for audit events,
  and every domain's vocabulary — the consumer's.

### Milestones

- **M1 — The component, the verb, the options (SD1, SD2, SD3, SD8).**
- **M2 — Loss accounting (SD4).**
- **M3 — Reads and the view (SD5).**
- **M4 — The forwarder (SD6).**

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| runtime vocabulary | `runtimeKindAuditEvent` and the `auditEvent*` memberships added, ordinals from 290 | the membership golden |
| `trail` generated store | `AuditEvent` in `ComponentPaths`; `TrailComponentSQL` gains the kind | regenerated store and DDL file |
| `trail.Recorder` | `Event`; `NewRecorder` options; `Counts`; read helpers; `ForwardWindow` | hostboot's construction is unchanged |
| `<Store>StoreConfig` (every generated store) | `ReadTable` added | all stores regenerated in one commit |
| trailviews | `dm_trail_audit_events`; the timeline; `ViewsVersion` 10 | the views golden; deployed servers re-run the views half of keelsonddl |
| ADR-0277 §SD1, §SD10; ADR-0295 deferred items | dated Updates | — |

## Alternatives

- **A membership per action.** Rejected: a new action would be a registry
  change and a regeneration, and a reader on an older registry would see
  it as absent without error. Values need neither.
- **The actor stamper (ADR-0295 §SD8) instead of `Principal` on the row.**
  Rejected: it refuses a row without a principal, and the recorder's own
  rows and host-service rows have none.
- **A cursor over `(ts, id)` as the backstop.** Rejected (SD6): late-landing
  rows have old timestamps.
- **Reusing the bus-request `audit` kind.** Rejected: it is a hand-written
  lane with its own fields, read by two readers; a generic component with
  the consumer's vocabulary is a different thing, and the old one can
  migrate onto it later.
- **Build the storage tables now.** Rejected: today's volume does not ask
  for it, the spike left (e) untested, and the switch changes every
  writer's failure semantics.

## Consequences

### Positive

- A consumer records its own vocabulary on the host's trail, joined to
  model calls, actions and statements by `Origin` and the call identity,
  without a table, a vocabulary or generated code of its own.
- Loss is in the table, not only in a log.
- Moving audit rows to their own storage later needs no writer change, and
  the constraints that keep it so are written down.

### Negative

- `Origin` on an audit event is a claim, and the ADR-0277 §SD1 table has a
  row that is weaker than the others.
- Every boxer host may now write `audit-gap` rows, with no principal and no
  delegation, whether or not it records audit events.
- Every generated store regenerates for a field none of them sets.
- The backstop re-scans a window on every pass until an insertion-order
  column exists.
- Reads by subject scan the table, narrowed only by the time-ordered
  primary key.

### Neutral

- An audit event is not written ahead. A revisit trigger in the consumer's
  ADR adds a write-ahead for reads of personal data if the system demands
  it.
- The recorder's own domain, `trail`, is a value like any other and takes
  no membership.

## Migration — Tier 1

Nothing to migrate for existing rows or callers. `NewRecorder`'s callers
pass no options and compile unchanged. Generated stores keep every verb;
`ReadTable` is a new field with a falling-back default. The views golden
and the vocabulary golden are regenerated in the commits that change them.
Deployed servers with trail views are one view behind until the views half
of keelsonddl is re-run.

## Verification plan — Tier 1

| Lane | Goes red when |
| --- | --- |
| `trail` unit tests (the gated executor) | a row outside the bounds is written instead of replaced; the identity on the context stops reaching `Origin`, `Principal` or `WrittenKey.Identity`; a forced outage and recovery yields no gap row or the wrong count; `Close` under a closed server reports no loss; the observer is told a key twice across a failed flush; the prompt path forwards a row before it is durable |
| `trail` tests on clickhouse-local | the read helpers miss a row in the window or return one outside it; a backstop pass misses a row that landed late inside its window |
| `recordstore/example` | a scan stops naming `ReadTable`, or an insert starts to |
| vocabulary and trailviews goldens | an ordinal moves, or a view changes without its version |

Gap: the storage-table dispatch of SD9 has no lane until the switch is
built; (e) in particular is a requirement on that milestone, not a verified
property.

## Status

Proposed — awaiting review by p@stergiotis.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) — the trail: components, `Origin`, the backlog, the read store.
- [ADR-0295](./0295-recordstore-call-identity-observed-executors-and-write-observers.md) — call identity, `BeginCtx`, write observers, batch ids.
- [ADR-0289](./0289-leeway-rows-for-readers-canonical-forms-a-read-model-and-kinds-in-projection.md) — the read model a presenter of events uses.
- [ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md), [ADR-0146](./0146-leeway-marshall-component-read-contract.md), [ADR-0183](./0183-leeway-component-consumer-simplification.md) — generated stores and the component read contract.
- [ADR-0184](./0184-sysmetrics-persistence-tee.md) §SD2 — chstore owns the facts table's DDL.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — the bus-request `audit` kind.
- [ADR-0027](./0027-pushout-forget-swiss-fadp.md) — the FADP reading.
- [Facts-bound record stores](../explanation/facts-bound-record-stores.md).
