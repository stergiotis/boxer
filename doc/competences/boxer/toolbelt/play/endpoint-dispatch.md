---
name: Endpoint dispatch and engine adapters
level: 3
parent_ids:
  - "[[play/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0141
  - adr:0144
  - adr:0133
  - adr:0142
  - pkg:public/keelson/runtime/queryengine
  - pkg:public/keelson/data/chlocalbroker
  - app:play
  - scene:28_endpoint_menu
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0141, scene:28_endpoint_menu]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0141, scene:28_endpoint_menu]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0141, scene:28_endpoint_menu]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A query goes to the endpoint the document names — a ClickHouse server, a local engine, the introspection tables — through an adapter, and the menu offers what is reachable.
