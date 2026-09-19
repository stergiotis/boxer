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

# params expr

Parameters — a SQL-valued knob (ADR-0187): {cond:Expr} and {cols:ExprList} take a whole SQL fragment, edited in a lexically-coloured field and substituted client-side before the body ships, while {col:Identifier} rides ClickHouse's own parameter channel and keeps its pin

```sql
SET param_col = 'number';
-- play: expr cond = number % 3 = 0
-- play: expr cols = number AS n, number * 2 AS doubled
SELECT {cols:ExprList}, {col:Identifier}
FROM numbers(12)
WHERE {cond:Expr}
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"19_params_expr"}
```
