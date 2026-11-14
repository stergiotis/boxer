---
title: A notebook in two records
records:
  readings: {blob: minimal.blobs/de4accb4a1f37c7882ebe8e009f05b23.arrow, of: 10969865b18c939f58bad38ef64e1ee9}
  totals: {blob: minimal.blobs/b5e36520d2985a5332130fa6cb31f088.arrow, of: 6c3d94dfaae0026346a1431a35436a8c}
---

# A notebook in two records

Over [24](val:totals.hours) hours the mean lag was
[302.2 ms](val:totals.mean_ms "gloss/duration;unit=ms").

The first record stands in for a table. It makes its own rows, so this
notebook collects against any ClickHouse. In a report it would read the
table the report is about.

```sql
SELECT toDateTime64(toDateTime('2026-11-02 00:00:00', 'UTC') + number * 3600, 3, 'UTC') AS t,
       round(300 + 180 * sin(number / 4), 1) AS lag_ms
FROM numbers(24)
```

^readings

The second record reads the first under its id. It has one row, which is
what a figure in a sentence reads.

```sql
SELECT count() AS hours, round(avg(lag_ms), 1) AS mean_ms FROM readings
```

^totals

A cell picks columns of a record and names them; nothing is computed where
the notebook is read. The alias says how the lag is shown.

```sql table
SELECT t AS hour, lag_ms AS "lag@gloss/duration;unit=ms" FROM readings LIMIT 6
```
