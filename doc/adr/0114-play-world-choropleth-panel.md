---
type: adr
status: proposed
date: 2026-07-11
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0114: `play` schematic world-choropleth result pane

## Status

Proposed. The design dialogue settled the asset form (verbatim gzipped
Natural Earth 110m), the projection (Natural Earth), the matching contract
(ISO codes + names) and that this ADR precedes the implementation.

Implemented in-tree (uncommitted): `widgets/worldmap` (asset + resolver +
projection + rasterizer + widget, unit-tested), the play World dock tab
(`play_world_panel.go`, panel tests), a widgets-tour demo, and the
`SPINNAKER_PLAY_FOCUS_WORLD` knob. Verified per §Validation: unit suites
green, and a live drive (egui inspection) confirmed hover ("Brazil (BRA) ·
216.4") and click→selection (Detail followed to the clicked row); scripted
captures confirm the choropleth, legend, and status line against a
17-row `values()` result with one deliberate unmatched key.

## Context

`play` renders query results through a dock of panels driven by the ADR-0097
channel negotiation: a `PanelI` declares typed input channels, accepts or
rejects the observed node's Arrow schema, and renders the claimed result. The
Table, Projection, Timeline and Schema tabs are observers of the active result;
the Map tab (ADR-0096) is different — it runs its *own* raster query on a
panel-local lane against a table the user names, and needs the walkers slippy
map (pan/zoom, camera feedback).

Results that carry one row per country — ISO codes or country names plus
metrics — have no spatial rendering today. The Map tab does not help: it
assumes point data in Web-Mercator columns, and a tiled, pannable map is the
wrong instrument for "which countries, roughly how much" — the reading a
choropleth answers at a glance. The ask is a *schematic* overview: the whole
world visible at once, fixed camera, countries filled by value.

Substrate facts that shape the design:

- egui fills **convex** polygons only (`Shape::convex_polygon`); country
  outlines are concave with holes, so per-country painter fills render wrong.
- imzero2 already has everything needed for a Go-rasterized image: `c.Image`
  (content-versioned RGBA texture — re-uploaded only when the version bumps),
  `PaintSenseRegion` + response registers for hover/click (the layeredgraph
  idiom), and the `colormap` widget (palette stops, value→color, legend).
- The `H3Region`/`H3CellsColored` overlays exist Rust-side but draw *on the
  walkers map* — they inherit exactly the pan/zoom chrome this pane avoids.

## Decision

A new reusable widget package
`public/thestack/imzero2/egui2/widgets/worldmap` — a schematic world
choropleth drawn entirely Go-side — and a thin `play` result pane ("World"
dock tab) that feeds it from the observed result. No Rust/IDL changes.

### SD1 — Geometry asset: verbatim Natural Earth 110m, gzipped, embedded

The country outlines are `ne_110m_admin_0_countries.geojson` from Natural
Earth (public domain), embedded **verbatim** (gzipped, ~206 KB) via
`go:embed` and parsed lazily on first use. Verbatim-upstream keeps provenance
auditable: the asset README records the source URL and the sha256 of the
uncompressed file, so anyone can diff against upstream. A preprocessed
compact binary (~80 KB) was rejected: it would need a converter and a bespoke
format to maintain, and breaks the checksum-against-upstream property.

Only five properties are consumed: `ADMIN`, `NAME`, `ISO_A2_EH`, `ISO_A3_EH`
(the `_EH` variants fix the upstream `-99` quirks for France, Norway and
Kosovo's alpha-2) and the geometry. Northern Cyprus and Somaliland carry no
ISO codes upstream and stay name-matchable only. 177 features, ~10.6k points
total — small enough that parse and rasterization are trivially cheap.

### SD2 — Projection: Natural Earth, fixed camera, explicit size control

Coordinates project through the Natural Earth projection (the Šavrič et al.
polynomial — a few lines of Go), aspect preserved. No pan, no zoom, no
tiles — an explicit requirement, and what "schematic overview" means.
Equirectangular was rejected for its polar stretch (Greenland and Antarctica
balloon); Robinson for needing an interpolation table where Natural Earth is
a closed formula designed for exactly this use.

The raster width is an explicit toolbar control (default 960 px, height
follows the aspect), **not** a fit-to-pane capture: the available-size
capture register (R18) is a single global last-writer-wins slot, and play's
editor pane already owns it every frame — a second capturer would clobber
editor sizing intermittently. Same reasoning, same idiom as the Map panel's
explicit width/height. The texture scales aspect-preserved into the pane
(`FitAspectMax`) between re-rasterizations.

### SD3 — Rendering: one Go-side rasterization into `c.Image`

Fills are produced by a scanline even-odd rasterizer over the projected
rings — even-odd handles concave outlines and hole rings uniformly, which is
precisely what the convex-only painter path cannot. The pass renders at 2×
supersampling and box-downsamples for anti-aliasing, baking country borders
(darker stroke) into the same buffer; alongside the RGBA buffer it fills a
1× country-index buffer for hit-testing. The result ships as one `c.Image`
whose `contentVersion` bumps only when (result fingerprint, pane size,
palette, value column) change — a still pane re-uploads nothing and re-sends
one cheap widget per frame. Per-triangle tessellation (streaming ~10k
painter polygons every frame, ballooning SVG exports) and in-DB
rasterization à la ADR-0096 (would require country polygons in the queried
database — a data-side dependency for what must work on *any* result batch)
were both rejected.

### SD4 — Country matching: ISO codes + names, resolver in the widget package

A value resolves to a country by, in order: exact `ISO_A2_EH` / `ISO_A3_EH`
match (case-insensitive), exact `ADMIN`/`NAME` match (case-insensitive,
trimmed), then a small alias table (e.g. "United States" vs "United States
of America", "XK"/"Kosovo"). Codes-only was rejected: ad-hoc SQL results are
as often name-keyed as code-keyed, and the pane should light up without
ceremony. The resolver lives in the widget package (reusable), the *column*
heuristic in the play panel (SD5).

### SD5 — Panel contract: claim, value channel, degenerate inputs

The pane is a `PanelI` observer of the active node (one required channel).
It claims a schema when a string-typed column **country-resolves**: ≥ 50% of
its distinct values (sampled) resolve per SD4. Name-hinted columns
(`country`, `iso`, `cc`, `nation`, …) are tried first, then remaining string
columns left-to-right — deterministic, no scoring. The value channel
defaults to the first numeric column, switchable via a combo (the
`colorByFeature` precedent). Degenerate inputs are surfaced, not guessed:
duplicate rows per country → last row wins plus a status-line count (GROUP
BY is the user's tool in a SQL playground — the pane must not silently
aggregate); unresolved values → status-line count; countries absent from
the result → neutral fill.

### SD6 — Interaction: hover tooltip, click → selection signal

Hover reads the pointer through a `PaintSenseRegion` over the image and maps
it via the country-index buffer — O(1), no point-in-polygon at frame time —
to a tooltip (country name · value). Clicking a country emits the selection
signal for that country's (last) row, the same viewof duality the Table
panel implements, so Detail and Table follow the click. The colormap legend
renders beside the map with min/max labels.

### SD7 — Deferred

Aggregation modes for duplicate keys, diverging palettes anchored at zero,
continent zoom presets, fuzzy name matching, and a per-country drill-down
query are all deferred until a concrete need shows up. None of them changes
the asset, projection, or panel contract above.

## Alternatives

- **Per-country `PaintPolygonFilled`** — egui's filled polygon is
  convex-only; concave country shapes render with artifacts. Killed by the
  substrate.
- **H3 cell regions** (`H3Region`/`H3CellsColored`) — needs the same polygon
  asset *plus* the walkers map this pane explicitly avoids; coarse cells
  erase microstates, fine cells explode cell counts. Right tool for
  geo-point overlays, wrong one for a fixed schematic.
- **Per-triangle tessellation via painter** — correctness OK, but ~10k
  shapes re-streamed per frame and SVG exports bloat; the raster ships once
  per data change instead.
- **In-DB rasterization (ADR-0096 pattern)** — couples a result-side pane to
  database contents; the World pane must render any result batch that names
  countries.
- **Compact binary / TopoJSON asset** — smaller, but adds a converter or
  decoder to maintain and loses verbatim-upstream auditability.

## Consequences

- ~206 KB gzipped asset in the binary; parse cost paid once, lazily.
- A new widget package with no FFI additions — works on every render host,
  including the headless SVG tour (textures are captured by the SVG
  visitor).
- Fully offline: no tile servers, no network — consistent with the
  sovereignty premise.
- The play dock gains an eleventh tab; the claim heuristic (SD5) can
  false-positive on short-code columns (language codes overlap ISO country
  codes) — mitigated by the name-hint ordering and the ≥ 50% distinct
  threshold, and harmless: the pane renders whatever resolves and counts the
  rest.

## Validation

- Unit: resolver (codes, names, `_EH` quirks, aliases, misses), projection
  sanity (sign, bounds, aspect), rasterizer determinism (hash of the RGBA +
  index buffers for a fixed input), panel accept/reject over synthetic
  schemas (country-only, no-string, language-code trap).
- Integration: screenshot tour entry; live run against a `VALUES`-literal
  countries query and a real dataset, hover + click verified via the
  egui_mcp driver.
