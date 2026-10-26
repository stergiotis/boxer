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

# table grouped

Table — the rows held grouped two levels deep from the picker on the pager row
(ADR-0336 M3): a band per group with its key and row count, the null group
at the edge, a fold by chevron, and the pager counting lines. Every gesture
goes through `set_table_options`, so the journal names them as the person's.
The query needs no table: the second column is NULL on every seventh row so
each bucket has a null group.

```sql
SELECT number % 3 AS bucket, if(number % 7 = 0, NULL, concat('k', toString(number % 2))) AS kind, number AS n
FROM numbers(24)
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"wait","valueContains":"rows 1–24 of 24","role":"label","comment":"flat: the pager counts rows"}

{"do":"note","text":"--- group by the first column from the picker ---"}
{"do":"click","role":"combo_box","value":"+ column","settleMs":400}
{"do":"click","name":"+ bucket","settleMs":400,"comment":"the picker lists the drawn columns not yet grouped"}
{"do":"key","text":"Escape","settleMs":300,"comment":"the combo's popup stays open after a pick; Escape closes it, as the chat scene does"}
{"do":"wait","valueContains":"rows 1–27 of 27","role":"label","comment":"three bucket groups and 24 rows: the pager counts lines"}

{"do":"note","text":"--- a chevron folds its group; the pager follows ---"}
{"do":"click","name":"","nth":0,"comment":"Phosphor caret-down on the first band, bucket 0, whose eight rows fold away"}
{"do":"wait","valueContains":"rows 1–19 of 19","role":"label"}
{"do":"click","name":"collapse all"}
{"do":"wait","valueContains":"rows 1–3 of 3","role":"label","comment":"the three bands alone"}
{"do":"click","name":"expand all"}
{"do":"wait","valueContains":"rows 1–27 of 27","role":"label"}

{"do":"note","text":"--- a second level nests in the first; its null group sits last in each bucket ---"}
{"do":"click","role":"combo_box","value":"+ column","settleMs":400}
{"do":"click","name":"+ kind","settleMs":400}
{"do":"key","text":"Escape","settleMs":300}
{"do":"wait","valueContains":"rows 1–36 of 36","role":"label","comment":"24 rows, 3 buckets, 3 kinds under each: k0, k1 and the null group"}
{"do":"click","name":"bucket \ue13c","comment":"the level's chip (its caret is the Phosphor glyph, the header's a text triangle) turns its groups around"}
{"do":"wait","name":"bucket \ue136","role":"button"}
{"do":"wait","valueContains":"rows 1–36 of 36","role":"label","comment":"the same lines, the buckets reversed"}
{"do":"capture","text":"02_table_grouped","comment":"two levels of bands over the rows, the aggregate-free band carrying key and count, the null group's weak label"}

{"do":"note","text":"--- the chip's third state drops the level: the single-level lines come back ---"}
{"do":"click","name":"kind \ue13c"}
{"do":"wait","name":"kind \ue136","role":"button"}
{"do":"click","name":"kind \ue136"}
{"do":"wait","valueContains":"rows 1–27 of 27","role":"label"}
```
