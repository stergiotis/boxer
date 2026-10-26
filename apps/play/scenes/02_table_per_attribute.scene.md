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
  requires: ["clickhouse", "table:boxer.facts"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# table per attribute

Table — a leeway result in the per-attribute granularity (ADR-0336 §SD12,
M3b): the page's rows exploded one row per attribute into a record the grid
groups by the entity and then by the membership path. A band per row of the
result carries its number and attribute count and selects the row when
clicked; a path's steps nest under it; the pager still counts the result's
rows. What paths appear depends on the facts the server holds.

```sql
SELECT * FROM boxer.facts LIMIT 12
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":2500}
{"do":"wait","valueContains":"rows 1–12 of 12","role":"label","comment":"the per-DB-row grid, twelve rows"}

{"do":"note","text":"--- the Leeway display bar's granularity switch explodes the page ---"}
{"do":"click","name":"per attribute","settleMs":600}
{"do":"wait","value":"row 1","role":"label","comment":"the first entity's band; a band is a label, not a cell"}
{"do":"wait","valueContains":"rows 1–12 of 12","role":"label","comment":"the pager counts the result's rows, not the exploded lines"}
{"do":"capture","text":"02_table_per_attribute","comment":"bands per entity over their attributes, the path's steps nested, sections stacked"}

{"do":"note","text":"--- a band click selects the entity; the Detail pane follows ---"}
{"do":"click","value":"row 2","role":"label","pointer":true,"comment":"the band's label; its row block reports the click as a selection"}
{"do":"wait","valueContains":"row 2 / 12","role":"label","comment":"the Detail pane's header names the selected row"}

{"do":"note","text":"--- the chevron folds the entity ---"}
{"do":"click","name":"","nth":0,"comment":"Phosphor caret-down on the first open band, row 1's; the chevrons of its path groups come after it"}
{"do":"wait","value":"row 3","role":"label","settleMs":400,"comment":"with row 1 folded, the third band is on screen; the fold went through set_table_options"}
```
