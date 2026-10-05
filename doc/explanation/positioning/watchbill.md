---
type: explanation
audience: prospective consumers and integrators evaluating adoption
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

> Where this page and an ADR disagree, the ADR is the record. The
> product-level statement is [positioning-statement](../positioning-statement.md);
> this page applies the same template to one subsystem. The set and its
> review checklist are in the [folder README](./README.md).

# watchbill — positioning

Positions durable work: a job is a row on a store-owned table in the facts
layout, a worker claims it by one conditional update, and the lease is the
runtime's existing heartbeat. The management window and the headless
command group are clauses here. The ephemeral task primitive every run
becomes is [keelson](./keelson.md)'s; the table shape is
[recordstore](./recordstore.md)'s.

## Short form

For a developer giving an app or a headless binary work that must survive
the process — a download, an export, a long computation — on a box that has
a ClickHouse server and nothing else, watchbill is durable work as records:
a job table in ClickHouse, a claim that is one conditional update plus a
read-back, a lease that is the runtime's existing heartbeat, and five verbs
over named queues.

Unlike a job queue on a second store — a relational database, a file, or a
broker whose state nothing can query — watchbill keeps every job's whole
life as rows next to the launches and grants it can be joined with, readable
from the SQL workbench. The cost is throughput in the tens of jobs a minute,
no periodic or workflow jobs, and a queue that is gone when the server is.

## Full form

**For** a developer giving an app or a headless binary durable work — a job
claimed once, surviving the process that requested it, visible while it runs
— on a box with a ClickHouse server and no daemon, file or broker to spare,

**watchbill is** durable work as records: a job table in ClickHouse in the
facts shape[^facts], a claim that is one conditional update[^claim], a lease
that is the runtime's existing heartbeat, and five verbs over named queues,

**that** makes a job's whole life a query, joinable to the launches and
grants around it, makes every run an ordinary runtime task so progress and
cancel come with it, and runs the same worker in a window or a headless
process,

**unlike** a job queue on a second store — a relational database or a file
holding opaque state, or a broker whose queue state nothing can query
without copying it out,

**watchbill** keeps the queue in the one store everything else is in. The
cost is throughput in the tens of jobs a minute, a queue that is gone when
the server is, an engine feature still marked beta in one server line, and
no periodic or workflow jobs.

## What each clause rests on

| Slot | Clause | Rests on |
| --- | --- | --- |
| For | a developer giving an app or headless binary durable work | the audience of [watchbill-jobs](../../howto/watchbill-jobs.md); [ADR-0223](../../adr/0223-watchbill-durable-work-on-facts.md) Context |
| For | a box with ClickHouse and nothing else | [ADR-0223](../../adr/0223-watchbill-durable-work-on-facts.md) Consequences ("the appliance needs only ClickHouse") |
| is a | job table in the house layout; conditional update plus read-back | [ADR-0223 §SD3](../../adr/0223-watchbill-durable-work-on-facts.md); [watchbill-consistency-model](../watchbill-consistency-model.md) |
| is a | lease is the runtime heartbeat; bus as doorbell | [ADR-0223 §SD4](../../adr/0223-watchbill-durable-work-on-facts.md) |
| is a | five verbs, named queues, a known contract | [ADR-0234](../../adr/0234-watchbill-client-protocol-and-worker-presence.md) ("if you know River, the verbs are River's") |
| is a | presence as facts; management window; command group | [ADR-0237](../../adr/0237-watchbill-worker-presence.md), [ADR-0236](../../adr/0236-watchbill-management-app.md) |
| that | a job's life as a query, joinable to launches and grants | [ADR-0223](../../adr/0223-watchbill-durable-work-on-facts.md) C1 and Decision |
| that | every run an ordinary task | [ADR-0223 §SD5](../../adr/0223-watchbill-durable-work-on-facts.md); [ADR-0038](../../adr/0038-keelson-background-task-primitive.md) |
| that | same worker windowed or headless | [ADR-0223 §SD8](../../adr/0223-watchbill-durable-work-on-facts.md), [ADR-0234 §SD6](../../adr/0234-watchbill-client-protocol-and-worker-presence.md) |
| unlike | a queue library on a relational database or file | [ADR-0223](../../adr/0223-watchbill-durable-work-on-facts.md) Alternatives O1, O2 (a second substrate with opaque state; the appliance has no writable file); [ADR-0038](../../adr/0038-keelson-background-task-primitive.md) Update (the queue-library backend withdrawn) |
| unlike | a broker's work queue | [ADR-0223](../../adr/0223-watchbill-durable-work-on-facts.md) Alternatives O3 (state in the broker; no query without a tee) |
| watchbill | the queue in the one store; compare-and-set as the claim | [ADR-0223 §SD3](../../adr/0223-watchbill-durable-work-on-facts.md); [watchbill-consistency-model](../watchbill-consistency-model.md) |
| trade | throughput ceiling, patch parts, one server, no queue without the server, beta feature, no periodic or workflow jobs | [ADR-0223](../../adr/0223-watchbill-durable-work-on-facts.md) Consequences; [ADR-0234 §SD7](../../adr/0234-watchbill-client-protocol-and-worker-presence.md) |

## Boundary

- The consistency model is measured under concurrent claimants and not
  proven by an external checker; the job row and its event row are not
  atomic, and the durability page records what a power loss can lose
  ([watchbill-consistency-model](../watchbill-consistency-model.md)).
- The first consumer is in a repository outside this one; in-tree, the
  demo app is the client. The statement's customer is the developer that
  ADR and how-to address, not an in-tree app.
- The vocabulary is *kinds* and *queues*; there are no lanes.
- The ephemeral task primitive ([ADR-0038](../../adr/0038-keelson-background-task-primitive.md))
  is keelson's; watchbill positions only what survives the process.

## Further reading

- [why-boxer](../why-boxer.md) P3 — the premise this queue enacts.
- [positioning-statement](../positioning-statement.md) — the product-level statement.
- [watchbill-jobs](../../howto/watchbill-jobs.md); [watchbill-architecture](../watchbill-architecture.md); [watchbill-consistency-model](../watchbill-consistency-model.md).
- Decisions: [ADR-0223](../../adr/0223-watchbill-durable-work-on-facts.md),
  [ADR-0234](../../adr/0234-watchbill-client-protocol-and-worker-presence.md),
  [ADR-0236](../../adr/0236-watchbill-management-app.md),
  [ADR-0237](../../adr/0237-watchbill-worker-presence.md).
- Reference: https://pkg.go.dev/github.com/stergiotis/boxer/public/keelson/runtime/watchbill

[^facts]: `boxer.facts`: the one ClickHouse table shape every durable record lands in, so any record can be joined with any other in SQL.
[^claim]: A worker takes a job by one conditional update that succeeds for exactly one claimant, then reads the row back to confirm it won.
