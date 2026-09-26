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
holds. The plan file the wizard names for itself lands under
`BOXER_JACKSTAY_PLAN_DIR`.

```jsonl trace
{"do":"wait","name":"Discover the servers","role":"button","settleMs":500}
{"do":"click","name":"Discover the servers","role":"button"}
{"do":"read","valueContains":"ClickHouse","role":"label","nth":0,"pattern":"^(source|target) .* (?P<dbs>\\d+) databases?, up ","settleMs":4000}
{"do":"expect","of":"dbs","min":1}
{"do":"capture","text":"jackstay-connect"}
{"do":"click","name":"2 Databases","role":"button"}
{"do":"capture","text":"jackstay-databases"}
{"do":"click","name":"default","role":"check_box"}
{"do":"click","name":"Plan the structure","role":"button"}
{"do":"read","valueContains":" tables:","role":"label","pattern":"^(?P<tables>\\d+) tables?:$","settleMs":4000}
{"do":"expect","of":"tables","min":1}
{"do":"capture","text":"jackstay-structure"}
{"do":"click","contains":"Next: Differences","role":"button"}
{"do":"wait","name":"Compare content","role":"button","settleMs":500}
{"do":"capture","text":"jackstay-differences"}
{"do":"click","name":"5 Sync","role":"button"}
{"do":"read","valueContains":"Recommended:","role":"label","pattern":"^Recommended: (?P<mode>full|repair|sample), because","settleMs":4000}
{"do":"capture","text":"jackstay-sync"}
```
