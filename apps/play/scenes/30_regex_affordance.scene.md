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

Affordances — the inline regex tester the editor attaches to a recognised multiMatch* call: each pattern compiled in-process and counted against a shared test input. The scene types the input and asserts the first pattern's count

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
{"do":"wait","value":"test input","role":"label","comment":"the affordance block is drawn"}
{"do":"type","name":"Regex test input","role":"text_input","text":"UNITED AIRLINES INC / DELTA AIR LINES INC TRUSTEE","comment":"the shared test input every regex affordance reads"}
{"do":"read","valueContains":"match(es)","role":"label","nth":0,"pattern":"→ (?P<matches>\\d+) match","comment":"the first pattern, AIR ?LINES, against the input"}
{"do":"expect","of":"matches","eq":2}
{"do":"hover","value":"AFFORDANCES","role":"label","comment":"park the pointer over the affordance block; a wheel step goes to whatever is under it"}
{"do":"scroll","x":0,"y":-200,"settleMs":500}
{"do":"capture","text":"30_regex_affordance","settleMs":800}
```
