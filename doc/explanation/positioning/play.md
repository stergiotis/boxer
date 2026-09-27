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

# play — positioning

Positions the SQL workbench: one buffer of ClickHouse SQL that becomes a
graph of queries, panels that observe its nodes, parameters that panels
write back, and the applet form in which a committed markdown document
becomes an app in its own right. SQL-defined applets are a clause here, not a
page. The runtime that mounts play is positioned by [keelson](./keelson.md),
the widgets its panels draw with by [widgets](./widgets.md), the rewrite
pipeline every run passes through by [nanopass](./nanopass.md).

## Short form

For someone who writes ClickHouse SQL at the keyboard and wants a result as
a table, chart, map, board or graph they can steer by interaction, play is a
SQL workbench over a reactive query graph[^graph]: the buffer is the
artifact, its common table expressions are the nodes, panels watch nodes,
parameters that panels write are the signals, and a query nothing watches
never runs.

Unlike a dashboard layer that links panels through a global filter bus and
matches them by column name, or a modelling language placed between the
author and the engine, play recovers the graph from the SQL itself, so the
pasted query stays the truth. The cost is a reactive runtime that can
surprise, plain SQL's lack of define-once measures, and a desktop app rather
than a web page.

## Full form

**For** a person who writes ClickHouse SQL at the keyboard — an operator on
a deployed box, a data engineer, whoever holds the query — and wants the
result as more than a grid, steered by interaction, with every run recorded,

**play is** a SQL workbench over a reactive query graph[^graph]: one buffer
split into nodes, run on demand and cached, panels that light up from the
shape of a result, typed parameter editors, and an applet[^applet] form in
which one markdown file is manifest, help page and query at once,

**that** keeps the buffer the artifact — the graph is recovered by analysis,
never authored beside it — runs nothing that no panel watches, and turns a
markdown file into a launchable, audited app,

**unlike** a dashboard layer that links panels through a global filter bus
and matches them by column name, or a modelling language between the author
and the engine after which the pasted SQL is no longer the truth,

**play** recovers the graph from the SQL the user typed. The cost is a
reactive runtime that is real machinery and can surprise, plain SQL's lack
of define-once measures, applet names that become durable public names, and
a desktop app rather than a web page.

## What each clause rests on

| Slot | Clause | Rests on |
| --- | --- | --- |
| For | a person writing SQL at the keyboard | the end-user audience of [play's help](../../../apps/play/help/overview.md); [ADR-0132](../../adr/0132-sqlapplet-sql-defined-applets.md) O4 (an operator authoring on a deployed box) |
| is a | one buffer, a graph of CTE nodes, demand-driven, memoized | [ADR-0097 §SD2, §SD3, §SD13](../../adr/0097-play-reactive-query-graph.md) |
| is a | panels from result-shape conventions | [ADR-0122](../../adr/0122-play-kanban-panel.md), [ADR-0129](../../adr/0129-play-layered-graph-panel.md), [ADR-0123](../../adr/0123-play-content-typed-detail-cells.md), [ADR-0172](../../adr/0172-play-chart-panel.md), [ADR-0227](../../adr/0227-play-graphview-panel.md), [ADR-0245](../../adr/0245-play-cards-panel-and-row-value-glosses.md), [ADR-0250](../../adr/0250-a-sql-backed-vector-field-source-and-plays-vector-field-pane.md) |
| is a | typed parameter editors; pins and signals | [ADR-0124](../../adr/0124-play-param-editing-widgets.md), [ADR-0097 §SD8](../../adr/0097-play-reactive-query-graph.md) |
| is a | publish-as-dataset | [ADR-0238](../../adr/0238-projection-feature-sets-hashed-structural-identity.md) Update; [ADR-0240](../../adr/0240-adhoc-datasets-v2-sealed-store-owned-capability.md) |
| is a | a pass panel | [ADR-0108](../../adr/0108-keelson-sql-pass-registry.md) Update; [ADR-0192](../../adr/0192-nanopass-cost-profiling.md) |
| is a | the applet form | [ADR-0132](../../adr/0132-sqlapplet-sql-defined-applets.md) |
| that | the buffer is the artifact; graph recovered, never authored | [ADR-0097 §SD3](../../adr/0097-play-reactive-query-graph.md) |
| that | nothing unobserved runs | [ADR-0097 §SD2](../../adr/0097-play-reactive-query-graph.md) |
| that | scope is the reference graph | [ADR-0097](../../adr/0097-play-reactive-query-graph.md) Alternatives (the cross-filter scoping subsystem) |
| that | every run stamped; liftable into facts | [play-architecture](../play-architecture.md); [ADR-0115](../../adr/0115-query-observability-data-plane-strategy.md) |
| that | a markdown file becomes an audited app with run attribution and a derived security class | [ADR-0132 §SD5](../../adr/0132-sqlapplet-sql-defined-applets.md) |
| unlike | a global filter bus with eager push; scoping by column matching | [ADR-0097](../../adr/0097-play-reactive-query-graph.md) Alternatives (the search-dashboard bus needing an apply gate; the BI cross-filter needing a scoping subsystem) |
| unlike | a modelling language between author and engine | [ADR-0097](../../adr/0097-play-reactive-query-graph.md) addendum (the pasted SQL would stop being the truth; no ClickHouse target) |
| play | recovers the graph from the SQL typed | [ADR-0097 §SD3](../../adr/0097-play-reactive-query-graph.md) |
| trade | reactive machinery; stale on a wrong hash; no define-once measures; durable slugs; desktop surface | [ADR-0097](../../adr/0097-play-reactive-query-graph.md) Consequences and addendum; [ADR-0132](../../adr/0132-sqlapplet-sql-defined-applets.md) Consequences |

## Boundary

- **The Model tab** ([ADR-0254](../../adr/0254-model-inference-as-a-keelson-capability.md))
  is built and, per that ADR's Status, not checked live against a model;
  it is not a clause. The natural-language ask panel
  ([ADR-0120](../../adr/0120-play-natural-language-ask-panel.md)) is
  withdrawn.
- Proposed and unbuilt, absent from the clauses: canonical record identity
  ([ADR-0219](../../adr/0219-play-canonical-record-identity.md)), the time
  strip ([ADR-0251](../../adr/0251-a-time-strip-for-stepped-series.md)).
  Built under proposed ADRs: the chat panel
  ([ADR-0239](../../adr/0239-play-chat-panel-and-chatview-widget.md)),
  column-handle resolution ([ADR-0116](../../adr/0116-play-leeway-column-handle-resolution.md)),
  send-to-play ([ADR-0217](../../adr/0217-mdedit-send-to-play-mddoc-facts.md)).
- Deferred in [ADR-0097](../../adr/0097-play-reactive-query-graph.md):
  explicit multi-cell authoring, incremental view maintenance,
  materializing shared intermediates.
- The time-series workbench buffers no longer run on bare ClickHouse
  ([ADR-0163](../../adr/0163-play-timeseries-workbench.md)); the "pasted
  SQL stays the truth" clause holds for the rewrite pipeline's input, not
  for its output.

## Further reading

- [why-boxer](../why-boxer.md) P7 — the premise this workbench enacts.
- [positioning-statement](../positioning-statement.md) — the product-level statement.
- [play-architecture](../play-architecture.md); [sqlapplet-authoring](../../howto/sqlapplet-authoring.md); [adhoc-datasets](../../howto/adhoc-datasets.md).
- Decisions: [ADR-0097](../../adr/0097-play-reactive-query-graph.md),
  [ADR-0124](../../adr/0124-play-param-editing-widgets.md),
  [ADR-0132](../../adr/0132-sqlapplet-sql-defined-applets.md),
  [ADR-0141](../../adr/0141-play-endpoint-dispatch-seam.md),
  [ADR-0238](../../adr/0238-projection-feature-sets-hashed-structural-identity.md),
  [ADR-0240](../../adr/0240-adhoc-datasets-v2-sealed-store-owned-capability.md).
- Reference: https://pkg.go.dev/github.com/stergiotis/boxer/apps/play

[^graph]: The sub-queries in one SQL buffer and the references between them; reactive means a change to one node re-runs only what depends on it.
[^applet]: A small app defined entirely by one markdown document holding SQL, launched like any other app.
