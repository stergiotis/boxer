---
type: explanation
audience: contributors
status: draft
---

> **Status: draft — pre-human-review.** Rendered by the capability
> inspector for the watchbill cap. Refine as consumers beyond the demo and
> the management window land.

# watchbill — durable work

[ADR-0223](../../../doc/adr/0223-watchbill-durable-work-on-facts.md) puts
restart-worthy work on the one substrate: a job is a row on a store-owned
ClickHouse table, claimed by one conditional lightweight update and read
back; every transition is an event row; a running job is a keelson task
with the job's id. [ADR-0234](../../../doc/adr/0234-watchbill-client-protocol-and-worker-presence.md)
made it a capability an app can hold, and
[ADR-0237](../../../doc/adr/0237-watchbill-worker-presence.md) lets a
worker declare what it drains. How the pieces fit, and what crosses the
bus in which encoding, is
[doc/explanation/watchbill-architecture.md](../../../doc/explanation/watchbill-architecture.md).

An app inside the runtime declares the client set and drives the queue
through the typed client:

```go
Caps: append(watchbill.ClientCaps(), task.ObserverCaps()...),

func (inst *App) Mount(ctx app.MountContextI) error {
    inst.queue = watchbill.NewClient(ctx.Bus())
    return nil
}

job, err := inst.queue.Enqueue(watchbill.Request{Kind: "tender.download", Subject: rowId, MaxAttempts: 3})
```

The five verbs — enqueue, cancel, retry, get, list — are River's client
verbs on `watchbill.job.<op>`, request/reply with generated codecs. The
worker that serves them attributes an enqueue to the bus envelope's
sender, so the owner app on the row is never a field the caller wrote.

## What the schematic shows

- **Subjects.** The five request subjects, the doorbell `watchbill.wake`
  a store holder rings after a row, and `watchbill.changed`, which the
  worker publishes after every row it flushed — never before.
- **Backends.** The worker is the service, under the id
  `runtime.watchbill`; `SqlStore` is the production store over the
  generated tables, `MemStore` the test double with the same semantics.
- **Schema.** The job table, one section per attribute so a claim
  rewrites one element and guards on another. What that update
  guarantees, in Jepsen's terms, is
  [doc/explanation/watchbill-consistency-model.md](../../../doc/explanation/watchbill-consistency-model.md).

## Where the rows are read

- `keelson('watchbill')` and `keelson('watchbill_event')` — the job rows
  and the transitions.
- `keelson('watchbill_worker')` — every worker run seen on the cell with
  what it drains and whether it is alive.
- The `watchbill` applet book, and the Watchbill window
  (`apps/watchbill`), which manages the queue over these tables and the
  client verbs.

## Handlers

A binary registers a handler per kind at init (`watchbill.Register`);
every worker in such a binary drains the kind. Delivery is at-least-once
and handlers are idempotent. `apps/watchbilldemo` is the in-tree example;
the how-to is [doc/howto/watchbill-jobs.md](../../../doc/howto/watchbill-jobs.md).
