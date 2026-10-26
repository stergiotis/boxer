---
name: Conformance suites
level: 3
parent_ids:
  - "[[verifying/capability]]"
domain: boxer-practice
catalog: boxer
realised_by:
  - adr:0267
  - pkg:public/thestack/imzero2/egui2/widgets/conformance
  - pkg:public/caching/stashtest
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0267, pkg:public/thestack/imzero2/egui2/widgets/conformance, pkg:public/caching/stashtest]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0267, pkg:public/thestack/imzero2/egui2/widgets/conformance, pkg:public/caching/stashtest]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0267, pkg:public/thestack/imzero2/egui2/widgets/conformance, pkg:public/caching/stashtest]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

One package per contract that walks every package of that kind and checks the mechanical rules, with an allowlist that should stay empty.
