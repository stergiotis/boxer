---
name: The framed render pipeline
level: 3
parent_ids:
  - "[[imzero2/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0049
  - adr:0062
  - adr:0261
  - adr:0128
  - pkg:public/thestack/imzero2/egui2
  - pkg:public/thestack/fffi2
  - skill:imzero2
  - skill:fffi2
  - trial:keelson-wasm-frame-cost
assessment:
  - dimension: readiness
    rating: 8
    basis: [adr:0049, skill:imzero2, trial:keelson-wasm-frame-cost]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0049, skill:imzero2, trial:keelson-wasm-frame-cost]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0049, skill:imzero2, trial:keelson-wasm-frame-cost]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A frame is assembled on one goroutine from widget calls, crosses the FFI boundary once, and is rendered by the host; deferred blocks are memoised so an unchanged subtree costs nothing to re-emit.
