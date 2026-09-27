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

# observability — positioning

Positions the family rather than any one tool: the system-metrics
collectors and their data plane, the resource monitor and the Go runtime
dashboard, error shredding, log and coverage bridges, query-run capture,
and profiling. The premise it enacts is that the toolkit is its own first
workload ([why-boxer](../why-boxer.md) P4). The table shape the facts land
in is positioned by [recordstore](./recordstore.md); the dashboards are
[imzero2](./imzero2.md) apps.

## Short form

For an operator of the running toolkit — at a desktop, in a sandboxed
appliance, or acting through an agent — who needs load, runtime behaviour,
errors and query runs next to the data they concern, boxer's observability
is a set of collectors, bridges and dashboards that put what the system does
into the same facts table[^facts] as what it stores, drawn with the
toolkit's own widgets.

Unlike a metrics stack beside the data — a collector, a time-series store
and a dashboard of its own, each with its own model — it turns errors into
queryable rows, copies metrics into the facts table on request, replays
stored history through the live view, and captures query runs from the
engine itself. The cost is a reimplemented collector that can drift from its
model, an observer effect in the process it measures, and growth of the
facts table.

## Full form

**For** an operator of the running toolkit — at a desktop, in a sandboxed
appliance where one process may not read others, or an agent acting through
machine-readable tables — who needs load, runtime behaviour, errors and
query runs beside the data they concern,

**boxer's observability is** a family: pure-Go system collectors on a
one-way data plane[^plane], a resource monitor and a runtime dashboard drawn
by the toolkit's own UI, errors broken into per-field rows, and query-run
capture inside the engine,

**that** makes the system's own behaviour a query: metrics land as rows in
the facts table[^facts] on request, an error is one row with no decoder
needed, stored history replays through the same code as the live view, and
the scraper runs where the UI may not,

**unlike** a metrics and error stack beside the data — a collector, a time-
series store and a dashboard, each with a model, a service and a wire of its
own,

**boxer's observability** lands what the system does in the one model the
system stores everything else in, and draws it with the widgets it ships.
The cost is a reimplemented collector that can drift from its model, an
observer effect in the measured process, a lossy error encoding, and growth
of the facts table.

## What each clause rests on

| Slot | Clause | Rests on |
| --- | --- | --- |
| For | an operator in a sandboxed appliance | [ADR-0090](../../adr/0090-sysmetrics-pubsub-data-plane.md) Context (the monitor failed under a restricted `/proc`) |
| For | an agent through machine-readable surfaces | [aiops-operability](../aiops-operability.md) (draft); [why-boxer](../why-boxer.md) P7 |
| For | load beside lifecycle events | [ADR-0184](../../adr/0184-sysmetrics-persistence-tee.md) Context |
| is a | pure-Go collectors, no cgo | [ADR-0019](../../adr/0019-observability-sysmetrics-linux-collector.md) |
| is a | a one-way data plane; the UI imports no collector | [ADR-0090](../../adr/0090-sysmetrics-pubsub-data-plane.md) |
| is a | a resource monitor and a runtime dashboard on the toolkit's UI | [ADR-0020](../../adr/0020-imzero2-imztop-resource-monitor.md), [ADR-0061](../../adr/0061-imzero2-imzrt-go-runtime-dashboard.md) |
| is a | errors as per-fact rows | [ADR-0041](../../adr/0041-rowmarshall-error-shredding.md) |
| is a | logs and coverage as facts over the bus | [ADR-0026](../../adr/0026-app-runtime-and-capability-subjects.md) ("logs are facts too"); [ADR-0169](../../adr/0169-continuous-coverage-keelson.md) |
| is a | query-run capture by materialized views | [ADR-0115](../../adr/0115-query-observability-data-plane-strategy.md); [query-observability](../query-observability.md) |
| is a | profiling behind a split listener | [ADR-0212](../../adr/0212-split-pprof-http-listener.md) |
| that | metrics as rows in the facts table, opt-in | [ADR-0184 §SD1](../../adr/0184-sysmetrics-persistence-tee.md) (default off); note that metrics-as-facts *on the wire* was abandoned for CBOR in [ADR-0090](../../adr/0090-sysmetrics-pubsub-data-plane.md)'s Update |
| that | replay through the live fold | [ADR-0197](../../adr/0197-imztop-replay-mode.md) |
| that | per-pass cost as data | [ADR-0192](../../adr/0192-nanopass-cost-profiling.md) |
| unlike | a collector, a store and a dashboard of their own | the rejected collectors in [ADR-0019](../../adr/0019-observability-sysmetrics-linux-collector.md) (a cross-platform library, a subset parser, a cgo wrapper); the HTTP-hop dashboards in [ADR-0061](../../adr/0061-imzero2-imzrt-go-runtime-dashboard.md); the SDK, collector and wire refused as "a second data model and a service tether" in [aiops-operability](../aiops-operability.md) (draft; the foil is accepted on that citation) |
| observability | one model, rendered by the toolkit's own widgets | [why-boxer](../why-boxer.md) P4 and its *Enacted by* list |
| trade | a parallel collector that drifts; observer effect; lossy errors; facts growth | [ADR-0019](../../adr/0019-observability-sysmetrics-linux-collector.md), [ADR-0061](../../adr/0061-imzero2-imzrt-go-runtime-dashboard.md), [ADR-0041](../../adr/0041-rowmarshall-error-shredding.md), [ADR-0184](../../adr/0184-sysmetrics-persistence-tee.md) Consequences |

## Boundary

- The structured error idioms (`eh`, `eb`) are a house convention every
  package inherits; this page positions their *shredding into rows*, not
  the idiom, which [why-boxer](../why-boxer.md) lists under "What this costs
  you".
- The introspection tables ([ADR-0094](../../adr/0094-keelson-introspection-tables.md))
  are positioned by [keelson](./keelson.md).
- Not built, and absent from the clauses: the Go runtime collector on the
  data plane and its persistence, an instrument registry, health verdicts,
  alerting, and spans in an open telemetry format. The first observability
  pipeline ADR was superseded before it was built and is not cited here.

## Further reading

- [why-boxer](../why-boxer.md) P4 — the premise this family enacts.
- [positioning-statement](../positioning-statement.md) — the product-level statement.
- [query-observability](../query-observability.md), [aiops-operability](../aiops-operability.md) (drafts).
- Decisions: [ADR-0019](../../adr/0019-observability-sysmetrics-linux-collector.md),
  [ADR-0020](../../adr/0020-imzero2-imztop-resource-monitor.md),
  [ADR-0041](../../adr/0041-rowmarshall-error-shredding.md),
  [ADR-0061](../../adr/0061-imzero2-imzrt-go-runtime-dashboard.md),
  [ADR-0090](../../adr/0090-sysmetrics-pubsub-data-plane.md),
  [ADR-0115](../../adr/0115-query-observability-data-plane-strategy.md),
  [ADR-0184](../../adr/0184-sysmetrics-persistence-tee.md),
  [ADR-0197](../../adr/0197-imztop-replay-mode.md).
- Reference: https://pkg.go.dev/github.com/stergiotis/boxer/public/observability/sysmetrics

[^plane]: The one-way channel the metrics scraper publishes on; the UI subscribes to it instead of reading the operating system itself.
[^facts]: `boxer.facts`: the one ClickHouse table shape every durable record lands in, so any record can be joined with any other in SQL.
