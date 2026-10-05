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

# keelson — positioning

Positions the app runtime: the host process that mounts apps, the bus they
talk over, the capability model that mediates what they may touch, and the
introspection tables that expose what the runtime is doing. The pool of
local query workers, the external-binary registry, the bus codec, launch
requests, working sets and durable app state are clauses here. The UI stack
the host draws with is positioned by [imzero2](./imzero2.md); the table
shape the runtime's facts land in by [recordstore](./recordstore.md);
durable work by [watchbill](./watchbill.md).

## Short form

For an app author who wants to write a data app as four methods and let the
host decide where it runs and what it may touch, keelson is boxer's app
runtime: one Go process that mounts apps, passes every request over a
message bus[^bus] whose addresses double as permissions[^capability], and
records grants, audit, logs, launches and app state as rows in the facts
table[^facts].

Unlike a host that gives each app its own process on a message broker, with
state kept in the broker or in a second store, keelson keeps the runtime's
whole record in one queryable place, so the SQL workbench doubles as the
runtime's debugger. The price is a hygiene boundary rather than a security
one, and an indirection on every call.

## Full form

**For** an app author who writes a data app as `Manifest`, `Mount`, `Frame`
and `Unmount`, leaves placement — desktop, browser, headless, appliance — to
the host, and wants what the app may touch declared rather than assumed,

**keelson is** boxer's app runtime: one Go process that mounts apps over a
message bus[^bus] whose addresses are the permission model[^capability],
with introspection tables[^introspection] that expose the runtime's own
state to SQL,

**that** records every grant, audit row, log line, launch and piece of app
state in the facts table[^facts], so the SQL workbench doubles as a debugger
for the runtime, and runs the same app source unchanged on every host,

**unlike** a host that isolates each app as its own process on a message
broker, with state held in the broker or in a second store nothing else can
query,

**keelson** makes the permission an address and the record a table. The
price is a hygiene boundary rather than a security one, an indirection on
every call, and an address scheme the runtime must keep stable.

## What each clause rests on

| Slot | Clause | Rests on |
| --- | --- | --- |
| For | four methods; placement is the host's call | [ADR-0026 §SD8](../../adr/0026-app-runtime-and-capability-subjects.md); the app author audience of [runtime/app EXPLANATION](../../../public/keelson/runtime/app/EXPLANATION.md) |
| For | capabilities declared, not assumed | [ADR-0026](../../adr/0026-app-runtime-and-capability-subjects.md) Decision |
| is a | subject filters as the capability model; one API over in-process and NATS | [ADR-0026](../../adr/0026-app-runtime-and-capability-subjects.md) (C3: the swap is a transport change) |
| is a | canonical CBOR codec | [ADR-0036](../../adr/0036-runtime-buscodec.md) |
| is a | local query workers | [ADR-0028](../../adr/0028-chlocal-low-latency-sql-cap.md) |
| is a | external binaries resolved and digested through one registry | [ADR-0118](../../adr/0118-extbin-external-process-chokepoint.md) |
| is a | introspection tables | [ADR-0094](../../adr/0094-keelson-introspection-tables.md) |
| that | grants, audit, logs, launches, working sets, effects as facts | [ADR-0026 §SD6](../../adr/0026-app-runtime-and-capability-subjects.md), [ADR-0135](../../adr/0135-app-launch-requests.md), [ADR-0148](../../adr/0148-app-workingsets.md), [ADR-0188](../../adr/0188-app-instance-effect-tracking.md); see *Boundary* on which kinds are generated |
| that | manifest caps checked against the call graph | [ADR-0026 §SD10](../../adr/0026-app-runtime-and-capability-subjects.md) (runs in compare mode; hard-fail ahead) |
| that | per-table, declared, audited reads of runtime state | [ADR-0253](../../adr/0253-introspection-table-reads-as-a-bus-capability.md) |
| that | the same source on every host | [ADR-0026 §SD8](../../adr/0026-app-runtime-and-capability-subjects.md); [ARCHITECTURE §1](../../ARCHITECTURE.md) |
| unlike | a process per app on a broker | [ADR-0026](../../adr/0026-app-runtime-and-capability-subjects.md) Alternatives (OS process isolation per app; an embedded broker library) |
| unlike | state in the broker or a second store | [ADR-0223](../../adr/0223-watchbill-durable-work-on-facts.md) Alternatives (broker work queues, a second substrate); [ADR-0148](../../adr/0148-app-workingsets.md) Update (framework persistence files) |
| keelson | capability as subject filter, trail as table | [ADR-0026](../../adr/0026-app-runtime-and-capability-subjects.md) Decision and C3 |
| trade | hygiene not security; indirection; stability surfaces; leeway tooling for queries | [ADR-0026](../../adr/0026-app-runtime-and-capability-subjects.md) Consequences ("the lint guard is the only enforcement"); [ADR-0036](../../adr/0036-runtime-buscodec.md) (CBOR not human-readable) |

## Boundary

- **No consent prompt.** The grant broker exists in the tree but is never
  constructed in production ([ADR-0026 §SD12](../../adr/0026-app-runtime-and-capability-subjects.md)
  defers the grant overlay). Capabilities are static manifest declarations
  plus file-system handles; the statement claims declaration and audit, not
  interactive consent.
- **NATS.** A NATS client ships for the metrics plane; per-app
  authorisation (keys, tokens) is accepted and unbuilt
  ([ADR-0026](../../adr/0026-app-runtime-and-capability-subjects.md) M4).
  "One API over both transports" is the shipped claim; "a secured NATS
  fleet" is not.
- **Generated versus hand-written record.** The core runtime kinds — grant,
  audit, log, run, heartbeat, lifecycle, launch — still write through the
  hand-rolled store; the generated record-store adoption for them is not
  built ([ADR-0105](../../adr/0105-keelson-adopts-generated-record-stores.md)
  D3b). The "one facts table" clause holds; "generated end to end" would not.
- Accepted and unbuilt, so absent from the clauses: the streaming reply
  channel ([ADR-0143](../../adr/0143-bus-streaming-reply-channel.md)), the
  embed interface ([ADR-0155](../../adr/0155-app-embed-seam.md)), the SQL console
  ([ADR-0094 §SD6](../../adr/0094-keelson-introspection-tables.md)),
  lifecycle subjects and CLI unification.
- Model inference as a capability ([ADR-0254](../../adr/0254-model-inference-as-a-keelson-capability.md))
  is deferred to its own page per the [folder README](./README.md).

## Further reading

- [why-boxer](../why-boxer.md) — P3 and P7 are the premises this runtime enacts most directly.
- [positioning-statement](../positioning-statement.md) — the product-level statement.
- [keelson EXPLANATION](../../../public/keelson/EXPLANATION.md); [ARCHITECTURE §1](../../ARCHITECTURE.md).
- Decisions: [ADR-0026](../../adr/0026-app-runtime-and-capability-subjects.md),
  [ADR-0036](../../adr/0036-runtime-buscodec.md),
  [ADR-0094](../../adr/0094-keelson-introspection-tables.md),
  [ADR-0118](../../adr/0118-extbin-external-process-chokepoint.md),
  [ADR-0135](../../adr/0135-app-launch-requests.md),
  [ADR-0148](../../adr/0148-app-workingsets.md),
  [ADR-0185](../../adr/0185-durable-app-state-manager.md),
  [ADR-0188](../../adr/0188-app-instance-effect-tracking.md),
  [ADR-0253](../../adr/0253-introspection-table-reads-as-a-bus-capability.md).
- Reference: https://pkg.go.dev/github.com/stergiotis/boxer/public/keelson/runtime/app

[^bus]: The in-process message bus every app request travels over; a NATS client can stand in for it.
[^capability]: What an app may request; here a filter on bus addresses, declared in the app's manifest and checked against its code.
[^facts]: `boxer.facts`: the one ClickHouse table shape every durable record lands in, so any record can be joined with any other in SQL.
[^introspection]: Tables named `keelson.*` that expose runtime state — grants, launches, app state — to any SQL client.
