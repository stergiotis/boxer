---
name: Continuous coverage
level: 3
parent_ids:
  - "[[observability/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0169
  - pkg:public/observability/coverage
  - pkg:public/keelson/runtime/covscrape
  - howto:continuous-coverage
  - applet:cov-overview
  - applet:cov-uncovered
  - table:keelson/coverage_pkgs
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0169, howto:continuous-coverage, applet:cov-overview]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0169, howto:continuous-coverage, applet:cov-overview]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0169, howto:continuous-coverage, applet:cov-overview]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Test coverage landed as rows on every run, so coverage is a query over time rather than a report per build.
