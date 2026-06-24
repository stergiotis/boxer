---
type: adr
status: proposed
date: 2026-06-23
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0096: In-DB-rendered geo-raster panel for `play` (serverless slippy map via a `mapRaster` walkers overlay)

## Context

`play` is an interactive ClickHouse SQL playground: it runs a query over HTTP,
pulls the result as Apache Arrow, and renders it through a dock of panels
(table, UMAP projection, timeline, detail). Query parameters are first-class —
`{name:Type}` placeholders surface as widgets and materialize a `SET param_*`
prelude in the editor buffer (`play_param_inject.go`).

A known technique renders slippy-map tiles *inside the database*. ClickHouse's
[`adsb.exposed`](https://github.com/ClickHouse/adsb.exposed) demo stores points
in Web Mercator (`mercator_x/y` over the full `UInt32` world range, ordered by
`mortonEncode(mercator_x, mercator_y)` so a viewport is a near-contiguous key
range with minmax skip-index pruning), then a parameterized query bins the
points into a `w×h` pixel grid (`pos = py*w + px`, `GROUP BY pos … ORDER BY pos
WITH FILL FROM 0 TO w*h`) and derives an RGBA value per pixel. The query output
*is* a dense, row-major framebuffer — N rows of `(r,g,b,a) UInt8`.

ADR-0056 already binds the `walkers` slippy map into imzero2: basemap (or
`noTiles` for a basemap-less canvas), pan/zoom/pinch/inertia, a `Projector`,
Go-configurable XYZ tile servers, an overlay register-drain
(`mapMarker`/`mapPolyline`/`h3CellsColored`/`h3Region`), and a
`fetchR15WalkersCamera` fetcher returning the viewport bbox (WGS84), the map
pixel size, hover (already inverse-projected to lat/lon), `clicked`, and a
quantized `viewHash`.

We want a `play` panel that visualizes such an in-DB-rendered raster on an
interactive map, reusing the param-injection + Arrow path and the walkers
binding, **without standing up a tile server**.

Constraints the design must respect:

- **`play`'s `QueryStore` is single-flight with one shared result and a history
  ring** (`play_store.go`). A map that reruns on every viewport change cannot
  drive it: it would overwrite the result the other tabs render, flood history,
  and churn the query FSM on every pan.
- **Air-gapped builds + screenshot-tour testing** (ADR-0057). A hard dependency
  on `tile.openstreetmap.org` conflicts with the airgap path; and `walkers`'
  `HttpTiles` load tiles outside the painter, so basemap imagery is invisible to
  the SVG/screenshot tour (svgexport `TexturePixelCache` gap).
- **Immediate-mode FFFI2** (register-drain overlays, frame-level culling).
- **Projection alignment.** The raster must register to the basemap exactly
  under pan/zoom.

## Design space (QOC)

**Question.** How should `play` get an in-DB-rendered raster onto an interactive
map, inside the egui2 process, with the least new surface and without breaking
`play`'s one-query model?

**Options.**

- **O1 — Serverless bbox-per-view via a new `mapRaster` walkers overlay
  (chosen).** A new dock tab hosts a `walkersMap`. Each frame it reads
  `fetchR15WalkersCamera`, and on a settled `viewHash` it injects the viewport
  bbox (+ output `w×h`) as reserved params, runs a bbox-variant raster query on
  a panel-local async lane, packs the Arrow result to RGBA, and emits it as a
  `mapRaster` overlay pinned to the viewport's geographic bounds.
- **O2 — Faithful tile grid via a custom `walkers::Tiles` ClickHouse source.**
  Implement `Tiles::at((z,x,y))` to render one tile per coord through ClickHouse,
  fed as a `with_layer` overlay (walkers 0.53 supports stacked tile layers).
  Inherits the flood-fill compositor and fractional-zoom scaling, but the source
  owns the async fetch, texture cache, and lower-zoom fallback.
- **O3 — Standalone Go tile server → walkers `HttpTiles` `with_layer`.** A
  service maps XYZ → ClickHouse query, caches rendered PNG tiles, and exposes
  `/{z}/{x}/{y}`; walkers consumes it as an overlay tile layer for free.
- **O4 — Browser Leaflet client over O3.** Reproduce `adsb.exposed` against our
  own ClickHouse; inherit the entire mature slippy-map engine.
- **O5 — Generic raster panel, no map.** Interpret a `K`-color-column × `W·H`-row
  result as an image and blit it with the `Image` widget; z/x/y stay manual.

**Criteria.**

- **C1 — Minimal new surface / dev cost in the egui2+play world.**
- **C2 — Fits `play`'s one-query / single-result model.**
- **C3 — Air-gapped + screenshot-tour capturable.**
- **C4 — Reusability beyond `play`.**
- **C5 — Interaction/UX quality (continuity, no-blank).**
- **C6 — Projection-exactness / correctness risk.**
- **C7 — Forward path to faithful cached tiles.**

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | ++ | −  | −− | −− | ++ |
| C2 | +  | +  | +  | +  | ++ |
| C3 | ++ | +  | −  | −− | ++ |
| C4 | −  | +  | ++ | ++ | −  |
| C5 | +  | ++ | ++ | ++ | −− |
| C6 | +  | +  | +  | ++ | ++ |
| C7 | +  | ++ | ++ | +  | −  |

O1 is the Pareto pick for *an interactive map inside `play`, serverless, minimal
surface, tour-friendly, now*. It reuses four existing seams — `walkersMap`,
`fetchR15WalkersCamera`, the param-injection lane, and the `Image`
content-version texture cache — and adds exactly one binding node (`mapRaster`).
O5 is strictly cheaper but is not a map. O2/O3/O4 win on continuity (C5) and
reuse (C4) but each is a larger commitment; they remain the forward path (C7)
and are explicitly *not* foreclosed — O1's `mapRaster` and panel-local lane
generalize, and a `with_layer` tile source can be added later additively.

## Decision

Adopt **O1**. Five components:

1. **`mapRaster` overlay** (the only IDL/Rust addition). A register-drain
   overlay carrying `(rasterId, bounds=minLat/minLon/maxLat/maxLon, widthPx,
   heightPx, contentVersion, pixels[])` + `.opacity`/`.nearest`. Pixels are
   `0xRRGGBBAA`, row-major, row 0 = north; shipped only when `contentVersion`
   changes (empty otherwise → per-`rasterId` cached texture reused). The
   `OverlayPlugin` projects the two bound corners via the `Projector` and draws
   one textured quad. Follows ADR-0056's derived overlay shape; reuses the
   `Image` content-version idiom and registers in `TexturePixelCache` so the
   tour captures it.
2. **A bbox-variant raster query** — the `adsb.exposed` single-tile WITH-block
   generalized from a fixed 1024² tile to a `{out_w}×{out_h}` raster over an
   injected mercator bbox. Shipped as a `play` snippet; the panel injects the
   reserved geo params and leaves `{table}/{sampling}/filters` to the user.
3. **A panel-local async query lane** — `client.ExecuteArrowStream` with its own
   context + cancel, following `play_timeline_bands.go`. **Not** `QueryStore`.
4. **viewHash debounce + supersession** — fire on a stable `viewHash`; cancel
   the prior lane and rerun with a stable `query_id` + `replace_running_query=1`;
   retain the last-good raster (re-projected every frame) until the new one lands.
5. **Basemap as a Go-side choice** — `noTiles` (offline, tour-capturable), the
   default OSM source (online, not tour-captured), or a custom `.TileUrl`.

### Reserved-param contract and the bbox query (SD6)

The panel drives six reserved params; the rest of the query is the user's:

| param | type | owner | meaning |
|---|---|---|---|
| `vp_min_x` `vp_max_x` `vp_min_y` `vp_max_y` | `UInt32` | panel | viewport mercator bbox (`vp_min_y` = north) |
| `vp_w` `vp_h` | `UInt32` | panel | output raster size in pixels |
| `table` | `Identifier` | human | table / sample |
| `sampling` | `UInt32` | human | brightness sampling factor |

The query is the `adsb.exposed` single-tile render generalized to an arbitrary
viewport — three changes, everything else (the colour `WITH` block, `GROUP BY
pos`, `WITH FILL`, the `round(…)::UInt8` projection, alpha-0 fill on empty
pixels) verbatim:

1. tile `z/x/y` bbox → the injected `vp_*` mercator bbox (still filters the
   morton-indexed `mercator_x/y`, so index pruning holds — SD4);
2. power-of-two `bitShiftRight` binning (fixed 1024) → linear
   `(mercator_x − vp_min_x)·vp_w DIV span_x` at arbitrary `vp_w/vp_h`;
3. `1024` / `zoom_factor = 2^z` → `vp_w/vp_h` / a `zoom_factor` derived from the
   per-pixel mercator footprint, so adsb's brightness heuristic carries over
   with no extra param.

```sql
WITH
    toUInt64({vp_max_x:UInt32}) - {vp_min_x:UInt32} AS span_x,
    toUInt64({vp_max_y:UInt32}) - {vp_min_y:UInt32} AS span_y,

    mercator_x >= {vp_min_x:UInt32} AND mercator_x < {vp_max_x:UInt32}
        AND mercator_y >= {vp_min_y:UInt32} AND mercator_y < {vp_max_y:UInt32} AS in_view,

    -- linear mercator → pixel; uniform in mercator, so it lines up with the
    -- basemap and a single stretched quad (SD5) is exact.
    least((toUInt64(mercator_x - {vp_min_x:UInt32}) * {vp_w:UInt32}) DIV span_x, {vp_w:UInt32} - 1) AS px,
    least((toUInt64(mercator_y - {vp_min_y:UInt32}) * {vp_h:UInt32}) DIV span_y, {vp_h:UInt32} - 1) AS py,
    py * {vp_w:UInt32} + px AS pos,

    -- brightness normaliser: 2^22 / sqrt(pixel mercator area), matching adsb's
    -- 2^z at 1024 px/tile.
    (span_x / {vp_w:UInt32}) * (span_y / {vp_h:UInt32}) AS pixel_area,
    pow(2, 22) / sqrt(pixel_area) AS zoom_factor,

    count() AS total,
    greatest(1000000. / {sampling:UInt32} / zoom_factor, toFloat64(count())) AS max_total,
    pow(total / max_total, 1/5) AS transparency,
    greatest(0, least(avg(altitude), 5000)) / 5000 AS color1,
    greatest(0, least(avg(altitude), 50000)) / 50000 AS color3,
    greatest(0, least(avg(ground_speed), 700)) / 700 AS color2,
    255 AS alpha,
    (1 + transparency) / 2 * (1 - color3) * 255 AS red,
    transparency * color1 * 255 AS green,
    color2 * 255 AS blue

SELECT round(red)::UInt8, round(green)::UInt8, round(blue)::UInt8, round(alpha)::UInt8
FROM {table:Identifier}
WHERE in_view
GROUP BY pos
ORDER BY pos WITH FILL FROM 0 TO toUInt64({vp_w:UInt32}) * {vp_h:UInt32}
```

The panel derives the `vp_*` values from `fetchR15WalkersCamera`: the lat/lon
viewport (inflated ~1.3× per SD7) → mercator via the `setup.sql` formula
(`mx(lon)=0xFFFFFFFF·(lon+180)/360`,
`my(lat)=0xFFFFFFFF·(½ − ln(tan((lat+90)/360·π))/(2π))`), with the y-flip
`vp_min_y = my(maxLat)` (north); `vp_w/vp_h = clamp(screenPx·DPR, ≤ cap)`. It
reads exactly `vp_w·vp_h` rows × 4 `UInt8` and emits them via `mapRaster`.

The geometry header (`span_*`, `in_view`, `px/py/pos`, `zoom_factor`) is
render-agnostic: the four upstream modes become snippet variants that share it
and swap only the colour `WITH` block + `WHERE`. Two items to verify at wiring:
`WITH FILL … TO` with a param expression (else substitute the literal product),
and the panel guarding a non-degenerate bbox (`span_x`, `span_y` > 0).

### Subsidiary design decisions

- **SD1 — bbox-per-view, not tiles.** Each settled view triggers one full
  re-aggregate over the visible rows; no tile cache, no reuse on pan. Rationale:
  fits `play`'s one-query model and needs no tile engine. Cost is bounded by an
  output-size cap (SD7) + `{sampling}`. Faithful cached tiles are deferred to
  O2/O3 (forward path), not rejected.
- **SD2 — panel-local lane, not `QueryStore`.** The shared single-flight store
  would feed raster garbage to the other tabs, flood history, and churn the FSM.
  The timeline-bands lane is the precedent.
- **SD3 — `mapRaster` carries pixels + `contentVersion`.** Self-contained
  texture cache keyed by `rasterId`, mirroring `egui2_image.go`. The buffer
  ships once per settled view, not per frame. A texture-id reference into the
  `Image` widget was rejected for coupling.
- **SD4 — one projection contract.** Bounds are WGS84; Go converts lat/lon →
  mercator *only* for the SQL filter/bin (so it prunes the morton-indexed
  `mercator_x/y` columns), and hands `mapRaster` lat/lon so walkers projects.
  Go↔walkers alignment is then automatic (both standard Web Mercator); the lone
  exactness contract is **Go's mercator formula == the SQL's** (both ours). 
  ADR-0056 SD5 (double-`center()` drift) is the cautionary tale.
- **SD5 — single stretched quad is exact.** The SQL bins linearly in mercator and
  the screen rect is linear in mercator, so the raster is mercator-uniform and a
  single `add_rect_with_uv` needs no per-pixel warp. Watch the y-flip
  (row 0 = `min_mercator_y` = north = top).
- **SD6 — reserved-param contract, two namespaces.** The six `vp_*` params (see
  the contract + query above) define map-drivability; the panel owns them, the
  human owns `{table}/{sampling}` and any `WHERE`. Binding is opt-in/confirmed,
  not silent auto-capture; the bbox query ships as the reference snippet.
- **SD7 — output size + keepBuffer analog.** `out_w×out_h = screen px × DPR`,
  capped (~≤1536²) to bound query cost and Arrow size; the requested bbox is
  inflated ~1.3× beyond the viewport so small pans stay covered before the next
  query.
- **SD8 — basemap choice carries the airgap/tour trade.** `noTiles` is offline
  and the raster overlay is *always* tour-captured (uploaded via our path →
  `TexturePixelCache`); OSM gives context but is online and its tiles are not
  tour-captured (the svgexport `HttpTiles` gap).
- **SD9 — supersession via cancel + `replace_running_query`.** Panel-owned
  context cancel plus a stable `query_id` dedupe in-flight reruns; last-good
  retention avoids flicker. `play` is otherwise run-on-demand — auto-rerun is new
  and lives only in this panel.
- **SD10 — descope (deferred with triggers).** Progressive sample100→10→full
  refinement ladder; hover→info secondary query (hover is already lat/lon in the
  camera fetcher); parent/child tile fallback (mostly moot — one viewport raster,
  not a tile grid). Each lands additively when a real need appears.

## Alternatives

- **O2 — custom `walkers::Tiles` ClickHouse source.** Faithful tiles, cache-
  friendly on pan, inherits the flood-fill compositor. Rejected for the first cut:
  the source must own async fetch + texture cache + lower-zoom fallback
  (`interpolate_from_lower_zoom` is `pub(crate)`, so an external impl reimplements
  it), and per-tile ClickHouse calls from inside the Rust render path are a
  heavier binding than one overlay. Remains the forward path for faithful tiles.
- **O3 — standalone Go tile server.** The conventional, most decoupled shape and
  the right backbone for *multiple* clients (a browser map, QGIS) — but it does
  not spare the egui2 panel the viewport work it would still need, it adds a
  service (lifecycle, auth, cache-invalidation over *live* data via a data-epoch
  key), and its `HttpTiles`-served tiles are not tour-captured. Deferred to when a
  second consumer or a browser UI justifies it.
- **O4 — browser Leaflet over O3.** Inherits the entire mature engine for free,
  but leaves the egui2/`play` world (a browser app + the server), and is online.
  The escape hatch if a "real" map UI becomes the goal.
- **O5 — generic raster panel, no map.** Trivial (`Image` widget, no walkers, no
  lane), and a fine Cut-0 — but it is not a map: no basemap, no pan/zoom, z/x/y
  typed by hand. Kept as the fallback if the map path stalls.

## Consequences

### Positive

- **One additive binding node + a focused Go driver.** No tile engine, no
  server, and no projector math in Go (walkers projects). The biggest reuse:
  `walkersMap`, `fetchR15WalkersCamera`, the param-injection lane, the `Image`
  texture cache.
- **Validates the param-injection seam** as the map↔SQL interface: pan/zoom
  become typed param mutations on a panel-local lane.
- **Tour-capturable** (with `noTiles`) — unlike `HttpTiles` basemaps.
- **Forward path preserved.** `mapRaster` + the lane generalize; O2/O3 can be
  added later as `with_layer` tile sources without reworking this panel.

### Negative

- **Full re-aggregate per settled view; no tile reuse on pan** (SD1). Bounded by
  the output-size cap + sampling, but a dense low-zoom view is a heavy query.
- **Resolution cap = blur trade** at the largest viewports (SD7).
- **One map per frame** — ADR-0056 SD3's single shared camera register; multiple
  simultaneous map panels would collide.
- **Projection-exactness is the lone correctness risk** (SD4/SD5); a wrong
  mercator constant slides the raster under the basemap.
- **Basemap is online unless `noTiles`** (SD8).

### Neutral

- `mapRaster` reuses the existing overlay-drain and image-upload idioms; no new
  protocol shape.
- The bbox-variant query is a ~10-line generalization of the upstream technique;
  the in-DB rendering itself is unchanged.

### Derived practices

- **New overlays follow ADR-0056's shape** — `mapRaster` is an instance (one
  struct, one pending Vec, one prerender, one plugin branch).
- **The bbox-variant raster query is the reference snippet**; geo params are
  panel-owned, data params human-owned.

## Status

Proposed — 2026-06-23. Gated on agreeing the `mapRaster` IDL shape and the
bbox-query reserved-param contract. First spike: wire `mapRaster` and draw a
*static* single-bbox raster (no lane, fixed bbox) to validate projection-exactness
end to end, then add the camera→params→panel-local-lane→debounce loop. Descoped
items (SD10) carry explicit triggers.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.

## References

- [ADR-0056](0056-walkers-map-h3-binding.md) — the `walkers` slippy-map binding
  this extends (overlay drain, `Projector`, camera fetcher, SD5 projection note).
- [ADR-0057](0057-demo-registry-and-drivers.md) — the demo registry + capture
  drivers the headless screenshot tour runs through (the `mapRaster` overlay is
  captured via `TexturePixelCache`; `HttpTiles` basemaps are not).
- `public/thestack/imzero2/egui2/definition/egui2_definition_d_walkers.go` —
  walkers IDL; `mapRaster` is added here.
- `public/thestack/imzero2/egui2/bindings/egui2_image.go` — the content-version
  texture-cache idiom `mapRaster` reuses.
- `apps/play/play_param_inject.go`, `play_store.go`, `play_timeline_bands.go` —
  the param-injection seam, the single-flight store the panel avoids, and the
  panel-local async-lane precedent.
- [`adsb.exposed`](https://github.com/ClickHouse/adsb.exposed) — the upstream
  in-DB tile-rendering technique the bbox-variant query generalizes.
- [`walkers`](https://crates.io/crates/walkers) — slippy map widget; 0.53
  `with_layer` is the forward path for faithful tiles (O2/O3).
