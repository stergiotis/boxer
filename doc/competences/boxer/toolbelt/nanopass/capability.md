---
name: SQL transformation
abbrev: NP
synopsis: "small passes over ClickHouse SQL, composed"
level: 2
parent_ids:
  - "[[toolbelt/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - positioning:nanopass
  - adr:0002
  - adr:0006
  - adr:0098
  - adr:0108
  - adr:0192
  - pkg:public/db/clickhouse/dsl/nanopass
  - skill:nanopass-sql
  - scene:12_passes
  - scene:12_passes_cost
  - table:keelson/sql_passes
assessment:
  - dimension: readiness
    rating: 7
    basis: [positioning:nanopass, skill:nanopass-sql, scene:12_passes]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [positioning:nanopass, skill:nanopass-sql, scene:12_passes]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [positioning:nanopass, skill:nanopass-sql, scene:12_passes]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Rewriting ClickHouse SQL by small, named passes that each do one thing — canonicalise a shape, qualify a table, expand a macro, extract a literal — composed into pipelines a reader can inspect pass by pass. The customer is someone who generates or rewrites SQL and wants to see what changed and why.

Its competences: [[pass-discipline]], [[rewrite-combinators]], [[pass-registry]], [[parser-bounding]], [[cost-profiling]], [[literal-marshalling]], [[passthrough-classification]].

# Mechanisms

A parser bounded by a DFA cache, two-stage prediction and exact qualifiers; an environment every pass reads and writes; combinators that sequence passes and run one to a fixed point; a registry that names every pass and the cost profiler that times it.

# Evidence

The positioning page, the skill, and play's pass-inspection scenes.

# Boundaries

Not a planner and not an engine: nanopass rewrites text to text, and the engine runs what comes out.
