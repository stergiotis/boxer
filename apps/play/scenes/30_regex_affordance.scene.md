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
  requires: ["clickhouse", "table:default.planes_mercator_sample100"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# regex affordance

Affordances — the inline regex tester the editor attaches to a recognised multiMatch* call: each pattern compiled in-process and counted against a shared test input

```sql
SELECT ownOp     AS operator,
       uniq(icao) AS aircraft,
       count()    AS pings
FROM default.planes_mercator_sample100
WHERE multiMatchAny(ownOp, ['AIR ?LINES', '^(FEDERAL|UNITED)', 'TRUSTEE$'])
GROUP BY operator
ORDER BY pings DESC
LIMIT 20
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"focus","role":"text_input","nth":1,"comment":"the shared test input — nameless, so by index","settleMs":400}
{"do":"type","role":"text_input","nth":1,"text":"UNITED AIRLINES INC / DELTA AIR LINES INC TRUSTEE"}
{"do":"click","x":600,"y":520,"comment":"park the pointer over the affordance block"}
{"do":"scroll","x":0,"y":-200,"settleMs":500}
{"do":"capture","text":"30_regex_affordance","settleMs":800}
```
