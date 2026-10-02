---
type: reference
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

# Map tile addressing — logbook

Chronological, append-only record of runs of the
[map tile addressing](./README.md) trial, per the
[directory convention](../README.md). Newest entry last. Each entry's raw
evidence lives in its own `./runs/<YYYY-MM-DD-slug>/` directory, which
`measure.sh <slug>` writes. Entry template:

```markdown
## YYYY-MM-DD — <slug> — <one-line outcome>

- **Build under test:** boxer <commit>, ClickHouse <version>
- **Environment:** <CPU, threads, memory, OS, load> — no hostnames or
  personal paths
- **Attempted:** <tests run, MTA_REPS>
- **Findings:** one line per proximate obstacle, per the trials README's
  *Finding classification*
- **Results:** <one line per measure>
- **Run dir:** <./runs/YYYY-MM-DD-slug/>
```

## 2026-10-02 — first-run and repeat — tiles break even with bbox-per-view over the path, and cost twice as much per fresh view

- **Build under test:** boxer d7518efc, the Map pane and portolan unmodified
  in the working tree; ClickHouse 26.8.1.1939, local server.
- **Environment:** AMD Custom APU 0932, 8 threads, 14 GiB with swap in use,
  Linux 6.11; other sessions loading the machine (load average 4 to 6 before
  and after each run).
- **Attempted:** all six harness tests, twice: `first-run` by invoking
  `go test` directly with the environment written by hand afterwards, and
  `repeat` through `measure.sh`. Same build, same tests, default repetitions
  (three launches for cost, seven for first pixels). The optional visual
  spike was not attempted (README §5).
- **Findings:**
  - **[missing geospatial → proposed:map-raster-tiles /
    functional-suitability.functional-completeness / S3]** The raster
    template's `{vp_*:UInt32}` slots cannot carry the right or bottom edge of
    a tile in the last column or row of the world, which is 2^32; the
    harness holds bounds as `UInt64` and asserts that no tile on the path
    reaches the edge (evidence: `harness/common_test.go`, `box`;
    `harness/sequence_test.go`, `TestSequenceCounts`).
  - **[missing geospatial → proposed:portolan-tile-loader /
    functional-suitability.functional-appropriateness / S3]** The portolan
    loader delivers one arrival per request and the pyramid loads a tile
    once, so the Map's sampling ladder, three results per tile each
    replacing the last, has no place in the contract; the harness models
    the per-tile ladder outside portolan (evidence:
    `harness/sequence_test.go`, `simulateTiles`).
  - **[pain play → proposed:play-map-template /
    maintainability.reusability / S4]** The raster template, the colour
    block and the projection are unexported in package `play`, so the
    harness copies them and the logbook pins the commit; a template change
    does not reach the harness (evidence: `harness/common_test.go`).
  - **[note geospatial]** portolan's `View` and `Pyramid` ran headless,
    without a host, and counted each arm's tile requests and reuse over the
    path with the shipped code rather than a model of it (evidence:
    `counts.tsv`).
- **Results:**
  - **M1 server cost, whole path, medians of three launches.** bbox-sd1
    2.05 / 1.49 s, tile1024 1.99 / 1.54 s, tile512 3.10 / 2.48 s
    (first run / repeat). On the five steps with new tiles: 0.95 / 0.69,
    2.01 / 1.51 and 3.06 / 2.40 s. Rows read: 8.96 M, 5.35 M and 5.49 M.
    Wire bytes: 18.7, 16.5 and 15.5 MB. Both runs gave the same rows and
    bytes. One arm's launches differed by up to a factor of two in the
    repeat.
  - **M2 queries sent.** bbox-sd1 33, tile1024 42, tile512 108. Without the
    loader's byte cache: 72 and 180.
  - **M3 server-cache hits, second pass.** 25/33, 37/42 and 106/108. The
    default 1 MiB limit refused 5 of 8 full-level bbox views and 5 of 14
    full-level 1024-pixel tiles.
  - **M4 complete view, zoom 9, full table, medians of seven.** bbox 80 /
    84 ms. tile1024 six at a time 144 / 160 ms, one after another 278 /
    300 ms.
  - **M5 result shape, full-level 1024-pixel tiles, first run.** PNG was
    pixel-identical to Arrow in all 150 results. It used 0.41 times the
    bytes, 1.36 times the server time and 3.7 times the Go decode.
  - **M6 brightness.** Adjacent tiles and a whole-zoom view matched their
    tiles exactly. At zoom 9.4 and 9.5 the mean absolute difference in
    transparency was 0.015. With a zoom offset of one, dense pixels kept
    0.85 of their transparency.
- **Run dirs:** [./runs/2026-10-02-first-run/](./runs/2026-10-02-first-run/),
  [./runs/2026-10-02-repeat/](./runs/2026-10-02-repeat/)
