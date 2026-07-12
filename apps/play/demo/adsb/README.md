# ADS-B demo data for the `play` Map panel

Loads a Zürich-centred slice of ADS-B aircraft positions into a local
ClickHouse — one day by default, or several (see [Loading more](#loading-more))
— so the `play` Map panel (ADR-0096) renders a real in-database raster without
reaching a remote instance at render time.

It is the local, offline counterpart to ClickHouse's
[`adsb.exposed`](https://github.com/ClickHouse/adsb.exposed) demo, whose
in-DB tile-rendering technique the Map panel generalizes. The schema
(`setup.sql`) is adopted from that project; the data is pulled once from its
public instance.

## What it does

`demo.sh` runs two statements against a local `clickhouse-server`:

1. `setup.sql` — creates `planes_mercator` (+ the `sample10`/`sample100` tables
   and their materialized views), adopted from the upstream schema. `mercator_x`
   / `mercator_y` are `MATERIALIZED` from lat/lon with the same formulas
   `play_map.go` mirrors in Go (ADR-0096 §SD4).
2. `ingest.sql` — one `INSERT … SELECT * FROM remoteSecure(<public instance>)`
   filtered to the bbox and day. No file download and no JSON parsing: the rows
   arrive already-shaped, and the local materialized columns + sample views
   populate on insert.

After that the data is entirely local; rendering needs no network.

`switzerland.sh` is a preset wrapper over `demo.sh` for the common case — a
full-resolution week for the whole country (see [below](#switzerlandsh-preset)).

## Prerequisites

- A local `clickhouse-server` running with the **default user and no password**
  (native `9000` for the loader, HTTP `8123` for `play`). This script does not
  start one.
- Network egress **once**, at load time, to the public instance. The staging
  instance idles to zero, so the first connection can take ~30–60 s; `demo.sh`
  already disables hedged requests and widens the timeouts to let it wake.

## Run

```sh
apps/play/demo/adsb/demo.sh                                  # one day (yesterday)
ADSB_FROM=2026-07-01 ADSB_TO=2026-07-07 apps/play/demo/adsb/demo.sh   # a whole week
ADSB_APPEND=1 ADSB_DAYS="2026-07-08" apps/play/demo/adsb/demo.sh      # add a day on top
```

### `switzerland.sh` preset

`switzerland.sh` is a thin wrapper that captures a ready-made recipe — the whole
of Switzerland, a rolling week, at **full resolution** (~35 M rows) — by setting
Swiss-national defaults and handing off to `demo.sh`. Every `demo.sh` knob still
overrides:

```sh
apps/play/demo/adsb/switzerland.sh                         # last 7 days, full res
ADSB_WEEK_DAYS=14 apps/play/demo/adsb/switzerland.sh       # last two weeks
ADSB_SRC=planes_mercator_sample10 apps/play/demo/adsb/switzerland.sh   # ~10× lighter
```

It defaults the bbox to Switzerland's extent (lat `45.8`–`47.85`, lon
`5.9`–`10.55`), the window to the last `ADSB_WEEK_DAYS` (default 7) complete UTC
days ending yesterday, and `ADSB_SRC` to full-resolution `planes_mercator`. An
explicit `ADSB_FROM`/`ADSB_TO` still wins over the rolling window; if a recent
day comes back empty, the public instance's data lags — shift the window back.

See [Loading more](#loading-more) for the multi-day / wider-area / accumulate
knobs. Then view it in `play` (default endpoint is already
`http://localhost:8123/`):

```sh
BOXER_PLAY_MAP_TABLE=planes_mercator \
BOXER_PLAY_MAP_CENTER=47.3769,8.5417 \
BOXER_PLAY_MAP_ZOOM=8 \
<launch the play HMI>          # open the Map panel; "no basemap" keeps it offline
```

For a small slice (a city-day, or a few) the Map panel reads `planes_mercator`
directly; the sampled tables matter at billions of rows, not here.

## Tunables (env)

| var | default | meaning |
| --- | --- | --- |
| `ADSB_MIN_LAT` `ADSB_MAX_LAT` `ADSB_MIN_LON` `ADSB_MAX_LON` | `45.5 49.0 5.5 12.0` | bbox (WGS84); default covers Zürich and the surrounding Alpine airspace |
| `ADSB_DAY` | yesterday (UTC) | the single day (`YYYY-MM-DD`) used when neither `ADSB_DAYS` nor a range is given |
| `ADSB_DAYS` | just `ADSB_DAY` | UTC days to load, space-separated — the **multi-day** knob. Every `(day, hour)` pair is a separate `INSERT`, so this multiplies the transfer |
| `ADSB_FROM` `ADSB_TO` | — | inclusive UTC date range (`YYYY-MM-DD`); when **both** are set they expand into `ADSB_DAYS` (e.g. a whole week), overriding it |
| `ADSB_APPEND` | `0` | `1` keeps existing rows (skips the initial `TRUNCATE`) and adds this slice on top — **accumulate** across runs. Overlapping day+bbox re-loads duplicate rows (MergeTree doesn't dedupe) |
| `ADSB_SRC` | `planes_mercator_sample10` | remote source; the 10% sample (~0.8 M rows/day) is quick and dense. `planes_mercator` is full resolution — viable now that the load is chunked (see below) |
| `ADSB_HOURS` | `0`…`23` (all) | which UTC hours to load, space-separated. Each `(day, hour)` is **one `INSERT`**, so set e.g. `"10 11 12"` for a fast partial (midday) load |
| `CH` | `clickhouse-client` | client binary |

Reference volume: the default box for one day is ≈0.84 M rows / ~5 k aircraft
from the 10% sample. The load runs **one `INSERT` per `(day, UTC hour)`** (≈1/24
of a day each), with per-chunk retry — so a single slow chunk can't blow the
receive timeout the way a whole-day pull did, and a failed chunk is retried in
isolation. Keeping each query small also holds it under the public `website`
user's **1,048,576-row result cap**, which a whole-day full-resolution pull
(~8.4 M rows) exceeded — so `ADSB_SRC=planes_mercator` (full res) is viable too,
at ~10× the transfer. `planes_mercator_sample100` stays the lightest option;
`ADSB_HOURS` narrows to a partial day.

## Loading more

Three orthogonal knobs grow the slice; each multiplies the number of `(day,
hour)` chunks, so mind the total against the idled instance's slow link:

- **More days.** `ADSB_DAYS="2026-07-01 2026-07-02 …"` (explicit list) or a range
  with `ADSB_FROM`/`ADSB_TO`. A week of the default box is ≈ 7 × 0.84 M ≈ 5.9 M
  rows over 7 × 24 = 168 small INSERTs. The final summary's `first_day`…`last_day`
  then spans the range.
- **Wider area / full resolution.** Widen the bbox (`ADSB_MIN_*`/`ADSB_MAX_*`) or
  switch `ADSB_SRC=planes_mercator`; the per-hour chunking keeps each query under
  the row cap.
- **Accumulate instead of replace.** By default each run `TRUNCATE`s first. Set
  `ADSB_APPEND=1` to keep the previous slice and add to it — handy for stitching
  several regions or date ranges together across runs. Re-loading an overlapping
  day+bbox while appending duplicates rows (MergeTree doesn't dedupe); clear
  (drop `ADSB_APPEND`) or don't overlap.

## Data provenance and licensing

The rows come from ClickHouse's public `adsb.exposed` instance, which aggregates
ADS-B feeds from **adsb.lol** (Open Database License, ODbL), **airplanes.live**,
and **adsbexchange.com**. This directory redistributes no data — it fetches on
demand and stores only in your local ClickHouse. If you republish anything
derived from the ODbL portion, carry its attribution and share-alike terms. See
the upstream project for the full source and license notes.
