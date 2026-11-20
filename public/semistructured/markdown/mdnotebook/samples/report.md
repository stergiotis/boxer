---
notebook: 1
title: Ingest lag, week 45
records:
  p99_by_hour: {blob: report.blobs/e0d96558806a6208bcfabf5bab4c1184.arrow, of: 86634fdcd341e2110f295f3089e96734}
  stats: {blob: report.blobs/41b266438286b705045d1f8383bb63a7.arrow, of: ef46b42f7851a10102df52ab01aa8307}
  by_region: {blob: report.blobs/3271b1d07bc7c8c069004471b2d14d39.arrow, of: bd66b0d6e81a9c0b14b1a6d53030f3ce}
  slowest: {blob: report.blobs/d71298792106241bc22c5fae0bc9651a.arrow, of: 5a9b99f58babc9c57a2fbad818189ef4}
  calls: {blob: report.blobs/be1840478374b6f5f53178dd4784939d.arrow, of: 8f843e42714ecf28fc1e360c43192d74}
---

# Ingest lag, week 45

The worst hour of the week reached [519.5 ms](val:stats.worst_ms "gloss/duration;unit=ms");
the mean over [48](val:stats.hours) hours was
[299.9 ms](val:stats.mean_ms "gloss/duration;unit=ms"). The slower region was
[emea](val:slowest.region).

## What was said

The stand-up where the slow hours came up, as it was recorded. A recording
is a file beside the notebook, named by its content like a stored result.

![Stand-up, Tuesday](report.blobs/6c05c8c146cad65573d80da58ab6492b.wav)

## By hour

```sql chart
SELECT t AS x, region AS series, p99 FROM p99_by_hour
```

## By region

```sql table
SELECT region, worst AS "worst@gloss/duration;unit=ms", mean AS "mean@gloss/duration;unit=ms" FROM by_region
```

## Where the calls go

```sql sankey
WITH flows AS (SELECT source, target, calls AS value FROM calls)
SELECT * FROM flows
```

## How the figures were collected

A reader who wants the picture stops above. What follows is how it was
made: each statement ran once, on the server, when the notebook was
collected, and its result is stored beside this file.

The readings. This record makes its own rows so that the sample collects
against any ClickHouse; a report reads the table it is about here.

```sql
SELECT toDateTime64(toDateTime('2026-11-02 00:00:00', 'UTC') + intDiv(number, 2) * 3600, 3, 'UTC') AS t,
       if(number % 2 = 0, 'emea', 'apac') AS region,
       round(300 + 180 * sin(intDiv(number, 2) / 4 + (number % 2)) + 40 * sin(intDiv(number, 2) / 1.3), 1) AS p99
FROM numbers(96)
```

^p99-by-hour

Everything a cell may not do is done in a record: the aggregate, the
order, the filter.

```sql
SELECT max(p99) AS worst_ms, round(avg(p99), 1) AS mean_ms, uniqExact(t) AS hours FROM p99_by_hour
```

^stats

```sql
SELECT region, max(p99) AS worst, round(avg(p99), 1) AS mean
FROM p99_by_hour GROUP BY region ORDER BY mean DESC
```

^by-region

```sql
SELECT region FROM by_region LIMIT 1
```

^slowest

```sql
SELECT source, target, calls
FROM values('source String, target String, calls UInt64',
  ('gateway', 'ingest', 9120), ('gateway', 'auth', 4410),
  ('ingest', 'queue', 9120), ('queue', 'writer', 9120),
  ('writer', 'store', 9120), ('auth', 'store', 4410))
```

^calls
