---
name: Introspection tables
level: 3
parent_ids:
  - "[[keelson/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0094
  - adr:0122
  - pkg:public/keelson/runtime/introspect
  - pkg:public/keelson/runtime/introspect/providers
  - applet:runtime-apps
  - applet:runtime-env
  - scene:07_keelson_introspection
  - howto:topology-queries
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0094, applet:runtime-apps, scene:07_keelson_introspection, howto:topology-queries]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0094, applet:runtime-apps, scene:07_keelson_introspection]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0094, applet:runtime-apps, scene:07_keelson_introspection]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

The process answers questions about itself through SQL: the apps it hosts, its environment variables, its build, its providers, the decision and competence corpora of the checkout it runs in. A new table is a provider that registers; a reader needs no new API.
