---
type: adr
status: proposed
date: 2026-08-22
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0204: port Leaflet's map core to Go on the painter lane

## Context

[ADR-0056](./0056-walkers-map-h3-binding.md) bound the `walkers` crate as the
basemap widget. [ADR-0203](./0203-map-widget-without-the-http-stack.md) measured
what the crate costs the render client — an HTTP/TLS stack with no feature to
switch it off, two owners in the manifest (walkers and imzero2's own `reqwest`),
`ring` as one of the two crates standing between the headless host and a static
musl build — and proposed an upstream feature gate, with reimplementation held
back as a fallback. The dialogue around that ADR asked why reimplementation was
ranked last, and the analysis in
[leaflet-port-analysis](../adr-background-work/leaflet-port-analysis.md) answered
with a third route: not a clean-room rewrite and not an adoption of walkers'
own core, but a port of **Leaflet's map kernel** — projection model, view state,
gesture handlers, tile pyramid — into Go on the painter lane.

What that analysis established, and this ADR rests on:

- **Leaflet is BSD-2-Clause**, so a port is a derivative work with attribution,
  the form [ADR-0149 §SD8](./0149-implot-core-port-painter-lane.md) already set
  for ImPlot; no clean-room discipline is needed. Upstream `main` is
  2.0.0-alpha.1: 14,012 source lines, 14,860 spec lines, and the kernel worth
  porting is roughly 2,500 lines of logic inside ~4,500 lines of DOM-shaped
  code.
- **Its behaviour is better than what the tree has.** Loaded ancestor and
  descendant tiles are retained, scaled, until the current level fades in (no
  grey or quarter-tile flashes on zoom); zoom and pan animate; `flyTo`,
  `fitBounds`, bounded panning with viscosity, zoom snapping, keyboard and box
  zoom; a CRS abstraction whose `SimpleCRS` turns the same widget into a tiled
  viewer for any large raster. walkers' raster core has none of these, and
  nothing this tree uses is in walkers but not in Leaflet.
- **The substrate for a Go-side port is in stock.** `paintImage`
  (ADR-0149 M4: textured rect by image id, per-id texture cache, `opacity`,
  `nearest`, clipped), `PaintClipPush`/`Pop`, `fetchR24CanvasPointers` for
  drag, `fetchR23CanvasWheel` for anchored wheel and pinch zoom
  ([ADR-0140](./0140-imzero2-hover-scoped-wheel-capture.md)),
  `RequestRepaint`/`RequestRepaintAfter` from Go, textures on the headless mesh
  lane ([ADR-0128 §SD2](./0128-imzero2-mesh-draw-stream-codec-lane.md)),
  `.CaptureKeys` ([ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md)),
  `image/png` and `image/jpeg` from the standard library, the Go H3 wrapper's
  `CellsToBoundariesE`. The node-editor survey reached the same "nothing new
  needed" conclusion for a larger widget.
- **The precedent is ADR-0149**: ImPlot's pan, anchored zoom and box-select
  already run in Go on the painter lane with a one-frame input lag, and that
  port took about a month for a larger core.
- **Outside the demo gallery the map's overlay surface is small** — `play`
  uses the raster overlay, `terrainscope` two markers and a polyline; the H3
  overlays are demo-only.

The forks the analysis left open were resolved in dialogue on 2026-08-22 and
are recorded in the Decision: the Go entry is named `Map`, in a package with a
nautical name — neither the crate's nor Leaflet's (§SD1); ADR-0165 is folded
in; the source is the 2.0-alpha line; the H3 overlays are ported; `SimpleCRS`
is in scope from the first milestone; the headless driver's `drag` verb comes
first. Two remain open (Q1, Q2).

## Design space (QOC)

**Question.** How should this tree own its map widget — and should it at all?

**Options.**

- **O1 — Keep walkers; gate its HTTP client upstream.** ADR-0203's current
  decision: a ten-line feature gate, a fork until it merges, ADR-0165 for the
  renderer's own client.
- **O2 — Adopt walkers' raster core.** Copy the MIT-licensed ~1,200-line core
  (`position`, `mercator`, `projector`, `zoom`, `center`, `map`, `tiles` minus
  HTTP) into imzero2 as a Rust module; drop the crate. Identical behaviour,
  no upstream.
- **O3 — Port Leaflet's kernel to Go on the painter lane.** View state,
  handlers as state machines over R23/R24 samples, tile pyramid, URL
  templating, fetch and decode, overlays — all Go; Rust gains nothing.
- **O4 — Port Leaflet's kernel to Rust inside imzero2.** The same algorithms
  on egui-native input, keeping the `walkersMap` opcode and the overlay plugin.

**Criteria.**

- **C1 — Supply surface and independence** — crates and build scripts out of
  the client; no fork to rebase, no upstream timeline.
- **C2 — Map behaviour** — retention, animation, bounds, CRS, as measured in
  the analysis.
- **C3 — Feel risk** — input lag, porting bugs in pan/zoom/pyramid.
- **C4 — Testability** — whether Leaflet's specification suite becomes tests
  in the default `go test` lane.
- **C5 — Fit with the tree's direction** — interactive widgets on the painter
  lane, a thin render client, state in Go.
- **C6 — Effort** — weeks, and lines owned.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | +  | ++ | ++ | ++ |
| C2 | −  | −  | ++ | ++ |
| C3 | ++ | ++ | −  | +  |
| C4 | −− | −  | ++ | +  |
| C5 | −  | −  | ++ | −  |
| C6 | ++ | ++ | −− | −  |

O1 and O2 are the cheap ways to stop paying for walkers; they are not ways to
own the widget, and neither improves what the map does. O3 is the only option
that scores on behaviour, testability and direction at once; its cost is real
(C6) and its one open risk is C3, which is gated by a measurement that costs
days. O4 is O3's fallback: same algorithms, no lag, but a second interaction
runtime in the artifact the tree keeps thin, and the specs stay outside the Go
lane.

## Decision

*Proposed, not implemented.* **We will port Leaflet's map kernel to Go on the
painter lane (O3)**, as a new widget whose entry point is `Map`, and retire
the walkers binding — its crate, imzero2's own `reqwest`, the `walkersMap`
opcode and its overlay registers — when the port reaches parity. **O4 is the
recorded fallback**, selected only if the first milestone's feel measurement
fails (§SD8). ADR-0165 is folded in: the Go kernel fetching its own tiles is
how it gets bytes, not a separate design.

### Subsidiary design decisions

- **SD1 — Name and home.** The entry is `Map`; neither the crate's name nor
  Leaflet's appears in the Go surface. The package is `widgets/portolan` — a
  portolan is the nautical chart ruled with rhumb lines, which is what a
  Web-Mercator basemap is — so the widget is a `*portolan.Map`, made once
  with `portolan.New(ids, opts)` and drawn every frame with `Render`, the way
  charts are `implot.Begin(…)`. House names are nautical (`keelson`,
  `lading`, `leeway`, `tally`), and unlike ADR-0149 §SD8 the package name
  does not carry the provenance: that lives in the in-package licence text,
  the package comment and `THIRD_PARTY_NOTICES.md` (§SD3). A bare `c.Map` is
  not available without a cycle: `c` is the generated `bindings` package,
  which every widget package imports. `c.WalkersMap` and the
  `mapMarker`/`mapPolyline`/`h3Cells`/`h3Region`/`mapRaster` builders are
  retired at M4; `basemap.Apply` gains a `portolan`-typed form and its
  `BOXER_MAP_TILE_*` registry block keeps every name and meaning.
- **SD2 — Port contract: protocol, not API; core, not chrome.** What is
  preserved from Leaflet is the model — view, limits, event vocabulary
  (`movestart`/`move`/`moveend`, `zoomstart`/`zoom`/`zoomend`, tile
  load/error/unload), pyramid semantics, handler behaviour and constants —
  re-idiomised to Go: structs and a `CRS` interface, handlers as state
  machines fed pointer samples and times, a pyramid that maps `(view,
  viewport)` to a draw list plus load/unload events. Not ported: `core/`,
  `dom/`, controls, popups and tooltips, markers as DOM icons, the SVG/Canvas
  renderers (the painter lane is the renderer; `simplify`/`clipSegment`/
  `clipPolygon` are kept as utilities), image/video/SVG overlays beyond what
  `paintImage` already is, WMS, geolocation, tap-hold. No attempt at
  Leaflet's API shape, plugin model, or `L` namespace.
- **SD3 — Source and provenance.** The port reads upstream `main` at a
  recorded commit on the 2.0-alpha line — cleaner than 1.9.4, same
  algorithms. The package carries Leaflet's BSD-2-Clause text and attribution
  in-package and an entry in `THIRD_PARTY_NOTICES.md`; authorship follows the
  git-trailer discipline ([ADR-0083](./0083-retire-llm-generated-build-tags.md)),
  no in-file markers. Files that transliterate an upstream module name it in
  their package comment.
- **SD4 — ADR-0165 folded in.** Go fetches tile bytes through its own HTTP
  client under the env registry; `BOXER_MAP_TILE_CA_FILE` and
  `BOXER_MAP_TILE_INSECURE_TLS` become ordinary `http.Transport`
  configuration and keep their names. The two are not the same size, and the
  transport reflects that: `BOXER_MAP_TILE_CA_FILE` moves the trust anchor and
  changes nothing else, while `BOXER_MAP_TILE_INSECURE_TLS` also drops
  `MinVersion` to TLS 1.0 and admits the suites `crypto/tls` keeps out of its
  default list (static-RSA key exchange, 3DES, RC4). Lowering only the
  certificate check would have left the knob unable to reach the servers it
  exists for — every static-RSA suite an old appliance offers is in Go's
  insecure list, so the handshake fails on version or cipher negotiation
  instead, which reads as a knob that does not work. Once verification is off
  the peer is unauthenticated, so neither the floor nor the cipher list is
  defending anything: an attacker who can present an arbitrary certificate is
  already the peer. Both halves are covered by round-trip tests against
  in-process TLS 1.0-only and static-RSA-only servers, not just by asserting
  the `tls.Config` fields. ADR-0165's open questions close as follows. *Cache ownership (Q1):* Go holds a bounded LRU of compressed bytes
  and a negative cache; decoded RGBA is handed to `paintImage` once per tile
  and lives in the renderer's per-id texture cache; a tile-source change
  invalidates by id prefix. *Backpressure (Q2):* the pyramid's load queue is
  the request set — ordered by distance to the view centre, bounded to six
  in-flight requests per host, and a request whose tile is no longer
  `current` is dropped before it is issued. *Failure (Q3):* a failed tile
  draws nothing (or `errorTileUrl` if configured), is negative-cached with a
  TTL, is retried on the next view change that makes it current again, and a
  persistently failing source raises a flag in the map's readback state so an
  operator sees a cause rather than a grey map. *Bandwidth (Q4):* RGBA crosses
  the in-process channel once per tile version — 256 KB per 256 px tile, a
  screenful on first paint, nothing afterwards; measured in M0, not assumed.
  *Keelson HTTP facility (Q5):* unchanged — the successor boundary, not drawn
  here.
- **SD5 — Pyramid semantics are Leaflet's.** Tile range from the projected
  viewport with a `keepBuffer` of 2; load order by distance to centre;
  ancestors retained up to five levels up and descendants two down while the
  current level loads; prune after the 200 ms fade; `minNativeZoom`/
  `maxNativeZoom` over-zoom by scaling; `noWrap` and source bounds; requests
  during a pan throttled to the pyramid's update interval. walkers'
  quarter-tile interpolation is not carried over.
- **SD6 — Interaction is Leaflet's, with one default changed.** Inertia from
  the last 50 ms of samples at `easeLinearity` 0.2 and 3400 px/s²; wheel with
  a 40 ms debounce at 60 px per level through Leaflet's sigmoid, anchored at
  the pointer; pinch as zoom factor plus anchor (what egui exposes; the
  midpoint pan of Leaflet's pinch is not available and not missed); double
  click, box zoom, keyboard pan and zoom; `maxBounds` with viscosity. The
  default `ZoomSnap` is 0 — continuous zoom, which is what the map does today
  — with Leaflet's 1 available as an option (Q6). M0 settled how the
  handlers read their samples: a sense region over the canvas owns the drag,
  because its R24 row on the drag-started frame carries the press origin and
  the canvas's own row does not (the press and the first move share a host
  frame more often than not, and then the canvas's press position is already
  the moved one); positions come from the frame-end pointer (R20) minus the
  canvas origin, which leads the R24 row by one event; the view during a drag
  is the view at the press plus the offset from the origin — Leaflet's
  formulation, never a sum of per-frame deltas — and the release frame's
  position is applied too. With that recipe a 24-move, 240 × 120 px drag
  lands at 240.01 × 119.98 px; with per-frame deltas off the canvas's own row
  it lost 20 × 10 px. A wheel notch arrives spread over a dozen frames of
  smooth scroll; anchored zoom applied per frame is fine. M3 made the
  animations `View` state machines stepped by `Tick(now)` — the pan
  (0.25 s ease-out), the zoom animation (250 ms, Leaflet's cubic-bezier; the
  view interpolated silently, the pyramid told the target level once through
  `ZoomAnimStart`), the fly (van Wijk–Nuij) and the pinch's end — and
  Leaflet's handlers sampled-input machines over them, with four deviations
  the carrier forced or the port chose. (1) A zoom request during a zoom
  animation restarts it from the interpolated view instead of being dropped,
  and the wheel and the double click build their next target on the running
  animation's target (`TargetZoom`): egui spreads one notch over a dozen
  frames, so under Leaflet's rule a notch lost most of itself (measured 0.52
  levels with drop-and-jump, 0.69 with the restart; three notches 100 ms
  apart add exactly three notches). (2) `Stop` ends every animation where it
  is — Leaflet's `_stop` for the pan and the fly, and an in-place end for
  the zoom animation, which Leaflet never interrupts (a press during its
  250 ms is ignored there; here the drag starts from where the map is).
  (3) The map takes keyboard focus on a press and again on the click's
  release: egui's `SurrenderFocusOn::Clicks` default hands focus back at
  the release, because only the clicked widget counts as hovered during a
  click, so a press-time request alone left the arrows dead (the capture
  Frame of ADR-0177, which must not be focusable itself). (4) The
  keyboard's zoom keys are deferred: the `keycodes` vocabulary has no `+`
  or `-`. The pyramid's per-move update carries GridLayer's 200 ms
  `updateInterval` throttle since M3 (§SD5 described it a milestone
  early); a pinch ends 120 ms after its last factor, egui reporting no
  gesture end.
- **SD7 — CRS from the first milestone.** `EPSG3857` by default; `EPSG4326`,
  `EPSG3395` and `SimpleCRS` alongside it, behind one interface, because the
  projection code is the cheapest part of the port and `SimpleCRS` is what
  makes the widget a tiled raster viewer.
- **SD8 — The one-frame lag is measured before anything else is ported.**
  R24 is read at frame end, so tiles follow the pointer one frame late. M0
  builds a fixed-camera tile canvas on `paintImage` with R24 drag and R23
  zoom and measures it on the desktop host and over the carrier, with the
  `drag` verb (§SD10) so the measurement is reproducible. If the lag is
  perceptible on the desktop host the first remedy is generic: a pan preview
  in the canvas drain that applies the in-flight drag delta of the canvas's
  own sense region before Go has seen it — a few lines of Rust that serve
  every draggable canvas. Only if that fails does O4 replace O3; the
  algorithms, the source and the milestones are the same either way. M0's
  headless half (2026-08-22, `headless_soft` host at 60 fps) measured the lag
  rather than assumed it: every position is applied exactly one frame after
  the host saw it — R20 at Go frame N is the frame-end pointer of host frame
  N−1 — so the lag is one frame period, 16.7 ms there and 33 ms at the
  tour's 30 fps, with Go at ~0.5–1 ms and Rust at ~0.4–0.7 ms per frame.
  The desktop half followed the same day: on the desktop host the owner
  judged pan and zoom to feel right, so the one-frame lag is not perceptible
  there, the pan preview stays unbuilt, and O4 stays the fallback it was
  (Q1).
- **SD9 — H3 overlays are ported, with one bridge addition.** Cell outlines
  are already reachable in Go (`CellsToBoundariesE` over the `h3bridge` wasm
  export `h3_cell_to_boundary`); what the region overlay needs is the
  dissolve (cells → multipolygon), which imzero2 does today with `h3o`'s
  `SolventBuilder`. The bridge already builds `h3o` with its `geo` feature, so
  it gains one export (`h3_dissolve`), the Go wrapper one method, and the
  parity lane its case. Cells and regions then draw through
  `PaintPolygonFilled`/`PaintPolyline`/`PaintText`; imzero2's own `h3o` and
  `geo` dependencies leave with the binding if nothing else uses them.
- **SD10 — The `drag` verb comes first.** The headless trace driver gains
  `drag` (pointer down at a point, N moves over a duration, up), built from
  the carrier client's existing `SendInput`/`MoveMouse`, documented beside
  the other verbs. It is M0's instrument and M5's regression net, and it is
  the first thing that can catch a map-feel regression in this tree — which
  ADR-0203 Q3 recorded as missing. Shipped 2026-08-22:
  `carrierclient.Client.Drag` and the `drag` step (`x`,`y` → `toX`,`toY`;
  anchored, `x`,`y` is the delta; `steps`, `durationMs`).
- **SD11 — Coexistence and migration.** The walkers widget stays until M4;
  the three call sites (`play`'s map panel, `terrainscope`, the demo gallery)
  move to `portolan.Map` as each milestone covers what they use; there is no
  compatibility shim, because three call sites do not justify one. The
  screenshot tour shows both widgets until M4.

### Milestones

- **M0 — `drag` verb + feel spike.** ✓ §SD10, then a Go canvas at a fixed
  camera drawing OSM tiles through `paintImage`, panned by R24, zoomed by
  R23, no pyramid, no animation; measure desktop and carrier; measure the
  first-paint bandwidth (§SD4 Q4). Decides O3 vs O4. Days. *Headless half
  done 2026-08-22*: the spike is the gallery demo `portolan-m0`
  (`egui2_hl_portolan_m0.go`, deliberately not a widget), driven by the
  `drag` verb on the `headless_soft` host at 60 fps — pan and anchored zoom
  correct to the pixel (§SD6), one frame of lag (§SD8), 256 KB per tile
  through `paintImage` (Q5), zero re-ships across pan and zoom, Go ~0.5–1 ms
  per frame. *Desktop half done the same day*: on the desktop host the owner
  judged pan and zoom to feel right — the gate is passed, O3 stands.
- **M1 — geo and geometry.** ✓ `LatLng`, bounds, `Point`, `Transformation`,
  the `CRSI` interface with its four implementations, three projections,
  `LineUtil`/`PolyUtil`; Leaflet's geo/geometry/CRS/projection specs ported.
  *Done 2026-08-22*: package `widgets/portolan` (~1,060 lines of Go, ~1,380 of
  tests); 131 of the 153 upstream `it`s ported as 206 passing Go tests, the 22
  skipped being JavaScript-specific (constructor polymorphism, `validate`,
  altitude, `isFlat`, `console.warn` spies) and listed in each test file's
  header. No port bug surfaced; one evaluation-order change keeps EPSG:3857
  pixels bit-identical to Leaflet's, and `doc.go` states the two caveats that
  remain (libm `sin` ulps, FMA targets). The interfaces are `CRSI` and
  `ProjectionI`, per CS005.
- **M2 — view, pyramid, tiles.** ✓ View state, limits, `zoomSnap`,
  `getBoundsZoom`/`fitBounds`; the pyramid (§SD5); `TileSource` with
  Leaflet's URL template and options; Go fetch, LRU, negative cache, decode;
  `paintImage` emission. Parity with the walkers widget for pan, wheel and
  double-click; `play` and `terrainscope` move over; `GridLayerSpec`'s
  tile-count cases ported. *Done 2026-08-22*: `view.go` (Map.js's view
  state without DOM or animation), `pyramid.go` (GridLayer's bookkeeping —
  retention five up / two down, prune, centre-out load order, fade,
  `Sync` wiring the view's events the way `getEvents` does), `tilesource.go`
  (TileLayer's options and templating), `loader.go` (six workers, a 512-tile
  byte cache, a 30 s negative cache, the `BOXER_MAP_TILE_*` TLS knobs on
  Go's transport — ADR-0165's Q1–Q3 as §SD4 said), `widget.go` (the
  `Map`: M0's input recipe, Leaflet's wheel handler, double-click, hover and
  click readback, `RenderFill`, a no-tiles mode) and `overlay.go` (Marker,
  Label, Polyline and a bounds-pinned raster through the Projector that
  `Render` hands its overlay callback — the minimal overlay surface `play`
  and `terrainscope` needed; the builder-style overlays and H3 come with
  M4). `basemap` gained `PortolanSource`/`PortolanLoader`; the gallery gained
  the `portolan` demo; `play`'s map panel and `terrainscope` run on
  `portolan.Map` (terrainscope opens at zoom 8, where walkers' default was
  16). Headless parity on the `headless_soft` host: a 240 × 120 px drag
  lands at 240.01 × 119.98, a wheel notch zooms by Leaflet's sigmoid
  (+0.68 at 60 px) about the pointer, a double-click by one level, 47 tiles
  requested and 35 unloaded across a zoom sequence with zero re-ships.
  Specs ported: GridLayerSpec 32 of 37, MapSpec 110 of 238 (the rest DOM,
  animation or `throws` guards), TileLayerSpec 14 of 23 plus 26 hand-derived
  pins; the package's tests stand at 454 passing cases, ~3,400 lines of Go
  and ~4,000 of tests. No pyramid or view port bug surfaced; three URL
  templating fixes did (braces in hosts, the https upgrade, `{-y}` on an
  infinite CRS).
- **M3 — feel.** ✓ Done 2026-08-22: `anim.go` (the pan, zoom and fly
  animations; `Stop`, `SetViewAnimated`, `PanByAnimated`, `FlyTo`,
  `FlyToBounds`, `FitBoundsAnimated`) and `handlers.go` (drag with inertia
  and `maxBounds` viscosity, wheel, pinch, double click, box zoom, keyboard
  pan), wired into the widget under a key-capturing Frame, plus the
  pyramid's update throttle — 885 lines of Go and 1,612 of tests; the
  package stands at 525 passing cases, ~4,400 lines of Go and ~5,600 of
  tests. Specs ported: the six handler specs (21 of 50 `it`s — the rest
  DOM, touch plumbing, the enable/disable API, `worldCopyJump`, click
  suppression, the zoom keys and the `'center'` options the port has not
  got), MapSpec's animation cases (14: `#flyTo`, `#flyToBounds`, `#stop`,
  animated `#setView`/`#setZoom`) and GridLayerSpec's animated grid (5,
  MAD–TRD's 224 loaded / 209 unloaded among them, reproduced exactly when
  wired as the spec's grid is), with 14 port-only cases for inertia,
  viscosity, bounce and the keyboard rules the specs do not cover. No
  handler port bug surfaced; the ports found two in the widget's own
  seams — a wheel re-perform that jumped and lost the remainder (§SD6 (1))
  and a drag that seeded inertia with the press origin — and the
  tuning pass found the focus trap (§SD6 (3)). Headless on
  `headless_soft`: a 240 × 120 px drag over 600 ms lands at 243 × 121 px
  (three px of Leaflet's inertia at the release speed), a flick coasts
  43 px after release, one notch settles at +0.69 about the pointer with
  the zoom read mid-animation, a double click animates to the
  pointer-anchored level exactly, ArrowRight pans 80 px and a shift-arrow
  240 px after a click or a drag, and tiles load through it all with zero
  re-ships. Not instrumented headless: pinch (no touch verb) and box zoom
  (the `drag` verb holds no modifier) — unit tests only.
- **M4 — overlays and removal.** ✓ Done 2026-08-22. *Overlays:* the
  Projector gained Leaflet's vector pipeline — `Polyline` projects, clips
  segment by segment to the viewport padded by 10% (Cohen–Sutherland, parts
  split where the line leaves) and simplifies each part at `smoothFactor`
  1 px; `Polygon` clips by Sutherland–Hodgman inside the clip box grown by
  the stroke width, simplifies and fills ear-clipped with an optional
  stroke; `ConvexPolygon` is the same with the painter's feathered fan for
  rings known convex (H3 cells); `Marker`, `Label`, `Image` as before. The
  map gained `SetSource` (the pyramid restarts on the new source at the
  current view; the tile image version carries a source generation, so the
  host re-uploads under the same ids instead of showing the old source) and
  `Source`. *H3 (§SD9):* the `h3bridge` wasm gained `h3_dissolve` (cells →
  multipolygon as CSR vertices, ring offsets, polygon offsets; open rings,
  exteriors counter-clockwise, holes clockwise; a grow-once buffer protocol
  like `h3_polygon_to_cells`) and the Go wrapper `Handle.DissolveE`, with ten
  tests, a parity golden and the wasm rebuilt (340,768 → 383,580 bytes; no
  existing golden moved); the new package `portolan/h3overlay` draws cells
  (`Layer.Cells`, boundaries from `CellsToBoundariesE`, one convex fill per
  cell) and regions (`Region.Draw`: cell fills, the dissolved outline
  stroked holes included, a label at the largest exterior ring's centroid,
  the dissolve cached by cell set), plus `ViewportCells` and
  `ResolutionForZoom`; the map widget itself does not import the `h3`
  runtime — only a caller that wants H3 pays for it. *The gallery:* the
  walkers demo and the M0 spike are gone; the `portolan` demo carries
  markers, the route, the H3 region, the viewport-driven heatmap, a
  tile-server switch, the camera readout from the map itself and the
  NoTiles choropleth; `mapraster` runs on `Projector.Image`. *Removal:* the
  walkers IDL (`egui2_definition_d_walkers.go` and its six type
  constructors) deleted and both dispatches regenerated (−1,523 generated
  lines, the wire enum renumbered, so both sides rebuild together);
  `walkers_tiles.rs`, the interpreter's walkers/H3 preamble and its
  registers, the hand-written R15 camera cache (`WalkersCameraValue`,
  `GetWalkersCamera`) and `basemap.Apply` deleted; `walkers`, `reqwest` and
  `h3o` left the imzero2 manifest; SKILL.md §16 rewritten for portolan,
  ADR-0056 marked superseded with a dated Update, the howtos that promised
  "tiles are not captured headless" corrected (they are painter images now;
  the SVG mirror stays Q3), every doc link to a deleted file de-linked.
  *Figures (ADR-0203's Context re-taken, 2026-08-23):* distinct crates in
  `cargo tree -e normal` 435 → 313 for the desktop build and 321 → 166 for
  `headless_soft` — 117 lock entries gone, no additions, no version moves;
  `reqwest`, `rustls`, `ring`, `hyper`, `walkers`, `h3o`, `geo`, `resvg`
  and the second `png` appear in neither tree (`lru`, whose only user was
  `walkers_tiles.rs`, went with it); compiler warnings 77 → 43, the
  difference exactly the removed code's `geo_types` deprecations; and
  `cargo check --target x86_64-unknown-linux-musl --no-default-features
  --features headless_soft` passes with `ring` gone and `fast_alloc` off
  (ADR-0205 M6) — the only `cc` user left is `blake3`'s build dependency,
  which has a pure-Rust fallback, so no C toolchain stands between the
  headless host and a static musl appliance. The crate's `rust-version`
  stays 1.94 (its justification was `h3o`; a re-check finds 1.92 would
  compile — lowering is a separate decision). `scripts/ci/
  rust_imzero2_check.sh` passes.
- **M5 — regression net.** ✓ Done 2026-08-23.
  `scripts/dev/portolan-map-scene.sh` drives the gallery's portolan demo
  headless (a private Go host paired with the CPU-rasterised client, tiles
  from `scripts/dev/tile-stub-server.py` — a generated PNG per `{z}/{x}/{y}`
  on a loopback port, so the scene is offline and its pixels repeat) and
  asserts the CAMERA the demo reads back, not the picture: a 240 × 120 px
  drag by §SD10's verb moves the centre by that within 6 px (a measured run:
  243.01 × 121.00, the 3 px being inertia at 200 px/s), a wheel notch at the
  canvas centre zooms +0.55…0.80 about it (measured +0.69, the centre off by
  0.2 × 0.6 px), a double click zooms +1.00 anchored (measured +1.00, 0.4 px),
  ArrowRight after the click pans 80 px (79.88), and the pipeline ends with
  every tile loaded, no errors and no re-ships (55 of 55). The map reports its
  canvas origin (`Map.CanvasOrigin`, from the R24 row — the canvas is
  painter-only and has no accessibility node) and the demo prints it, which
  is how the scene aims its pointer. The screenshot tour gained `tiles=stub`
  (a scene knob that starts the stub and points `BOXER_MAP_TILE_URL` at it)
  and `scene_35_portolan`, a pan and a capture over the stub tiles.

Estimate: roughly 3,300 lines of Go plus ~1,500 of tests, five to seven
weeks, with the go/no-go after M0.

### Open questions

- **Q1 — O3 or O4 — answered: O3.** M0's headless half found nothing
  structural against it — no lost motion, one frame of lag, negligible frame
  cost — and the desktop feel test passed the same day. O4 stays recorded as
  the fallback; nothing selects it.
- **Q2 — What becomes of ADR-0203's O2/O3.** A one-day hedge still worth
  taking, or wasted once this ADR is accepted; either way ADR-0203's Decision
  is superseded by this one on acceptance (Status).
- **Q3 — `paintImage` and SVG export.** The command draws through an
  `ImageCache`, the type the image widget mirrors into the export pixel
  cache; whether the painter's instance has the mirror attached is to be
  verified in M0. If it does, ADR-0203 Q1's gap closes with the port.
- **Q4 — Pinch.** Zoom factor plus anchor (§SD6) is what every egui widget
  gets, and M3 built it so (a gesture ends 120 ms after its last factor and
  snaps); whether a map user misses the midpoint pan is still a question
  for the carrier's touch clients — the headless driver has no touch verb,
  so M3 could not answer it.
- **Q5 — Bandwidth — measured.** 256 KB per 256 px tile, exactly: the first
  full paint of a 720 × 460 view at zoom 12 shipped 12 tiles, 3.00 MB, in
  9–10 frames (about 150 ms with OSM over the internet); a pan plus two zoom
  levels reached 9.0 MB with zero re-ships. That is ~12× the PNG bytes, over
  the in-process channel, not a network; a 1400 × 900 view is ~30 tiles,
  ~7.5 MB on first paint. Closed.
- **Q6 — `ZoomSnap` default.** Stays 0 after M3: a notch zooms by +0.69
  through the sigmoid, animated; with 1 each notch would be one animated
  whole level. Both are a one-line option; which feels better on the
  desktop is the user's call, not the port's.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| egui2 IDL (`egui2_definition_d_walkers.go`) | `walkersMap`, `mapMarker`, `mapPolyline`, `h3Cells`, `h3Region`, `mapRaster`, `fetchR15WalkersCameras` removed at M4 | regenerated dispatch on both sides; `SKILL.md` §16; the interpreter's walkers sections; `walkers_tiles.rs` |
| Exported Go API under `public/` | new packages `widgets/portolan` (`Map`, overlays, view readback) and `widgets/portolan/h3overlay` (cells, regions); `c.WalkersMap*`, `c.MapMarker*`…, `StateManager.GetWalkersCamera`, `basemap.Apply(c.WalkersMapFluid)` removed at M4 | `play`, `terrainscope`, the demo gallery; `basemap` gained `PortolanSource`/`PortolanLoader` at M2 |
| Env registry (ADR-0009) | `BOXER_MAP_TILE_*` names and meanings kept; `_CA_FILE` / `_INSECURE_TLS` now configure Go's `http.Transport` instead of the renderer | `doc/env-vars.md` regeneration; the descriptions' "renderer" wording |
| `h3bridge` wasm exports + Go wrapper | `h3_dissolve` added (M4); `Handle.DissolveE` added | prebuilt wasm rebuilt (340,768 → 383,580 bytes); `scripts/ci/h3_wasm_parity.sh` gains the case (`golden_dissolve.ndjson`); ADR-0003 carries the dated Update |
| `imzero2` Cargo manifest | `walkers`, `reqwest` removed at M4; `h3o`, `geo` if unused; `[patch.crates-io]` untouched | `Cargo.lock`; the airgap bundle's crate set; ADR-0203's Context figures re-taken |
| Headless trace driver (ADR-0154) | `drag` verb added (M0, shipped 2026-08-22) | its verb list in `doc/howto/launch-apps-non-interactively.md`; `carrierclient` |
| `THIRD_PARTY_NOTICES.md` | Leaflet, BSD-2-Clause | the in-package licence text (§SD3) |

Untouched: the painter lane's IDL (no new op), the FFFI2 frame protocol, the
camera readback's consumers other than through the new package's view state.

## Alternatives

- **O1 / O2** — see the QOC matrix; cheap, and not ways to own the widget.
- **A clean-room reimplementation.** Rejected: BSD-2-Clause makes it
  unnecessary, and a clean room throws away the specification suite that is
  half the reason to port Leaflet rather than invent.
- **Port a larger library (OpenLayers, MapLibre).** Rejected on scope: both
  are an order of magnitude larger and vector-tile centric; the tree's map is
  a raster basemap with overlays, which is exactly Leaflet's kernel.
- **Adopt walkers' core now, evolve it toward Leaflet later.** Rejected as
  double work: the modules that would survive are the ~110 statements of
  projection math, and everything else is replaced.
- **O3 with a compatibility shim keeping `c.WalkersMap`.** Rejected: three
  call sites, and the decision was to retire the crate's name from the API.

## Consequences

### Positive

- The map becomes this tree's code: Leaflet's behaviour (retention,
  animation, bounds, snapping, CRS) and Leaflet's specs in the default
  `go test` lane.
- walkers, its HTTP/TLS chain, imzero2's own `reqwest`, and the `resvg`
  stack walkers enables leave the client — 96 crates on the desktop profile
  and 125 on the headless ones by ADR-0203's measurement plus the analysis's
  — and `ring` with them, leaving `mimalloc` as the only C-compiling crate in
  the headless closure.
- ADR-0165's question is answered without IDL work; one HTTP egress, under
  the env registry, as it asked.
- The render client gets thinner by about 2,000 lines; the IDL loses a
  widget family; the egui version ring loses a crate that had to move in
  lockstep.
- `SimpleCRS` gives the tree a tiled raster viewer it did not have.

### Negative

- Five to seven weeks, and ~3,300 lines of Go plus tests to own — bugs in
  pan, zoom and the pyramid are now ours to fix, with Leaflet's code as a
  reference rather than a dependency.
- A one-frame input lag on drag until and unless the pan preview (§SD8) is
  added; O4 if neither is acceptable.
- Two map widgets coexist until M4; screenshots, the gallery and the tour
  show both.
- The H3 region overlay needs a bridge extension and a wasm rebuild before
  the gallery can move.

### Neutral

- The `BOXER_MAP_TILE_*` knobs keep their names; what changes is which
  process honours them.
- ADR-0056's subsidiary decisions about walkers' shapes (`Plugin<'p>`,
  leaked `&'static str` attribution, the `HttpTiles` rebuild path) become
  history rather than constraints.
- Leaflet's 2.0 line is pre-release; the algorithms ported are the 1.x
  algorithms and do not depend on 2.0 stabilising.

## Migration — Tier 1

- **Breaks.** At M4: `c.WalkersMap`, the five overlay builders,
  `basemap.Apply(c.WalkersMapFluid)`, `StateManager.GetWalkersCamera` and the
  `WalkersCameraValue` type, and the `walkersMap` opcode family in the IDL.
  Before M4: nothing — the new package is additive.
- **Path.** Replace `c.WalkersMap(ids, lat, lon, noTiles)…Send()` with
  `portolan.Map(c, ids)` plus its view and source options; replace
  `basemap.Apply(mw)` with its `portolan`-typed form; replace overlay builders
  with the package's overlay methods; read the view through the package's
  readback instead of `GetWalkersCamera`. Three call sites; each moves in the
  milestone that covers what it uses.
- **Regeneration.** `app egui2gen generate` on both sides at M4; the
  `h3bridge` wasm and its parity baseline at M4 (or earlier, when §SD9
  lands); `doc/env-vars.md` when the registry descriptions change.
- **Old shape.** Removed outright at M4, not deprecated — the walkers
  widget's only consumers are in this repository.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the kernel, with Leaflet's geo, geometry,
  CRS, projection, handler, animation and `GridLayerSpec` cases as Go tests
  (525 in the package, plus `h3overlay`'s and the bridge's dissolve tests);
  `scripts/dev/portolan-map-scene.sh`, the headless scene that drags and
  zooms through §SD10's verb and asserts the camera readback (M5, on stub
  tiles, exit status the verdict); the screenshot tour's `scene_35_portolan`;
  `cargo tree` and the musl check from
  [ADR-0203's Verification plan](./0203-map-widget-without-the-http-stack.md),
  re-taken at M4.
- **What would fail.** A pyramid change that loads or unloads a different
  tile count than Leaflet's spec says; a projection that round-trips off by
  more than the spec's tolerance; a drag in the headless scene that leaves
  the camera where it started or moves it by the wrong amount, a notch that
  zooms about the wrong point, a double click that misses its level, an
  arrow the map does not hear, a tile that errors or ships twice; `walkers`,
  `reqwest`, `rustls`, `ring` or `hyper` named by `cargo tree` under the
  client; a C-compiling crate in the musl check.
- **Gap.** Feel — how the inertia and the animations look — is still a
  person's call; the scene asserts where the camera ends, not how it got
  there (a flick's coast and a zoom read mid-animation were M3's
  observations). The carrier's touch path (pinch) has no driver-side
  instrument, and neither has a modifier-held drag (box zoom); both rest on
  their unit tests. The scene runs on the CPU-rasterised client by default —
  the wgpu headless client is a rebuild away, not a second lane.

## Status

Proposed — 2026-08-22; revised in place through 2026-08-23 with M0's
results, both halves (§SD6, §SD8, Q1, Q5), and with M1's to M5's completion.
Every milestone has landed: the `drag` verb, M1's kernel, M2's widget, with
`play` and `terrainscope` on it, M3's animations and handlers, M4's overlays,
H3 and the removal of the walkers binding (ADR-0056 now carries its
supersession), M5's scene and tour scene. Open for the reader who accepts:
Q2 (F7), Q3, Q4, Q6. On acceptance this ADR
supersedes
[ADR-0165](./0165-imzero2-tile-transport-over-fffi2.md) (folded in, §SD4) and
the Decision of [ADR-0203](./0203-map-widget-without-the-http-stack.md)
(Q2 records what is left of its O2/O3); at M4 it supersedes
[ADR-0056](./0056-walkers-map-h3-binding.md), whose binding it removes.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [leaflet-port-analysis](../adr-background-work/leaflet-port-analysis.md) — the measurements, the three split shapes, the substrate check and the cut line this ADR decides on.
- [ADR-0203 — a map widget without the HTTP stack](./0203-map-widget-without-the-http-stack.md) — the dependency measurement and the musl finding.
- [ADR-0165 — imzero2 tile transport over FFFI2](./0165-imzero2-tile-transport-over-fffi2.md) — folded in by §SD4.
- [ADR-0056 — walkers map and H3 binding](./0056-walkers-map-h3-binding.md) — the binding being replaced.
- [ADR-0149 — porting the ImPlot core to Go on the painter lane](./0149-implot-core-port-painter-lane.md) — the precedent: port contract, `paintImage`, provenance, coexistence.
- [ADR-0140 — hover-scoped wheel capture](./0140-imzero2-hover-scoped-wheel-capture.md) — R23.
- [ADR-0154 — headless carrier tree and driver](./0154-headless-carrier-tree-and-driver.md) — the trace driver §SD10 extends.
- [ADR-0128 — imzero2 mesh draw stream codec lane](./0128-imzero2-mesh-draw-stream-codec-lane.md) — textures on the headless lane; the deferred musl-static target.
- [ADR-0009 — environment variable registry](./0009-environment-variable-registry.md) — the `BOXER_MAP_TILE_*` block.
- Leaflet — <https://github.com/Leaflet/Leaflet>, BSD-2-Clause.
