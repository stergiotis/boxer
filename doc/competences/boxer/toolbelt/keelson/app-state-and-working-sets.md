---
name: App state and working sets
level: 3
parent_ids:
  - "[[keelson/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0148
  - adr:0185
  - adr:0105
  - pkg:public/keelson/runtime/appstate
  - pkg:public/keelson/runtime/statestore
  - app:appstate
  - applet:appstate-workingsets
  - applet:appstate-persist
  - scene:appstate
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0148, app:appstate, scene:appstate]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0148, app:appstate, scene:appstate]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0148, app:appstate, scene:appstate]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

An app's persisted state — column widths, open items, the records it is working on — lives in facts-bound stores the runtime owns, so the state outlives the process and is queryable like everything else.
