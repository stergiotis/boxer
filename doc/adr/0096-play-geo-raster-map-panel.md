---
type: adr
status: accepted
date: 2026-06-23
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-10
---

> **Status: accepted (2026-07-10).** Built and committed — see the Status section.

# ADR-0096: In-DB-rendered geo-raster panel for `play` (serverless slippy map via a `mapRaster` walkers overlay)

## Context

`play` is an interactive ClickHouse SQL playground: it runs a query over HTTP,
pulls the result as Apache Arrow, and renders it through a dock of panels
(table, UMAP projection, timeline, detail). Query parameters are first-class —
`{name:Type}` placeholders surface as widgets and materialize a `SET param_*`
prelude in the editor buffer (`play_param_inject.go`).

A known technique renders slippy-map tiles *inside the database*. ClickHouse's
`adsb.exposed` demo, as its
[announcement post](https://clickhouse.com/blog/interactive-visualization-analytics-adsb-flight-data-with-clickhouse)
describes it, stores points
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

The query is the single-tile render published in ClickHouse's
[`adsb.exposed` announcement](https://clickhouse.com/blog/interactive-visualization-analytics-adsb-flight-data-with-clickhouse)
generalized to an arbitrary viewport — three changes, the rest of the published
skeleton (`GROUP BY pos`, `WITH FILL`, the `round(…)::UInt8` projection,
alpha-0 fill on empty pixels) kept. The colour block below is boxer's own; see
the 2026-10-01 Update:

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
    255 AS alpha,
    -- colour block: a render's red/green/blue expressions go here
    -- (redacted 2026-10-01 — see the Update of that date)
    transparency * 255 AS red,
    transparency * 255 AS green,
    transparency * 255 AS blue

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
render-agnostic: colour modes become snippet variants that share it
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

Accepted — 2026-07-10 (proposed 2026-06-23). Built and committed: the `mapRaster`
walkers overlay (IDL + Rust `OverlayPlugin` textured-quad draw) and the `MapDriver`
panel (`apps/play/play_map.go`) — camera→debounce→panel-local node lane→RGBA
repack→overlay, with the SD4/SD5 projection contract, SD9 supersession, and the
§SD6 bbox raster query. A local demo loader (`apps/play/demo/adsb/`, commit
`1e7ebe99`) makes it render offline against a real ADS-B slice. The remaining SD10
items (progressive sampling ladder, hover→info, keepBuffer margin) stay deferred
with their triggers; the configurable-render descope was lifted — see the
2026-07-10 Update.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.

## Update — 2026-07-10: pluggable render (SD6 realized)

The first cut hardcoded a single altitude-and-speed colour block, so the panel
only worked on tables with `altitude`/`ground_speed`. SD6 anticipated the fix —
the geometry + density header (`span_*`, `in_view`, `px/py/pos`, `zoom_factor`,
`total`, `max_total`, `transparency`, `alpha`) is render-agnostic, and a render
swaps only the colour `WITH` block + optional `WHERE`. That split is now real.

`apps/play/play_map.go` gains a `rasterRender` (name, `colorSQL` spliced after the
shared header, optional `where`, `needs` columns for the status hint, `custom`
flag) and a ComboBox render picker in the panel. Built-ins: an altitude-and-speed
render (default; the prior block, aviation — replaced 2026-10-01, see that Update), **Density** (table-agnostic — colour from
`count()` alone via `transparency`, assumes only `mercator_x/y`; the "any
geo-point table" unlock), **Speed** (assumes `ground_speed`), and **Custom** (the
user types the `red/green/blue` expression, matching the playground's existing
arbitrary-`table` freedom). The render is part of the fetch key, so switching it
supersedes any in-flight run and re-renders. Validated: all four modes emit a
dense `w×h` framebuffer with distinct colour distributions over the same points.

This lifts the configurable-render descope; the remaining SD10 items (progressive
sampling ladder, hover→info, keepBuffer margin) stay deferred.

## Update — 2026-07-10: SD6 param-contract divergence (inlined literals, not reserved params)

The render-agnostic-header half of SD6 shipped (previous Update); the other half —
the **reserved-`vp_*`-param contract** — did not, and the panel diverged to a
simpler shape. SD6 specified six `vp_*` `UInt32` params (viewport mercator bbox +
output `w×h`) that the panel injects into a *user-owned* query via the
param-injection seam, with the human owning `{table}`/`{sampling}` and any `WHERE`.
As built (`apps/play/play_map.go`):

- **Literals, not params.** `buildRasterSQL` inlines the bbox and `w`/`h` with
  `fmt.Sprintf` and builds the whole query itself; it does not touch
  `play_param_inject.go` or a `SET param_*` prelude.
- **The panel owns the entire query.** The user's only inputs are the `table`
  field (a plain name or a table-function source, guarded by `sanitizeTable` — the
  sole user-owned fragment), the render mode, and the `sampling` slider; no
  user-supplied `WHERE`/filter is fused in.
- **Independent of the editor.** The map query runs on a panel-local lane, not
  `QueryStore` (SD2), and never reads the editor buffer; the two paths share only
  play's endpoint (and, by naming convention, possibly the same table).

Consequence: the *"validates the param-injection seam as the map↔SQL interface"*
item under Consequences → Positive did **not** land — pan/zoom is a rebuilt-literals
query, not a typed param mutation, so that seam is still exercised only by the main
editor. The reserved-`vp_*` contract stays the forward path if a *shared*,
user-editable raster query (panel-injected viewport + user filters) is ever wanted.
SD6's bbox query does now "ship as a snippet" (the ADS-B section of the Snippets
library, commit `4178adc2`), but as an editor snippet with inlined literals — not
the param-bound reference query SD6 imagined. *Resolved — see the next Update.*

## Update — 2026-07-11: SD6's reserved-param contract realized (ADR-0097 slice 5c)

The divergence above is closed. With the ADR-0097 signal store in place
(slice 5a/5b), the raster is a panel-authored node whose SQL carries the six
reserved `{vp_*:UInt32}` slots verbatim from §SD6, and the panel EMITS the
settled viewport as `vp_*` signals — pan/zoom are typed param mutations at
last. Concretely (`apps/play/play_map.go`):

- **The SQL text is stable across pans.** `rasterTemplateSQL` replaces
  `buildRasterSQL`: the viewport is not in the text; the compiled
  `(template, vp_* values)` pair is the lane's memo key, so a pan re-executes
  with the SQL unchanged and only the `param_vp_*` URL entries moving.
- **The wiring check SD6 called out is verified server-side**: ClickHouse
  substitutes the slots everywhere they appear, *including* the
  `ORDER BY … WITH FILL FROM 0 TO toUInt64({vp_w:UInt32}) * {vp_h:UInt32}`
  bound — no literal-product fallback needed (checked on 26.6 with real
  params; a data point lands at the exact computed `pos`).
- **The overlay pin is self-describing**: repack recovers the raster's
  lat/lon bounds from the SERVED `vp_*` values by inverse Web-Mercator (the
  SD4 contract run backwards), so raster and query cannot disagree about
  bounds — correct even when the signals were seeded from elsewhere (a
  history restore). The former demanded-SQL→bounds side table retired.
- **Panel-owned vs human-owned held its 2026-07-10 shape**, not the original
  SD6 table: the six `vp_*` are panel-written signals; `table`, `sampling`,
  and the colour render remain panel *controls spliced into the template* (a
  control change is a template change). The `{table:Identifier}` /
  `{sampling:UInt32}` human-owned slots of the original SD6 sketch remain
  unbuilt — the forward path if a user-editable raster query is ever wanted.
- Being ordinary named signals (ADR-0097 SD8), the `vp_*` values are
  referenceable by any other node — a query reading `{vp_min_x:UInt32}` now
  cross-filters against the map viewport with no new mechanism.

## Update — 2026-07-28: SD4's world span moves to 2^32 (ClickHouse's MVT mercator space)

SD4 fixed the projection constant at `0xFFFFFFFF` (2^32 − 1) because that is
what upstream `adsb.exposed` materializes. ClickHouse 26.6 added an MVT function
family — `MVTEncodeGeom`, `MVTEncode`, `MVTBoundingBoxMercator`, `ST_AsMVTGeom`
— which projects into "Web Mercator over the full `UInt32` coordinate range"
with the same downward y axis, i.e. the same space SD4 describes but spanning
2^32. Measured on 26.7.1: `MVTBoundingBoxMercator(0,0,0)` returns
`(0, 0, 4294967296, 4294967296)`, and over 2952 points spanning the globe the
two conventions agree to within **one mercator unit** in both axes.

The panel's Go projection and `apps/play/demo/adsb/setup.sql` now scale by 2^32.
The reason is convention alignment, not accuracy: one unit in 4.29 × 10⁹ reaches
one raster pixel only when `span_x ≲ vp_w`, a viewport roughly a centimetre
across. A minor benefit is that the prime meridian and equator now land on
exactly 2^31 rather than a half-unit.

**What this costs.** SD4's guarantee — the Go bbox aligns with the column values
— now holds against boxer's own `setup.sql`, not against upstream. The two
cannot both be matched. A table filled by *copying* the
remote's `MATERIALIZED` columns verbatim (`doc/howto/play-adsb-map.md` Step 2's
`local_planes`) therefore reads back up to one unit off; a table that recomputes
on `INSERT`, as `setup.sql` does, has no offset. `ingest.sql`'s bbox and the
howto's `WHERE` deliberately keep `0xFFFFFFFF`, because they are predicates
against the *remote's* columns; both now say so at the site.

**A trap the migration exposed.** The constant silently served three roles:
scale factor, clamp ceiling, and inverse divisor. They coincided only because
2^32 − 1 is both the scale and the largest `UInt32`. At a span of 2^32 they
diverge, and both languages convert the overflow to zero rather than saturating
— verified: Go's `uint32(4294967296.0)` is `0`, and ClickHouse's
`toUInt32(4294967296.)` is `0`. Left unhandled, a point at lon = +180 would have
landed at `x = 0`, wrapping the antimeridian to the far left of the world. So:

- Go splits the roles into `mercWorld` (2^32, the span) and `mercUnitMax`
  (2^32 − 1, the ceiling `clampMerc` saturates against).
- The DDL wraps each expression in `greatest(0., least(4294967295., …))`. This
  also pins the poles, where `log(tan(…))` diverges — an unclamped divergence
  the previous schema shared.

**A residual this measurement surfaced — and closed.** Diffing the Go projection
against the DDL over 2555 points showed the two disagreeing by up to **2 units**,
so SD4's "pixel-for-pixel" wording had always been approximate; holding the
rounding shapes fixed and varying only the constant showed the same spread under
the old `0xFFFFFFFF`, so it predated this migration. Three distinct causes, all
now fixed, verified at **0 differing points over a 245861-point grid**:

1. **Rounding.** Go rounds (`math.Round`); the implicit `UInt32` cast truncates.
   `round()` is *not* the fix — ClickHouse's is banker's rounding
   (`round(0.5) = 0`, `round(2.5) = 2`) where Go's is half-away-from-zero. The
   DDL uses `floor(x + 0.5)`, which agrees with Go over the non-negative domain.
2. **Association.** Go computes `2^32 · (lon+180) / 360`, multiplying first; the
   DDL wrote `2^32 · ((lon+180) / 360)`. Matching the order makes the x axis
   agree bit-for-bit.
3. **`log()` precision.** The largest term, and the least obvious. ClickHouse's
   `log()` is a fast approximation carrying ~1.4e-9 of relative error — measured
   against the correctly-rounded value, `log(tan(…))` returns
   `1.0520656853338006` where the true value is `1.0520656867704066`. At a scale
   of 2^32 that is very nearly one whole mercator unit, and it put a symmetric
   ±1 disagreement on ~36% of points. `tan()` is accurate to the ULP; only
   `log`/`ln` is approximate.

   The fix is the isometric latitude's inverse-Gudermannian form:
   `ln(tan(π/4 + φ/2)) = asinh(tan φ)`, the same quantity by identity.
   ClickHouse's `asinh()` *is* correctly rounded, so both sides now spell it
   `asinh(tan(lat/180·π))` and agree exactly.

The three spellings are load-bearing and noted as such at each site. Changing
one side alone reintroduces a unit of shift across a third of the world.

**Why the SQL does not call the `MVT*` functions.** Expressing the projection
through them was evaluated and is not possible. The family is four functions —
`MVTBoundingBox`, `MVTBoundingBoxMercator`, `MVTEncode`, `MVTEncodeGeom` (plus
the `ST_AsMVT` / `ST_AsMVTGeom` aliases) — and every one is *tile-addressed*:
each takes a `(zoom, tile_x, tile_y)` triple. None maps a longitude/latitude to
a global mercator coordinate, and there is no lon/lat → tile function to bridge
the gap, so any attempt is circular: computing the tile index needs the very
projection being sought.

`MVTEncodeGeom` does project points, but into *tile-local* space with `extent`
capped at 2^31 − 1, and returns a `Geometry` rather than a number. At zoom 0
with maximum extent it yields exactly half the required resolution and needs a
`toString` round-trip to read the coordinates back out — strictly worse than the
arithmetic on every axis. Recovering full precision would mean descending to a
zoom whose tiles fit the extent, deriving each point's tile from hardcoded
inverse-mercator latitude thresholds, and reassembling `tile · size + local`:
slower, far less readable, and still approximate at tile seams.

`MVTBoundingBoxMercator` *is* useful, but for the inverse direction — a tile's
bbox in exactly this coordinate space. It has no role in building the columns.
Its optional `margin` argument is a ready-made server-side `keepBuffer` should
SD10's deferral ever be taken up, though SD1's bbox-per-view choice would have
to be revisited first.

Unchanged: the raster template text, the reserved `vp_*` param contract, the
bbox-per-view choice (SD1), and panel behaviour.

## Update — 2026-08-15: the unbuilt `{table:Identifier}` slot has a mechanism, and a shape it cannot carry

Two Updates above leave the same item open: SD6's `{table:Identifier}` /
`{sampling:UInt64}` human-owned slots "remain unbuilt — the forward path if a
user-editable raster query is ever wanted".
[ADR-0187](./0187-play-sql-expression-parameters.md) builds the
mechanism. `Identifier` is now a recognised parameter category with an editor,
and the panel's two raw `TextEdit` controls — the `table` field and the Custom
render's colour block — are `sqleditor.Field`s with syntax colour rather than
plain text.

**The constraint that matters here, found by probing the pinned server: one
`Identifier` carries one name, not a dotted path.** `FROM {t:Identifier}` with
`param_t=default.planes_mercator_sample100` fails with `Code: 60 … Unknown table
expression identifier` — the value is quoted whole. Every table this panel
actually reads is qualified, and `sanitizeTable` deliberately admits a whole
table-function source (`remoteSecure('…', default.planes_…, …)`), which is
further still from one identifier. So SD6's slot as written would not have
carried the panel's own values, and taking it up means either two slots
(`{db:Identifier}.{tbl:Identifier}`) or leaving the source spliced as it is
today and reserving the parameter for the *predicate* — which is the half SD6
never plumbed a control for (`play_map.go`'s per-render `where`, ANDed with
`in_view`).

Nothing about the raster query changes here; this records what the forward path
now costs, so the next reader does not re-derive it from a `Code: 60`.

## Update — 2026-10-01: upstream colour block removed; published sources cited

`adsb.exposed`'s repository is licensed CC BY-NC-SA 4.0. Its schema, the sampled
tables and their materialized views, the tile-query skeleton and the brightness
normaliser (`max_total` over `sampling` and `zoom_factor`, the fifth-root
`transparency`) are all published in the 2024
[announcement post](https://clickhouse.com/blog/interactive-visualization-analytics-adsb-flight-data-with-clickhouse);
this ADR, `apps/play/demo/adsb/setup.sql` and the Map panel now cite that post
rather than the repository. The default "Altitude & Velocity" colour block was
not found in any published material — only in the repository's source — so it
was removed from `play_map.go`, from the Map snippet in
`apps/play/help/snippets.md` and from §SD6's query above, and replaced by an
altitude-and-speed render designed without reference to it (named in
`builtinRenders`). Nothing else about the panel changes: the render split of
the 2026-07-10 Update, the `vp_*` contract and SD1 stand.

## Update — 2026-10-01: the panel as ADR-0204 rebuilt it; SD7 as built; antimeridian, alpha and offline land

ADR-0204 replaced the `walkers` binding with the Go `portolan` map, and this
ADR's body still describes the binding it replaced. What holds now:

- **SD3 / the view.** The panel reads the view from `portolan.Map.View()` and
  debounces on `ViewHash()`; the raster is drawn with `Projector.Image`, which
  carries the send-once texture protocol `mapRaster` had. `fetchR15WalkersCamera`
  and the `mapRaster` node are gone, and with them the Negative "one map per
  frame": each pane owns its map.
- **SD7 as built.** The raster is sized from `View.Size` in logical points, with
  no device-pixel factor, and `clampDim` caps each side at `mapMaxDim` (1024),
  not ~1536². No bbox inflation was built; the keepBuffer margin stays an SD10
  deferral, as the Status section says. 1024² is also the public ADS-B
  instance's result-row cap, so a larger dense raster would fail there.
- **SD8.** Basemap tiles are painter images (ADR-0204 M4), so captures include
  them, and they are fetched through the `basemap` egress destination
  (ADR-0262); `BOXER_MAP_TILE_URL` both moves the source and turns the basemap
  on by default. With no basemap the pane now paints the `landoverlay` atlas
  under the raster, as the Vector field pane does, in its design-system style
  over portolan's default background (`styletokens.NeutralBgPanel`).
- **SD4 at the antimeridian.** The map wraps in longitude and `mercator_x`
  covers one world. `foldViewLon` folds a settled view by whole turns onto that
  world before the request (a view a world wide or more asks for the whole
  world), sizes the raster to the share of the view it keeps, and
  `rasterCopies` draws the served raster on every world copy the view shows.
  Before, a view panned one world width projected to a degenerate bbox and the
  pane went dark. A narrower view straddling ±180 still loses the part past
  the edge; requesting both sides stays deferred. `landoverlay` paints world
  copies the same way.
- **SD6, alpha.** The header no longer fixes `255 AS alpha`: a colour block may
  define `alpha`, and the template appends the opaque default only when it does
  not (`alphaClause`). `WITH FILL` rows stay transparent either way.
- The chserver engine sends the HTTP progress settings only with the transport
  that reads them (ADR-0115 plane A), so an https endpoint or a wasm build
  no longer asks for progress it cannot receive.

Unchanged: SD1's bbox-per-view, the `vp_*` contract, and the remaining SD10
deferrals.

## Update — 2026-10-01: SD10's sampling ladder, under SD1

The progressive ladder is taken up without giving up bbox-per-view. A settled
view is demanded from the most-sampled table first and then one level at a
time towards the full one, under the same `vp_*` params on the panel's one
lane; each level supersedes the last by the lane's (SQL, params) key, and the
last-good raster stays on screen while the next loads. Levels come from the
source's name — `<base>_sample100`, `<base>_sample10`, `<base>`, starting at
the table the source control names — and each level's `sampling` is its own
factor, so the brightness normaliser keeps the levels alike. A derived level
the server does not have (`UNKNOWN_TABLE`) is skipped and remembered; the
named table is never skipped. A level slower than `mapLadderBudget` stops the
climb and the status line says so; Refresh climbs regardless. A pan or a
control change starts again at the coarsest level. A table function or
subquery source, or the **refine** toggle off, reads the one source at the
manual `sampling` as before. The mechanism is `apps/play/play_map_ladder.go`.

Still open from SD10: a ladder per source chosen by row count rather than by
name, hover→info, and the keepBuffer margin.

## Update — 2026-10-01: a sparse raster result

The template no longer fills the framebuffer server-side: it selects
`(toUInt32(pos), r, g, b, a)` per non-empty pixel with no `ORDER BY … WITH
FILL`, and `packRaster` scatters the rows into a zeroed `w×h` buffer. It
still reads the dense four-column form, which the Map snippet keeps.

Measured once on 2026-10-01 against the local demo slice at 1024×600, three
views from 0.04 % to 52 % of pixels non-empty: the sparse query took about a
third of the dense one's server time at every fill, `WITH FILL` being most of
the latter, while after ClickHouse's default lz4 Arrow compression the dense
result was up to about half the bytes once more than a few percent of pixels
were non-empty, because empty rows compress and an unordered `pos` column
does not (sorting it did not change that). Sparse was taken for the server
time, for the rows it saves the browser tab's single-threaded decode, and
because it takes the framebuffer size off the result-row count; a view that
is both large and mostly full costs more bytes than it did.

## Update — 2026-10-01: the server query cache, opt-in

The panel has a **server cache** toggle, off by default. On, the raster runs
carry `use_query_cache=1`; a Refresh computes every level of the climb it
restarts afresh (`enable_reads_from_query_cache=0`, the entries rewritten)
until the view changes. Parameter values are part of the cache key — the
server keys on the statement with the `{vp_*}` values substituted — so a
hit needs the same table, render and viewport again: under SD1 that is an
exact revisit, a history restore, or a second window on the same view.

It stays off by default because of what it can hold. On 2026-10-01, against
a server with the default 1 MiB `query_cache.max_entry_size_in_bytes`, a
1024×600 raster of the demo slice was cached when sparse (240 and 9,900
non-empty pixels, answered again in 3–5 ms instead of 56–62 ms) and was not
cached at 52 % fill (317,118 pixels, over the limit) — the expensive views
are the ones the default limit refuses. A server with a larger limit, or
tile addressing (O2), changes that.

A `readonly=1` user is not asked to skip reading the cache (ClickHouse's
public demo refuses that setting) but keeps `use_query_cache`, which that
demo lets its user change; the ADR-0181 2026-10-01 degrade otherwise holds.

## Update — 2026-10-01: a time window from the Timeline's brush

play's Timeline turns on its range brush (ADR-0043 §SD16) and publishes the
brushed window as two play-wide signals, `tl_from` and `tl_to`
(`DateTime64(3, 'UTC')`, declared in `play_signal_decl.go`). With nothing
brushed they span every instant `DateTime64` holds, and they are seeded that
way, so any query can filter on them from the first frame and an unbrushed
window keeps every row; ADR-0251's deferred "range as signals" could publish
the same pair from the time strip.

The Map takes a **time column** (default `time`, the ADS-B schema's). While
a window is brushed, the raster template ANDs `<column> BETWEEN
{tl_from:DateTime64(3, 'UTC')} AND {tl_to:DateTime64(3, 'UTC')}` onto its
WHERE, so a new window re-keys on params, restarts the ladder, and the status
line names the window; with no window, or no column, the template carries no
predicate, so a table without that column keeps working. This is SD6's
human-owned predicate, filled from the Timeline rather than typed.

## Update — 2026-10-02: tile addressing measured; SD1 stands

The [map-tile-addressing](../trials/map-tile-addressing/README.md) trial
re-costed O2 against SD1 as built, on the local demo slice; its §0 is the
citable claim. Tiles drawn through a portolan pyramid cost the server about
what one raster per settled view does over a pan/zoom path, spending more on
views that show new ground and nothing on revisits, so they do not earn an ADR
superseding SD1 on this evidence. A source where one view takes seconds —
remote, or far larger — is where the answer could change; the trial's §6
lists what such an ADR would have to decide. The smaller changes it points at
instead are a multi-entry raster memo on the lane, starting the ladder lower
on a fast source, and SD10's deferred overscan margin.

## Update — 2026-10-02: a raster memo for revisits

The first of the map-tile-addressing trial's smaller changes: the panel keeps
the rasters it recently drew, keyed by the node key (SQL and every param —
table, ladder level, render, viewport and time window), in a byte-bounded
LRU with a few minutes' expiry. When the ladder restarts on a view the memo
already holds, it starts at the finest level held, draws it with no query,
stops whatever the lane was still running for the view left behind, and
climbs on from there if that was not the last level; the status line says
"from memory". Refresh empties the memo. The lane's own one-entry memo, which
every other pane shares, is unchanged. The mechanism is
`apps/play/play_map_memo.go`.

## Update — 2026-10-02: the ladder starts where the source is fast

The second of the trial's smaller changes. The ladder remembers how long each
level's table took the last time its result landed, and a restarted ladder
starts at the finest level that answered within `mapLadderFast` (300 ms)
rather than at the coarsest: on a source that fast the coarse levels only add
their own cost before the picture the full level draws almost as soon. A
level never measured, or one that ran slower, starts the climb at the bottom
again, so a slow view measured once puts the coarse levels back. On the
public ADS-B instance, where the 1 % level of a world view takes seconds,
nothing is skipped; on the local demo slice the first view climbs all three
levels and the views after it go straight to the full table.

## Update — 2026-10-02: SD7's margin, built

SD7 as first written — the requested box inflated beyond the view, capped
near 1536² — is now what the panel does. A settled view is widened by 25 % of
its span on every side, clamped to the world, and sized at the view's own
pixel scale; `mapMaxDim` rises to 1536 so a 1024-point view keeps its
resolution. While a settled view stays inside the box last requested at the
same scale, the panel emits that box again, so a small pan sends nothing; a
change of table, render or window re-queries the same box, and a zoom or a
pan past the margin asks again. `vp_*` therefore describe the requested box,
as SD6 and SD7 always said, not the view itself.

The map-tile-addressing trial's §7 measured the choice: on a path of small
pans a 25 % margin halved queries and server time, and on a path of zooms and
large pans it cost about a fifth more server time and a third more bytes. One
cost is new: a raster up to 1536 wide holds more non-empty pixels, so a dense
view comes nearer the public ADS-B user's 1,048,576-row result cap.

## Update — 2026-10-02: an area selection, published as signals

With **select area** on, a drag on the Map draws a box instead of panning —
portolan's `SetBoxSelect`, which reports the released box in
`Events.Selected` rather than zooming to it — and the panel publishes it as
play-wide signals: `area_min_x` … `area_max_y` (`UInt32`, mercator, folded
onto the one world the columns cover) and `area_min_lat` … `area_max_lon`
(`Float64`, degrees). They are seeded to the whole world, so a query reading
them runs before anything is selected and keeps every row; **Clear area**
returns them to it. The box stays outlined on the map and the status line
names it. The editor's own query is the report on what the box holds, so
SD10's hover→info deferral has its first half: an area, not a point.

The same change fixes the raster memo's reach: it now serves only a key
other than the one the lane itself last landed, so a fresh raster is no
longer called a memory and keeps its run's accounting.

## Update — 2026-10-02: a hover readout, opt-in

SD10's hover→info, without a second query. With the **readout** checkbox on,
the raster query also returns each non-empty pixel's row count and one figure
the render names (`rasterRender.readout` — mean altitude for "Altitude &
Speed", mean ground speed for "Speed", none for "Density" and "Custom"),
rounded to an `Int32`. The panel keeps them sorted by pixel beside the
raster (and in the raster memo); under the pointer it finds the pixel, on
any world copy, and the status line reads, for example, "under the pointer:
261 positions · 2,027 ft mean altitude". At a sampled ladder level the count
is scaled by the level's factor and marked "≈".

It is off by default because the columns cost bytes on every query. Measured
once on 2026-10-02 on the local demo slice at 1024×600: the count alone added
22–45 % to the lz4-compressed Arrow result, and the count with the figure
70–95 % (as a `Float64` the figure added more, which is why it travels
rounded); server time did not move.

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
- [Announcing adsb.exposed](https://clickhouse.com/blog/interactive-visualization-analytics-adsb-flight-data-with-clickhouse)
  (ClickHouse blog, 2024-04-24) — the published in-DB tile-rendering technique
  the bbox-variant query generalizes; the
  [repository](https://github.com/ClickHouse/adsb.exposed) is CC BY-NC-SA 4.0
  and is not a source for this ADR's text or the panel's code.
- [`walkers`](https://crates.io/crates/walkers) — slippy map widget; 0.53
  `with_layer` is the forward path for faithful tiles (O2/O3).
