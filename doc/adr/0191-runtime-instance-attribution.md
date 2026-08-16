---
type: adr
status: proposed
date: 2026-08-16
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0191: Runtime instance attribution on the durable fact trail

## Context

The runtime writes its own history to two tables: `boxer.facts` (the
append-only fact trail, ADR-0026 §SD6) and `boxer.persiststate` (app state,
ADR-0105 D3a). Between them they hold ten kinds of runtime event — process
start, heartbeat, app lifecycle, launch, workingset, audit, grant, log,
query run, column-width override — plus every persist write.

Three of those kinds carry the identity of the *window* the event belongs
to. `AppLifecycleRow`, `LaunchRow` and `WorkingsetRow` each write
`MembLifecycleTileKey`, the host-minted per-window key that
`MountContextI.InstanceKey()` hands the app. The rest carry an app id and
nothing finer, so an audit row from `play` says only "some `play`" — and
`play` is routinely open in two windows at once, which is the case the key
exists to tell apart.

Two of them carry no run either. `AuditRow`, `GrantRow` and `LogRow` have
no `RunId` field, and `boxer.persiststate` has no run column at all. Rows
of those kinds can only be attributed to a process by their timestamp
falling inside its window — which is not attribution, because two boxer
processes may overlap, and is silently wrong rather than absent when they
do.

The pressure that surfaced this is a timeline over the trail: one lane per
app-run instance, limited to the current run. Under today's shape only
three kinds can be laned exactly, the busiest kind (audit) cannot be laned
at all, and "the current run" is a time range rather than a predicate. The
consumer can guess — join an event to whichever window of that app was open
at the time — but only unambiguously when exactly one was, and the guess
would then be re-implemented by every consumer that asks the same question.

The carrier already exists on the live side. ADR-0188 §SD1 put the instance
key on `inprocbus.Client` and on the subscription rows it accumulates, and
recorded, as a deferral with an explicit trigger, that
*"the wire (`Msg.Sender`) carries no instance key, so a handle cannot be
attributed to a window — trigger: an instance dimension on the bus
envelope"*. This decision supplies that dimension and extends the same
attribution from the live tables to the durable ones.

## Decision

We will make every runtime-written row carry the instance it came from:
the `(run id, instance key)` pair, on the memberships that already spell
it, sourced from the bus envelope for service-mediated writes and from the
store for the process-wide half.

### SD1 — The instance dimension is `(run id, instance key)`, on existing memberships

No new vocabulary. `MembRuntimeRun` already carries the run id as a mixed
low-card-ref whose high-cardinality parameter *is* the id, and
`MembLifecycleTileKey` already carries the window key on the `u64Array`
section. The change is that every kind carries them, not that a new term is
minted.

`Lifecycle` in `runtimeLifecycleTileKey` is historical: ADR-0135 reused it
for launch rows and ADR-0148 for workingset rows, both for the reason that
applies again here — one column to join on beats a per-kind spelling.
Renaming it would renumber an id that rows on disk already carry
(ADR-0183 D0), so the name stays and this ADR is where a reader finds out
that it means "instance key" everywhere.

The pair is what identifies an instance, not the key alone. The key is a
counter minted per host process, so it is unique only within a run.

### SD2 — The bus envelope carries the sender's instance

`app.Msg` gains `SenderInstance uint64`. `inprocbus` fills it from the
client's own key, which the host stamps at Open (ADR-0188 §SD1) and the
client has held ever since — so this is a field on a struct the transport
already builds, not new plumbing.

This is the "instance dimension on the bus envelope" ADR-0188 named as the
trigger for its deferred broker-side handle eviction. That deferral becomes
actionable here; taking it is a lifecycle change rather than an attribution
one and stays out of scope.

`natsbus` sets no `Sender` today, so it stamps no instance either. Zero is
"unattributed", which is what a service on that transport already sees for
the app id.

### SD3 — The store stamps the run; the producer stamps the instance

The run id is process-wide, so eight DTOs would otherwise repeat one
constant. `chstore.Store` learns it once at construction (from `runinfo`)
and every write stamps `MembRuntimeRun` — the row's own `RunId` when it has
one, the store's otherwise. Kinds that already set it are unchanged, and
kinds that never had the field gain the membership without gaining a field.

The instance key is per-window, so it does ride the DTOs:
`AuditRow`, `GrantRow` and `LogRow` gain `InstanceKey uint64`, alongside
the `TileKey` the other three already have. Zero means unattributed —
a write from the host itself, from a CLI bootstrap, or from a process
predating this ADR — and readers must treat it as unknown rather than as
window zero.

### SD4 — Each producer's source of the key

| Kind | Where the key comes from |
| --- | --- |
| audit | the `inprocbus.Client` recording the request — it already holds it |
| grant | `Msg.SenderInstance` on the grant request (SD2) |
| log | the per-window logger already tags `instance_id`; `logbridge` lifts that field to the first-class column |
| query run | `MountContextI.InstanceKey()`, through play's stamp identity |
| column width | the frame context's mount context, which the host already built with the key |
| persist state | `Msg.SenderInstance` on the persist request, through `StorageRef` |

The log path is worth naming separately: those rows already carry
`run_id` and `instance_id` as structured `LogField`s, because the host
pre-tags the per-window logger. Lifting them is a promotion, not a capture —
which is also why rows written before this ADR stay readable, through the
field rather than the column.

### SD5 — `boxer.persiststate` gains a run and an instance section

The generated store's `State` component gains `RunId string` and
`InstanceKey uint64`, on two new sections carrying `runtimeRun` and
`runtimeLifecycleTileKey` on the low-card-ref channel the store already
writes.

They are provenance, not identity: persist state is keyed `<appId>/<key>`
and stays app-scoped and latest-wins. Which window wrote a value is the
same class of fact as `WorkingsetRow.TileKey`, which ADR-0148 also records
as provenance beside an identity that does not include it.

### SD6 — The runtime vocabulary becomes nameable from SQL

`providers.MembershipLookup` and `keelson('memberships')` answer for every
vocabulary this repository writes to `boxer.facts` — vdd's, the runtime's,
sysmetrics', capmap's — rather than vdd's alone.

This is not a separate concern that got folded in; it is the read half of the
same problem. A reader asking "which window wrote this" writes
`LW_GET('symbol', 'runtimeLifecycleTileKey', 'chan:low-card-ref')`, and before
this that call was **refused**: play binds `MembershipLookup` as its
`constructsql.MembershipIdsI`, and it consulted a registry that does not hold
the runtime vocabulary. The author's recourse was to carry
`9223372049739677728` in the SQL text — the literal ADR-0171 §SD4 exists to
remove, reintroduced for the one vocabulary most `boxer.facts` rows are made
of. `keelson('memberships')` could not answer the reverse question either.

It is a declared list of four registries, not a registration seam: these are
the vocabularies this repository writes, and the point of the table is that a
reader holding a column of uint64s can ask what they are — a list a package
had to opt into would leave exactly the ids nobody thought about unnameable.
Each registry mints under its own claimed tag value, so the id spaces are
disjoint by construction; the names are checked for collision by the
provider's test, since a duplicate would make the search order load-bearing.

### SD7 — The trail is read through a provider, not decoded in SQL

`keelson('runtime_events')` exposes the current run's history as flat rows —
time, kind, app, instance, run, a rendered detail, and which of the two tables
it came from. The consumer applet becomes a projection over it.

This is a performance decision with a measurement behind it, not a tidiness
one. The applet first did the extraction in SQL, and it was slow for a reason
that has nothing to do with ClickHouse: reading twelve kinds out of
`boxer.facts` needs the membership vocabulary, which in SQL means a large
statement, and `nanopass.Sequence` hands a statement between passes as **text**
— so it is re-parsed once per pass, with parse cost growing faster than
linearly. The benchmark this left behind measures the whole pre-execute stage
at about **34 parses** of one statement per Run. Measured on this buffer: the server
answered in **90 ms** while the client spent **2.4–3.9 s** compiling, of which
~8 of the 12 canonicalize sub-passes rewrote nothing at all. Restructuring the
SQL bought 2.3×; moving the extraction into Go bought the rest.

| | buffer | client-side compile | in-app, first Run |
| --- | --- | --- | --- |
| SQL extraction, as first written | 9.4 KB | 3.9 s warm | 7.8 s |
| SQL extraction, restructured | 7.3 KB | 2.4 s warm | 4.2 s |
| projection over `keelson('runtime_events')` | 1.4 KB | 0.6 s warm | **1.2 s** |

The provider is scoped to **this run** by construction — the process knows its
own run id, so nothing is passed in — which also removes the run-span
discovery the SQL had to do. Reading an *earlier* run is what that costs, and
it stays possible only as raw SQL on the default endpoint: an ADR-0094
introspection table takes no arguments, so there is nothing to point at
another run.

Two seams keep it honest. The read capability is `factsstore.RunEventReaderI`,
asserted off the store rather than added to `FactsStoreI` — every writer must
implement that interface, and a reader only the ClickHouse-backed store can
answer would force the in-memory store and every fake to carry a method they
cannot serve. And the app-state half reads through the provider's **own**
read-only store over the persist executor, so a scan never contends with the
writer's pending buffer.

The extraction inside the provider is hand-written array arithmetic rather
than the `LW_GET` surface AGENTS.md prefers, for the reason `runsessions.go`
gives beside it: the store ships SQL directly with no client-side expansion
pass, so that family is not available to it — and the server-side read-back
UDFs it expands into are not something the store may assume is installed.

### Milestones

- **M0 — The envelope.** ✓ `app.Msg.SenderInstance`, stamped by `inprocbus`
  from the key the client has held since ADR-0188 §SD1; `natsbus` unchanged.
- **M1 — The store stamps the run.** ✓ `chstore.Config.RunId` plus one
  `stampRun` helper every kind calls, wired from `runinfo` at the carousel
  and the log host. This alone turns "limit to this run" from a time range
  into a predicate for audit, grant, log and column widths.
- **M2 — Audit and grant carry the instance.** ✓ Audit from the recording
  client's own key, grant from `Msg.SenderInstance`.
- **M3 — Log rows carry the instance.** ✓ `logbridge` lifts the
  `instance_id` field windowhost already tags onto every per-window logger.
- **M4 — Query-run rows carry the instance.** ✓ Through the log_comment
  stamp, so it survives the round trip via `system.query_log`.
- **M5 — Column-width rows carry the instance.** ✓ Provenance only; the
  override stays keyed by app.
- **M6 — Persist state carries both.** ✓ Two sections, regenerated store,
  and the DDL migration recipe.
- **M6a — The runtime vocabulary resolves by name.** ✓ §SD6. Ordered with
  M6 because it is what lets the consumer in M7 spell what M0–M6 wrote.
- **M7 — The event-timeline applet lanes on the instance.** ✓ The consumer
  this was found from, laning `(app, instance)` exactly instead of falling
  back to an app-level lane.
- **M8 — The trail becomes a table.** ✓ §SD7: `keelson('runtime_events')`,
  and the applet reduced to a projection over it. 7.8 s → 1.2 s per Run.

### Deferred

- **`natsbus` attribution.** That transport carries no sender at all, so
  the instance is not the missing half. Trigger: envelope headers landing
  with the NATS swap (ADR-0026 §SD4).
- **`fsbroker` handle eviction per instance.** Unblocked by SD2, not taken:
  it changes when a handle dies, which is a lifecycle decision.
  Trigger: ADR-0188's own — a handle outliving the window that opened it in
  a way someone notices.
- **Backfilling historical rows.** Rows already written cannot learn their
  instance; nothing reconstructs it. They stay unattributed.
- **Renaming `runtimeLifecycleTileKey`.** Costs a renumber of a durable id
  for a spelling. Trigger: a vocabulary migration that is happening anyway.
- **Naming an out-of-tree vocabulary from SQL.** §SD6's list is declared, so
  an adopter's own registry is not in it and its ids stay unnameable. A seam
  would fix that; nothing asks for one yet. Trigger: an adopter reading their
  own facts rows through `LW_GET` by name.
- **Reading an earlier run's trail.** §SD7's table is this process's own
  history, and an introspection table takes no arguments (ADR-0094), so there
  is no way to ask it for another run. The raw-SQL route on the default
  endpoint still works and is what the applet did before. Trigger: parameterised
  introspection tables, or a second table keyed by run.
- **Provenance on a persist tombstone.** `Delete` appends a row carrying only
  the lifecycle flag — no app id today, and so no run or window either. It is
  pre-existing and orthogonal: the fix is for the generated `Delete` to write
  the component, which is a record-store question. Trigger: a reader that has
  to tell whose key a deletion was.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `app.Msg` (exported API, wire between transports) | `SenderInstance uint64`, additive | `inprocbus` stamp; every service reading `Msg.Sender` may now read the instance |
| `audit.AuditRecord` (exported API) | `InstanceKey`, additive | `inprocbus.Client.Request`; `factsstore.AsAuditSink` |
| `factsstore.AuditRow` / `GrantRow` / `LogRow` (exported API) | `InstanceKey`, additive | `chstore` writers; `InMemoryFactsStore`; `recentlogs` reader |
| `chstore.Config` (exported API) | `RunId`, additive | every `chstore` write; the carousel's store construction |
| `boxer.facts` rows (on-disk encoding) | more kinds carry `runtimeRun` and `runtimeLifecycleTileKey` | no DDL — both memberships already ride existing sections |
| `persiststore.State` (generated-code input) | `RunId`, `InstanceKey` | `schema.go` sections; `TestGeneratePersistStore` output; `PersistMembershipIds` |
| `boxer.persiststate` (CH DDL) | two sections' worth of columns, additive | `EnsureTable` on an existing deployment |
| `persist.StorageRef` (exported API) | `InstanceKey`, additive | `persist.Service.handleSet` / `handleDelete` |
| `queryrunfacts.Stamp` (wire, additive) | `instance`, an omitempty JSON key on log_comment | `play.Client.SetStampIdentity` (signature); `queryrunfacts.EncodeEntity` |
| `colwidth.Opts` (exported API) | `InstanceKey`, additive | play's resolver construction |
| `keelson('memberships')` (introspection table) | rows for three more vocabularies | `providers.MembershipLookup`, and every `LW_GET` that can now take a name |
| `keelson('runtime_events')` (introspection table) | added | `providers.RegisterRunEvents`; `introspecthost.Deps.PersistExec`; the applet that projects it |
| `factsstore.RunEventRow` / `RunEventFilter` / `RunEventReaderI` (exported API) | added, optional capability | `chstore.Store` implements it; consumers type-assert |
| `introspecthost.Deps` (exported API) | `PersistExec`, additive | the carousel's `selectPersistBackend`, which now returns the executor it opened |

## Alternatives

- **Infer the instance from the lifecycle intervals.** A consumer can join
  an app-attributed event to the window that was open when it happened —
  but only unambiguously when exactly one was, which is precisely the case
  the key exists to distinguish. It also puts the same guess in every
  consumer and makes the answer depend on how complete the lifecycle rows
  are.
- **Mint a `runtimeInstanceKey` membership.** Either two columns to join on
  for the same fact, or a renumber of ids that rows on disk carry.
- **A `RunId` field on every DTO.** Repeats one process-wide constant in
  eight structs and lets them disagree.
- **A per-window `chstore.Store`.** Would carry both halves implicitly, at
  the cost of one CH client per window and a batching lane per window —
  the opposite of the batching `WriteLogs` exists for.

## Consequences

### Positive

- "This run" becomes a predicate on every kind, not a time range that
  quietly includes a concurrent process.
- An event can be attributed to the window it came from, which is the grain
  the runtime already manages elsewhere — the id stack, the logger, the
  subscription table (ADR-0188 §SD4) all key on the instance.
- ADR-0188's envelope deferral is discharged, which also unblocks its
  fsbroker follow-up without this ADR taking it.

### Negative

- Two more memberships per row on the busiest kinds. Both are
  low-cardinality within a run and the columns are already there, but the
  audit and log kinds are the highest-volume rows the runtime writes.
- `boxer.persiststate` needs a DDL change on deployments that already have
  the table.
- A reader must now distinguish "unattributed" (zero) from "window zero",
  and nothing in the type says so.

### Neutral

- The membership named `runtimeLifecycleTileKey` now appears on kinds with
  no lifecycle in them. The name was already inaccurate for launch and
  workingset rows; this makes it conspicuous.
- Rows written before this lands stay as they are: readable, unattributed,
  and indistinguishable from a host-written row.

## Migration — Tier 1

- **Breaks.** Two things, and they are not the same kind.

  In Go, one signature: `Client.SetStampIdentity(runId, appId)` gains an
  instance argument, and `Client.stampIdentity` returns three values. Every
  other change is an added struct field whose zero value means what absence
  meant, so nothing else stops compiling. `app.Msg` gains a field and stays
  comparable-free; `queryrunfacts.Stamp` gains one and stays comparable, which
  `ParseStamp` relies on.

  On disk, **`boxer.persiststate` is a breaking migration**, not an additive
  one — a claim this section made wrongly before M6 was built.
  `PersistStore.VerifySchema` runs at open, compares the live column list to
  the generated one *positionally*, and refuses on any difference; the host
  then silently drops to the in-memory persist backend and forgets app state
  at every restart. `EnsureTable` cannot repair it: `CREATE TABLE IF NOT
  EXISTS` leaves an existing table alone. `boxer.facts` is untouched — both
  memberships already ride sections that exist there.
- **Path.** Milestones in order. M0 and M1 are independent of the rest and
  carry most of the value; M6 is the only one with a DDL step.
  [The persiststate provenance recipe](../migration/2026-08-persiststate-provenance.md)
  is that step: twenty `ADD COLUMN`s, which reproduce the generated layout
  exactly because both new sections are declared last and therefore append.
  Dropping the table is the shorter alternative where the state is
  expendable — but runtime-saved applets live in it.
- **Regeneration.** `go test -tags "$(cat tags)" -run TestGeneratePersistStore
  ./public/keelson/runtime/persist/persiststore/` for M6. No FFI boundary is
  involved. `boxer runtimecodegen all` is *not* needed: the facts schema is
  unchanged.
- **Old shape.** Kept indefinitely. There is no version marker on a facts
  row and no backfill; a row without the memberships is a row from before,
  and a reader says so rather than defaulting.

## Verification plan — Tier 1

- **Lane.** Default `go test`, against a live ClickHouse where the store
  needs one (those cases skip when it is unreachable):
  - `TestStore_StampsRunAndInstance_LiveCH` — one subtest per kind that
    gained something (audit, grant, log, column width), reading the run and
    the window back off the *physical lanes* rather than through the
    writer's own helpers, so the test cannot agree with itself.
    `TestStore_RowRunIdWinsOverTheStores_LiveCH` pins §SD3's precedence and
    `TestStore_UnstampedStoreWritesNoRun_LiveCH` pins what absence means.
  - `TestClient_EnvelopeCarriesSenderInstance` and
    `TestInst_AuditSink_RecordsTheRequestingWindow` — both use two clients
    for **one app id**, which is the only arrangement that can fail: with a
    single window, `Sender` already answers.
  - `TestSink_LiftsInstanceKeyToItsOwnColumn` (and its non-numeric sibling)
    for M3 — including that the field is lifted rather than copied.
  - `TestBakedIdsAreTheVocabularys` for M6: the assertion is that adding two
    columns minted nothing, so no id on disk moved.
  - `TestMembershipsCoversEveryFactsWritingVocabulary` and
    `TestMembershipIdsAreDisjointAcrossVocabularies` for §SD6.
  - `TestStore_ListRunEvents_LiveCH` and
    `TestStore_ListRunEvents_SelectsOneRun_LiveCH` for §SD7's flattening and
    its attribution rule, plus the provider's table/merge/registration tests.
    The merge one matters most: the two halves are read separately, and an
    unsorted result draws a wrong timeline that looks right.
- **What would fail.** A kind that stops stamping goes red in its
  round-trip; a renumbered membership goes red in the id pin; a vocabulary
  dropped from §SD6's list takes all of its names with it and goes red on
  its sample member.
- **Gap.** `natsbus` is not covered — it carries no sender to attribute.
  Rows written before this ADR are not covered and cannot be. Nothing checks
  that a *new* kind added later remembers to stamp: the round-trip is a table
  of kinds, and a new kind arrives without a row in it. The persist path's
  provenance is covered by the store round-trip and the generation test, not
  by an end-to-end write through the bus.

## Status

Proposed — awaiting review by the runtime code owners.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0026 — app runtime and capability subjects](0026-app-runtime-and-capability-subjects.md) — §SD6 defines the facts vocabulary this extends, §SD4 the NATS swap the deferral waits on.
- [ADR-0105 — keelson adopts generated record stores](0105-keelson-adopts-generated-record-stores.md) — D3a moved app state onto `boxer.persiststate`, the table §SD5 changes.
- [ADR-0135 — app launch requests](0135-app-launch-requests.md) — first reuse of `MembLifecycleTileKey` outside the lifecycle kind.
- [ADR-0148 — app workingsets](0148-app-workingsets.md) — the tile key as provenance beside an identity that excludes it.
- [ADR-0183 — leeway component consumer simplification](0183-leeway-component-consumer-simplification.md) — D0's explicit ordinals, why a membership is not renamed.
- [ADR-0188 — app-instance effect tracking](0188-app-instance-effect-tracking.md) — §SD1 put the key on the bus client; its deferred envelope dimension is §SD2 here.
