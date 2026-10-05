---
type: reference
audience: contributor
status: draft
scene:
  launch: appcenter
  size: 1300x900
  requires: [clickhouse]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# appcenter — one page per app

ADR-0260's app center: the registered apps on the left, and the selected
app's page on the right, assembled from the introspection tables that name
it. The scene selects the app-state manager, which declares a read grant and
keeps no state of its own, and then the SQL playground. It asserts that the
list was read over the bus and that the page's sections render, the three
cross-run ones included. What each section lists depends on the trail the
host's store holds. Last it opens the Runs section in play, which draws the app's sessions on
its Timeline tab from the introspection endpoint.

```jsonl trace
{"do":"wait","contains":"Refresh","role":"button","settleMs":500}
{"do":"read","valueContains":"apps registered","role":"label","pattern":"(?P<apps>\\d+) apps registered","settleMs":4000}
{"do":"expect","of":"apps","min":2}
{"do":"click","contains":"App state","role":"button","nth":0}
{"do":"wait","name":"Capabilities","settleMs":2000}
{"do":"wait","name":"ADRs its code cites"}
{"do":"read","valueContains":"Declared by the manifest","role":"label","pattern":"(?P<declared>Declared)"}
{"do":"wait","name":"Runs"}
{"do":"wait","name":"Audited requests"}
{"do":"capture","text":"appcenter-appstate"}
{"do":"click","contains":"SQL playground","role":"button","nth":0}
{"do":"wait","name":"Logs","settleMs":3000}
{"do":"capture","text":"appcenter-play"}
{"do":"click","name":"Runs"}
{"do":"capture","text":"appcenter-play-logs","settleMs":500}
{"do":"click","name":"Runs"}
{"do":"click","id":12875594712639319915,"comment":"the Runs section's Open in play; every section's button shares the name"}
{"do":"read","valueContains":"rows ·","role":"label","pattern":"(?P<rows>\\d+) rows","comment":"play has run the statement"}
{"do":"expect","of":"rows","min":1}
{"do":"capture","text":"appcenter-open-in-play","settleMs":1000}
```
