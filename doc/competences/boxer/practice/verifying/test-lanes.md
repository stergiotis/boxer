---
name: The default and integration lanes
level: 3
parent_ids:
  - "[[verifying/capability]]"
domain: boxer-practice
catalog: boxer
realised_by:
  - adr:0212
  - howto:continuous-coverage
assessment:
  - dimension: readiness
    rating: 6
    basis: [howto:continuous-coverage]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: commodity
    basis: [howto:continuous-coverage]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [howto:continuous-coverage]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

The default lane runs on every machine with nothing installed; the integration lane carries a build tag and runs serially against a live server, skipping itself when none is reachable.
