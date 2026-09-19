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

# map custom color

Map — the Custom render's colour expression (ADR-0096 §SD6): the editor folded behind its own header, filling the panel width, and pinned to a fixed pane so a long expression scrolls inside it rather than shrinking the raster under it

```sql
SELECT icao, lat, lon, altitude, ground_speed
FROM default.planes_mercator_sample100
LIMIT 500
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":4000}
{"do":"click","role":"combo_box","value":"Altitude & Velocity","settleMs":400}
{"do":"click","name":"Custom","settleMs":400}
{"do":"key","text":"Escape","settleMs":400}
{"do":"capture","text":"05_map_custom_color","settleMs":1200}
{"do":"focus","role":"multiline_text_input","valueContains":"transparency * 255 AS red","settleMs":400}
{"do":"click","role":"multiline_text_input","valueContains":"transparency * 255 AS red","settleMs":800}
{"do":"type","role":"multiline_text_input","valueContains":"transparency * 255 AS red","text":" , 1 AS pad01, 2 AS pad02, 3 AS pad03, 4 AS pad04, 5 AS pad05, 6 AS pad06, 7 AS pad07, 8 AS pad08, 9 AS pad09, 10 AS pad10, 11 AS pad11, 12 AS pad12, 13 AS pad13, 14 AS pad14, 15 AS pad15, 16 AS pad16, 17 AS pad17, 18 AS pad18, 19 AS pad19, 20 AS pad20, 21 AS pad21, 22 AS pad22, 23 AS pad23, 24 AS pad24, 25 AS pad25, 26 AS pad26, 27 AS pad27, 28 AS pad28, 29 AS pad29, 30 AS pad30, 31 AS pad31, 32 AS pad32, 33 AS pad33, 34 AS pad34, 35 AS pad35, 36 AS pad36, 37 AS pad37, 38 AS pad38, 39 AS pad39, 40 AS pad40, 41 AS pad41, 42 AS pad42, 43 AS pad43, 44 AS pad44, 45 AS pad45, 46 AS pad46, 47 AS pad47, 48 AS pad48, 49 AS pad49, 50 AS pad50","settleMs":1500}
{"do":"capture","text":"05_map_custom_color_long","settleMs":1500}
```
