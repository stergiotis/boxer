---
type: reference
audience: contributor
status: draft
scene:
  launch: play
  size: 1920x1200
  stepSettleMs: 350
  env:
    BOXER_PLAY_WINDOW_SIZE: "1888x1100"
    BOXER_PLAY_AUTORUN: "1"
    BOXER_PLAY_FOCUS_TABLE: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# table taggedid

Table + Detail — gloss/taggedid (ADR-0186): a fibonacci-tagged id (ADR-0106) shown as tag value and counter in hex, and spelled out in Detail with Copy buttons; a UInt64 carrying no comma is refused in the warning tone

```sql
SELECT number AS n,
       toUInt64(12393906174523604992) + number + 1 AS `id@gloss/taggedid`,
       toUInt64(13835058055282163712) + number + 1 AS `hot@gloss/taggedid`,
       toUInt64(4294967296) AS `oops@gloss/taggedid`
FROM numbers(8)
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"02_table_taggedid","settleMs":600}
{"do":"click","name":"c:5"}
{"do":"capture","text":"02_table_taggedid_detail","settleMs":600}
```
