---
type: reference
audience: contributor
status: draft
scene:
  launch: jackstay
  size: 1280x820
  requires: [clickhouse]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# jackstay — plan a sync, step by step

ADR-0259's wizard against one server, which it uses as both source and target
when no target is configured. The scene discovers the server, chooses the
`default` database, and plans its structure onto a renamed copy. It stops
before anything is written: the DDL, the diff and the sync each wait for a
click the scene does not make. What is listed is whatever the live server
holds.

```jsonl trace
{"do":"wait","name":"Discover","role":"button","settleMs":500}
{"do":"click","name":"Discover","role":"button"}
{"do":"read","valueContains":"on the source","role":"label","pattern":"(?P<dbs>\\d+) databases? on the source","settleMs":4000}
{"do":"expect","of":"dbs","min":1}
{"do":"capture","text":"jackstay-connect"}
{"do":"click","name":"2 Databases","role":"button"}
{"do":"click","name":"default","role":"check_box"}
{"do":"click","name":"Plan the structure","role":"button"}
{"do":"read","valueContains":" tables: ","role":"label","pattern":"^(?P<tables>\\d+) tables: ","settleMs":4000}
{"do":"expect","of":"tables","min":1}
{"do":"capture","text":"jackstay-structure"}
```
