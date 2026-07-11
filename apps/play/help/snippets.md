---
type: reference
audience: end-user
status: draft
title: Snippets
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Snippets

A small library of ready-to-run queries against `anchor.facts`, the
self-contained demo table. Click **Insert** above any block to splice it into
the editor at the cursor; place the caret where you want it first (an empty
editor takes the snippet whole). Do not add a `FORMAT` clause — the app appends
one. The verified, explained versions live on the **Example queries** page. The
**ADS-B geo-raster** section below targets `planes_mercator` from the demo loader
(`apps/play/demo/adsb/demo.sh`), not `anchor.facts`.

## Whole entities

`SELECT *` returns the full leeway row, so the Detail tab renders the leeway
card. Run it, then click a row in Table.

```sql
SELECT * FROM anchor.facts LIMIT 50
```

## One scenario

Narrow to a single scenario to vary which tagged sections are populated.

```sql
SELECT * FROM anchor.facts
WHERE hasAny(`tv:symbol:value:val:s:m:0:24:0::data`,
             ['DDOS', 'PORT_SCAN', 'SQL_INJECTION'])
```

## Query graph (CTEs become nodes)

Each top-level CTE splits into its own node of the reactive query-graph
(ADR-0097): `recent` and `by_kind` below become nodes — `by_kind` reads
`recent`, and the final `SELECT` reads `by_kind`. The chain fuses back into a
single query for execution (identical to running it inline); the **Graph** tab
shows the nodes and their edges, and its *observe in panels* button points the
result tabs at an intermediate node instead of the sink.

```sql
WITH
  recent AS (
    SELECT * FROM anchor.facts LIMIT 50
  ),
  by_kind AS (
    SELECT
      `tv:symbol:value:val:s:m:0:24:0::data`[1] AS event_type,
      count()                                   AS n
    FROM recent
    GROUP BY event_type
  )
SELECT event_type, n
FROM by_kind
ORDER BY n DESC
```

## Recursive CTEs (WITH RECURSIVE)

A `WITH RECURSIVE` CTE may reference its own name: a seed branch and a
recursive branch combined with `UNION ALL` (needs a server with recursive-CTE
support, ClickHouse ≥ 24.4). The Graph tab shows such a node as
`CTE (recursive)` — the self-reference stays inside the node rather than
becoming a graph edge (ADR-0097 §SD9). Table-free, so it runs against any
endpoint:

```sql
WITH RECURSIVE fib AS (
  SELECT 1 AS n, toUInt64(0) AS a, toUInt64(1) AS b
  UNION ALL
  SELECT n + 1, b, a + b FROM fib WHERE n < 40
)
SELECT n, a AS fib FROM fib
```

A recursive series also works as a spine for downstream CTEs — the calendar
idiom. `days` is the recursive generator, `by_week` aggregates it, and
"observe in panels" on either node in the Graph tab materialises it standalone:

```sql
WITH RECURSIVE days AS (
  SELECT toDate('2026-01-01') AS day
  UNION ALL
  SELECT day + 1 FROM days WHERE day < toDate('2026-01-31')
),
by_week AS (
  SELECT toStartOfWeek(day) AS week, count() AS days_in_week
  FROM days
  GROUP BY week
)
SELECT week, days_in_week FROM by_week ORDER BY week
```

## One entity by id

The ids 10005, 10010, 10015, 10020, 500003 carry the sparse `geoArea` section.

```sql
SELECT * FROM anchor.facts WHERE `id:id:u64:2k:0:0:` = 10005
```

## Timeline contract

Map the `timeRange` section onto the canonical slot columns the Timeline tab
reads; timestamps must be `DateTime64`.

```sql
SELECT
  `tv:timeRange:beginIncl:val:z64:2k:0:0:0::data`[1] AS _tl_time,
  `tv:timeRange:endExcl:val:z64:2k:0:0:0::data`[1]   AS _tl_time_end,
  `tv:symbol:value:val:s:m:0:24:0::data`[1]          AS _tl_lane
FROM anchor.facts
WHERE length(`tv:timeRange:beginIncl:val:z64:2k:0:0:0::data`) > 0
ORDER BY _tl_time
```

## Timeline regions (background bands)

Unlike the others, this block belongs in the Timeline tab's **Background bands**
editor, not the main editor — Insert here would put it in the wrong box. Bands
return the `_tl_band_*` slots: a `from`/`to` `DateTime64` pair, a
`_tl_band_color` that must be an IDS token name (`neutral.default`,
`accent.default`, `warning.default`, `error.default`, …), and an optional
`_tl_band_label`. The `{tl_min:…}` / `{tl_max:…}` parameters carry the
events' time extent — the Timeline publishes them as signals after each
render — so a band can be sized relative to whatever the query returned. A
bands query that doesn't reference them runs on its own, without waiting for
events. This one shades the middle 50% of the visible window — adjust the
`0.25` / `0.75` fractions to move or resize the region.

```sql
WITH {tl_min:DateTime64(3, 'UTC')} AS lo,
     {tl_max:DateTime64(3, 'UTC')} AS hi
SELECT
  addMilliseconds(lo, toInt64(0.25 * dateDiff('millisecond', lo, hi))) AS _tl_band_from,
  addMilliseconds(lo, toInt64(0.75 * dateDiff('millisecond', lo, hi))) AS _tl_band_to,
  'accent.default'                                                     AS _tl_band_color,
  'mid 50% of window'                                                  AS _tl_band_label
```

## Ad-hoc columns

Aliased columns are not leeway-shaped, so Detail falls back to prefix grouping
and Table shows a plain grid.

```sql
SELECT
  `id:id:u64:2k:0:0:`                       AS id,
  `id:naturalKey:y:g:0:0:`                  AS natural_key,
  `tv:symbol:value:val:s:m:0:24:0::data`[1] AS event_type
FROM anchor.facts
ORDER BY id
```

## Parameter prelude

A top-level `SET param_*` statement rides the request URL; the `{name:Type}`
placeholder is substituted by ClickHouse.

```sql
SET param_event = 'DDOS';
SELECT * FROM anchor.facts
WHERE has(`tv:symbol:value:val:s:m:0:24:0::data`, {event:String})
```

## Signals (unbound parameter)

A placeholder with no `SET` is a live signal. Run refuses while nothing fills
`lim` (the status bar says what to do): open the Graph tab's **signals**
section, give `lim` a value, press **set**, then Run. Check **Live** in the
top bar and further **set** clicks re-run the query by themselves.

```sql
SELECT * FROM anchor.facts LIMIT {lim:UInt64}
```

## World choropleth (countries)

A result with a country column (ISO 3166 alpha-2/alpha-3 codes or country
names) plus a numeric column lights up the **World** tab (ADR-0114): countries
fill by value, hover shows `name · value`, clicking a country selects its row.
Table-free — the `values()` literal runs against any server. The `'XK'` row
exercises the code path, and `'atlantis'` deliberately resolves nowhere so the
status line shows an unmatched count. Values are illustrative, not
authoritative statistics.

```sql
SELECT *
FROM values('country String, population Float64',
  ('Germany', 83.2), ('France', 68.1), ('Brazil', 216.4),
  ('United States', 334.9), ('India', 1428.6), ('China', 1411.8),
  ('Australia', 26.5), ('Norway', 5.5), ('Egypt', 112.7),
  ('Japan', 124.5), ('Mexico', 128.5), ('Russia', 144.4),
  ('South Africa', 60.4), ('Canada', 40.1), ('Argentina', 46.7),
  ('XK', 1.7), ('atlantis', 0.1))
```

## ADS-B geo-raster (demo loader)

These target `planes_mercator`, the aircraft-position table loaded by
`apps/play/demo/adsb/demo.sh` (see its README) — point play's endpoint at that
local ClickHouse first. The **Map** tab renders the raster visually (ADR-0096);
the queries here run in the main editor.

Positions per 5,000-ft altitude band:

```sql
SELECT intDiv(altitude, 5000) * 5000 AS alt_band_ft, count() AS positions
FROM planes_mercator
GROUP BY alt_band_ft
ORDER BY alt_band_ft
```

Busiest aircraft types in the loaded slice:

```sql
SELECT t AS type, count() AS positions, uniqExact(icao) AS aircraft,
       round(avg(altitude)) AS avg_alt_ft
FROM planes_mercator
WHERE t != ''
GROUP BY type
ORDER BY positions DESC
LIMIT 20
```

The Map tab's raster query as a snippet (ADR-0096 §SD6): it bins the visible
points into a `W×H` grid and derives an RGBA value per pixel, so it returns one
row per pixel — a `W*H`-row framebuffer, not a readable table (the **Map** tab
draws it). Here the viewport is fixed to a Zürich box at 256×256; the Map tab
injects the live viewport instead.

```sql
WITH
  45.5 AS min_lat, 49.0 AS max_lat, 5.5 AS min_lon, 12.0 AS max_lon,
  256 AS W, 256 AS H, 100 AS sampling,
  toUInt32(0xFFFFFFFF * ((min_lon + 180) / 360)) AS min_x,
  toUInt32(0xFFFFFFFF * ((max_lon + 180) / 360)) AS max_x,
  toUInt32(0xFFFFFFFF * (1/2 - log(tan((max_lat + 90) / 360 * pi())) / 2 / pi())) AS min_y,
  toUInt32(0xFFFFFFFF * (1/2 - log(tan((min_lat + 90) / 360 * pi())) / 2 / pi())) AS max_y,
  toUInt64(max_x) - min_x AS span_x,
  toUInt64(max_y) - min_y AS span_y,
  mercator_x >= min_x AND mercator_x < max_x
    AND mercator_y >= min_y AND mercator_y < max_y AS in_view,
  least((toUInt64(mercator_x - min_x) * W) DIV span_x, W - 1) AS px,
  least((toUInt64(mercator_y - min_y) * H) DIV span_y, H - 1) AS py,
  py * W + px AS pos,
  (span_x / W) * (span_y / H) AS pixel_area,
  pow(2, 22) / sqrt(pixel_area) AS zoom_factor,
  count() AS total,
  greatest(1000000. / sampling / zoom_factor, toFloat64(count())) AS max_total,
  pow(total / max_total, 1/5) AS transparency,
  greatest(0, least(avg(altitude), 5000)) / 5000 AS color1,
  greatest(0, least(avg(altitude), 50000)) / 50000 AS color3,
  greatest(0, least(avg(ground_speed), 700)) / 700 AS color2,
  255 AS alpha,
  (1 + transparency) / 2 * (1 - color3) * 255 AS red,
  transparency * color1 * 255 AS green,
  color2 * 255 AS blue
SELECT round(red)::UInt8 AS r, round(green)::UInt8 AS g,
       round(blue)::UInt8 AS b, round(alpha)::UInt8 AS a
FROM planes_mercator
WHERE in_view
GROUP BY pos
ORDER BY pos WITH FILL FROM 0 TO toUInt64(W) * H
```

## Trivial states

```sql
SELECT * FROM anchor.facts LIMIT 1
```

```sql
SELECT 1 AS hello, now() AS ts
```
