---
name: Verifying
abbrev: Ver
synopsis: "two test lanes, conformance suites, golden files, scenes, coverage"
level: 2
parent_ids:
  - "[[practice/capability]]"
domain: boxer-practice
catalog: boxer
realised_by:
  - adr:0248
  - adr:0169
  - adr:0057
  - adr:0267
  - howto:continuous-coverage
  - skill:imzero2-drive
  - scene:01_table_leeway
  - applet:cov-overview
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0248, howto:continuous-coverage]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0248, howto:continuous-coverage]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0248, howto:continuous-coverage]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Tests run in a default lane that needs nothing and an integration lane that needs a live server or a heavy dependency; conformance suites check every package of a kind against one contract; golden files pin what must not move; headless scenes drive the UI; coverage lands as rows on every run.

Its competences: [[test-lanes]], [[conformance-suites]], [[golden-files]], [[headless-scenes]], [[screenshot-tour]].

# Mechanisms

Build tags select the lane; conformance packages walk the tree; golden files under testdata; scene documents under each app; the coverage scraper.

# Evidence

The engineering-practices page, the coverage how-to, the scene tour under play.

# Boundaries

A test proves a mechanism; a trial measures a cost. The two are kept apart.
