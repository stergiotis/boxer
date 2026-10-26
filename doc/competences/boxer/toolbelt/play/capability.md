---
name: SQL workbench
abbrev: Play
synopsis: a reactive query graph with panels that show a result
level: 2
parent_ids:
  - "[[toolbelt/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - positioning:play
  - adr:0097
  - adr:0141
  - adr:0124
  - adr:0270
  - pkg:public/keelson/runtime/queryengine
  - app:play
  - explanation:play-architecture
  - howto:play-pluggable-detail
  - howto:play-pluggable-docs
  - scene:01_table_leeway
  - scene:18_params_live
  - env:BOXER_PLAY_SQL
assessment:
  - dimension: readiness
    rating: 7
    basis: [positioning:play, explanation:play-architecture, scene:01_table_leeway]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [positioning:play, explanation:play-architecture, scene:01_table_leeway]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [positioning:play, explanation:play-architecture, scene:01_table_leeway]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Writing a query, running it against an engine, and seeing the result as a table, a chart, a map, a graph, a kanban or a conversation — with parameters, glosses and a detail pane, and with every panel a scene can drive. The customer is someone who explores data in ClickHouse and wants the exploration to be a document they can keep.

Its competences: [[reactive-query-graph]], [[result-panels]], [[parameters-and-expression-slots]], [[content-typed-detail]], [[endpoint-dispatch]], [[vocabulary-panel]], [[canonical-record-identity]], [[play-operations]].

# Mechanisms

A reactive query graph re-runs what a parameter change invalidates; panels are registered by the shape of record they accept; an endpoint dispatches to an engine adapter; the operations catalog exposes what an agent may do under a limit.

# Evidence

The positioning page, the play architecture explanation, the pluggable-detail and docs how-tos, and the scene tour under the app.

# Boundaries

Not a BI product with a semantic layer: play shows what a query returns, and the query is the model.
