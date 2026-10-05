---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-10-01 as background to
> [ADR-0096](../adr/0096-play-geo-raster-map-panel.md), which is `accepted`.
> Nothing here is a decision. §4 lists candidate changes for whoever revisits
> ADR-0096 to take or leave; §2 describes upstream as its public prose
> described it on the compile date, **not as it stands when you read this**.
>
> **Provenance — clean-room.** Every statement about adsb.exposed rests on
> public prose: the repository README as rendered on the GitHub landing page,
> the titles and descriptions of its pull requests, the messages of its
> commits, one GitHub issue, two ClickHouse blog posts, one ClickHouse release
> presentation, and two pages of ClickHouse's own documentation. **No source
> file of adsb.exposed was read** — not `index.html`, `config.js`, any `.sql`,
> `.sh` or `.py` file, no raw file view, no "Files changed" tab, diff, patch or
> blame view, and not the live site, whose response is its source. The
> repository was not cloned. Pull-request descriptions and commit messages were
> read as JSON from GitHub's REST list endpoints for pull requests and commits;
> those endpoints carry titles, bodies and messages and no patch content. Those
> descriptions quote identifiers and short SQL fragments of their own; this
> page describes the behaviour they report and copies none of it.
> adsb.exposed is licensed CC BY-NC-SA 4.0. The README and the first blog post
> were read through a page-fetch tool that returns a model's summary of the
> page, which is not reliable for exact values, so a figure from those two
> sources is given only where it sits inside quotation marks in the tool's
> output. ADR-0096's own description of the upstream query was **not** used as
> a source for anything said here about upstream. What is inferred rather than
> read is marked *(inference)*. §7 lists every page with its URL.

# adsb.exposed against play's Map pane

## 1 Question, scope and method

ADR-0096 built play's Map pane on a technique taken from ClickHouse's
adsb.exposed demo: points stored in Web-Mercator integer coordinates, binned
into a pixel grid and coloured inside the database, the query result being the
image. The ADR was written in June 2026 and took one snapshot of upstream. The
questions here are **how upstream has changed since, especially since late
June 2026, and which of those changes bear on decisions ADR-0096 took or
deferred.**

Boxer's side was read from the tree on the compile date: ADR-0096 (SD1–SD10,
the SD10 deferrals and its dated Updates), ADR-0097 for the signal store the
pane's query rides on, ADR-0204 (the `portolan` map kernel that replaced the
`walkers` binding), ADR-0251 (`proposed`; the time strip), the howto
[play-adsb-map](../howto/play-adsb-map.md), the demo loader under
`apps/play/demo/adsb/`, and `apps/play/play_map.go` (`MapDriver`). A statement
about boxer that rests on code names the symbol.

## 2 How upstream changed — a dated timeline

Dates are the creation date of a pull request, or the commit date of a commit
pushed without one. Pull-request numbers are upstream's.

### 2.1 Before ADR-0096 (2024-04 to 2026-03)

- **2024-04-24 — launch.** The announcement post describes the shape that
  ADR-0096 took over: a Leaflet map in two layers, the data layer a grid of
  canvas tiles of "1024x1024 size to lower the number of requests to the
  database"; three levels of detail where "loading starts with a 1% sample to
  provide instant response", then 10 %, then the full table; a pixel colour
  computed from per-pixel aggregates with a power function "for better
  uniformity"; an area report on a right-mouse selection; a picture of an
  aircraft type fetched from the Wikipedia API when the cursor rests on that
  type in a report; tiles cached client-side in a plain JavaScript object, with
  the stated cost that after a while "the page will eat too much memory"; a
  locally attached SSD cache on each server replica, with requests pinned to a
  replica for cache locality; a progress bar fed from the cluster's process
  list; and shareable links, the edited query being "converted to a 128-bit
  hash and saved in the same ClickHouse database".
- **2024-05-04 — a time slider is asked for** (issue #22), with the
  workaround of a hand-written time predicate in the query. The issue is now
  closed; #65 (§2.2) is the pull request that delivered one.
- **2025-05-02 to 2025-05-25 — several datasets.** "Generalize levels of
  detail in code" (#33, "Now we can easily support other datasets"), "Support
  for mapping multiple datasets" (#36), "Restore queries from history" (#37),
  then bird observations (#42), a "You" dataset that bypasses the cache (#48,
  #49), a switch to disable the client cache too (#50), and geotagged photos
  with a photo layer (#52, #53).
- **2026-03-14 to 03-17.** Self-hosted servers dropped for the cloud service
  only (#58); a user's edits to the `WHERE` clause carried across a switch of
  example query (#60, #61).

Nothing landed between 2026-03-17 and 2026-07-19 — neither a pull request nor
a commit on `main`. Late June 2026, when ADR-0096 was written, therefore saw
the 2026-03 state.

### 2.2 Since late June 2026

- **2026-07-19 — Ships.** An AIS dataset with three sampling levels "same
  pattern as Planes", seven preset queries and a report (#62, #63). A move to a
  query-parameter form of sticky routing (#64) was closed unmerged.
- **2026-07-23 — the time slider** (#65). Each dataset declares its time
  column and an optional predicate that keeps sentinel dates out. Under the
  examples row sits a chart of activity per day, computed from the most
  heavily sampled table, honouring the current filters but with the tile
  predicate neutralised, so it describes the whole dataset. Two handles narrow
  a window, and the window's condition is then added to the tile queries, the
  area reports and the photo overlay; while the window is not narrowed the
  queries run unchanged. The window can be dragged whole, reset by a button or
  a double-click, and read under the cursor. It is written into the URL and
  browser history. With an area selected, the chart switches to that area's
  share of the total per day, smoothed by a constant of 3 % of the busiest
  day's total so that an almost empty day does not spike. The same change made
  refreshes seamless: a tile keeps its previous image until the new one
  arrives, the slider refreshes tiles in place instead of tearing the layer
  down, tile fade is off, and a per-tile token stops a superseded response
  from overwriting a newer one. Follow-ups: the histogram's query-cache
  entries are capped at one hour so the most recent days do not go stale
  (#80, which also notes that a read-only user must be allowed to set the
  TTL); a thin window keeps a draggable middle (#84).
- **2026-07-23 — basemap visibility in the URL** (#66).
- **2026-07-24 — a 3D globe** behind `?3d=1` (#68). CesiumJS is loaded only
  when the mode is on. It drapes the same density tiles over a sphere through a
  custom imagery provider that "reuses the exact tile pipeline", with the same
  1 % → 10 % → full ladder, keep-until-loaded replacement, box selection and
  photo billboards, and states that draping corrects the flat map's
  high-latitude density distortion. #79 fixed how a selection carried in the
  URL appears on the globe.
- **2026-07-24 — selection reports made visible.** Report queries stream
  progress and end with elapsed time, rows and bytes processed (#71); the
  stated reason is that at conferences the reports appeared so fast "people
  don't believe ClickHouse is running the queries in real time". Concurrent
  report queries share one progress bar, guarded by a sequence number against
  a superseded selection. Tile queries were left as they were.
- **2026-07-24 — fixes around selection and hover.** Longitudes normalised
  into [−180, 180) before projection, because the map scrolls past the
  antimeridian and selections there came back empty (#70; a box straddling the
  antimeridian is left unhandled). A hover-token guard stops a late Wikipedia
  lookup from showing the previously hovered aircraft (#69). HTML from
  Wikimedia attribution fields no longer reaches the live page (#75).
- **2026-07-24 — the active filter shown** (#74). A condition added on top of
  the example's own `WHERE` — by clicking a report row, or by hand — is shown
  as a removable slab, so a reader who arrived by a shared link knows the map
  is narrowed. #83 made removal work on a query restored from a link.
- **2026-07-24 — device-resolution tiles, opt-in** (#76). A 1024-pixel tile
  had one data pixel per CSS pixel, so on a high-DPI screen each data pixel
  covered several device pixels. `?res=2` halves the tile's footprint and asks
  for a tile one zoom level deeper; `?res=4` supersamples. It is off by
  default because, measured over Europe at full detail, the four deeper tiles
  covering one tile came to 4.1× the bytes, and the setting moves every
  server-side cache key. The description leaves two follow-ups open: the 3D
  path does not take the shift yet, and the brightness normaliser dims a finer
  grid because per-pixel counts fall four-fold per level while the normaliser
  only halves.
- **2026-07-24 — OpenStreetMap** (#72): 10.7 billion nodes, ten visualisations,
  four drill-down reports and the time slider over edit timestamps. The
  description gives 9.3 s for the worst-case world tile on the full table, a
  one-off because the website user has "a 100-day query cache", and 0.29 s at
  the 1 % level.
- **2026-07-25 to 07-27 — ten more datasets** (#81 and the commits on its
  branch): biodiversity records, buildings, weather stations, taxi trips,
  fires, lightning, population, transit, plus a source column for Ships. Each
  follows the established recipe of a Mercator table with a Morton-ordered key
  and sample companions. Three were later taken off the menu with their tables
  kept. Along the way:
  - **A sampling policy by size.** Below roughly a billion rows no sample
    tables; up to roughly ten billion, one sample (moved from 1 % to 10 % for
    preview fidelity); above that, both; and no sampling at all up to two
    billion rows in the later revision.
  - **Per-dataset bounds**: switching to a regional dataset moves the map if
    the view does not overlap it.
  - **Alpha as confidence.** Several metric maps encode the metric in hue and
    drive alpha from density, with floors so colour survives at low density.
  - **Weather as a filled field.** Station data first became a Gaussian-kernel
    interpolation over a per-station climatology table (no time slider, since
    climatology has no time); a day later it was replaced by a mip-map style
    fill computed inside the tile query — aggregate to a fine grid, reduce by
    2×2 to coarser grids, and take each pixel from the finest level that has
    data — with coarser levels drawn fainter and alpha scaled by the
    observation count, which brought the time slider back. The commit messages
    record the query-shape lessons: nested array levels were re-inlined by the
    analyzer and grew super-linearly, rebuilding each level as a grouped
    aggregate made the chain linear, and a tile's work came under a second at
    any zoom.
  - **Pruning.** A Morton-range predicate on the weather tiles overflowed at
    low zoom and blanked all but one tile; it was then dropped because a plain
    bounding-box predicate over minmax skip indexes "prunes to the exact same
    granules".
  - **Area reports grew charts**: per-metric sparklines with 1–99 % bands, a
    bucket width chosen from the selection's time span, gaps drawn as gaps,
    and a shared hover crosshair.
- **2026-08-10 — vessel names in the Ships report** (#86).
- **2026-09-26 — external-link datasets** (#88): a dataset entry may be a link
  to another site, the first pointing to an image-embedding viewer.
- **Release 26.8 call.** ClickHouse's release presentation used adsb.exposed
  data for an animated PNG, a year of traffic over one airport at one frame
  per week. The `PNG` output format it rests on renders a result as an image —
  implicit scanline order or explicit `x`/`y` columns, RGB, RGBA or grayscale
  by column name, an animated PNG when an integer `t` column is present. The
  documentation does not say when the format was introduced.

*(inference)* Upstream's direction since July is breadth (seventeen datasets
were configured at one point), a time axis, and the report as an analysis
surface; the tile engine itself — 1024-pixel canvas tiles, the three-level
ladder, RGBA bytes per pixel — was not changed, only reused (3D) and refined
(`?res=`).

## 3 Side by side

"Upstream" is as of the compile date per §2; "boxer" is play's Map pane.

| aspect | upstream | boxer (play Map pane) |
|---|---|---|
| addressing | XYZ tiles of 1024 px in a Leaflet grid layer | one raster per settled viewport (SD1); the bbox inflation SD7 describes was not built (ADR-0096 2026-10-01 Update) |
| raster size | fixed 1024² per tile; `?res=` trades bytes for detail | the map's view size clamped to `mapMaxDim` (1024) per side; see §4.5 on DPR |
| sampling | 1 % → 10 % → full in sequence; per-dataset policy by row count | one table chosen in the pane; a `sampling` slider scales brightness only; the ladder is an SD10 deferral |
| refresh | keep-until-loaded per tile, per-tile token against stale responses | last-good raster re-projected until the new one lands; supersession by `query_id` + `replace_running_query` (SD9) |
| time | per-dataset column, day histogram, brushed window injected into tiles and reports, kept in the URL | none in the Map pane; the Timeline publishes its data extent as `tl_min`/`tl_max` (ADR-0097), not a brushed window; ADR-0251's strip serves stepped series |
| area selection | right-drag box → streamed reports with progress, sparklines, drill-down filters | none |
| hover | Wikipedia picture over a report row; time readout on the histogram | none on the raster; deferred in SD10 (`portolan` exposes `Map.Hover`) |
| colour | many preset queries per dataset; whole query editable | four renders (Altitude & Velocity, Density, Speed, Custom) spliced into a panel-owned template (2026-07-10 Update) |
| sparse data | in-query mip-map fill, alpha by count and by fill level | nothing beyond alpha 0 on empty pixels |
| filter visibility | active filter shown as a removable slab | no user predicate exists to show |
| client cache | tiles in memory, can be disabled | a one-entry memo on the node lane (`nodeLane`); the raster texture by version |
| server cache | query cache, long TTL for tiles, one hour for the histogram; replica pinning | no query-cache setting on the Map's requests |
| progress | progress on tiles and, since #71, streamed on reports | per-lane progress and stats (ADR-0115) |
| datasets | menu of datasets, each a config entry | any table with `mercator_x`/`mercator_y`; free text source field |
| 3D | CesiumJS globe draping the same tiles, behind a flag | none |
| shared state | URL carries dataset, view, query hash, basemap, time window, box, 3D, `res` | a desktop app; `BOXER_PLAY_MAP_*` seeds; `vp_*` restored from history (2026-07-11 Update) |
| basemap | faint OSM under the data, toggle in the URL | none by default (`noTiles`, SD8); `BOXER_MAP_TILE_URL` points every app at a self-hosted server |
| projection | `0xFFFFFFFF` world span | 2^32, aligned with ClickHouse's MVT space (2026-07-28 Update) |

## 4 What boxer could learn

Ranked by how much each would change for a reader of the pane against what it
costs. Each names the ADR-0096 item it touches; none is a decision.

### 4.1 Re-cost O2 now that the map kernel is Go

**Touches** O2's rejection, SD1, the SD10 "parent/child tile fallback" and
SD7's keepBuffer.

ADR-0096 rejected a ClickHouse tile source (O2) because the `walkers` crate
kept its lower-zoom fallback crate-private and per-tile calls would run from
the Rust render path. ADR-0204 has since replaced `walkers` with `portolan`, a
Go port of Leaflet's map kernel, which carries a tile pyramid with parent and
child retention, a `KeepBuffer`, a loader with a byte cache, and a
`TileFetcherI` seam that takes a URL and returns bytes, decoded as an image.
Both reasons for rejecting O2 rest on `walkers` and no longer hold
*(inference)*. Upstream's July changes show what tile addressing makes cheap:
keep-until-loaded per tile, the 3D view draping the same tiles, `?res=` as one
zoom offset, and server cache keys that repeat.

- **Change.** A `TileFetcherI` that maps a tile URL onto a raster query, with a
  `TileSource` whose `TileSize` and `ZoomOffset` give 1024-pixel tiles. The
  ClickHouse `PNG` output format would let the server return bytes the loader
  already decodes *(inference — not tried)*; the Arrow path the pane uses today
  is the alternative.
- **Cost.** Six concurrent tile queries per view against SD9's one
  query per lane; the brightness normaliser (`zoom_factor`) would be per tile
  zoom rather than per viewport, which is upstream's form; the reserved `vp_*`
  signals (SD6, ADR-0097) would no longer describe the query the raster came
  from and would become a description of the view only.
- **Does not settle.** Cache invalidation over live data (ADR-0096 O3's
  data-epoch point); whether the one-query panel lane (SD2) or the loader owns
  cancellation.

### 4.2 Take up the sampling ladder

**Touches** the SD10 deferral "progressive sample100 → 10 → full refinement
ladder".

Upstream has run the ladder since launch and, in July, added a policy for when
a dataset deserves sample tables at all: none under roughly one to two billion
rows, one in the middle, two above. The 3D path reuses it and colours its
progress bar by level.

- **Change.** The pane demands the coarsest level first and the next once one
  lands, serving last-good in between, which the node lane already does. The
  `sampling` factor already in the template is the brightness correction per
  level.
- **Cost.** A naming convention for sample tables beside the base table, or a
  per-source list; `apps/play/demo/adsb/setup.sql` already creates them.
- **Does not settle.** The demo README notes that at local slice sizes the
  samples "matter at billions of rows, not here"; the trigger SD10 names may
  not arrive for local data. Upstream's size policy is a reason to make the
  ladder per source rather than global.

### 4.3 A time window on the raster

**Touches** SD6's human-owned predicate (the 2026-08-15 Update names it as the
half SD6 never plumbed a control for), ADR-0097's signal store, and ADR-0251's
deferred "range as signals".

The time slider is upstream's largest change since June. Its parts separate:
a per-source time column, a cheap histogram from the most-sampled table, a
brushed window injected as a predicate into every map and report query, and an
area-relative view of the histogram.

- **Change.** Two window signals the raster template reads in an optional
  predicate; the source of the window is a separate question. ADR-0251's
  `timescrubber` is built for steps at instants with playback, not a brushed
  range over a daily histogram; the Timeline widget (ADR-0043) has brushing
  but publishes only its data extent (`tl_min`/`tl_max`). *(inference)* A
  brushed window on the Timeline published as signals would reach the Map with
  no new widget.
- **Cost.** One histogram query on its own lane; a `DateTime64` signal pair;
  a template change per source because the column name varies (upstream
  declares it per dataset).
- **Does not settle.** Whether the window belongs to the Map or to play as a
  whole; the area-relative variant needs §4.4 first.

### 4.4 An area selection published as signals

**Touches** the SD10 "hover→info secondary query" deferral, which is the
nearest item; ADR-0096 says nothing about area selection.

Upstream's report is a fixed feature with per-dataset queries. play already
has the general form: a pane publishes a signal and any query reads it
(ADR-0097 SD8). *(inference)* Publishing the selected box as four mercator
signals, alongside the viewport's, would let the editor's own query be the
report, and reuse play's per-lane progress, which is what #71 added upstream
for an audience.

- **Cost.** A box gesture on `portolan` that does not collide with box zoom;
  signal declarations; drawing the box.
- **Does not settle.** Antimeridian-straddling boxes, which upstream also
  leaves open (#70, #79).

### 4.5 Say what resolution the raster has

**Touches** SD7 and the brightness normaliser in the SD6 header.

SD7 says `out_w×out_h = screen px × DPR`, capped at about 1536². In
`play_map.go`, `clampDim` sizes the raster from `portolan`'s `View.Size` with a
ceiling of `mapMaxDim` = 1024, and no device-pixel-ratio factor appears in the
file. If `View.Size` is in logical points *(inference — not checked against the
host)*, a raster pixel covers four device pixels on a 2× display, which is the
defect upstream fixed in #76. Upstream's measurements are a guide to the cost:
4.1× the bytes for 2×, which is why it is opt-in there. Upstream also records
that a finer grid comes out dimmer under its normaliser; the boxer template
derives `zoom_factor` from pixel area *(inference: the same property should
follow)*.

- **Change.** Reconcile SD7's text with the code first; then decide whether DPR
  is an option.
- **Does not settle.** How much detail the cap of 1024 per side hides on a
  large window.

### 4.6 Use the server's query cache where keys repeat

**Touches** SD1 and SD9.

Upstream leans on the ClickHouse query cache — a 100-day entry lifetime for
the website user per #72, one hour for the histogram per #80 — and pins
requests to a replica because the cache "exists once per ClickHouse server
process". The Map's requests set no query-cache setting. Under bbox-per-view
(SD1) a pan changes the `vp_*` parameters, so a cache entry is hit only on an
exact revisit or a Refresh *(inference)*; under tiles (§4.1) keys repeat on
every pan back. This lesson mostly pays after §4.1.

- **Cost.** One setting; a read-only user may reject a TTL setting (#80);
  non-deterministic functions such as `now()` are not cached by default.
- **Does not settle.** Staleness on a table that is still being written.

### 4.7 Confidence as alpha, and fill for sparse sources

**Touches** the render-agnostic header of SD6 (2026-07-10 Update).

Upstream's July colour work converged on hue for the metric and alpha for
confidence — density, observation count, and fill level — with floors so
colour survives at low density. In `rasterTemplateSQL` the shared header fixes
`alpha` at 255 for every non-empty pixel, before the colour block is spliced
in, so density reaches the picture only as RGB brightness through
`transparency`, and a render cannot set alpha. Letting the colour block define
`alpha`, with 255 as the default when it does not, would cover upstream's
convention. The weather fill is a different query
shape (a reduction pyramid before colouring) that a colour block cannot carry;
ADR-0250's trigger for precomputed levels is the nearest boxer item.

### 4.8 Small items that cost little

- **Pruning confirmation (SD4).** Upstream found a plain bbox over minmax skip
  indexes pruned the same granules as an explicit Morton range, and that the
  Morton range overflowed at low zoom. SD4's choice of a bbox predicate is
  consistent with both; no change.
- **Antimeridian.** `bboxFromLatLon` clamps to the world edge through
  `clampMerc`. If `portolan`'s bounds run past ±180 as Leaflet's do
  *(inference — not checked)*, a view across the antimeridian draws the raster
  only up to the edge. Upstream normalised longitudes (#70).
- **Filter visibility.** If §4.3 or §4.4 add predicates, show them on the pane,
  for upstream's reason in #74: a reader forgets the map is narrowed.

## 5 Where boxer differs on purpose

- **One lane, not a tile engine (SD1, SD2).** The pane fits play's model of
  one result per query and a panel-local lane; upstream is a single-purpose web
  page. §4.1 is the case for revisiting this now that its cost has moved, not a
  case that the choice was wrong at the time.
- **No server (O3, O4).** Upstream is a browser client against a hosted
  service. ADR-0096 keeps rendering inside the desktop host with no tile
  server; ADR-0204 kept that true under the Go kernel.
- **Offline and capturable (SD8, criterion C3).** `noTiles` by default, so the
  air-gapped path and the headless capture both work. Upstream draws a faint
  public basemap under the data.
- **One exact quad (SD4, SD5).** The raster is mercator-uniform and pinned to
  bounds recovered from the served parameters, so it cannot disagree with the
  query.
- **Projection convention (2026-07-28 Update).** 2^32 to match ClickHouse's MVT
  space, against upstream's `0xFFFFFFFF`; a table copied from upstream reads up
  to one unit off.
- **A panel-owned template.** Upstream lets a reader edit the whole query and
  share it by hash. ADR-0096 moved the other way: the pane owns the template,
  the reader owns table, render and sampling, and the editor stays separate
  (2026-07-10 and 2026-07-11 Updates). Arbitrary SQL lives in play's editor.
- **Arrow over HTTP**, play's one result path, where upstream fetches raw RGBA
  rows compressed with zstd.

## 6 Open questions

1. Does `portolan`'s `View.Size` report logical points or physical pixels
   (§4.5)?
2. Would the `PNG` output format be available on the server version play is
   tested against, and is its decode cheaper than the Arrow repack (§4.1)?
3. Should a time window be play-wide or the Map's own (§4.3)?
4. Outside this page's question, surfaced while reading: ADR-0096 §SD6 says
   its bbox query keeps the upstream colour block and other parts "verbatim",
   and the demo loader's README says `setup.sql` is "adopted from" the upstream
   schema. Upstream is CC BY-NC-SA 4.0. Whether those passages need
   attribution, a share-alike note, or a rewrite is for the owner to judge;
   this page did not examine them further.

## 7 Sources

Each was read on 2026-10-01.

- README, rendered on the repository landing page —
  <https://github.com/ClickHouse/adsb.exposed> (read twice through the
  page-fetch tool; file names in the landing page's listing were seen, no file
  was opened).
- Pull-request titles and descriptions, #16 to #88, from
  <https://api.github.com/repos/ClickHouse/adsb.exposed/pulls?state=all&per_page=100>;
  the same text is on each conversation page, e.g.
  <https://github.com/ClickHouse/adsb.exposed/pull/65> (time slider),
  [#68](https://github.com/ClickHouse/adsb.exposed/pull/68) (3D),
  [#71](https://github.com/ClickHouse/adsb.exposed/pull/71) (report progress),
  [#72](https://github.com/ClickHouse/adsb.exposed/pull/72) (OSM),
  [#76](https://github.com/ClickHouse/adsb.exposed/pull/76) (device
  resolution),
  [#80](https://github.com/ClickHouse/adsb.exposed/pull/80) (histogram cache
  TTL),
  [#81](https://github.com/ClickHouse/adsb.exposed/pull/81) (ten datasets).
- Commit messages on `main` since 2026-03-01, from
  <https://api.github.com/repos/ClickHouse/adsb.exposed/commits?since=2026-03-01T00:00:00Z>
  (list endpoint: messages only, no patches).
- Issue #22, "Time slider" — <https://github.com/ClickHouse/adsb.exposed/issues/22>.
- "Announcing adsb.exposed", ClickHouse blog, 2024-04-24 —
  <https://clickhouse.com/blog/interactive-visualization-analytics-adsb-flight-data-with-clickhouse>.
- ClickHouse Release 26.8 call, slides —
  <https://presentations.clickhouse.com/2026-release-26.8/>.
- ClickHouse documentation, `PNG` format —
  <https://clickhouse.com/docs/reference/formats/PNG>.
- ClickHouse documentation, query cache —
  <https://clickhouse.com/docs/operations/query-cache>.

Search results also listed a "Birds in the cloud" blog post and a Hacker News
thread; neither was read, and nothing here rests on them. One search result
pointed at a shell script's file view in the repository; it was not opened.
