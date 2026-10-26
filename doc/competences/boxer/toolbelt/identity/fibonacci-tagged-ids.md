---
name: Fibonacci-tagged ids
level: 3
parent_ids:
  - "[[identity/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0106
  - pkg:public/identity/fibonacci
  - pkg:public/identity/fibonaccicode
  - howto:fibonacci-tagged-ids
  - app:fibscope
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0106, howto:fibonacci-tagged-ids, app:fibscope]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0106, howto:fibonacci-tagged-ids, app:fibscope]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0106, howto:fibonacci-tagged-ids, app:fibscope]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

One integer carries a self-delimiting tag and a body, so a reader can tell an id's kind from its value alone and a table can hold ids of many kinds in one column.
