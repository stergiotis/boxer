---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-08-22 as background to
> [ADR-0203](../adr/0203-map-widget-without-the-http-stack.md), whose Design
> space lists reimplementation of the map core (its O4/O5) only as a fallback.
> Nothing here is a decision; the decisions taken from it the same day are in
> [ADR-0204](../adr/0204-leaflet-map-core-port.md), and §9 below records which
> forks it closed. Provenance: (a) Leaflet figures were measured on the compile date
> from a checkout of upstream `main` (2.0.0-alpha.1, 2025-08-16), and its
> algorithms were read, not summarised from memory; (b) claims about this
> repository were verified against the working tree on the compile date; (c)
> the crate-closure figures are the ADR-0203 measurement re-run with walkers
> removed entirely; (d) effort figures are estimates.

# Porting Leaflet's map core to Go and Rust — what it would take, and what it would buy

## 1 Question and scope

[ADR-0056](../adr/0056-walkers-map-h3-binding.md) bound the `walkers` crate as
the basemap widget; [ADR-0203](../adr/0203-map-widget-without-the-http-stack.md)
measured that the crate costs the render client an HTTP/TLS stack it cannot opt
out of, and proposes an upstream feature gate with reimplementation held back as
a fallback. The question asked here is the third reimplementation strategy,
raised in the dialogue around that ADR: **not a clean-room rewrite and not an
adoption of walkers' own core, but a port of Leaflet's map kernel — its
projection model, view state, gesture handlers and tile pyramid — into a
mixture of Go and Rust code.**

Three things keep this from being a bigger question than it is:

- Leaflet is BSD-2-Clause. A port is a derived work with attribution, the form
  [ADR-0149 §SD8](../adr/0149-implot-core-port-painter-lane.md) already
  established for the ImPlot port; no clean-room discipline is required.
- Leaflet's *value* to this tree is its algorithms and its behaviour, not its
  code. Its code is DOM-shaped — tiles are `<img>` elements positioned by CSS
  transforms, zoom animation is a CSS transition on per-zoom-level containers,
  input arrives as pointer events. What transfers is the model underneath.
- The substrate a Go-side port needs was built for the ImPlot port and the
  node-editor survey
  ([snarl-port-analysis §4](./snarl-port-analysis.md)): the painter lane, its
  textured-rect command, per-canvas pointer and wheel readback. §4 checks this
  item by item.

## 2 Leaflet, measured

Upstream `main` is `2.0.0-alpha.1` (CHANGELOG dated 2025-08-16): 14,012 source
lines, 14,860 spec lines, ES modules and classes, pointer events only, no `L`
global. The algorithms are unchanged from the 1.9 line; 2.0 is the better
reading source because the browser-compatibility code is gone.

### 2.1 The kernel, by module

| Area | Files | Lines | Portable content |
| --- | --- | ---: | --- |
| geo | `LatLng`, `LatLngBounds`, `CRS` (+ `EarthCRS`, `EPSG3857`, `EPSG4326`, `EPSG3395`, `SimpleCRS`), projections (`SphericalMercator`, `LonLat`, `Mercator`) | 830 | all of it — pure math |
| geometry | `Point`, `Bounds`, `Transformation`, `LineUtil` (Douglas–Peucker simplify, Cohen–Sutherland `clipSegment`, closest-point), `PolyUtil` (Sutherland–Hodgman `clipPolygon`, centroid) | 957 | all of it |
| map | `Map.js` | 1,772 | roughly a third: view state, `setView`/`setZoomAround`/`fitBounds`/`getBoundsZoom`, `panInside`, `maxBounds` limiting (`_limitCenter`/`_limitOffset`/`_getBoundsOffset`/`_rebound`), `zoomSnap`/`zoomDelta`, `flyTo` (the van Wijk–Nuij trajectory, ~60 lines), the zoom-animation state machine, the `movestart/move/moveend`, `zoomstart/zoom/zoomend`, `viewreset` event model. The rest is panes, DOM events, container sizing, geolocation. |
| handlers | `DragHandler` + `Draggable`, `PosAnimation`, `ScrollWheelZoomHandler`, `PinchZoomHandler`, `DoubleClickZoomHandler`, `BoxZoomHandler`, `KeyboardHandler`, `TapHoldHandler` | 1,281 | about half: the inertia model (last 50 ms of positions → velocity × `easeLinearity` 0.2 → deceleration 3400 px/s² → `panBy` with ease-out), the wheel model (40 ms debounce, 60 px per zoom level, a sigmoid on accumulated delta, snap, anchored `setZoomAround`), pinch (scale from pointer distance, centre from pointer midpoint, `bounceAtZoomLimits`), box zoom, keyboard pan/zoom. The DOM capture/release scaffolding does not transfer. |
| tile pyramid | `GridLayer.js`, `TileLayer.js` | 1,194 | most of it: tile range from pixel bounds, `keepBuffer`, load queue sorted by distance to centre, `_retainParent` (up to 5 levels up) / `_retainChildren` (2 down) so already-loaded tiles cover the view while the current level loads, `_pruneTiles`, per-level scale transforms during zoom animation, 200 ms fade-in, `noWrap`/`bounds`/`_isValidTile`, `minNativeZoom`/`maxNativeZoom` over-zoom, URL templating (`{s}{x}{y}{z}{r}{-y}`, subdomains, `tms`, `zoomOffset`, `zoomReverse`, `errorTileUrl`). |
| everything else | `core/` (Class, Events, Browser, Util), `dom/`, `control/`, `layer/marker`, `layer/vector` renderers, popups/tooltips, `ImageOverlay`/`VideoOverlay`, WMS | ~6,600 | not ported — replaced by Go, egui widgets and the painter lane (§5). |

Reading all of the kernel is ~4,000–4,500 lines; the logic in it is perhaps
2,500. The resulting port is estimated at **2,500–3,500 lines of Go** (or
Rust) plus tests.

### 2.2 What Leaflet does that walkers does not

Measured against walkers 0.56's raster core (the one this tree runs):

- **Tile retention across zoom levels.** walkers fills a missing tile by
  drawing a quarter of its lower-zoom ancestor (UV sub-rect); Leaflet keeps
  every loaded ancestor and descendant that covers the view, scaled, until the
  current level has loaded and faded in — no grey or blurred flashes on zoom.
- **Animated zoom and pan** (`zoomAnimation` up to 4 levels, `PosAnimation`
  with ease-out, `flyTo`, `fitBounds`/`flyToBounds`), **bounded panning**
  (`maxBounds`, `maxBoundsViscosity`), **zoom snapping** (`zoomSnap`,
  `zoomDelta`, fractional zoom), **keyboard** and **box** zoom.
- **A CRS abstraction.** `EPSG3857` is one of four; `SimpleCRS` maps a flat
  pixel space, which turns the same widget into a tiled viewer for any large
  raster — a capability this tree does not have.
- **A specification suite.** 14,860 lines. The geo/geometry/CRS/projection
  specs (1,369 lines) port directly; `GridLayerSpec` (1,713 lines) pins the
  pyramid numerically — "loads 32, unloads 16 tiles zooming 10→11", "loads
  224, unloads 209 on a flyTo", `keepBuffer` and `noWrap` cases — and ports as
  long as the port exposes tile load/unload events; `MapSpec` (2,740) is
  mostly view-state API (`getBoundsZoom`, `fitBounds`, `panInsideBounds`,
  `setZoomAround`, `flyTo`, wrap); the handler specs (1,485) are pointer-event
  driven and port as sampled-input tests of the state machines.

What walkers has that Leaflet's raster core does not: nothing this tree uses.
walkers' MVT/vector tiles and PMTiles are feature-gated off here.

## 3 Three shapes for the Go/Rust split

The "mixture" in the question resolves into a choice about where the
interaction state lives. The three candidates, with Leaflet's modules assigned:

| | **S1 — Go kernel on the painter lane** | **S2 — Rust kernel** | **S3 — hybrid** |
| --- | --- | --- | --- |
| Go | CRS/geometry, `Map` view state + limits + animations, handlers as state machines fed by R23/R24 samples, `GridLayer` pyramid, `TileLayer` URL, fetch/decode/cache, overlays on the painter lane, app API | tile fetch (ADR-0165), app API, camera readback | `Map` model + `GridLayer` bookkeeping + fetch |
| Rust | nothing new — `paintImage`, clip, sense region, R23/R24 already exist | CRS/geometry, `Map`, handlers on egui input, `GridLayer`, draw; keeps the `walkersMap` opcode and `OverlayPlugin` | gesture recognition + draw list |
| Precedent | [ADR-0149](../adr/0149-implot-core-port-painter-lane.md) — ImPlot's pan / anchored zoom / box-select in Go | walkers today | ADR-0203 O5 |
| Pan latency | one frame (R24 arrives at frame end) | none | one frame |
| Animations | Go drives repaints (`RequestRepaint[After]` exist) | `request_repaint_after`, native | Go |
| Testability | whole kernel `go test`; Leaflet specs port | `cargo test` for math and pyramid | split |
| IDL change | none (painter lane); the walkers IDL is deleted | none | one draw-list op |
| Client gets | thinner by ~2,000 Rust lines and the walkers + reqwest closure | a second interaction runtime's worth of owned Rust | both |

S3 converges on S1: R23/R24 *are* Rust-side gesture recognition, so the only
thing S3 adds is a zero-lag pan preview, which is better framed as a generic
painter-lane improvement (§4, gaps) than as a third architecture.

**S1 is the shape to evaluate first**, for four reasons. It is the shape this
tree has been moving interactive widgets to (ImPlot done, node editor
surveyed); its substrate is in stock; it makes ADR-0165 intrinsic rather than
a separate design (Go fetching tiles is simply how the Go kernel gets bytes);
and it is the only shape under which the Leaflet specs become Go tests. Its
one real risk — drag feel with a one-frame lag — is cheap to measure before
anything is ported (§6, M0). S2 is the fallback if that measurement fails:
same algorithms, egui-native input, and it keeps the `walkersMap` contract and
the overlay plugin untouched.

## 4 Substrate check for S1

| Need | Primitive | Status |
| --- | --- | --- |
| Draw a tile | `paintImage` (ADR-0149 M4): textured rect by image id, pixels shipped once per version and cached per id, starved-texture re-ship via `fetchR22`, `nearest`, `opacity`, clipped | in stock |
| Zoom animation, over-zoom | the same command at a scaled rect — Leaflet's per-level CSS scale becomes per-tile rect math | in stock |
| Fade-in | `paintImage` `opacity` + Go-driven repaints for 200 ms | in stock |
| Viewport clip | `PaintClipPush`/`PaintClipPop` | in stock |
| Drag, press origin, modifiers | `fetchR24CanvasPointers` | in stock |
| Wheel zoom anchored at the pointer, pinch | `fetchR23CanvasWheel` (ADR-0140); the carrier forwards `PinchZoom` into egui's zoom delta | in stock |
| Keyboard pan/zoom | `.CaptureKeys` (ADR-0177) | in stock |
| Box zoom | `PaintRectStroke` + R24 | in stock |
| Overlays: markers, polylines, polygons, labels, raster | `PaintMarkers`, `PaintPolyline`, `PaintPolygonFilled`, `PaintText`, `paintImage` | in stock |
| Tile bytes | Go's HTTP client under the env registry — ADR-0165's destination, reached without its IDL work | in stock |
| Decode | `image/png`, `image/jpeg` from the standard library | in stock |
| Textures on the headless mesh lane | `TexturesDelta` verbatim ([ADR-0128 §SD2](../adr/0128-imzero2-mesh-draw-stream-codec-lane.md)) | in stock |
| Repaint during animations | `RequestRepaint` / `RequestRepaintAfter` from Go | in stock |

Gaps, and what each forces:

- **One-frame pan lag.** R24 is read at frame end, so the tiles move one frame
  after the pointer. ADR-0149 accepted this for plots; a map drag is the most
  latency-sensitive gesture in the tree and may not. If M0 finds it
  perceptible on the desktop host, the mitigation is generic: let the canvas
  drain apply the in-flight drag delta of its own sense region as a translation
  before Go has seen it — a pan preview of a few lines in Rust, useful to every
  draggable canvas. Over the remote-access carrier the lag is dwarfed by the
  round trip.
- **No UV sub-rect image command.** Not needed: Leaflet's retention model draws
  whole tiles at scaled rects; only walkers' quarter-tile interpolation needs
  UVs, and the port replaces it.
- **Pinch geometry.** egui reduces a pinch to a zoom factor plus the pointer
  anchor; Leaflet's pinch also pans by the midpoint motion. The reduced form is
  what every other egui widget gets and is acceptable; note it.
- **SVG export.** `paintImage` draws through an `ImageCache`, the same type the
  image widget mirrors into the export pixel cache; whether the painter's
  instance has the mirror attached is to be verified, not assumed. If it does,
  ADR-0203's Q1 gap closes for free.
- **H3 overlays.** Cell outlines are already reachable in Go
  (`CellsToBoundariesE`, over the `h3bridge` export `h3_cell_to_boundary`);
  what the *region* overlay needs is the dissolve (cells → multipolygon),
  which imzero2 does Rust-side with `h3o`'s `SolventBuilder`. The bridge
  already builds `h3o` with its `geo` feature, so the dissolve is one more
  wasm export plus a Go wrapper method plus a parity case — small, but a
  rebuild of the prebuilt wasm. Outside the demo gallery nothing uses these
  overlays today — `play` uses the raster overlay, `terrainscope` markers and
  a polyline (§9, F5).

## 5 The cut line

Ported: everything in §2.1's first five rows, re-idiomised to Go — structs for
`LatLng`/`Bounds`/`Point`, a `CRS` interface with four implementations, a
`View` (centre, zoom, size, limits), handler state machines that take pointer
samples and times rather than DOM events, a `TilePyramid` that takes
`(view, viewport)` and yields a draw list plus load/unload events, and a
`TileSource` (URL template + options). The Leaflet event vocabulary survives as
Go callbacks and as the camera fetcher's flags, which is what apps consume
today.

Not ported, and why:

- `core/` and `dom/` — Go and egui provide class, event, browser and DOM
  equivalents or make them unnecessary.
- `control/` (zoom buttons, attribution, layer switcher, scale) — imzero2
  widgets; attribution is a label the widget already draws.
- `Popup`/`Tooltip`/`DivOverlay`, `Marker` as DOM icon — egui tooltips and
  windows; markers are painter markers or images, marker drag is a sense region
  plus R24.
- `layer/vector` renderers (SVG, Canvas) — the painter lane *is* the renderer;
  `LineUtil.simplify`/`clipSegment` and `PolyUtil.clipPolygon` are kept as
  utilities for large overlays.
- `ImageOverlay`/`VideoOverlay`/`SVGOverlay` — `paintImage` covers the first
  (it is what `mapRaster` already is); the others have no consumer.
- WMS, geolocation, `TapHold` — no consumer; WMS is a URL builder that can come
  later.

## 6 Cost and milestones (S1)

Effort is given as new lines to own, with Leaflet's spec lines ported where
they apply. ADR-0149 ported a larger core in about a month; the map kernel is
smaller, but it carries a feel risk ImPlot did not.

| Milestone | Scope | Estimate |
| --- | --- | --- |
| **M0 — feel spike** | A Go canvas at a fixed camera drawing OSM tiles through `paintImage`, panned by R24 and zoomed by R23; no pyramid, no animation. Measure drag feel on the desktop host and over the carrier. **This is the gate between S1 and S2** and it costs days because the substrate exists. *Done 2026-08-22, both halves — S1 passed; results and the input recipe in [ADR-0204](../adr/0204-leaflet-map-core-port.md) M0/§SD6/§SD8.* | 2–3 days |
| **M1 — geo + geometry** | `LatLng`, bounds, `Point`, `Transformation`, `CRS` ×4, three projections, `LineUtil`/`PolyUtil`; ported specs | ~600 Go + ~500 test; 1 week |
| **M2 — view + pyramid + tiles** | `View` state, limits, `zoomSnap`, `getBoundsZoom`/`fitBounds`; `TilePyramid` (range, `keepBuffer`, retain parent/children, prune, load order, fade state); `TileSource` URL; Go fetch + LRU + negative cache + decode; `paintImage` emission. Parity with the walkers widget: pan, wheel, double-click, retention instead of quarter-tile interpolation. `play` and `terrainscope` move to the new entry point (the analysis assumed a `c.WalkersMap(...)` shim; ADR-0204 §SD1/§SD11 decided against one — three call sites). | ~1,300 Go + ~700 test (incl. `GridLayerSpec` counts); 1–2 weeks |
| **M3 — feel** | inertia, pan animation, zoom animation, `flyTo`, `maxBounds` + viscosity, pinch, box zoom, keyboard; handler specs as sampled-input tests | ~600 Go + ~300 test; 1 week |
| **M4 — overlays and removal** | markers, polylines (simplify + clip), polygons, labels, raster via `paintImage`; H3 overlays deferred or ported behind a Go cell-to-boundary; delete `walkers_tiles.rs`, the walkers sections of the interpreter and the walkers IDL; remove `walkers` and `reqwest` from the manifest; re-measure ADR-0203's figures; musl check | ~800 Go; −~2,000 Rust; 1 week |
| **M5 — regression net** | a `drag` verb for the headless trace driver (shared with ADR-0203 M3) and a map scene that asserts the camera after synthetic pan and zoom | days |

Total: **roughly 3,300 Go lines plus ~1,500 of tests, 5–7 weeks**, with the
go/no-go after M0. The Rust side shrinks by about 2,000 lines and the crate
closure by the walkers and reqwest trees together — 96 crates on the desktop
profile and 125 on the headless profiles, taking ADR-0203's two-tier figures
(−74 / −99) and adding the 22 / 26 that only dropping walkers itself removes
(the `resvg`/`usvg`/`tiny-skia` stack walkers enables for attribution logos,
`zune-jpeg`, and a second copy of `png`). `ring` leaves with them, which leaves
`mimalloc` as the only C-compiling crate between the headless host and a
toolchain-free static musl build.

## 7 What it buys, against the other routes

| | O2/O3 gate + ADR-0165 | O4′ adopt walkers' core | **Leaflet port (S1)** |
| --- | --- | --- | --- |
| HTTP/TLS stack out of the client | yes, in two steps | yes | yes, in one |
| walkers' other ~22–26 crates out | no | yes | yes |
| Independent of upstream | no (fork until merged) | yes | yes |
| Behaviour | identical | identical | better (retention, animation, bounds, CRS); *different*, which is also the risk |
| Code owned | ~0 | ~1,200 Rust | ~3,300 Go + tests |
| Tests | none new | Rust unit tests | Leaflet's specs as Go tests |
| SVG export gap | open (fixable) | open (fixable) | closes with `paintImage` (to verify) |
| Go API | unchanged | unchanged | new entry `portolan.Map` (no shim, per ADR-0204); IDL surface deleted |
| Time | days + ADR-0165's open questions | days | 5–7 weeks |

The honest framing: O2/O3 and O4′ are the cheap ways to stop paying for
walkers; the Leaflet port is the way to *own the map widget*, and it should be
decided on that basis — the dependency relief comes with it but does not
justify it alone.

## 8 Risks and named non-goals

- **Feel.** The one-frame lag (gated by M0) and the inertia/zoom constants —
  Leaflet's are known-good, but they were tuned for pointer events at browser
  frame rates; the carrier's input cadence differs. Budget a tuning pass.
- **Porting bugs in the pyramid.** The retention/prune logic is where tile
  widgets flicker; the `GridLayerSpec` counts are the defence and should be
  ported before the logic is trusted.
- **Overlay performance.** Per-frame projection of overlay geometry moves from
  Rust to Go. ImPlot handles thousands of points per frame the same way;
  polylines beyond that get `simplify` + `clipSegment`.
- **Two sources of truth for a while.** Until M4, the walkers widget and the Go
  widget coexist; the shim keeps callers on one API, but screenshots and the
  demo gallery will show both.
- Non-goals: vector tiles (MVT), PMTiles, WMS, controls and popups as ported
  code, a plugin model, any attempt at Leaflet's API shape — the *protocol*
  (view, pyramid, events) is what is preserved, as ADR-0149 §SD2 did for ImPlot.

## 9 Open forks for a design dialogue

Resolved 2026-08-22 and carried into [ADR-0204](../adr/0204-leaflet-map-core-port.md):
**F2** the entry is `Map`, package `widgets/portolan` — a nautical name in the
house style, with neither the crate's nor Leaflet's name in the API (a bare
`c.Map` would need the generated `bindings` package to import a widget package
— a cycle — so the call reads `portolan.Map(c, …)`); **F3** ADR-0165 folded in; **F4** the
2.0-alpha source; **F5** the H3 overlays are ported, with the dissolve added
to the bridge; **F6** `SimpleCRS` from M1; **F8** the `drag` verb first, as
M0's instrument. **F1** was settled by M0 the same day — S1, the Go kernel:
no lost motion once the input recipe was right, one frame of lag, and the
desktop host judged to feel right by the owner. Still open: **F7** (what
becomes of ADR-0203's O2/O3). The forks are kept below as they were
asked, so the reasoning behind each answer stays legible.

- **F1 — S1 or S2.** Decided by M0's feel measurement, not by preference. If
  S2, the same analysis holds with the kernel in Rust, egui-native input, and
  the `walkersMap` contract kept.
- **F2 — API name.** Keep `c.WalkersMap(...)` through a shim, or introduce
  `c.Map(...)` and retire the crate's name from the Go surface in the same
  move.
- **F3 — ADR-0165.** Fold into the port (Go fetching is intrinsic to S1) or
  keep as its own ADR for the keelson HTTP-facility boundary question it
  raises.
- **F4 — Source and provenance.** Port from 2.0-alpha (cleaner) or 1.9.4
  (released); either way the attribution form of ADR-0149 §SD8.
- **F5 — H3 overlays.** Port (needs a Go cell-to-boundary, and a dissolve) or
  drop from the gallery; nothing outside the demo uses them today.
- **F6 — `SimpleCRS` in scope from M1** (cheap, and it is what makes the widget
  a tiled raster viewer) or later.
- **F7 — What happens to ADR-0203's O2/O3.** A one-day hedge worth doing
  anyway, or wasted if S1 is green-lit; ADR-0203 would be superseded by the
  port's ADR either way.
- **F8 — The `drag` verb first or last.** It is the regression net for M0's
  measurement as much as for M5.

## References

- [ADR-0203 — a map widget without the HTTP stack](../adr/0203-map-widget-without-the-http-stack.md) — the measurement this page extends; its O4/O5 are the shapes refined here.
- [ADR-0056 — walkers map and H3 binding](../adr/0056-walkers-map-h3-binding.md) — the binding being replaced.
- [ADR-0165 — imzero2 tile transport over FFFI2](../adr/0165-imzero2-tile-transport-over-fffi2.md) — the fetch move S1 makes intrinsic.
- [ADR-0149 — porting the ImPlot core to Go on the painter lane](../adr/0149-implot-core-port-painter-lane.md) — the precedent for a Go-side interactive port, `paintImage`, and the provenance form.
- [ADR-0140 — hover-scoped wheel capture](../adr/0140-imzero2-hover-scoped-wheel-capture.md) — R23.
- [ADR-0128 — imzero2 mesh draw stream codec lane](../adr/0128-imzero2-mesh-draw-stream-codec-lane.md) — textures on the headless lane; the deferred musl-static target.
- [snarl-port-analysis](./snarl-port-analysis.md) — the substrate checklist this page's §4 mirrors.
- Leaflet — <https://github.com/Leaflet/Leaflet>, BSD-2-Clause; `src/layer/tile/GridLayer.js`, `src/map/Map.js`, `src/map/handler/*`, `src/geo/crs/*`, `spec/suites/layer/tile/GridLayerSpec.js`.
