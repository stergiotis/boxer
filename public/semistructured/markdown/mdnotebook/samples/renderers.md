---
title: One cell per renderer
records:
  readings: {blob: renderers.blobs/9ba02b64606a2edcc8f72b2d350c6447.arrow, of: df43464a463301658a6b0fe576013fe4}
  hourly: {blob: renderers.blobs/cf2e3affcc5195b79de7d2d7ee7aad0c.arrow, of: 453f460a68226850bc1229b99b26ce44}
  slow_hours: {blob: renderers.blobs/fcb765d65ca0c3058c2ab090dcf6c8da.arrow, of: c330c13419617a25153313c420e7a0f4}
  spread: {blob: renderers.blobs/3cb8386c19c762df800a9343d077a85f.arrow, of: f2a21398a28f7641ec2fc9117bed2d41}
  calls: {blob: renderers.blobs/be1840478374b6f5f53178dd4784939d.arrow, of: 8f843e42714ecf28fc1e360c43192d74}
  disk: {blob: renderers.blobs/06cba3adc83f87f6b6c814aaff94167d.arrow, of: 1fea72609c612bf6d5ed241a9526fb31}
  frames: {blob: renderers.blobs/e82673f99a740dc5d99d0ae69d11dc06.arrow, of: 4bdd6166be3caed42fc01c5b0b86426e}
  offices: {blob: renderers.blobs/dc476d26913681e8e57586a3b6d94850.arrow, of: 80c0a380920da290163f54ebc0ef06de}
  tickets: {blob: renderers.blobs/c5472fce1bf09c56786087c358fb9168.arrow, of: cbc9c2297c8265f01c1cc5cb2ec9f792}
  barriers: {blob: renderers.blobs/1248eb72d92470f654532defde6b6e53.arrow, of: 1082e40e5071764e1518fe58d1b91e50}
  risks: {blob: renderers.blobs/4ca71109dd0ad63cc4436c6b955946bd.arrow, of: df75dbbb366ec95a554a89c645b1e36e}
  recovery: {blob: renderers.blobs/12227d2bf87e9b042d6eb40f9b559258.arrow, of: aae81858384f094c7574cb8013c64a66}
---

# One cell per renderer

Each section draws one renderer from the smallest record that satisfies it.
A renderer reads columns by name, so the cell's aliases are how a record is
fitted to it. Every record makes its own rows.

## Readings over time

```sql
SELECT toDateTime64(toDateTime('2026-11-02 00:00:00', 'UTC') + intDiv(number, 2) * 3600, 3, 'UTC') AS t,
       if(number % 2 = 0, 'emea', 'apac') AS region,
       round(300 + 180 * sin(intDiv(number, 2) / 4 + (number % 2)), 1) AS p99
FROM numbers(48)
```

^readings

### table

Any record; the columns as the cell names them.

```sql table
SELECT t AS hour, region, p99 AS "p99@gloss/duration;unit=ms" FROM readings LIMIT 4
```

### chart

`x` is the abscissa, `series` splits the rows into groups, and every number
left is a lane. With `x`, `y` and `z` it is a heatmap.

```sql chart
SELECT t AS x, region AS series, p99 FROM readings
```

### series

The first time column is the axis and every numeric column a lane, whatever
they are called. The record is wide: one row per instant.

```sql
SELECT t, maxIf(p99, region = 'emea') AS emea, maxIf(p99, region = 'apac') AS apac
FROM readings GROUP BY t ORDER BY t
```

^hourly

```sql series
SELECT * FROM hourly
```

### timeline

`_tl_time` places a mark; `_tl_time_end` makes it an interval and
`_tl_lane` puts it on a lane.

```sql
SELECT t AS begins, t + toIntervalMinute(40) AS ends, region
FROM readings WHERE p99 > 400 ORDER BY t
```

^slow-hours

```sql timeline
SELECT begins AS _tl_time, ends AS _tl_time_end, region AS _tl_lane FROM slow_hours
```

### dist

One row per distribution: its name as `series`, the count `n`, the
probabilities `ps` and the quantiles `qs` at them.

```sql
SELECT region AS series, count() AS n,
       [0.05, 0.25, 0.5, 0.75, 0.95] AS ps,
       quantilesExact(0.05, 0.25, 0.5, 0.75, 0.95)(p99) AS qs
FROM readings GROUP BY region ORDER BY region
```

^spread

```sql dist
SELECT * FROM spread
```

## Edges

```sql
SELECT source, target, calls
FROM values('source String, target String, calls UInt64',
  ('gateway', 'ingest', 9120), ('gateway', 'auth', 4410),
  ('ingest', 'queue', 9120), ('queue', 'writer', 9120),
  ('writer', 'store', 9120), ('auth', 'store', 4410))
```

^calls

### graphview

Reads the member named `edges`, with `source` and `target`. `vertices` and
`graph_opts` are optional members; a member with no FROM is a row of
settings.

```sql graphview
WITH edges AS (SELECT source, target FROM calls),
     graph_opts AS (SELECT 'hierarchical' AS layout)
SELECT * FROM edges
```

### network

The same `edges` member, laid out in layers.

```sql network
WITH edges AS (SELECT source, target FROM calls)
SELECT * FROM edges
```

### sankey

Reads the member named `flows`, with `source`, `target` and `value`.

```sql sankey
WITH flows AS (SELECT source, target, calls AS value FROM calls)
SELECT * FROM flows
```

## A hierarchy

```sql
SELECT path, bytes, age_days
FROM values('path String, bytes UInt64, age_days UInt32',
  ('var/log/ingest.log', 4200, 1), ('var/log/auth.log', 900, 3),
  ('var/lib/store/a.part', 18000, 40), ('var/lib/store/b.part', 12500, 12),
  ('var/lib/queue/wal', 6100, 0), ('etc/ingest.yaml', 2400, 200))
```

^disk

### files

Reads `path`. `size`, `mtime` and `is_dir` are read by name, and every other
column is a column of the listing.

```sql files
SELECT path, bytes AS size, age_days FROM disk
```

### icicle

A `stack` array with a `value`, or one row per node with `id`, `parent` and
`value`.

```sql
SELECT splitByChar('/', path) AS stack, bytes AS value, age_days FROM disk
```

^frames

```sql icicle
SELECT stack, value FROM frames
```

### treemap

The same two contracts as the icicle, drawn as area. `color` is a second
measure, a number or a category.

```sql treemap
SELECT stack, value, age_days AS color FROM frames
```

## Places

```sql
SELECT country, sites
FROM values('country String, sites UInt32',
  ('CH', 4), ('DE', 9), ('FR', 6), ('US', 12), ('JP', 3), ('BR', 2))
```

^offices

### world

A text column of ISO country codes or country names, and a number to fill
by.

```sql world
SELECT * FROM offices
```

## Work and risk

### kanban

```sql
SELECT lane, title, owner
FROM values('lane String, title String, owner String',
  ('open', 'Queue depth alarm is noisy', 'ops'),
  ('open', 'Writer retries are unbounded', 'storage'),
  ('doing', 'Move auth off the gateway host', 'platform'),
  ('done', 'Hourly p99 report', 'ops'))
```

^tickets

`lane` and `title`; `subtitle` is optional. Lanes come in first-seen order,
which the record's order decides.

```sql kanban
SELECT lane, title, owner AS subtitle FROM tickets
```

### bowtie

```sql
SELECT 'Ingest stops for a day' AS top_event, side, path, step, barrier, status
FROM values('side String, path String, step UInt8, barrier String, status String',
  ('prevention', 'Disk fills', 1, 'Usage alarm at 80%', 'effective'),
  ('prevention', 'Disk fills', 2, 'Retention job', 'degraded'),
  ('prevention', 'Bad deploy', 1, 'Canary stage', 'untested'),
  ('mitigation', 'Reports are late', 1, 'Replay from the queue', 'effective'),
  ('mitigation', 'Data is lost', 1, 'Off-site copy', 'failed'))
```

^barriers

`top_event`, `side` (`prevention` or `mitigation`) and `path`, one row per
barrier of a path; `step`, `barrier` and `status` describe it.

```sql bowtie
SELECT * FROM barriers
```

### riskmatrix

```sql
SELECT id, name, p_lo, p_hi, impact_lo, impact_hi
FROM values('id String, name String, p_lo Float64, p_hi Float64, impact_lo Float64, impact_hi Float64',
  ('R1', 'Store loses a volume', 0.02, 0.10, 200e3, 3e6),
  ('R2', 'Queue backlog over a day', 0.20, 0.60, 20e3, 400e3),
  ('R3', 'Credentials leaked', 0.03, 0.15, 500e3, 8e6))
```

^risks

`id` and two ranges, likelihood per year as `p_lo` and `p_hi`, impact as
`impact_lo` and `impact_hi`. The aliases say the impact's unit.

```sql riskmatrix
SELECT id, name, p_lo, p_hi,
       impact_lo AS "impact_lo@gloss/money;unit=EUR",
       impact_hi AS "impact_hi@gloss/money;unit=EUR"
FROM risks
```

### tolerance

```sql
SELECT service, tolerance_hours, scenario, recovery_hours
FROM values('service String, tolerance_hours Float64, scenario String, recovery_hours Float64',
  ('ingest', 8, 'region outage', 5), ('ingest', 8, 'bad deploy', 1),
  ('reports', 48, 'region outage', 30), ('reports', 48, 'bad deploy', 60))
```

^recovery

One row per service and scenario: `service`, `tolerance_hours`, `scenario`
and `recovery_hours`.

```sql tolerance
SELECT * FROM recovery
```
