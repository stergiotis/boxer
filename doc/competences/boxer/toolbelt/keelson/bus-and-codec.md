---
name: The bus and its codec
level: 3
parent_ids:
  - "[[keelson/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0036
  - adr:0143
  - adr:0290
  - pkg:public/keelson/runtime/inprocbus
  - pkg:public/keelson/runtime/natsbus
  - pkg:public/keelson/runtime/buscodec
assessment:
  - dimension: readiness
    rating: 4
    basis: [adr:0036, adr:0143, pkg:public/keelson/runtime/buscodec]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0036, adr:0143, pkg:public/keelson/runtime/buscodec]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0036, adr:0143, pkg:public/keelson/runtime/buscodec]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Apps and services exchange messages over a bus — in-process by default, NATS across processes — under one codec the bus owns, so a payload is encoded once and read the same way on every side.
