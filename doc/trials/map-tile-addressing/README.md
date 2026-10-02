---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Two runs on one loaded machine,
> against a 1.39-million-row local slice; §0 says what that is worth. Do not
> cite as authoritative.

# Map tile addressing — bbox-per-view against tiles, measured

## 0 The claim, and how to cite it

**On the local ADS-B demo slice and an 11-step pan/zoom path at 1024×600,
drawing the Map from 1024-pixel tiles held by a portolan pyramid costs the
server about what one raster per settled view (ADR-0096 SD1, as built) costs
— medians of 1.99 s against 2.05 s in one run and 1.54 s against 1.49 s in
the other, where launches of one arm differed by up to a factor of two.** The tiles
get there by spending twice the bbox arm's server time (2.1 and 2.2 times)
and about twice its wire bytes on the five steps that showed new ground, and
nothing on the six that did not, where the bbox arm re-ran its whole ladder.
512-pixel tiles, nearer the six queries per view the earlier analysis
assumed, cost 1.5 and 1.7 times the bbox arm over the path: per-query overhead
outweighs their smaller overfetch on this slice.

**A view that needs new tiles completes later than its bbox raster: 144 and
160 ms for four 1024-pixel tiles fetched concurrently, against 80 and 84 ms
for the bbox query, at zoom 9 over Zürich on the full table; at the
whole-slice overview the two are even (104–114 against 119–125 ms).** At the
coarsest ladder level every arm draws its first pixels in 10 to 16 ms.

**The server query cache, at its default 1 MiB entry limit, refuses most
full-table entries of both the bbox arm (5 of 8 views) and the 1024-pixel
tiles (5 of 14), and keeps 34 of 36 512-pixel ones.** With a client byte cache
in front, the tile arms send the server no repeated key within a session;
the server cache pays only across sessions or viewers within its 60 s TTL.

| 11-step path, 1024×600, ladder on, no server cache; one local server | `bbox-sd1` — SD1 as built | `tile1024` — 1024-px tiles, zoom offset −2 | `tile512` — 512-px tiles, zoom offset −1 |
| --- | --- | --- | --- |
| Queries sent, whole path (identical in both runs) | 33 | 42 | 108 |
| Rows read, whole path (identical in both runs) | 8.96 M | 5.35 M | 5.49 M |
| Wire bytes, whole path, Arrow (same in both runs to within 1 %) | 18.7 MB | 16.5 MB | 15.5 MB |
| Server time, whole path, median of 3 launches, run 1 / run 2 | 2.05 s / 1.49 s | 1.99 s / 1.54 s | 3.10 s / 2.48 s |
| Server time on the 5 steps with new tiles, run 1 / run 2 | 0.95 s / 0.69 s | 2.01 s / 1.51 s | 3.06 s / 2.40 s |
| Server time on the other 6 steps, run 1 | 1.11 s | 0 | 0 |
| Server-cache hits, path run twice within the TTL, 2nd pass | 25 of 33 | 37 of 42 | 106 of 108 |

**What this trial does not say.**

- **Not "tiles halve the server cost."** The rows-read row (0.60 times) is
  the one figure that favours tiles by a margin; server time did not follow
  it, because a tile's fixed per-query cost and its overfetch (four
  1024-pixel tiles cover 6.8 times the view's area at zoom 9) offset the
  rows it does not read.
- **Not a property of tiles in general.** The total depends on the path:
  this one revisits or stays inside held tiles on six of eleven steps. An
  exploring path that never returns favours the bbox arm by the 2.1 to 2.2
  times of the new-tile steps; a path that pans back and forth more favours
  tiles. The per-step figures are in each run's `cost.tsv`.
- **Not the `-nocache` arms' numbers as a proposal.** `tile1024-nocache` and
  `tile512-nocache` are the tile arms without the loader's byte cache, there
  only to show what that cache saves (30 and 72 queries) and that the server
  cache then catches the same repeats.
- **Not a remote or large source.** A remote source can take seconds per
  raster (the pane's `mapFetchTimeout` comment cites about 20 s over
  `remoteSecure`), so fixed per-query cost is
  a smaller share and reuse is worth more; nothing here measures that.
- **Not a frame-time or visual result.** No tile was drawn. §5 says what
  drawing them would take.
- **Not reviewed, barely replicated.** One machine, a low-power APU, loaded
  by other sessions (load average 4 to 6 on 8 threads); two runs of the
  same build; one dataset. Counts, rows, bytes and cache hits are identical
  between runs; server time for one arm's whole path differed between
  launches by up to a factor of two, so whole-path differences of a few
  tens of percent are not differences. The new-tile steps' ratio (2.1 and
  2.2) held in both runs.

**If you need a number**, take it from a run's TSVs —
[the first run](./runs/2026-10-02-first-run/) and
[the repeat](./runs/2026-10-02-repeat/) — which hold one row per query with
raw microseconds, rows and bytes. **No figure from this trial travels without
the pair of arms it compares and the path it was measured on.**

## 1 Question and scope

The [ADS-B comparison](../../adr-background-work/adsb-exposed-comparison.md)
(§4.1) argued that ADR-0096 rejected tile addressing (O2) for reasons that
rested on the `walkers` crate, which
[ADR-0204](../../adr/0204-leaflet-map-core-port.md)'s portolan has replaced, and
listed what tiles would buy and cost. This trial measures those items, so
that an ADR superseding
[ADR-0096](../../adr/0096-play-geo-raster-map-panel.md)'s SD1 is written, or
not, on numbers. The domain question leads here; the toolbelt side is what it
took to measure it and what got in the way, recorded as findings in the
[logbook](./logbook.md).

In scope: server cost, client-side reuse, server query cache, time to first
and complete pixels, result shape (Arrow against PNG), and the brightness
normaliser across addressing schemes. Out of scope: drawing tiles in the
pane, the desktop and browser hosts, any table but the ADS-B slice, and any
server but the local one.

## 2 The workload

- **Data.** `planes_mercator` (1,385,478 rows, one day around Zürich and
  southern Germany) with `_sample10` and `_sample100`, ordered by
  `mortonEncode(mercator_x, mercator_y)`, as the ADS-B how-to builds them.
- **Viewport.** 1024×600 logical points; zooms are whole numbers except in
  the brightness check, which also uses the pane's continuous zoom.
- **Path.** Eleven settled views (`sequence` in
  [harness/sequence_test.go](./harness/sequence_test.go)): the slice at
  zoom 7; zoom 8; Zürich at zoom 9; a pan east by a third of the view, then
  by two thirds more; a pan south by half; back to the zoom-9 view; zoom
  out to 8 and in to 9 again; zoom 10; back to the first view.
- **Query.** The Map's template — geometry and density header, the default
  "Altitude & Speed" colour block, sparse `(pos, r, g, b, a)` rows — copied
  into [harness/common_test.go](./harness/common_test.go) from
  `apps/play/play_map.go` at the commit a run records. A tile's query is the
  same template with the tile's mercator bounds (what
  `MVTBoundingBoxMercator(z, x, y)` returns) and its size as the `vp_*`
  values.

## 3 Arms

| Arm | What sends queries | Client-side reuse |
| --- | --- | --- |
| `bbox-sd1` | One raster per settled view, the ladder climbing `_sample100` → `_sample10` → full each time | The lane's one-entry memo on (SQL, params) |
| `tile1024` | A second portolan `Pyramid` on the same `View`, `TileSize` 1024, `ZoomOffset` −2 (one data pixel per point at whole zooms), the ladder run per tile | The pyramid's retention and `KeepBuffer` (2), then a 512-tile byte cache like the loader's |
| `tile512` | As `tile1024`, `TileSize` 512, `ZoomOffset` −1 | As `tile1024` |
| `tile1024-nocache`, `tile512-nocache` | As above | The pyramid only — shows what the byte cache saves |

What each arm sends is counted by driving the real `portolan.View` and
`portolan.Pyramid` through the path, not assumed. The per-tile ladder is
modelled as three queries per tile miss with only the finest kept; the
loader's actual contract has one arrival per request (finding F2).

## 4 Method

- **Run** `measure.sh <slug>`: it records the environment and runs the
  integration-tagged tests in [harness/](./harness/) against the server at
  `CLICKHOUSE_URL` (default the local one). Every query carries a `query_id`
  prefix per test, arm and repetition; server time, rows and bytes are read
  back from `system.query_log`.
- **M1 server cost** (`TestServerCost` → `cost.tsv`): each arm's queries in
  path order, `use_query_cache=0`, three launches with the arm order rotated.
- **M2 reuse** (`TestSequenceCounts` → `counts.tsv`): per step, tiles
  requested by the pyramid, answered by the byte cache, and queries sent.
- **M3 server cache** (`TestQueryCache` → `querycache*.tsv`): each arm's
  queries twice in a row with `use_query_cache=1` and an arm-specific
  `query_cache_tag`, server defaults otherwise; hits from `ProfileEvents`,
  entry sizes from `system.query_cache`.
- **M4 first and complete pixels** (`TestFirstPixels` → `first-pixels.tsv`):
  the zoom-7 and zoom-9 views, coarsest and full level; the bbox query, the
  view's tiles one after another, and six at a time; client wall clock;
  seven launches, modes rotated.
- **M5 result shape** (`TestResultShape` → `shape.tsv`): every distinct tile
  and level as sparse Arrow and as ClickHouse's `PNG` output format (explicit
  `x`/`y`, RGBA, `output_format_image_width`/`height` set to the tile size);
  bytes, server time, and Go decode time — the Arrow scatter of the pane
  against portolan's `decodeTile` path (`image.Decode`, a draw into NRGBA,
  packing) — median of five decodes; and a pixel-by-pixel equality check.
- **M6 brightness** (`TestBrightness` → `brightness.tsv`): adjacent tiles
  against one raster spanning both; a view's raster against the tiles under
  it at whole and fractional zoom; a tile against its four children (a zoom
  offset of one, as a device-resolution option would ask for).

## 5 Results

### Reuse (M2)

Over the path the bbox arm sent 33 queries and answered none locally: every
settled view changes the `vp_*` values, so the memo never hits and the ladder
restarts. The tile arms sent queries on five steps only. The pans of a third
and of a half stayed inside tiles already held, and the pan back found the
zoom-9 tiles still in the pyramid, kept by `KeepBuffer`. The zoom out and in
again and the return to the overview were answered by the byte cache:
10 requests for `tile1024` and 24 for `tile512`, each saving its three
ladder queries.

### Server query cache (M3)

Within one pass the bbox arm hit the server cache on its three exact
revisits at the two coarse levels (6 of 33); its full-level revisits were
over the entry limit. A second pass hit 25 of 33. Stored entry sizes were up
to 0.69 MB for the bbox arm, 0.98 MB for 1024-pixel tiles and 0.92 MB for
512-pixel tiles (compressed, `query_cache_compress_entries=1`). The refused
1024-pixel tiles were the five densest: the zoom-5 and zoom-6 tiles over the
slice and three of the four zoom-7 tiles around Zürich. The server's
`query_cache_ttl` is 60 s, and the pane does not set one, so a revisit a
minute later misses on either arm.

### Result shape (M5)

The PNG output format exists on this server (26.8) and decoded to exactly
the Arrow scatter's pixels for all 150 tile results. Totals over the
distinct full-level 1024-pixel tiles of the path:

| 14 full-level 1024-px tiles, first run | Sparse Arrow (the pane's form) | `PNG` output format |
| --- | --- | --- |
| Bytes | 13.7 MB | 5.6 MB |
| Server time | 1.17 s | 1.59 s |
| Go decode, median of five | 60 ms | 223 ms |
| Largest single tile | 3.2 MB | 1.6 MB (under the loader's 4 MiB cap) |

PNG costs a fixed server encode — 34 ms for a nearly empty tile against
11 ms as Arrow — and three to four times the decode, and saves about 60 % of
the bytes. On a local link the bytes are not the bottleneck; over a slow one
they would be.

### Brightness (M6)

The normaliser derives `zoom_factor` from pixel area only, so:

- **Adjacent tiles agree exactly.** Two neighbouring zoom-7 tiles against one
  2048×1024 raster of both: 0 differing pixels of 439,378 non-empty.
- **At a whole zoom a view's raster and its tiles agree exactly.** portolan
  snaps the pixel origin to whole pixels, so the view's grid falls on the
  tiles' grid: all 176,298 non-empty pixels have equal transparency.
- **At the pane's continuous zoom they do not.** At zoom 9.4 the tiles
  (zoom 9, rounded) have pixels 1.32 times wider than the view's and at 9.5
  (rounded to 10) 0.71 times; the mean absolute difference in transparency
  is 0.015 on a 0 to 1 scale, and the median per-pixel ratio is 0.95 and
  1.07. The bbox raster's own brightness moves with continuous zoom just as
  much; tiles make it move in steps instead.
- **A zoom offset of one dims dense pixels.** Children against their parent,
  where all four children are non-empty and none saturates, have 0.85 of the
  parent's transparency (545 pixels); 2^(−1/5) = 0.87 is what the formula
  predicts for uniform density. Counts are conserved exactly. This is the
  dimming the comparison recorded for device-resolution tiles.

### Drawing the tiles (not done)

The visual spike was skipped. What it would need, from reading portolan:
the pane would own a second `Pyramid` and `TileLoader` (both exported),
`Sync` them from the `ViewEvents` that `Map.Render` returns (a frame after the
overlay callback that would draw them), `Tick` them, and draw
`Pyramid.Draw`'s tiles with `Projector.Image` at `TileBounds`, one image key
per tile. The
loader's `TileFetcherI` takes a URL and returns bytes, so a ClickHouse
fetcher fits it with the PNG format, or an Arrow decode in its place. No
portolan change appears needed for one level per tile; the per-tile ladder
does not fit (F2).

## 6 What this means for an ADR

**Does tile addressing earn an ADR superseding SD1? Not on these numbers.**
On a local, small source it moves cost from revisits to first visits, with
no change in total. It also makes each fresh view about twice as expensive
and twice as slow to complete. Superseding SD1 would also unsettle the
`vp_*` contract, the lane's cancellation and the ladder's one-arrival shape.
Nothing measured here pays for that.

**What would change the answer** is a source where one view's full level
takes seconds: a remote or billion-row table, or several viewers on one
server. There, fixed per-query cost stops mattering and reuse is worth its
overfetch. A rerun of this protocol against such a source, with
`CLICKHOUSE_URL` pointed at a server one may load, is the evidence an ADR
would need.

**If such an ADR is written, it must decide:**

- **Tile size and zoom offset.** 1024 sends fewer queries and overfetches
  more; 512 fits the server cache and cost 1.5 to 1.7 times as much per path here.
- **Result shape.** Arrow scatter or `PNG` bytes through the loader as it is.
- **The ladder per tile.** The loader delivers one arrival per request (F2).
- **Cancellation.** A per-tile context in the loader replaces one `query_id`
  with `replace_running_query`.
- **What `vp_*` means.** The view, not the query.
- **The world edge.** Tile bounds reach 2^32, which the `UInt32` slots cannot
  hold (F1).
- **Brightness under continuous zoom.**
- **Cache entry limit and TTL**, which it should set rather than inherit.

**The smaller changes the numbers point at instead:**

1. **A multi-entry raster memo on the lane.** A revisit would then be
   served from the client and start at the finest level held. On this path
   the three exact revisits are 9 of the bbox arm's 33 queries and about
   0.6 s of its 2.05 s (first run).
2. **Start the ladder lower when the source is fast.** On this slice the two
   coarse levels add about 45 % to the full level's server time (0.62 s on
   1.40 s; 0.44 s on 0.95 s), and the full level never took more than about
   0.2 s for a view. Choosing the start level from the last full level's
   elapsed time is a change inside `play_map_ladder.go`.
3. **SD10's deferred overscan margin.** A bbox inflated beyond the view,
   reused while the view stays inside it, is what absorbed the small pans
   for the tile arms. It was not measured; it would carry the same
   overfetch the tiles did.
