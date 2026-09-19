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

# table regexp

Table + Detail — gloss/regexp (ADR-0186): stored RE2 patterns with the verdict Go's engine gives them inline, and in Detail the pattern highlighted (group parens by nesting depth) with the anchor that opens the regex explorer tethered to the cell

```sql
SELECT ['phone','email','broken'][1 + number % 3] AS kind,
       ['^(\\d{3})-(\\d{4})$', '\\w+@\\w+\\.\\w+', '\\d{3}-(\\d{4}'][1 + number % 3] AS `rule@gloss/regexp`
FROM numbers(9)
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"02_table_regexp","settleMs":600}
{"do":"click","name":"email","nth":0}
{"do":"capture","text":"02_table_regexp_detail","settleMs":600}
{"do":"click","x":1392,"y":778,"comment":"the anchor toggle under the highlighted pattern; a glyph in a Frame, so it carries no accessibility name to resolve by"}
{"do":"capture","text":"02_table_regexp_explorer","settleMs":900}
```
