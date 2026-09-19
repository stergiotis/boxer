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

# table glosses

Table — glosses (ADR-0186): the gloss(…) macro and explicit `label@gloss/…` aliases per column, a `-- play: gloss` directive binding a plain column by name, a Luhn ✓/✗ tone, a masked secret, a tagged id split into tag and counter, and a mistyped parameter refused out loud

```sql
-- play: gloss gloss/length;unit=m name:height
SELECT number AS n,
       toUInt64(12393906174523604992) + number + 1 AS `id@gloss/taggedid`,
       gloss(20 + number * 1.7, 'gloss/temperature', 'unit', 'C', 'label', 'temp'),
       1.5 + number * 0.31 AS height,
       ['4111111111111111', '4111111111111112', '378282246310005'][1 + number % 3] AS `card@gloss/luhn`,
       1024 * number * number AS `size@gloss/bytes`,
       'hunter2' AS `pw@gloss/masked`,
       toUnixTimestamp(now()) + number * 86400 AS `when@gloss/epoch`,
       number * 90500 AS `took@gloss/duration;unit=ms`,
       'https://example.com/' || toString(number) AS `link@gloss/url`,
       20 + number * 1.7 AS `oops@gloss/temperature;unti=C`
FROM numbers(12)
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"02_table_glosses"}
```
