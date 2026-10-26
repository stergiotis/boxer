---
name: Operations
abbrev: Ops
synopsis: what an agent may ask an app to do
level: 2
parent_ids:
  - "[[cell/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0269
  - adr:0315
  - adr:0318
  - adr:0313
  - adr:0270
  - adr:0329
  - pkg:public/keelson/runtime/appops
  - pkg:public/keelson/runtime/harness
  - table:keelson/app_operations
  - app:opsdemo
  - scene:agentconsole-play
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0269, app:opsdemo, scene:agentconsole-play]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0269, app:opsdemo, scene:agentconsole-play]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0269, app:opsdemo, scene:agentconsole-play]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

An app exposes what can be done to it as a catalog of commands and queries, an agent drives it under a task grant, and the schema the agent reads is the same one a person reads. Inference and the coordinator's tools are exposed through the harness so an agent reaches them the same way.

Its competences: [[command-query-contract]], [[harness-tools]], [[agent-readable-schema]].

# Mechanisms

The operations contract; the harness that exposes inference and coordinator tools; agent-readable schema; per-app catalogs for the SQL workbench and the process monitor.

# Evidence

The operations decision and the agent-console scenes that drive an app through it.

# Boundaries

An operation is what the app offered; an agent cannot reach a widget the catalog did not name.
