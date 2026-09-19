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
    BOXER_PLAY_FOCUS_TIMELINE: "1"
    BOXER_PLAY_TIMELINE_BANDS_SQL: |-
      WITH {tl_min:DateTime64(3, 'UTC')} AS lo,
           {tl_max:DateTime64(3, 'UTC')} AS hi
      SELECT lo                                                                AS _tl_band_from,
             addMilliseconds(lo, toInt64(0.5 * dateDiff('millisecond', lo, hi))) AS _tl_band_to,
             'info.subtle'                                                     AS _tl_band_color,
             'first half'                                                      AS _tl_band_label
      UNION ALL
      SELECT addMilliseconds(lo, toInt64(0.5 * dateDiff('millisecond', lo, hi))),
             hi,
             'accent.subtle',
             'second half'
  requires: ["clickhouse", "table:anchor.facts"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# timeline

Timeline — the interval contract (_tl_time + _tl_time_end + _tl_lane) drawn as lanes, with a background bands channel

```sql
SELECT
  `timeRange:beginIncl`[1] AS _tl_time,
  `timeRange:endExcl`[1]   AS _tl_time_end,
  `symbol:value`[1]        AS _tl_lane
FROM anchor.facts
WHERE length(`timeRange:beginIncl`) > 0
ORDER BY _tl_time
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"04_timeline"}
```
