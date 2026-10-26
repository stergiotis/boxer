---
name: Measuring
abbrev: Msr
synopsis: "trials with a citable claim, benchmarks, cost profiles"
level: 2
parent_ids:
  - "[[practice/capability]]"
domain: boxer-practice
catalog: boxer
realised_by:
  - adr:0192
  - trial:jsonbench-on-facts
  - trial:leeway-second-substrate
  - trial:keelson-wasm-frame-cost
  - trial:map-tile-addressing
assessment:
  - dimension: readiness
    rating: 8
    basis: [trial:jsonbench-on-facts, trial:leeway-second-substrate]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [trial:jsonbench-on-facts, trial:leeway-second-substrate]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [trial:jsonbench-on-facts, trial:leeway-second-substrate]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A performance claim comes from a trial — a reproducible protocol repeated across builds — and is quoted only through the trial's citable claim with the conditions it holds under; benchmarks run in the default lane; a pipeline's cost is profiled pass by pass.

Its competences: [[trials]], [[cost-profiles]].

# Mechanisms

The trials directory and its citation rule; the §0 claim; per-arm runs under it; the cost profiler.

# Evidence

The seven trials in the tree and the rule in the trials README.

# Boundaries

A figure travels with the pair of arms it compares, or not at all.
