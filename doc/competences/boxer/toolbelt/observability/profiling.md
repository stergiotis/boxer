---
name: The profiling listener
level: 3
parent_ids:
  - "[[observability/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0212
  - pkg:public/observability/profiling
  - applet:profile-flame
  - applet:profile-heap
  - applet:profile-leaks
assessment:
  - dimension: readiness
    rating: 4
    basis: [adr:0212, applet:profile-flame]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: commodity
    basis: [adr:0212, applet:profile-flame]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0212, applet:profile-flame]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

pprof on a listener of its own, split from the app's traffic, with a book of applets over the profiles it yields.
