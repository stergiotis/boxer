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
    BOXER_PLAY_FOCUS_WORLD: "1"
  requires: ["clickhouse", "table:default.planes_mercator_sample100"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# world choropleth

World — the country choropleth (ADR-0114): a string column detected as country-shaped, values binned onto the atlas

```sql
SELECT country, count() AS flights, round(avg(altitude)) AS avg_alt
FROM (
  SELECT multiIf(lon < 6, 'FR', lon < 10, 'CH', lon < 14, 'AT', lon < 20, 'IT', 'DE') AS country, altitude
  FROM default.planes_mercator_sample100
  WHERE altitude > 0
  LIMIT 200000
)
GROUP BY country
ORDER BY flights DESC
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":3000}
{"do":"capture","text":"06_world_choropleth","settleMs":600}
{"do":"click","role":"combo_box","value":"Natural Earth","settleMs":400}
{"do":"click","name":"Equal Earth","settleMs":500}
{"do":"key","text":"Escape","settleMs":400}
{"do":"capture","text":"06_world_equal_earth","settleMs":1500}
```
