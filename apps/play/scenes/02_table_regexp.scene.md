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

Table + Detail — gloss/regexp (ADR-0186): stored RE2 patterns with the verdict Go's engine gives them inline, and in Detail the pattern highlighted (group parens by nesting depth) with the anchor that opens the regex explorer tethered to the cell. The scene asserts the row count, opens the explorer from the anchor by its name, and ends on play's own help

```sql
SELECT ['phone','email','broken'][1 + number % 3] AS kind,
       ['^(\\d{3})-(\\d{4})$', '\\w+@\\w+\\.\\w+', '\\d{3}-(\\d{4}'][1 + number % 3] AS `rule@gloss/regexp`
FROM numbers(9)
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"read","valueContains":"rows · ","role":"label","pattern":"^(?P<rows>\\d+) rows ·","comment":"the status bar's result line"}
{"do":"expect","of":"rows","eq":9}
{"do":"capture","text":"02_table_regexp","settleMs":600}
{"do":"click","name":"email","nth":0}
{"do":"capture","text":"02_table_regexp_detail","settleMs":600}
{"do":"click","name":"regex: rule","role":"unknown","pointer":true,"comment":"the anchor toggle under the highlighted pattern, named after the window it opens; a glyph in a Frame, so the press lands where it is drawn"}
{"do":"wait","contains":"regex: rule","role":"window","comment":"the explorer opened, tethered to the cell"}
{"do":"capture","text":"02_table_regexp_explorer","settleMs":900}
{"do":"note","text":"--- F1 over the window opens play's own help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"The SQL Playground","role":"label","comment":"the reader shows play's overview, not the index"}
{"do":"capture","text":"02_table_regexp_help"}
```
