---
type: how-to
audience: engineer with a specific task
status: stable
reviewed-by: "@stergiotis"
reviewed-date: 2026-06-27
---

# How to render the ADS-B map in the `play` app

The `play` app's **Map** tab ([ADR-0096](../adr/0096-play-geo-raster-map-panel.md))
renders a geo-raster: points are binned into a pixel grid and coloured *inside
ClickHouse*, then drawn on a [walkers](../adr/0056-walkers-map-h3-binding.md)
slippy map that re-queries as you pan and zoom. This recipe reproduces the
canonical case — aircraft density over London, the
[adsb.exposed](https://github.com/ClickHouse/adsb.exposed) technique — end to
end.

The panel code ships with boxer; what it needs is a **ClickHouse HTTP server**
and a **table in the adsb schema** (`mercator_x` / `mercator_y` / `altitude` /
`ground_speed`), neither of which is in the repo. The steps below supply both.

Verified end to end 2026-06-24: a local clickhouse-server (26.5) reading the
upstream adsb.exposed corpus via `remoteSecure`, rendering a London raster
(the Heathrow hub and airways) in the Map tab.

## Prerequisites

- A local **clickhouse-server** reachable over HTTP. `play` speaks the HTTP
  protocol; `clickhouse-local` (used elsewhere in boxer) does not serve HTTP, so
  you need the server binary (`clickhouse server`).
- **Network access** to the upstream adsb.exposed instance — the ingest pulls
  from it.
- The **adsb connection string** (below).

## The data source

The upstream adsb.exposed demo exposes its corpus over ClickHouse's native
protocol. Read it with `remoteSecure(...)`:

```
remoteSecure('<adsb-host>:9440', default.planes_mercator_sample100, 'website', '')
```

User `website`, empty password. `<adsb-host>` is the upstream demo's published
endpoint; [`apps/play/play_map.go`](../../apps/play/play_map.go) elides the
literal because it is third-party infrastructure, so take the value from the
adsb.exposed source and keep it in your shell — not in a committed file.

Three things make a naive attempt fail:

1. **Use the native protocol (`remoteSecure`, port 9440), not HTTPS.** The bare
   HTTPS endpoint relies on the demo's sticky-hostname routing and otherwise
   times out.
2. **The `website` user caps results at 1,048,576 rows.** A whole dense region
   exceeds it (`TOO_MANY_ROWS_OR_BYTES`); sample or window it (Step 2).
3. **A `VIEW` over `remoteSecure` blocks predicate push-down** — it scans the
   full corpus and times out. Query `remoteSecure(...)` directly, or ingest into
   a real local table. This recipe does the latter: the panel re-aggregates on
   every settled viewport, so a local table is what makes pan/zoom fast.

## Step 1 — start a local ClickHouse server

Serves HTTP on `:8123` by default, which is also `play`'s default
`CLICKHOUSE_URL`:

```sh
clickhouse server          # from a scratch working directory, or use your own server
curl -s http://localhost:8123/ping     # -> Ok.
```

## Step 2 — ingest a region into a local table

```sh
CH='curl -sS http://localhost:8123/'

$CH --data "CREATE OR REPLACE TABLE default.local_planes
  (mercator_x UInt32, mercator_y UInt32, altitude Int32, ground_speed Float32)
  ENGINE = MergeTree ORDER BY (mercator_x, mercator_y)"

$CH --data "
INSERT INTO default.local_planes
SELECT mercator_x, mercator_y, altitude, ground_speed
FROM remoteSecure('<adsb-host>:9440', default.planes_mercator_sample100, 'website', '')
WHERE mercator_x >= toUInt32(4294967295*(-0.7+180)/360) AND mercator_x < toUInt32(4294967295*(0.4+180)/360)
  AND mercator_y >= toUInt32(4294967295*(0.5-log(tan((51.85+90)/360*pi()))/2/pi()))
  AND mercator_y <  toUInt32(4294967295*(0.5-log(tan((51.10+90)/360*pi()))/2/pi()))
  AND rand() % 2 = 0"

$CH --data "SELECT count() FROM default.local_planes"
```

This is a London box (`lon ∈ [-0.7, 0.4]`, `lat ∈ [51.10, 51.85]`), a one-time
pull of roughly a minute or two. Notes:

- The `WHERE` is the same Web-Mercator projection the panel uses
  (`x = 0xFFFFFFFF·(lon+180)/360`; `y = 0xFFFFFFFF·(½ − ln(tan((lat+90)/360·π))/2π)`),
  and `y` is inverted, so the northern latitude is the lower bound.
- `rand() % 2 = 0` halves the transferred rows to stay under the 1,048,576 cap
  **with even spatial coverage** — a bare `LIMIT` would bias the sample, since
  the corpus is Morton-ordered. A time window (`AND time >= '<date>'`) is an
  alternative that also pushes down.
- Widen or move the box freely; just keep the post-filter count under the cap.

## Step 3 — launch `play` at the local table

From the repo root:

```sh
CLICKHOUSE_URL=http://localhost:8123/ \
SPINNAKER_PLAY_MAP_TABLE=local_planes \
SPINNAKER_PLAY_MAP_CENTER=51.5,-0.15 \
SPINNAKER_PLAY_MAP_ZOOM=10 \
SPINNAKER_PLAY_MAP_SIZE=860x460 \
bash rust/imzero2/hmi.sh --launch play
```

Open the **Map** tab. The init view is pinned to London at zoom 10 because the
default zoom-4 continental view is too heavy to aggregate. The
`SPINNAKER_PLAY_MAP_*` knobs set only the initial table and view; sampling,
opacity, and the basemap toggle are interactive controls.

## Verify without the GUI

The gated live test runs the panel's real query + Arrow→RGBA path and writes a
PNG you can eyeball:

```sh
PLAY_MAP_LIVE_URL=http://localhost:8123 \
PLAY_MAP_LIVE_TABLE=local_planes \
PLAY_MAP_LIVE_PNG=/tmp/london.png \
go test -tags="$(cat ./tags)" ./apps/play/ -run TestMapRasterLive -v -timeout 60s
```

## Optional — OpenStreetMap basemap

Uncheck **"no basemap"** in the Map controls to draw the raster over OSM tiles
(the walkers default source), and lower **opacity** to about 0.5–0.7 so the map
reads through. Two caveats:

- It is **online** — tiles load from `tile.openstreetmap.org`, which breaks the
  offline/airgap path (that is why `noTiles` is the default).
- The basemap is **not captured in headless screenshots**: walkers' `HttpTiles`
  load outside the egui painter, so the SVG/screenshot export
  ([ADR-0096](../adr/0096-play-geo-raster-map-panel.md) §SD8) sees only the
  raster overlay. OSM shows in an interactive window only. There is no env knob
  for the toggle.

A custom XYZ tile server can be substituted, but the panel currently hardcodes
the default source; passing a real `.TileUrl("https://.../{z}/{x}/{y}.png")`
needs a small code change.

## Notes and limits

- The panel is a first cut ([ADR-0096](../adr/0096-play-geo-raster-map-panel.md)
  §SD10): one render mode ("Altitude & Velocity"), no hover→info query, no
  progressive sample refinement, one map per frame.
- The render SQL assumes the adsb schema. **Any** table with `mercator_x` /
  `mercator_y` / `altitude` / `ground_speed` works — including a synthetic one,
  if you only want to exercise the panel without the upstream corpus.
- Querying `remoteSecure(...)` directly in the table control also works (no local
  ingest), but each tile is a transatlantic round-trip (~20–40 s); the local
  table avoids that.
