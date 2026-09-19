---
type: reference
audience: contributor
status: draft
scene:
  launch: play
  size: 1920x1200
  stepSettleMs: 350
  env:
    BOXER_PLAY_WINDOW_SIZE: "900x640"
    BOXER_PLAY_AUTORUN: "1"
    BOXER_PLAY_FOCUS_CHART: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chart small pane

Chart in a SMALL window: the plot box follows the pane instead of a fixed height, so a top-N ranking keeps its zero baseline and every category label

```sql
SELECT * FROM values(
  'x String, rows UInt64',
  ('alpha', 1200000), ('bravo', 980000), ('charlie', 870000), ('delta', 610000),
  ('echo', 520000), ('foxtrot', 348603), ('golf', 300000), ('hotel', 280000),
  ('india', 250000), ('juliett', 210000), ('kilo', 190000), ('lima', 170000),
  ('mike', 150000), ('november', 130000), ('oscar', 110000))
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"08_chart_small_pane"}
```
