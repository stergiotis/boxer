# ADS-B demo data for the `play` Map panel

Loads a small, one-day, Zürich-centred slice of ADS-B aircraft positions into a
local ClickHouse, so the `play` Map panel (ADR-0096) renders a real in-database
raster without reaching a remote instance at render time.

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

## Prerequisites

- A local `clickhouse-server` running with the **default user and no password**
  (native `9000` for the loader, HTTP `8123` for `play`). This script does not
  start one.
- Network egress **once**, at load time, to the public instance. The staging
  instance idles to zero, so the first connection can take ~30–60 s; `demo.sh`
  already disables hedged requests and widens the timeouts to let it wake.

## Run

```sh
apps/play/demo/adsb/demo.sh
```

Then view it in `play` (default endpoint is already `http://localhost:8123/`):

```sh
SPINNAKER_PLAY_MAP_TABLE=planes_mercator \
SPINNAKER_PLAY_MAP_CENTER=47.3769,8.5417 \
SPINNAKER_PLAY_MAP_ZOOM=8 \
<launch the play HMI>          # open the Map panel; "no basemap" keeps it offline
```

The full local table is small (one city-day), so the Map panel reads
`planes_mercator` directly; the sampled tables matter at billions of rows, not
here.

## Tunables (env)

| var | default | meaning |
| --- | --- | --- |
| `ADSB_MIN_LAT` `ADSB_MAX_LAT` `ADSB_MIN_LON` `ADSB_MAX_LON` | `45.5 49.0 5.5 12.0` | bbox (WGS84); default covers Zürich and the surrounding Alpine airspace |
| `ADSB_DAY` | yesterday (UTC) | the single day to load (`YYYY-MM-DD`) |
| `ADSB_SRC` | `planes_mercator_sample10` | remote source; the 10% sample (~0.8 M rows/day here) stays under the result cap below. `planes_mercator` is full resolution but only for a narrow bbox/short window |
| `CH` | `clickhouse-client` | client binary |

Reference volume: the default box for one day is ≈0.84 M rows / ~5 k aircraft
from the 10% sample — a quick transatlantic pull and a dense raster. The public
`website` user caps any single query result at **1,048,576 rows**, so the
full-resolution `planes_mercator` (~8.4 M rows/day for this box) fails with
`TOO_MANY_ROWS_OR_BYTES` unless you narrow the bbox or load a sub-day window;
`ADSB_SRC=planes_mercator_sample100` is the always-safe fallback for busy days or
wide boxes.

## Data provenance and licensing

The rows come from ClickHouse's public `adsb.exposed` instance, which aggregates
ADS-B feeds from **adsb.lol** (Open Database License, ODbL), **airplanes.live**,
and **adsbexchange.com**. This directory redistributes no data — it fetches on
demand and stores only in your local ClickHouse. If you republish anything
derived from the ODbL portion, carry its attribution and share-alike terms. See
the upstream project for the full source and license notes.
