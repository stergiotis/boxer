---
name: "The system-metrics collector, data plane and tee"
level: 3
parent_ids:
  - "[[observability/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0019
  - adr:0090
  - adr:0184
  - pkg:public/observability/sysmetrics
  - pkg:public/keelson/runtime/sysmetricsbus
  - pkg:public/keelson/runtime/sysmtee
  - app:imztop
  - scene:imztop-process-sort
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0019, app:imztop, scene:imztop-process-sort]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0019, app:imztop, scene:imztop-process-sort]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0019, app:imztop, scene:imztop-process-sort]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

CPU, memory, disk, network, GPU, sensors and processes sampled into facts under one vocabulary, carried by a data plane and teed to several sinks.
