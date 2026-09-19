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
    BOXER_PLAY_FOCUS_PASSES: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# passes cost

Passes — Rewrite cost on a buffer that is expensive to compile: the run split (compile vs server), the per-pass waterfall under it, and which sub-passes of the costliest unit rewrote nothing

```sql
WITH a AS (SELECT number AS n FROM numbers(10)),
b AS (SELECT n + 1 AS n FROM a WHERE n % 2 = 0),
c AS (SELECT n * 2 AS n FROM b WHERE n > 0),
d AS (SELECT n + if(n > 3, 1, 2) AS n FROM c),
e AS (SELECT n, [n, n+1, n+2] AS arr FROM d),
f AS (SELECT n, arrayMap(x -> x * 2, arr) AS arr FROM e),
g AS (SELECT n, arrayFilter(x -> x % 3 = 0, arr) AS arr FROM f),
h AS (SELECT n, CASE WHEN n > 5 THEN 'hi' WHEN n > 2 THEN 'mid' ELSE 'lo' END AS bucket FROM g),
i AS (SELECT n, bucket, toString(n) || '-' || bucket AS k FROM h),
j AS (SELECT n, k, cast(n AS Float64) AS fn FROM i),
k AS (SELECT n, fn, if(fn > 4.0, fn * 1.5, fn / 2) AS w FROM j),
l AS (SELECT n, w, multiIf(w > 8, 'big', w > 4, 'med', 'small') AS sz FROM k)
SELECT n, w, sz FROM l ORDER BY n LIMIT 20
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":4000}
{"do":"capture","text":"12_passes_cost","settleMs":600}
{"do":"click","x":1400,"y":450,"comment":"park the pointer inside the Passes body"}
{"do":"scroll","x":0,"y":-320,"settleMs":500}
{"do":"capture","text":"12_passes_cost_waterfall","settleMs":600}
```
