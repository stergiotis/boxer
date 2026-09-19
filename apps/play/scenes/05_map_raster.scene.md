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
    BOXER_PLAY_FOCUS_MAP: "1"
    BOXER_PLAY_MAP_TABLE: "planes_mercator_sample100"
    BOXER_PLAY_MAP_CENTER: "47.4,8.5"
    BOXER_PLAY_MAP_ZOOM: "7"
  requires: ["clickhouse", "table:default.planes_mercator_sample100"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# map raster

Map — an in-database mercator raster (ADR-0096) over 350k ADS-B positions, rendered server-side and panned/zoomed client-side

```sql
SELECT icao, r, t, lat, lon, altitude, ground_speed
FROM default.planes_mercator_sample100
LIMIT 500
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":4000}
{"do":"capture","text":"05_map_raster"}
```
