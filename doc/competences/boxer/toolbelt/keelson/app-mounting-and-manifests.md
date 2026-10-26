---
name: App mounting and manifests
level: 3
parent_ids:
  - "[[keelson/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0026
  - adr:0135
  - adr:0158
  - adr:0214
  - adr:0260
  - adr:0335
  - pkg:public/keelson/runtime/app
  - table:keelson/apps
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0026, adr:0158, pkg:public/keelson/runtime/app]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0026, adr:0158, pkg:public/keelson/runtime/app]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0026, adr:0158, pkg:public/keelson/runtime/app]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

An app declares itself once — id, display, summary, topics, keywords, surface — and the registry mounts it, observes where it came from, and tells every other surface about it. A consumer adds an app by writing a manifest and a factory, not by editing the host.
