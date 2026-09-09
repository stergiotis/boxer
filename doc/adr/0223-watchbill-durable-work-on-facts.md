---
type: adr
status: accepted
date: 2026-09-09
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-09
---

# ADR-0223: watchbill — durable work as facts, claimed by writing

## Context

Three coordination mechanisms exist or are recorded in this repository, and
none of them is a durable queue.

**The bus is ephemeral by decision.** [ADR-0026](./0026-app-runtime-and-capability-subjects.md)
§SD4 makes NATS an external server boxer connects to; [ADR-0090](./0090-sysmetrics-pubsub-data-plane.md)
§SD4 takes NATS *core* only, no JetStream and no KV, and the airgapped unit
of ADR-0026's Update 2026-07-20 is built that way. The broker persists
nothing; where history is wanted, a tee lands the same facts-shaped payload
in ClickHouse and history is a query ([ADR-0184](./0184-sysmetrics-persistence-tee.md)).

**Keelson tasks coordinate and audit; they do not resume.** [ADR-0038](./0038-keelson-background-task-primitive.md)
stopped deliberately at coordination: apps own the goroutine, the
supervisor records a task that stops emitting as `abandoned` and never
restarts it. Its Context named the restart-worthy workloads — a
minutes-long export, an import proportional to a tree, a Kafka catch-up —
and its O5 rejection left open what happens to them across a restart.
The answer in the tree is that they die.

**The recorded answer was never built, and the rule has since moved under
it.** ADR-0038's Update of 2026-06-22 records River (open-core) on embedded
SQLite as the optional durable executor beneath `HandleI`, with a Postgres
driver as the server tier. Nothing of it exists: `riverqueue` is in no
`go.mod` (2026-09-09). Five weeks later [ADR-0148](./0148-app-workingsets.md)'s
Update of 2026-07-30 made data-centricity a rule rather than a preference —
runtime and app state lives in the runtime's modelled fact substrate and
is modelled there — and [ADR-0105](./0105-keelson-adopts-generated-record-stores.md)
D3a moved persist state onto a generated leeway table on ClickHouse for
exactly that reason. A River job file is a second durable substrate beside
ClickHouse, holding state as opaque JSON, which is the shape the rule
retired `runtime.persist` for.

Two properties of the substrate bound what "queue" can mean on it, and
both were measured rather than assumed
([the background note](../adr-background-work/watchbill-clickhouse-primitives.md),
2026-09-09, ClickHouse 26.8). ClickHouse has no row locks and no `SKIP
LOCKED`, but its lightweight `UPDATE` serialises concurrent updates whose
read set meets another's write set: twenty parallel conditional updates of
one row yielded exactly one winner in every round, with no settle and no
lost increment. A claim can therefore be a conditional update, on the
condition that `async_insert` — on by default at 26.8 — never sits in the
claim path, and that the table carries the two block-position columns the
feature needs. Many small inserts still make parts, and every update leaves
a patch part until a merge folds it. The primitive is therefore for work
measured in seconds to hours, tens of jobs a minute at most — the
granularity rule ADR-0038 applied to the frame loop, applied here to the
store.

A deployment is a **cell**: one ClickHouse server, any number of boxer
processes against it. Keeper — and with it `KeeperMap`, whose strict-mode
insert is the other compare-and-set the note measured — is a daemon a
single-node cell does not run and a lane `clickhouse local` cannot
exercise. The design uses nothing a single server does not provide.

## Design space (QOC)

**Question.** Where does restart-worthy work wait, such that it survives
the process, is distributed safely across workers, and is modelled as facts
on the one substrate the house keeps?

**Options.**

- **O1 — River on embedded SQLite,** as recorded in ADR-0038's Update.
- **O2 — River on Postgres,** the server tier that Update named.
- **O3 — NATS JetStream work-queue streams:** durable consumers, acks,
  redelivery, on the external server.
- **O4 — A job kind on the facts substrate, claimed by writing** — a
  generated leeway table in the house layout, events as rows, a claim as a
  `running` row, a lease against the runtime heartbeat, a worker library,
  the bus as a doorbell. (chosen)
- **O5 — Each consumer its own state machine** on its own kind — the
  shape shadow-boxer's first draft took.

**Criteria.**

- **C1 — Modelled as facts, one substrate.** The ADR-0148 rule: typed
  rows on ClickHouse, queryable from play, no second store.
- **C2 — Survives the process.** A job outlives a crash and a deploy; a
  dead worker's job is retried.
- **C3 — Safe across workers without locks.** What keeps two workers from
  one job, and whether the winner is told.
- **C4 — No new substrate or daemon.** No file database, no Postgres, no
  JetStream provisioning beside the server ADR-0026 already asks for.
- **C5 — Appliance fit.** [ADR-0206](./0206-gokrazy-appliance-image.md):
  ClickHouse external, `/perm` unformatted, Go-only supervision.
- **C6 — Consumer cost,** for the first consumer and the fourth.
- **C7 — Honest throughput bound,** stated rather than discovered.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | −− | −− | −− | ++ | +  |
| C2 | ++ | ++ | ++ | ++ | +  |
| C3 | ++ | ++ | ++ | ++ | −  |
| C4 | −  | −− | −  | ++ | ++ |
| C5 | −− | −− | −  | ++ | ++ |
| C6 | +  | +  | −  | ++ | −− |
| C7 | +  | ++ | ++ | −  | −  |

O4. O1 and O2 buy transactional claims at the price of a second substrate
holding opaque state, and O1 additionally needs a writable local file the
appliance does not have. O3 keeps state in the broker, which ADR-0090
refused for metrics and which fails C1 the same way; it also asks every
worker host for JetStream provisioning. O5 is the status quo the first
consumer already showed the cost of. O4 loses only on C7, and the bound is
written down rather than discovered.

## Decision

We will build **watchbill**, `public/keelson/runtime/watchbill`: a
generated record store holding one row per job, updated in place by
conditional lightweight `UPDATE`, and one row per transition beside it,
both leeway rows in the house layout; a worker library that claims a job
by one conditional update and reads its own mark back, leases it against
the runtime heartbeat and retries it on a declared policy; two
introspection tables; and two bus subjects that carry an id and nothing
else. It stands in the place ADR-0038's Update of 2026-06-22 gave
River, and withdraws that Update: **watchbill is durable distribution, the
bus is ephemeral fan-out**, the split the Update drew, on one substrate.

A watch bill is the roster that assigns each hand a duty on each watch; a
worker stands a watch and takes the duties listed for it. Recorded in
README § House names with this ADR.

### Subsidiary design decisions

- **SD1 — A store-owned table in the house layout.** Not a kind on
  `boxer.facts`: `watchbillstore` is a generated record store on the
  ADR-0105 D3a pattern: its own table, `<db>.watchbill`, with a `Layout`
  carrying the database as `ladingschema.Layout` does, so a consuming
  repository places it beside its own facts (shadow-boxer:
  `shadowboxer.watchbill`). A writer on the shared facts table cannot
  control its indexes, its retention or its table settings (ADR-0184,
  Consequences), and this table needs all three: an immutable key so its
  rows can be updated in place, `enable_block_number_column` and
  `enable_block_offset_column` for the lightweight update, and its own
  expiry. Two tables, two kinds:

  | Table | Kind | Carries | Frame |
  | --- | --- | --- | --- |
  | `watchbill` | `watchbillJob` | kind, subject, queue, priority, max attempts, backoff class, timeout, owner app id, requester run id — and the mutable state: state, attempt, run-after, worker run id, finished-at, last error | one row per job, updated in place |
  | `watchbill_event` | `watchbillEvent` | job id, state, attempt, worker run id, error chain, note | one row per transition, append-only |

  The job table is keyed `(kind, id)` and nothing mutable is in the key:
  a lightweight update cannot touch a key column. The queue read is a
  filter over it, not an order over the key, and the table is small
  enough — open work, plus finished work until its expiry — that a scan
  per poll is the cost.

  `subject` is the identity of the work, not its description: the entity
  id and natural key of the row in the consumer's own table that says what
  to do — a download row, an export row — so the job never duplicates the
  domain and the domain row stays the single source. A consumer with no
  row to point at carries `args` as facts-CBOR of a vocabulary kind with a
  generated codec, the [ADR-0135](./0135-app-launch-requests.md) §SD2 rule
  that keeps a free-form payload unrepresentable; the doc comment says
  which to prefer. Kind names and ordinals are declared in the vdd registry
  ([ADR-0183](./0183-leeway-component-consumer-simplification.md) D0).

- **SD2 — The job row is the state; a transition is also an event.**
  States: `queued`, `running`, `succeeded`, `failed` (an attempt
  ended in error and attempts remain — the row carries the next
  `runAfter`), `discarded` (attempts exhausted), `cancel` (requested by
  anyone), `cancelled` (acknowledged by the worker), `abandoned` (the
  worker's run went silent). A transition is one conditional update of the
  job row followed by one event row; the update carries the guard (`WHERE
  state = 'queued'`, `WHERE state = 'running' AND workerRun = …`) so a
  stale actor changes nothing, and the event is written only after the
  update was read back as won. The queue is `SELECT … WHERE state =
  'queued' AND runAfter <= now() ORDER BY priority, runAfter`; a client
  that wants to enqueue inserts a job row, and one that wants to cancel,
  retry or inspect updates or reads rows and nothing else. A retry is the
  row set back to `queued` with a later `runAfter`, by the worker under
  the policy or by a person. Progress is not watchbill's: a running job is
  a keelson task and its progress rides `task.<id>.progress` as today; a
  consumer that wants throughput as data samples it on its own kind.

- **SD3 — Claim by one conditional update, and read the mark back.** A
  worker takes a `queued` row with `UPDATE watchbill SET state =
  'running', workerRun = <run>, attempt = attempt + 1 WHERE id = … AND
  state = 'queued'`, then reads the row: it holds the job exactly when
  `workerRun` is its own. The server serialises competing updates of one
  row, so of N workers one wins and N−1 find another run's mark and move
  on; there is no settle, no tie-break and no inert row. The claim
  statement is hand-written SQL over the store's generated column names,
  pinned by a golden, because the record store generates no update verb
  and this ADR does not ask it to. Delivery is at-least-once and handlers
  are idempotent, as River's are. What makes this sound is a property of
  one server: the cell model above. A design that spread one watchbill
  across several servers would need a claim of another shape, and is out
  of scope.

  Two rules keep the property, both measured rather than read (the
  background note, 2026-09-09, later): every update carries
  `update_parallel_mode = 'sync'` explicitly rather than trusting the
  default's dependency analysis, and **no heavyweight mutation runs on the
  job table** — a mutation rewriting a part under a patch was the one
  condition found to let two claims both win, with the attempt counter
  showing the second evaluated against a snapshot from before the first.
  The expiry therefore deletes as a lightweight update, and the only
  `ALTER` is a `MODIFY SETTING` at provisioning.

- **SD4 — The lease is the runtime heartbeat.** A `running` row names its
  worker's run id. Liveness is not renewed per job: the runtime already
  writes a `HeartbeatRow` for the run every 30 s (ADR-0026, heartbeat), and
  a `running` job whose run id's latest heartbeat is older than
  `AbandonAfter` (default 90 s) is abandoned. Every worker sweeps for those
  at its poll with one conditional update over the join — `WHERE state =
  'running' AND workerRun IN (<runs with no fresh heartbeat>)` — that sets
  `abandoned` or, where attempts remain, `queued` with a `runAfter`, and
  writes the events for the rows it changed; the same sweep deletes
  finished rows past their expiry with a `DELETE` in lightweight-update
  mode — never the default heavyweight mutation, for §SD3's reason — since
  a `TTL` on a column the update rewrites is not a contract worth relying
  on. A job that exceeds its own `timeout` in a live worker is cancelled
  by that worker. The join reaches `boxer.facts` from the layout's
  database, which is one server; where the facts store fell back to
  memory the heartbeat is invisible and watchbill is unbound rather than
  half-bound, the posture `runtime.persist` takes.

- **SD5 — The worker is a library with handlers per kind.**

  ```go
  type HandlerI interface {
      Kind() string
      RunE(ctx context.Context, job Job, h task.HandleI) (err error)
  }
  ```

  `Worker` polls the job table every `Poll` (default 5 s) or on
  `watchbill.wake`, takes up to `MaxWorkers` per kind, claims (§SD3), spawns
  each run as a keelson task (kind = the job kind, cancellable, the job id
  on the task) when a bus exists and as a goroutine otherwise, and writes
  `watchbill.changed` with the job id after every row it flushes. A
  `cancel` state is honoured within a poll — the worker re-reads its
  running rows — and the task's own cancel subject writes that state,
  so a click in the task monitor and a `cancel` from another machine are
  the same event. The row is flushed before the message, every time.

- **SD6 — Policy is on the job row.** `maxAttempts`, a backoff class
  (`none`, `linear`, `exponential`, each with a base on the row), `timeout`
  and `runAfter`. Scheduling is `runAfter` in the future. Periodic and
  DAG-shaped work are out of scope, as the withdrawn Update also kept them
  out; a need for either is its own ADR.

- **SD7 — Observability is two tables and the task stream.**
  `keelson('watchbill')` serves the job table and
  `keelson('watchbill_event')` the transitions
  ([ADR-0094](./0094-keelson-introspection-tables.md)); an applet book
  over it — in flight, failed with their chains, attempts by kind,
  abandonments by run — is the first UI, on the ADR-0132 rule that a book
  comes before an app. Live progress and cancel are the task monitor's
  already.

- **SD8 — Host wiring.** `hostboot.Services` gains `Watchbill`: when on, the
  host opens the store in the facts store's database, starts a `Worker`
  with the handlers registered in the process, and mints it a bus client
  for the two subjects and the task producer caps. A headless binary runs
  the same `Worker` from a CLI verb with no bus. Handlers register through
  a process-wide registry the way apps do, so a binary that links a
  consumer's package has its handler.

- **SD9 — What this withdraws and what it leaves.** ADR-0038's Update of
  2026-06-22 is withdrawn by a dated entry there pointing here; nothing
  else in ADR-0038 moves — the task protocol, the estimator, the
  supervisor's `abandoned` for ephemeral tasks all stand, and a watchbill
  run is one more task to them. NATS core stays the transport; nothing
  here needs JetStream, and the case that would — ordered replay with acks
  where a query is too slow — has no consumer in the tree.

### Milestones

- **M1 — Store.** `watchbillstore`: the two kinds in the vdd registry, the
  generated stores, `Layout`, the table settings in the DDL tail, the
  claim and sweep statements with their golden.
- **M2 — Worker.** `watchbill`: `HandlerI`, the registry, `Worker`, the
  claim, the rescuer sweep, the retry policy; the default lane over an
  in-memory executor, the integration lane over clickhouse-local with
  twenty workers racing one job.
- **M3 — Host.** `hostboot.Services.Watchbill`, the subjects, the task
  bridge, the CLI verb.
- **M4 — First consumer.** shadow-boxer's tender (its ADR-0006) runs its
  downloads as watchbill jobs of kind `tender.download`; its `Download` row
  is the subject.
- **M5 — Surface.** `keelson('watchbill')` and the applet book.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| ClickHouse DDL | new tables `<db>.watchbill` (block-position columns enabled) and `<db>.watchbill_event`, store-owned | `watchbillstore`'s generated DDL; provisioned at host boot and by the CLI verb |
| ClickHouse version floor | lightweight `UPDATE` with `enable_lightweight_update`, tier Beta at 26.8 | the pinned server and `clickhouse local` of the integration lane; a server that lacks it refuses the claim at first use, loudly |
| vdd vocabulary registry | kinds `watchbillJob`, `watchbillEvent` and their memberships, ordinals declared | the registry's golden; every binary that links the store |
| `hostboot.Options.Services` | gains `Watchbill` | `AllServices()`; the carousel and every hostboot adopter that names its services |
| Bus subjects | added `watchbill.wake`, `watchbill.changed` | the `Caps` of every enqueuing app's manifest |
| Environment-variable registry | added `KEELSON_WATCHBILL_POLL`, `_ABANDON_AFTER`, `_KEEP` | `env gen-docs` |
| `keelson()` introspection | gains `watchbill` and `watchbill_event` | the applet book; the introspection provider list |
| ADR-0038 | Update 2026-06-22 withdrawn by a dated entry | README § House names gains `watchbill` |

## Alternatives

- **River on SQLite (O1) and on Postgres (O2).** The matrix: a second
  substrate with opaque state, against the rule that arrived after the
  Update; and on the appliance, no writable file to keep it in.
- **JetStream work queues (O3).** State in the broker, which ADR-0090
  refused for metrics and which fails the same rule; provisioning on every
  worker host; and no query over it without a tee, at which point the tee
  is the table this ADR builds.
- **A `job` kind on `boxer.facts`.** No `ORDER BY` for the queue's read, no
  `TTL` on finished work, and every enqueue a row on the audit table's
  index (ADR-0184's consequence); persist state left that table for the
  same reasons (ADR-0105 D3a).
- **A per-job heartbeat row.** Doubles the event rows to answer a question
  the runtime heartbeat already answers per run.
- **A broker of our own on ClickHouse,** WarpStream-shaped. Kafka's
  contract is an ordered, offset-addressed log with acks; ClickHouse can
  order but cannot sequence, so the metadata service WarpStream adds over
  object storage would have to be written here, for a slower NATS. The
  consumer that was away does not replay; it reads the view.
- **`KeeperMap` in strict mode as the claim.** Measured as a true
  compare-and-set — the winner is told by the response — and rejected for
  the cell: it needs Keeper, which a single-node server does not run,
  `keeper_map_path_prefix` on the server, and a lane `clickhouse local`
  cannot exercise; and its rows live in Keeper's memory, not in the
  table.
- **Claim by insert, settle, and earliest-row tie-break** — the first
  draft of this ADR. Correct on a store with no compare-and-set, and this
  store has one; the settle and the inert losing rows go with it.
- **A `ReplacingMergeTree` current-state fed from events, read with
  `FINAL`.** Measured and fine as a projection, but a claim needs a row it
  can guard, and a table updated in place is that row with no second
  copy; the event table keeps the history.
- **The NATS table engine as the doorbell.** Measured: a view into a NATS
  table publishes every row in order — and fails the row's insert while
  the NATS server is down, coupling the store's durability to the bus.
  The doorbell stays the client's; the engine is an optional second bell
  where the bus is known up, never in the insert path.
- **Exactly-once claims via a lock table.** There is no lock to take; a
  lock row is a claim row under another name.
- **Payload on the job row as opaque bytes.** Would recreate persist's
  `[]byte` shape one level up; the subject rule keeps the description in
  the consumer's own kind.

## Consequences

### Positive

- Restart-worthy work has one home, queryable from play, and the fourth
  consumer writes a handler and a row.
- A job's whole life — asked, claimed, retried, abandoned, done — is rows
  on one table with the requester's app and run ids, joinable to launches,
  grants and audit on the same columns.
- Nothing new runs: no daemon, no file, no JetStream; the appliance needs
  only its external ClickHouse.
- The bus stays what ADR-0090 made it, and the task primitive stays what
  ADR-0038 made it; a watchbill run is an ordinary task to every observer.

### Negative

- A claim costs an update and a read-back, and a lost claim costs the
  same; at tens of jobs a minute this is nothing, at hundreds a second it
  is the wrong tool, and the ADR says so rather than growing toward it.
- Every transition is an insert and a patch part; a job with many retries
  is many parts until they merge, and the sweep's `DELETE` bounds the
  table, not the churn.
- The claim rests on a Beta-tier feature of one server version line. It is
  on by default at 26.8 and measured there; a server line that withdraws
  it takes the claim with it, and the version floor is a surface.
- The job table admits no heavyweight mutation, ever: an operator's
  `ALTER … UPDATE` or `DELETE` in its default mode on that table can make
  two claims win. The rule is stated in the store and enforced by nothing
  but the store issuing no such statement.
- One server per watchbill, by construction.
- Without a reachable ClickHouse there is no queue at all — the posture of
  every durable thing in the house since ADR-0148's Update.
- The heartbeat join makes liveness only as fresh as the heartbeat: a dead
  worker's job waits `AbandonAfter` before anyone else takes it.

### Neutral

- At-least-once delivery is a contract on handlers, as it was on River
  workers.
- The name is open at review.
- Cross-host workers cost nothing further: the table is shared by every
  process on the server, and the doorbell is NATS.

## Migration — Tier 1

Nothing to migrate: the withdrawn River path was never built, the task
protocol is unchanged, and the new table is provisioned on first use.
shadow-boxer's tender adopts at its own M4 and removes its private state
kind in the same commit.

## Verification plan — Tier 1

- **Lane: default `go test`.** `watchbill` over an in-memory executor: the
  queue read orders by priority then run-after; a `cancel` honoured within
  a poll; a `failed` attempt re-queued with the policy's `runAfter` and a
  `discarded` after the last; the event written only after the update was
  read back as won; the row flushed before `watchbill.changed`; the claim
  and sweep statements against their golden.
- **Lane: `//go:build integration`.** Under `clickhouse local`: one job's
  whole life — a second claim inert, a transition guarded on the wrong run
  inert, events in order, expiry at and past the cutoff. Against the
  server the `CLICKHOUSE_*` variables name, skipped when it is not there:
  twenty workers race one job and exactly one handler runs, repeated; a
  run that died mid-job is swept and its job re-queued. The race needs the
  server because `clickhouse local` runs one process per statement and so
  cannot race anything with itself; the serialising property is the
  server's. `keelson('watchbill')` and `keelson('watchbill_event')` answer;
  every applet chapter parses.
- **What would fail.** Two handlers running one attempt; a message
  published before its row; a `running` row with a stale run id surviving
  two sweeps; a `queued` row read out of priority order; a claim against a
  table without the block-position columns.
- **Gap.** The race is measured at twenty on one machine; the serialising
  property is the server's, not this repository's, and a server line that
  changes it is caught by the lane, not by the design. Throughput past the
  stated bound is not measured, by design.

## Status

Accepted 2026-09-09. The name stands. `args` on the job row stays, with
the subject preferred, as §SD1 records.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0038](./0038-keelson-background-task-primitive.md) — the task primitive; the Update of 2026-06-22 this ADR withdraws.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — §SD4 external NATS; the runtime heartbeat; Update 2026-07-20, the airgapped unit without JetStream.
- [ADR-0090](./0090-sysmetrics-pubsub-data-plane.md) — NATS core only, persistence is the tee's.
- [ADR-0184](./0184-sysmetrics-persistence-tee.md) — history as a query; what a writer on the shared table cannot control.
- [ADR-0148](./0148-app-workingsets.md) Update 2026-07-30 — the data-centricity invariant.
- [ADR-0105](./0105-keelson-adopts-generated-record-stores.md) D3a — a store-owned generated table for runtime state.
- [ADR-0135](./0135-app-launch-requests.md) §SD2 — a payload is a vocabulary kind with a codec or it is not representable.
- [ADR-0094](./0094-keelson-introspection-tables.md) — `keelson()` tables.
- [ADR-0206](./0206-gokrazy-appliance-image.md) — the appliance's constraints.
- [ClickHouse primitives for watchbill](../adr-background-work/watchbill-clickhouse-primitives.md) — the measurements behind §SD3 and the rejected claims (2026-09-09, 26.8).
- River — <https://github.com/riverqueue/river> — the contract this ADR keeps (at-least-once, idempotent handlers, policy on the row) on another substrate.
- shadow-boxer ADR-0006 (proposed) — the first consumer.
