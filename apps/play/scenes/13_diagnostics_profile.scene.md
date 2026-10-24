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
    BOXER_PLAY_FOCUS_DIAGNOSTICS: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# diagnostics profile

Diagnostics — the Execution profile: the active result's query run again under EXPLAIN ANALYZE, its costliest steps named and toned in the plan

```sql
SELECT number % 10 AS k, count() AS n
FROM numbers(10000000)
WHERE number % 3 = 0
GROUP BY k
ORDER BY k
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted and the autorun finished"}
{"do":"wait","name":"Profile the active result","role":"button"}
{"do":"click","name":"Profile the active result","role":"button"}
{"do":"wait","valueContains":"Most of the time went to","role":"label"}
{"do":"scroll_into_view","name":"Profile again","role":"button"}
{"do":"capture","text":"13_diagnostics_profile_head"}
{"do":"scroll_into_view","valueContains":"Shares are of the query","role":"label"}
{"do":"capture","text":"13_diagnostics_profile","sidecars":["tree"]}
```
