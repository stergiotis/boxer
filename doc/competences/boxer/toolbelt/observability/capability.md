---
name: Observability
abbrev: Obs
synopsis: "metrics, query observability, coverage, profiling and errors, kept as facts"
level: 2
parent_ids:
  - "[[toolbelt/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - positioning:observability
  - adr:0019
  - adr:0090
  - adr:0184
  - adr:0115
  - adr:0169
  - adr:0212
  - pkg:public/observability
  - pkg:public/observability/sysmetrics
  - explanation:query-observability
  - howto:continuous-coverage
  - applet:cov-overview
  - applet:profile-top
  - app:imztop
  - scene:imztop-process-sort
assessment:
  - dimension: readiness
    rating: 7
    basis: [positioning:observability, explanation:query-observability, howto:continuous-coverage]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [positioning:observability, explanation:query-observability, howto:continuous-coverage]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [positioning:observability, explanation:query-observability, howto:continuous-coverage]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Seeing what the system and its queries are doing: a metrics collector with a data plane and a tee, query observability over ClickHouse's own logs, continuous coverage of the test suite, resource monitors with replay, structured errors, and a profiling listener. The customer is someone who runs boxer and wants its telemetry in the same store as their data.

Its competences: [[system-metrics]], [[query-observability]], [[continuous-coverage]], [[resource-monitors-and-replay]], [[error-shredding]], [[profiling]], [[code-volume-self-inspection]].

# Mechanisms

Collectors write facts; the tee fans them out; scrapes and replays read them back; coverage lands as rows the applet book reads; errors are shredded into fields; pprof is split off onto its own listener.

# Evidence

The positioning page, the query-observability explanation, the coverage and code-volume how-tos, and the pprof and coverage applet books.

# Boundaries

Not a monitoring product with alerting: the facts are there to query, and what to alert on is a consumer's rule.
