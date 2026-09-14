---
type: adr
status: accepted
date: 2026-09-14
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-14
---

# ADR-0234: watchbill as a capability — the client protocol, queues, and what a worker says about itself

## Context

[ADR-0223](./0223-watchbill-durable-work-on-facts.md) built watchbill as
durable work on the facts substrate: a job table claimed by conditional
update, a worker library, two doorbell subjects, two introspection tables
and a CLI group. It shipped every milestone, and it stopped one step short
of the runtime's own definition of a capability.

[ADR-0026](./0026-app-runtime-and-capability-subjects.md) makes a
capability a set of subject filters an app declares in its manifest and
the host mints a bus client against. Watchbill had the frame of one —
a service id, a `Services` flag, a `ClientCaps()` an app could declare —
and no verb behind it: the two subjects carry a job id and nothing else,
and every call that changes a row (`Enqueue`, `RequestCancel`, `Retry`)
takes a `StoreI`, a handle an app in the window host cannot reach through
its mount context. The CLI was the only client. Every other runtime
service — persist, the window host's open, the capability broker, the task
supervisor's in-flight list — is a typed request/reply pair with generated
codecs; watchbill was not.

Four smaller gaps sat beside that one, each a place where the tree
disagreed with itself:

- **A headless worker's jobs were taken from it.** The CLI verb minted a
  run id and wrote no heartbeat, so a host worker on the same cell found
  no run events for it, called it dead after `AbandonAfter`, and re-queued
  jobs the CLI handler was still executing. At-least-once made this
  correct and a mixed deployment made it silent double work.
- **The queue column meant nothing.** Written on every row, defaulted to
  `default`, read by no query. A consumer could set it and nothing would
  change.
- **Nobody could say who serves a kind.** Handler registration is
  in-process memory. A job of a kind no running binary links sits
  `queued` forever, and no table shows it.
- **The read side was a window, not a query.** `ListJobs` took a limit
  and no filter; a management surface has to ask by state, kind, queue
  and owner.

ADR-0223 named River as the contract it keeps on another substrate. This
ADR closes the gaps above **without widening past River's client surface**:
a verb is added only where River has the same verb, and the long tail River
carries — unique jobs, periodic jobs, snooze, middleware, workflows —
stays out, as ADR-0223 §SD6 already said of periodic and DAG-shaped work.

## Design space (QOC)

**Question.** How does an app reach the queue, and what does the worker
owe the cell about itself, such that watchbill is a capability in
ADR-0026's sense and stays mappable to River?

**Options.**

- **O1 — A store handle on the mount context.** Give every app the
  `StoreI` and let it call the Go verbs.
- **O2 — Request/reply on the bus, served by the worker** (chosen): one
  request DTO and one reply DTO with generated codecs, five verbs on
  `watchbill.job.<op>`, answered by the process that holds the store.
- **O3 — Row actions inside sqlapplet.** A verbs block in applet
  frontmatter over the existing book.
- **O4 — Enqueue by direct insert from the app.** Apps write the row
  with their own executor and ring the doorbell.

**Criteria.**

- **C1 — A capability in the ADR-0026 sense.** Declared in the manifest,
  minted by the host, audited by the bus.
- **C2 — Reaches a worker in another process.** The verb crosses the bus,
  so a NATS deployment serves it unchanged.
- **C3 — Attribution from the envelope.** The owner app is `Msg.Sender`,
  not a payload field a caller can spoof.
- **C4 — Mappable to River.** Each verb has a River counterpart.
- **C5 — No second write path onto the table.** One process writes the
  table's rows: the worker.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | ++ | −  | −− |
| C2 | −− | ++ | −  | −  |
| C3 | −  | ++ | −  | −− |
| C4 | +  | ++ | −  | +  |
| C5 | −  | ++ | +  | −− |

O2. O1 hands out the store and with it a write path the bus never sees.
O3 bends the applet model, which is documents over read-only SQL. O4 is
what ADR-0223 §SD2 said a client could do, and it is the shape that fails
attribution: a row inserted by an app names whatever owner the app wrote.

## Decision

Watchbill becomes a capability an app declares and exercises: the worker
serves a request/reply protocol on the bus, and everything an app can do
to the queue goes through it. Beside it: a headless worker is a run like
the host's, a worker drains named queues, a worker reports itself as an
introspection table, and the list read takes a filter.

### Subsidiary design decisions

- **SD1 — The client protocol is five verbs on `watchbill.job.<op>`.**
  The verbs are River's client verbs and nothing more:

  | Subject | River | Does |
  | --- | --- | --- |
  | `watchbill.job.enqueue` | `Client.Insert` | writes a queued row, wakes the worker, announces the row; the owner app is `Msg.Sender`, the requester run the worker's |
  | `watchbill.job.cancel` | `JobCancel` | a queued row is cancelled now; a running one is marked and honoured within the wake the service rings |
  | `watchbill.job.retry` | `JobRetry` | a final row goes back to queued with attempts reset |
  | `watchbill.job.get` | `JobGet` | one row |
  | `watchbill.job.list` | `JobList` | rows by state and kind, newest request first, bounded |

  The payload is `watchbillrequest.WatchbillRequest`, the reply
  `watchbillreply.WatchbillReply`, both generated on the ADR-0135 §SD2
  path from the `wbReq…` / `wbJob…` memberships in vdd. The op in the
  subject and in the payload must agree, and a refusal is a reply with
  `Ok` false and a reason, never a drop. Cancel and retry record the
  sender and its note on the event row. Args bytes travel in but not
  back: the reply reports the kind they claim, and the consumer's own row
  is where the work is described (ADR-0223 §SD1). `WorkerCaps` gains the
  subjects and the inbox; `ClientCaps` is what an app declares to hold
  the capability. `watchbill.Client` is the typed face of the protocol,
  and `WithArgs` / `ArgsOf` keep args a generated DTO by construction —
  the kind is read from the codec, never typed by the caller.

- **SD2 — The list read takes a filter.** `StoreI.List(ctx, ListFilter,
  limit)`: any of the states, any of the kinds, any of the queues, one
  owner app; every empty field matches. It is the read side of River's
  `JobList`, and it is bounded — a richer question is a query over
  `keelson('watchbill')`.

- **SD3 — A worker drains named queues.** `Config.Queues` filters the
  queue read; nil is every queue, which is what every worker did before
  the column meant anything, so nothing already running changes. River's
  per-queue concurrency is not adopted: concurrency stays per kind
  (ADR-0223 §SD5), and a need for a per-queue cap is a dated entry here,
  not a silent widening.

- **SD4 — The doorbell rides with the row.** `Submit` is `Enqueue` then
  `Wake` for a store holder; a bus client's enqueue is answered by the
  worker doing both. `Enqueue` and `Wake` stay for the caller that has a
  reason to separate them, and the doc comment says which to prefer.

- **SD5 — A worker says what it drains.** `Worker.Status()` and
  `keelson('watchbill_worker')`: run id, kinds, queues, concurrency, the
  ids held, last tick, whether it serves the protocol and whether it
  sweeps. One row, this process only. The cross-process answer — which
  runs on the cell serve which kinds, River's `river_client` table — is a
  presence fact on the facts store and is **deferred**: it is the piece
  the first management surface will want, and it needs a kind, a codec
  and a retention rule of its own, which this ADR does not gate on.

- **SD6 — A headless worker is a run like the host's.** The CLI verb
  initialises `runinfo`, writes its runtime-start row and heartbeats to
  the facts store on the same server, and reads heartbeats back, so it
  sweeps what others left and is not itself swept. Where the facts store
  is unreachable it stands without either and says so at start. This is
  River's client heartbeat, on the row the runtime already writes
  (ADR-0223 §SD4).

- **SD7 — What stays out.** Unique jobs, periodic jobs, snooze,
  middleware and hooks, per-queue concurrency, workflows and sequences.
  Each is a River feature with a name; each arrives, if it does, as its
  own ADR with that name in it.

### Milestones

- **M1 — Vocabulary and codecs.** The `wbReq…` / `wbJob…` memberships,
  the two generated DTOs with their goldens.
- **M2 — Store and worker.** The list filter and the queue filter; the
  service on the worker; the typed client; the args helpers; the status
  snapshot and the third introspection table.
- **M3 — Host and CLI.** hostboot passes the worker to the introspection
  host; the CLI verb stands a run with heartbeats, drains named queues,
  lists by filter, and carries a note on cancel and retry.
- **M4 — Surface.** A `watchbill-workers` chapter in the applet book; a
  how-to.
- **M5 — First app client.** Deferred with the management app: the first
  in-tree app to declare `ClientCaps` is the one that manages the queue,
  and it is its own design (ADR-0132's rule, the book already exists).

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| vdd vocabulary registry | memberships `wbReqOp` … `wbJobTimeoutMs`, ordinals 156–191 | the assignments golden |
| Bus subjects | added `watchbill.job.*` request/reply, served by `runtime.watchbill` | `WorkerCaps`, `ClientCaps`; the `Caps` of every app that uses the queue |
| Codec kinds | `watchbillRequest`, `watchbillReply` | kindcheck registry; the codec goldens |
| `watchbill.StoreI` | `Queue` gains queues; `List` added | `SqlStore`, `MemStore`, every implementer |
| `watchbill.RequestCancel` / `Retry` | gain a note | the CLI verbs |
| `watchbill.Config` | gains `Queues` | the CLI's `--queue` |
| `keelson()` introspection | gains `watchbill_worker` | `introspecthost.Deps.WatchbillWorker`; the applet book |
| `watchbill run` (CLI) | writes a runtime-start row and heartbeats; `--queue`; `list` filters; `--note` | the facts table on the server the CLI targets |

## Alternatives

- **A store handle on the mount context (O1).** A write path the bus
  never sees, and one that cannot reach a worker in another process.
- **Row actions in sqlapplet (O3).** The applet model is documents over
  read-only SQL; a verbs block is a second application model inside it.
- **Enqueue by direct insert (O4).** ADR-0223 §SD2 allowed it; it fails
  attribution, and every consumer would carry a copy of the row's
  defaults.
- **One subject per verb with its own DTO.** Five codecs for five verbs
  that share every field; one request DTO with an op is the persist
  service's shape.
- **Attempt-level or per-run heartbeats for the CLI.** ADR-0223 rejected
  a per-job heartbeat; the run heartbeat the host writes is the one the
  sweep already reads, so the CLI writes that.
- **Per-queue concurrency now.** River has it; the first consumer has
  one queue. Named in SD3 and SD7 so it is added under its name.

## Consequences

### Positive

- An app declares `ClientCaps` and has the queue: enqueue, cancel,
  retry, get, list, each audited by the bus like every other request.
- A cancel from the task monitor, a cancel from the CLI and a cancel from
  an app are the same row transition; the sender is on the event.
- A headless worker and a host worker share a cell without taking each
  other's jobs.
- A queue name does what it says.

### Negative

- The worker is now also a service: a request is answered by the process
  that holds the store, so a request on a cell with no running worker
  times out rather than being refused. The reply says nothing about
  which worker answered.
- The cross-process presence question is still open (SD5); a job of a
  kind no process serves is visible only as a row that never leaves
  `queued`.
- Two more codec kinds and thirty-six more memberships in a vocabulary
  that is already the tree's largest.

### Neutral

- Nothing on the job table changed: no column, no statement, no setting.
  The claim is ADR-0223's claim.
- River's verbs map one to one; River's long tail does not exist here,
  and the ADR says which parts by name.

## Migration — Tier 1

The `StoreI` signature change reaches two implementations, both in the
package. `RequestCancel` and `Retry` gain a parameter; their callers are
the CLI and the tests. A worker with no `Queues` drains as before. No row
changes shape.

## Verification plan — Tier 1

- **Lane: default `go test`.** Over an in-proc bus and the memory store:
  a client with `ClientCaps` enqueues and the worker claims within the
  wake, with the sender as owner; a cancel of a queued job lands at once,
  of a running job within the wake, with the sender in the event note; a
  retry re-queues a final job; get and list answer with the filter
  honoured; a client without caps is refused by the bus; an op that
  disagrees with its subject, and an unknown backoff class, are refused
  with a reason. A worker with queues named takes only their jobs. Args
  round-trip through the codec kind and a mismatched claim is refused
  before any decode. The list predicate and the queue read are pinned in
  the store's golden. The worker table answers with one row and, with no
  worker, none.
- **Lane: `//go:build integration`.** Unchanged: the twenty-worker race
  and the store round trip under clickhouse local.
- **What would fail.** A verb reaching the store without a bus request; a
  request answered before its row is flushed; a queued job of another
  queue taken by a worker that named its queues; an args blob decoded
  under a kind it does not claim.
- **Gap.** The CLI's heartbeat is not exercised by a test: it needs a
  facts store on a server, and the lane that has one is the integration
  lane, where a second process is what the test would have to stand.

## Status

Accepted 2026-09-14.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0223](./0223-watchbill-durable-work-on-facts.md) — the substrate, the claim, the worker; what this ADR completes.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — capabilities as subject filters; the runtime heartbeat.
- [ADR-0135](./0135-app-launch-requests.md) §SD2 — a payload is a vocabulary kind with a codec or it is not representable.
- [ADR-0094](./0094-keelson-introspection-tables.md) — `keelson()` tables.
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) — a book before an app.
- River — <https://github.com/riverqueue/river> — the client verbs this ADR mirrors (`Insert`, `JobCancel`, `JobRetry`, `JobGet`, `JobList`) and the features it names to keep out.
