---
name: Data mapping
abbrev: LW
synopsis: "describing, marshalling and reading columnar records"
level: 2
parent_ids:
  - "[[toolbelt/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - positioning:leeway
  - adr:0066
  - adr:0074
  - adr:0146
  - adr:0183
  - adr:0181
  - pkg:public/semistructured/leeway
  - howto:leeway-marshalling
  - explanation:leeway-sql-read-surface
  - trial:leeway-second-substrate
  - trial:jsonbench-on-facts
  - applet:lw-anatomy
  - skill:leeway-beginner
  - scene:01_table_leeway
assessment:
  - dimension: readiness
    rating: 8
    basis: [positioning:leeway, howto:leeway-marshalling, trial:leeway-second-substrate, trial:jsonbench-on-facts]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [positioning:leeway, howto:leeway-marshalling, trial:leeway-second-substrate]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [positioning:leeway, howto:leeway-marshalling, trial:leeway-second-substrate]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Turning a Go value, a JSON document or a stream of events into columns ClickHouse can hold, and reading them back, without a schema per record type: a backbone of plain values beside a payload of tagged values, where a tag is a membership minted from a registry. The customer is someone with heterogeneous records who wants one table and one read path rather than a migration per shape.

Its competences: [[table-description-and-ddl]], [[marshalling]], [[membership-roles]], [[components-and-read-contracts]], [[sql-read-surface]], [[canonical-record-form]], [[aspects-and-vocabulary]], [[params-codec]], [[co-ragged-functions]].

# Mechanisms

A six-stage pipeline — describe, IR, map, DDL, marshal, query — with a pass downstream of every stage. Memberships are registered names with timeless ids; components are structurally typed views over slots; the SQL read surface expands a handle into the lane arithmetic so a query names a section and a membership and nothing physical.

# Evidence

The positioning page; the leeway skills and the marshalling and read-surface how-tos; the second-substrate and jsonbench trials; the applet book that inspects a table's anatomy.

# Boundaries

Not an ORM and not a query language: leeway maps values to columns and reads them back; what to ask is SQL's job, and nanopass's.
