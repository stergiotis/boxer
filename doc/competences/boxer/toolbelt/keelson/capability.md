---
name: App runtime
abbrev: Keel
synopsis: "mounting apps, a bus, capability subjects, introspection"
level: 2
parent_ids:
  - "[[toolbelt/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - positioning:keelson
  - adr:0026
  - adr:0135
  - adr:0158
  - adr:0094
  - adr:0036
  - adr:0211
  - pkg:public/keelson/runtime/app
  - pkg:public/keelson/runtime/introspect
  - app:play
  - scene:07_keelson_introspection
  - table:keelson/apps
  - table:keelson/env
assessment:
  - dimension: readiness
    rating: 7
    basis: [positioning:keelson, adr:0026, adr:0094, scene:07_keelson_introspection]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [positioning:keelson, adr:0026, adr:0094]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [positioning:keelson, adr:0026, adr:0094]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Running several apps in one process or across processes, each mounted under a manifest, talking over a bus whose subjects are capabilities, and able to answer questions about itself through SQL-readable tables. The customer is someone who has data in ClickHouse and wants a runtime that already knows how to host, isolate and observe the small apps they write against it.

Its competences: [[app-mounting-and-manifests]], [[bus-and-codec]], [[capability-subjects-and-grants]], [[introspection-tables]], [[background-tasks]], [[app-state-and-working-sets]], [[embedding-the-runtime]], [[instance-attribution-and-effects]], [[external-process-chokepoint]], [[one-render-goroutine]].

# Mechanisms

A manifest declares what an app is and what it may touch; the registry mounts it and observes its origin at registration. Messages cross an in-process or NATS bus under a codec the bus owns. Capability subjects and grants decide who may read which table and perform which operation. Introspection providers expose the registry, the environment and the ADR corpus as keelson tables. Background tasks, working sets, instance attribution and the host bootstrap are the smaller parts the positioning page lists.

# Evidence

The positioning page's rests-on table; the play scene that reads the introspection tables; the runtime's own test lanes.

# Boundaries

Not a web framework and not a service mesh: the runtime hosts apps that share a data substrate, and stops at the process boundary the external-process chokepoint guards.
