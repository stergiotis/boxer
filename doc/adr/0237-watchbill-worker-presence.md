---
type: adr
status: accepted
date: 2026-09-15
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-15
---

# ADR-0237: a worker says what it drains — presence as an appended fact

## Context

[ADR-0234](./0234-watchbill-client-protocol-and-worker-presence.md) §SD5
gave a worker a status snapshot and `keelson('watchbill_worker')` for its
own process, and deferred the cell-wide question: which runs serve which
kinds. Without it a job of a kind no running binary links sits `queued`
with nothing to say so, and the management window's worker line names only
the process it runs in. River answers the question with a client table
whose rows carry a heartbeat.

Two facts already in the tree shape the answer. Everything a worker has to
say is fixed for its run — the run id, the host, the kinds, the queues, the
concurrency — so no row needs updating in place. And liveness is already
the runtime heartbeat, which the sweep joins today (ADR-0223 §SD4). So
presence needs none of the guarded-update machinery the claim rests on
([the consistency page](../explanation/watchbill-consistency-model.md)
says what that machinery buys and costs); an appended row and a join are
enough.

## Design space (QOC)

**Question.** Where does a worker declare what it drains, so that any
process on the cell can read it?

**Options.**

- **O1 — A presence row on the store-owned watchbill table** with a
  last-seen column updated in place: River's shape, a heartbeat per worker.
- **O2 — Append-only rows on `boxer.facts`** (chosen): a `watchbillWorker`
  kind, one row at start and one at a clean stop, liveness by the runtime
  heartbeat join.
- **O3 — Announce on the bus** and let every process keep a map: what the
  bus already is for; lost on restart, invisible without the bus.

**Criteria.** C1 modelled as facts on the one substrate (ADR-0148); C2 no
second liveness signal beside the heartbeat; C3 no guarded update, no new
table settings; C4 queryable from play and joinable to the run's other rows.

O2. O1 doubles the heartbeat and needs the update path for a value the
heartbeat already carries. O3 is not a fact.

## Decision

- **SD1 — The kind.** `watchbillWorker` on `boxer.facts`, a facts-bound
  record store (`watchbillpresence`) generated over one DTO on the runtime
  vocabulary: run id, host, phase (`started` or `stopped`), kinds, queues,
  concurrency. Key is the run id, so a run's rows form one series.
- **SD2 — When it is written.** The worker writes `started` after it
  subscribes and before its first poll, and `stopped` when it stops
  cleanly; a run that dies writes nothing, and its silence is its
  heartbeat's.
- **SD3 — How it is read.** A reader scans rows since a window (a day),
  folds them per run to the latest phase, and asks the heartbeat whether
  each unstopped run has shown life within `AbandonAfter` — the sweep's
  own rule, so the window and the sweep agree about who is dead.
- **SD4 — Where it shows.** `keelson('watchbill_worker')` lists every run
  seen in the window with what it drains and whether it is alive; the
  process's own worker adds its live fields (held jobs, last tick). The
  management window's worker line lists the live workers on the cell.
- **SD5 — Out.** A per-worker heartbeat; a presence verb on the protocol
  (River has none); pausing a worker from the window (River's pause is a
  named feature, and would be its own ADR).

## Alternatives

- **O1**: a second liveness signal and an in-place update for a fact the
  runtime heartbeat already carries.
- **O3**: not durable, not a fact, not visible from a process that joined
  late.
- **A field on the job row naming the workers that could run it**: a job
  does not know its workers; the cell does.

## Consequences

### Positive

- The cell-wide answer costs one appended row per start and stop and no
  new mechanism; retention is the facts table's.
- A stranded job is now visible: its kind against the live workers' kinds.
- The row joins the run's start, heartbeat and job rows on the run id.

### Negative

- Presence is as stale as the heartbeat: a dead worker reads alive for up
  to `AbandonAfter`, exactly as its jobs do.
- A worker that changes what it drains mid-run does not exist today; if
  one did, it would write another `started` row, and the fold takes the
  latest.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| runtime vocabulary | `runtimeKindWatchbillWorker` and six memberships, ordinals 95–101 | the assignments golden |
| `boxer.facts` | a new kind, append-only | nothing to provision: chstore owns the table |
| `watchbill.Config` | gains `Presence` | hostboot and the CLI verb wire it |
| `keelson('watchbill_worker')` | one row per run seen, cross-process | the applet chapter; the management window |

## Verification plan — Tier 1

- **Lane: default `go test`.** The fold: a started run is alive when its
  heartbeat is fresh, dead when not, gone when stopped; the latest phase
  wins; the worker writes `started` on Start and `stopped` on Stop.
- **Lane: `//go:build integration`.** Against the server the `CLICKHOUSE_*`
  variables name: a started and a stopped row round-trip through the store
  and the reader folds them.
- **Live.** Two processes on one cell, the host and a CLI worker; the
  window lists both with their kinds.

## Status

Accepted 2026-09-15.

## References

- [ADR-0234](./0234-watchbill-client-protocol-and-worker-presence.md) §SD5 — the deferral this closes.
- [ADR-0223](./0223-watchbill-durable-work-on-facts.md) §SD4 — liveness as the runtime heartbeat.
- [ADR-0184](./0184-sysmetrics-persistence-tee.md) — the first facts-bound store, and why it runs no DDL.
- [doc/explanation/facts-bound-record-stores.md](../explanation/facts-bound-record-stores.md) — the lane this store was added on.
- [doc/explanation/watchbill-consistency-model.md](../explanation/watchbill-consistency-model.md) — why presence needs no guarded update.
