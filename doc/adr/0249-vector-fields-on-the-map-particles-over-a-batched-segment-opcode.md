---
type: adr
status: accepted
date: 2026-09-19
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-19
---

# ADR-0249: vector fields on the map — Go-advected particles over a batched segment opcode

## Context

[The portolan map widget](../../public/thestack/imzero2/egui2/widgets/portolan/) draws what an
app hands it through the `Projector` it passes to `Map.Render`'s callback:
markers, polylines, polygons, a raster pinned to bounds (ADR-0204), H3 cells,
country outlines, and — as a guest on the same canvas — a graphview graph
(ADR-0228, [graph-on-a-map](../howto/graph-on-a-map.md)). Every one of those
is a finite set of shapes. A gridded vector field — 10 m wind from a weather
model, an ocean current, the gradient of a scalar — is not: a global
quarter-degree grid is about a million vectors per time step and level, a
kilometre-scale regional model is past ten million, and a forecast is tens of
steps of that.

Four things make it a decision rather than another overlay helper.

- **The presentation that reads best moves.** Arrows on a grid show direction
  at the grid's points; particles drifting along the field show the flow's
  structure — fronts, gyres, convergence — and that is the presentation asked
  for first. It is an animation of tens of thousands of short line segments,
  re-sent every frame.
- **The painter lane has no batched line primitive.** `paintMarkers` and
  `paintRectsFilled` carry a series in one opcode (ADR-0149 §SD3); a line is
  one `paintLine` each. graphview paints an edge per opcode and that holds at
  its sizes; it is not known to hold at particle-trail sizes, and nothing in
  the tree has measured it.
- **The data is larger than the screen can show at any zoom.** A canvas
  resolves at most one vector every few pixels, so what is drawn is always a
  decimated window of the field, and the decimation has to come from somewhere
  other than the per-frame path.
- **The file formats are a project of their own.** GRIB2 is the usual
  container. Artifacts build with `CGO_ENABLED=0` (ADR-0215), which rules out
  the reference C decoders, and GRIB2's packing templates are several — which
  of them a pure-Go decoder must cover depends on whose files are to open.
  That question is heavy, separable, and is left out of this decision (see
  *Deferred*).

None of it is new. A
[survey of the published implementations, the literature and the data](../adr-background-work/vector-field-flow-visualization-survey.md)
— clean-room: documentation, papers, changelogs and issue prose, no source —
found the combinations that work and a catalogue of defects their builders
shipped and fixed. The subsidiary decisions below cite it where they decide
against one of those defects.

The scope asked of the design: one global field, a time series with scrubbing,
kilometre-scale regional grids, and more than one level or variable — so the
data contract is a generic two-component field, not a wind layer.

## Design space (QOC)

**Question.** Where are the particles advected, and by what route do their
trails reach the screen?

**Options.**

- **O1** — advect in Go, paint with the opcodes that exist: one `paintLine`
  per trail segment.
- **O2** — advect in Go, paint through a new batched opcode, `paintSegments`:
  parallel endpoint arrays and a colour per segment, one opcode per layer per
  frame.
- **O3** — advect in Go, rasterise the fading trails into an RGBA buffer in Go
  and ship it through `Projector.Image` every frame.
- **O4** — a purpose-built host widget, the shape ADR-0058's scrolling texture
  took: the field ships once as a texture under the send-once protocol, and
  the Rust host advects and draws.

**Criteria.**

- **C1 — per-frame FFFI traffic.** Bytes and opcodes per frame, derived from
  the wire shape.
- **C2 — host surface.** Code that lands in the Rust host, outside the reach
  of `go test`, and whether anything else can use it.
- **C3 — the look.** Trails that fade along their length.
- **C4 — reach.** Whether every host — wgpu, the ADR-0205 CPU rasterizer, SVG,
  the ADR-0128 mesh lane — shows the layer without per-host work.
- **C5 — testability.** Whether the advection is a Go function a property test
  and a deterministic capture can pin.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | +  | −− | ++ |
| C2 | ++ | +  | ++ | −− |
| C3 | +  | +  | ++ | ++ |
| C4 | ++ | ++ | +  | −  |
| C5 | ++ | ++ | ++ | −  |

C1 is arithmetic, not measurement: O2 is four `f32` and one `u32` per segment,
20 bytes; O3 is four bytes per canvas pixel per frame whatever the particle
count; O1 is O2's payload plus an opcode header and a dispatch per segment.

## Decision

We will draw a vector field as particles advected in Go and painted through a
new batched `paintSegments` opcode (O2), by a portolan sub-package that is one
more guest in `Map.Render`'s callback, reading its data through a windowed,
step-indexed sampling contract whose first implementation is an in-memory
pyramid.

### Subsidiary design decisions

#### SD1 — the data contract is a windowed sample of one two-component field

A source is **one** field: two `float32` components, east and north, in the
source's declared unit, over time steps it enumerates. The layer asks for a
window — geographic bounds, a step index, and the most columns and rows worth
returning — and gets back a grid regular in latitude and longitude, no finer
than asked, with `NaN` for missing, and a version.

```go
type SourceI interface {
	Describe() (meta Meta)
	SampleE(ctx context.Context, req Request) (win Window, err error)
}
```

- **Level and variable select a source; they are not axes of the contract.**
  The layer draws one horizontal slice at a time. An n-dimensional cube in the
  contract would move axis selection into the widget and make every source
  implement slicing; an app that offers ten pressure levels holds ten sources.
- **The window is bounded by the request, not by the data.** That is what
  keeps the layer's cost independent of the field's size, and what lets an
  out-of-core or a ClickHouse-backed source replace the in-memory one without
  the layer changing.
- The contract lives in a package of its own under
  [public/science/geo](../../public/science/geo/) and imports no widget, so an
  ingester or a SQL-side producer can implement it without the UI in its
  import graph.

**What a window promises**, each clause a bug some tool has shipped
([survey §6, §8](../adr-background-work/vector-field-flow-visualization-survey.md)):

- **Components are earth-relative east and north, never grid-relative.** A
  source on a Lambert, polar-stereographic, rotated or curvilinear grid
  collocates staggered components, rotates them, resamples them *as a vector*
  and only then decimates — in that order. The error of skipping the rotation
  is smooth and leaves speed intact, so nothing downstream can detect it.
- **Rows run north to south, columns west to east, and bounds are node
  positions** — the coordinates of the first and last sample, not pixel edges.
- **`NaN` is in both components or in neither**, and is the only spelling of
  missing: no fill sentinel leaves a source.
- **A gutter.** Where the data exists, the window reaches at least one sample
  beyond the requested bounds on every side, so interpolation at the edge has
  its neighbours.
- **Periodicity belongs to the source.** `Meta` says whether the field wraps
  in longitude. A periodic source serves a window across the antimeridian — or
  across its own 0/360 seam — as one contiguous grid with increasing
  longitudes, and its gutter column is the wrapped one. A regional source
  returns `NaN` beyond its extent and never wraps.
- **A step is an instant or an interval**, with a valid time, a reference
  time, and for an interval its bounds and process. Steps are not assumed
  evenly spaced. A step that does not exist is an error distinct from a
  window of `NaN`.

`Meta` carries what a reader needs to not mistake one field for another — the
quantity, the unit, the vertical surface by kind and value (a 10 m wind and a
10 hPa wind share a number), the native grid and what the source did to reach
lat/lon, the policy at coasts — and the magnitude range the palette spans.

`SampleE` runs off the render thread. The layer keeps the last good window on
screen and asks again when the view leaves the window's margin or crosses to
another level, with separate thresholds up and down so a view resting on a
boundary does not alternate, and no more often than a debounce allows. A
window that is too coarse for the zoom is not asked for again once the source
has nothing finer, or a view zoomed past the data would ask on every debounce. It
cancels a request a newer one supersedes, accepts a reply only if it answers
the latest request, never keeps a partial one, and replaces a window in one
assignment. Removing the layer cancels what is in flight.

#### SD2 — the first source is an in-memory pyramid, built per step

Each level halves the one below by a box mean of the components — the
**vector mean**, meteorology's *resultant* wind, and what the surveyed systems
that document a vector-aware downsampling do. `SampleE` serves from the
finest level no finer than the request, so a window's spacing is between one
and two times what was asked and a world view of a ten-million-vector grid
reads a few thousand cells. A request coarser than the top level is reduced on
the fly, by whole factors, with the same weighted mean the levels are built
with.

- **A third plane carries the scalar mean of the magnitude.** The vector mean
  is never longer than the scalar mean and is much shorter where directions
  disagree, so colouring by its length would make a convective region read as
  calm at low zoom. Particles are advected by the vector mean, which is the
  right mean for transport; colour and any readout of speed come from the
  scalar mean, and a readout says which it shows. The ratio of the two is the
  field's *constancy* per cell, free to whoever wants it. **No system or paper
  surveyed builds this plane**; the justification is the terminology, not
  precedent. It costs half again the memory of the two component planes.
- **Missing samples are weighted, and a threshold decides the coast.** A
  `NaN`-aware mean composes from level to level only if each cell remembers
  how much of it was valid, so a field with missing samples carries a coverage
  plane; one without does not pay for it. A coarse cell is valid when at least
  half of what it covers is: "any valid" floods an ocean current over the
  land as levels coarsen, "all valid" erodes the coast.
- **Each level has its own origin and spacing.** A box mean puts a coarse
  sample at the centre of its children, half a fine cell from where naive
  halving would put it. An odd dimension leaves a last sample that averages
  what exists, and whose node is then not quite the centre of what it covers —
  an error at the far edge of a regional field, at coarse levels only. A
  periodic source halves in longitude only while its column count is even, so
  the seam stays a sample boundary; past that the on-the-fly reduction takes
  over, which addresses columns modulo the circle and needs no such care.
- **Steps load through a callback and sit in a cache bounded in bytes**, not
  entries — a decoded step of a regional model is over a hundred megabytes —
  with the two steps bracketing the display time pinned and the next in the
  direction of play fetched ahead.

#### SD3 — time is interpolated in the layer, linearly

The layer holds the windows of the two steps bracketing the display time and
blends components, and the scalar-mean plane separately, per particle sample.
Sources stay free of time arithmetic, and a scrub between two loaded steps
costs no fetch. While one of the pair is still loading the layer shows the
other alone, and takes the pair up when it arrives.

**Linear blending is wrong for anything that moves.** A front or a cyclone
between two steps does not translate; it fades out in one place and in at the
other, with a lull between. At hourly spacing that is invisible, at six-hourly
spacing with a fast system it is a double image. Better interpolators exist in
the literature and in no display tool found; this ADR takes the common
practice and records the defect.

**A trail is not a trajectory.** Display time and data time are decoupled — a
particle crosses the screen while the forecast clock stands still — so each
trail is a streamlet of the instantaneous blended field. It shows where the
flow points now, not where the air will go. The package doc says so where a
caller will read it.

#### SD4 — particles: world coordinates, a fixed tick, stylised speed

- **Position is `float64` in the view's CRS, zoom-independent.** A pan moves
  no particle state and trails stay attached to the ground. World-to-canvas is
  the affine map the `Projector` already has. Thirty-two-bit positions are
  what made a shipped layer of this kind stop moving above zoom 15.
- **Simulation advances on a fixed tick, not per rendered frame.** Elapsed
  time is consumed in whole ticks, a bounded number per frame, and a trail
  gains a point per tick. Speed and trail length are then the same on a 60 Hz
  and a 120 Hz display, and determinism is a tick count: for a capture the layer runs a fixed
  number of ticks from a fixed seed, asks its source on the frame goroutine
  rather than on one of its own, and then rests — the picture is a function
  of the seed, the view and the field, complete on the first frame. The
  random stream is the layer's own.
- **The integrator is the midpoint rule.** Forward Euler — what every surveyed
  renderer appears to use — lengthens the radius on each step round a closed
  circulation, so a cyclone is drawn as a weak source: the display misstates
  what kind of critical point it is, more so the longer a particle lives. The
  midpoint rule closes the orbit for one more field sample per tick. A
  fourth-order scheme buys nothing on bilinearly interpolated data. A step is
  at most one sample of the window.
- **Direction goes through the projection; speed is a screen quantity.**
  Screen direction at a window node is found by projecting a small east/north
  displacement through the view's CRS, once per window, so it is right in
  EPSG:3857, where conformality makes it trivial, and in the other CRSs
  portolan carries — and the screen's downward y is in that map, not in a
  sign somebody remembers. Displacement per tick is a clamped linear function
  of magnitude **in screen pixels**, converted to world units at the current
  zoom: the same wind moves at the same pace on screen at every zoom and every
  latitude. The floor keeps slow flow creeping instead of freezing into dots;
  the cap keeps a step inside a sample. **The animation therefore shows
  direction and relative speed, not transport**, and colour is the honest
  channel for speed.
- **The layer asks for windows at one sample per few screen pixels**, no
  finer. That is the pyramid's level choice, and it is also what keeps the
  field's spatial frequency below what the step can follow.
- **Count follows the canvas area**, by a density, under a cap.
- **Lifetime is randomised three ways.** A new particle starts at a random
  age, so cohorts do not die together and the field does not pulse. Each tick
  a particle is re-seeded with a probability that rises with its speed,
  because fast particles cover more pixels and fast regions otherwise look
  denser. A hard maximum age ends the ones circling an eddy.
- **Re-seeding is uniform over the viewport**, which in a Mercator world is
  uniform on screen. A particle that leaves the padded viewport, would land
  on `NaN`, or would land below a calm threshold is retired too; a zoom-in
  empties the viewport of old particles and refills it. Sampling is strict —
  any `NaN` corner is `NaN` — and a step is taken only if the field has a
  direction where it lands, so particles stop a sample short of a coast or a
  regional edge and never stand past it. A seed that finds no direction after
  a few tries is left for the next tick, which is what gathers the particles
  of a regional field inside it.
- **A retired particle drains; it does not vanish.** It stops, and for a
  trail's length of ticks its stroke shrinks toward its head before the
  particle is seeded again. Removing a stroke in one frame is the popping
  that insertion and deletion are known for, and the head-first shrink keeps
  the direction cue to the end.

A trail is the particle's last few ticks, drawn as segments whose alpha falls
with age and whose colour is the scalar mean through a palette of the
[colormap package](../../public/thestack/imzero2/egui2/widgets/colormap/). A
stroke that fades into the background at one end is read as flowing away from
that end, so the trail shows direction in a still capture; a new particle's
trail grows from nothing and is never dimmed as a whole, which would undo
that. The default palette's slow end is light: a perceptual sequential ramp
starts dark and loses the slow half of a field against a dark map, where the
tail's alpha already does the fading.

#### SD5 — `paintSegments`: one opcode, a colour per segment

```
paintSegments(x0s, y0s, x1s, y1s []f32, cols []u32, strokeWidth f32)
    .tessellated()   // a hint a host may ignore
```

Arrays truncate to the shortest, as `paintRectsFilled`'s do. A colour per
segment rather than one per opcode: trail fade and magnitude colouring give
nearly every segment its own colour, and bucketing them into
single-colour batches trades four bytes a segment for a batch per (age,
colour bin) pair and a quantised look.

**The opcode does not promise feathered antialiasing.** The host may emit one
untextured mesh of quads for the whole batch instead of a tessellated shape
per segment, and that is what the host does. `tessellated()` asks for the
feathered line shapes instead; it exists so that M5 can measure one against
the other on the wgpu and the CPU-rasterizer hosts without a host rebuild, and
which becomes the default is settled there, not here.

#### SD6 — the layer is a guest, takes no input, and paces itself

`flowoverlay` is a portolan sub-package the map does not import, like
`h3overlay` and `landoverlay`. It paints in the callback, so its place under
or over a graphview guest is the caller's call order. It claims no pointer and
no key. It exposes the blended field value at a `LatLng` for an app's own
readout.

While it animates it asks for a repaint at its tick rate, default 30 Hz,
independent of the map's 60 Hz interaction repaint. **A window showing an
animating layer never idles**; `Paused` stops the requests and leaves the last
trails on screen.

### Milestones

- **M1 — `paintSegments`.** ✓ The IDL node, the host's draw, a gallery entry
  beside the other painter primitives.
- **M2 — the contract, its conformance suite, and the pyramid.** ✓ `SourceI`; a
  test suite any implementation runs against itself; the in-memory pyramid
  with the scalar-mean and coverage planes and the step cache; an analytic
  source (solid rotation plus a vortex, and a masked variant) that needs no
  data file.
- **M3 — the particle layer.** ✓ `flowoverlay` over M1 and M2 for a single
  step; the `flowonmap` gallery demo, alone and under a graphview guest; the
  deterministic capture.
- **M4 — time.** ✓ Bracketing windows, interpolation, a display-time setter a
  host binds to its own scrubber.
- **M5 — the trial.** ✓ Under [doc/trials](../trials/): frame cost against
  particle count, O1's `paintLine`-per-segment arm against `paintSegments`,
  mesh against tessellated segments, on the wgpu and CPU-rasterizer hosts.
  Its §0 is the figure this ADR's Updates will cite; SD5's host choice and the
  default density follow from it.

### Deferred

- **GRIB2 ingestion — its own ADR.** The
  [survey's §7](../adr-background-work/vector-field-flow-visualization-survey.md)
  has what public documentation says of the packing each open product uses
  and of the pure-Go decoders' stated coverage; for several products no
  provider statement was found, and one file each would settle it. Until then
  a source is filled from whatever the caller can already read into two
  `float32` planes.
- **A ClickHouse-backed source for play**, on the ADR-0096 pattern of a
  viewport-keyed query; SD1's window is shaped so that it can be one.
- **Static presentations** — an arrow or wind-barb grid at screen-uniform
  spacing, and a magnitude raster underlay through `Projector.Image`. Both
  read the same `Window` and neither needs a new opcode. The literature's
  preference, glyphs along evenly spaced streamlines rather than on a grid, is
  a decision for then.
- **An out-of-core pyramid** — tiled levels on disk — for a field whose
  single step exceeds the cache budget.
- **Pole rows.** Near a pole the east/north basis turns with longitude, so a
  box mean across longitudes cancels a real cross-polar flow. The effect is
  small below about 80° and the map stops at 85°; a pyramid that averages the
  polar rows in a fixed local frame is the remedy.
- **Stricter density control** — an occupancy grid that caps births in full
  cells — and **cubic sampling**, which one surveyed layer defaults to.
- **O4, host-side advection**, held as the fallback M5 can call for.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| egui2 IDL, painter definitions | `paintSegments` added | the generated Go bindings and Rust dispatch (`app egui2gen generate`); a `PaintCmd` variant and its draw in the host, hand-written; both sides of the FFFI boundary rebuild |
| Exported Go API under `public/` | new packages: the field contract with its pyramid under `science/geo`, and `portolan/flowoverlay` | `package_props.go` for each and the proptable harvest; nothing in portolan's existing API changes |

## Alternatives

O1, O3 and O4 are assessed in the QOC matrix. Their kill-reasons, and the
smaller ones:

- **O1 — `paintLine` per segment.** An opcode and a dispatch per segment at
  tens of thousands of segments a frame. It stays in the tree as the trial's
  baseline arm, which is what will show whether the new opcode was needed.
- **O3 — a Go-rasterised trail texture.** Gives the accumulation fade for
  free, and costs four bytes per canvas pixel per frame across the FFFI
  boundary and a full texture upload per frame in the host, whatever the
  particle count. It is also a screen-space buffer, which every pan and zoom
  invalidates: the maintainer of one surveyed layer shipped without trails for
  that reason, and proposed stored positions as the way out.
- **O4 — a host-side field widget.** The least traffic, and the most code
  where `go test` does not reach: a simulation in Rust, specific to this one
  layer, that every host must carry. Deferred, not dismissed.
- **One `paintPolyline` per particle.** Exists already and halves the
  coordinates, but a polyline has one colour, so the trail cannot fade along
  its length, and it is still an opcode per particle.
- **Single-colour segment batches.** See SD5.
- **An n-dimensional field contract** with level and variable axes. See SD1.
- **Particles in screen space.** A pan would have to translate or discard
  every particle and trail; in world coordinates it is free. The surveyed
  layers that keep screen-space state reset on every interaction and take
  most of a second to a couple of seconds to recover.
- **Forward Euler**, which is what the surveyed renderers appear to use. See
  SD4: it opens closed orbits, and the cost of not doing so is one field
  sample.
- **A step per rendered frame**, normalised by the measured frame rate. One
  surveyed layer needed six releases to get that stable, and then had to
  normalise trail length separately; a fixed tick has neither problem.
- **Velocity constant in world units.** Particles then run faster, longer and
  denser on screen with every zoom-in — shipped, and called expected, by one
  surveyed layer; fixed over several releases by another.
- **A width per segment**, so that a trail's head can be wider than its tail —
  the one design cue from the perception literature this ADR does not take.
  Alpha already gives the asymmetry that makes direction readable; a width
  array adds a fifth to the opcode's payload for a second cue to the same
  thing. The opcode can grow it if a capture shows the need.
- **Interpolating or averaging speed and direction** instead of components.
  Direction has a branch cut, is undefined at calm, and makes the field
  discontinuous; components shorten where they disagree, which SD2's second
  plane answers where it matters.
- **A widget of its own rather than a portolan guest.** It would re-own the
  camera, the tiles and the gestures, and could not sit under a graph; the
  guest seam (ADR-0228) exists so that this is not needed.

## Consequences

### Positive

- The layer's per-frame cost depends on the particle count and the canvas,
  not on the field's size; the field's size is met once per window, off the
  render thread.
- `paintSegments` is general. The directional-particle gap recorded in the
  [force-graph gap analysis](../adr-background-work/force-graph-graphview-gap-analysis.md)
  and graphview's straight edges are candidates for it; neither is decided
  here.
- The advection, the pyramid and the interpolation are plain Go under the
  default test lane.
- A source is small to write, so the GRIB decision can be taken on its own
  evidence without holding this one up.

### Negative

- An animating layer re-sends its whole segment batch every frame and keeps
  the window from idling. On the mesh lane (ADR-0128) each frame's trails are
  new geometry for the viewer; what that costs a remote session is not known
  and is outside M5's two hosts.
- The animation is not physical (SD4) and a trail is not a trajectory (SD3).
  A reader who takes particle travel for transport is misled, and only the doc
  comment stands in the way.
- Moving weather fades between steps instead of moving (SD3), visibly so at
  coarse step spacing.
- Coarse pyramid levels shorten vectors where the field is incoherent (SD2);
  the scalar-mean plane corrects the colour, not the particle's pace, and
  small vortices legitimately disappear from coarse levels.
- A source has real work to do before it satisfies SD1, and a wrong one —
  unrotated grid-relative components above all — produces a plausible map.
  The conformance suite checks the contract's form, not a source's
  meteorology.
- The SVG host shows a single frame of trails, as it does of any animation.
- One more hand-written `PaintCmd` draw in a hybrid Rust file.

### Neutral

- Static glyphs, the usual first step, come after the animation here; the
  contract is the same for both.
- The pyramid holds a step's planes in memory at 4/3 of three base planes,
  four where the field has missing samples;
  the step cache's byte budget is the knob, and a field too large for it
  waits on the out-of-core deferral.

## Migration — Tier 1

Nothing to migrate: the opcode and both packages are additive.

- **Regeneration.** `app egui2gen generate`; the Go side and the Rust host
  rebuild together, since a host without the opcode cannot decode a frame
  that carries it.

## Verification plan — Tier 1

Each property below is a defect the survey found shipped somewhere; its §8 has
the symptom and who paid for it.

- **Lane — the contract's conformance suite**, default `go test`, run by every
  `SourceI` implementation against itself: a globally covering source yields
  no missing sample in any window, including one across the antimeridian and
  one across the source's own seam; a regional source yields `NaN` past its
  edge and never wrapped data; the gutter is present; bounds are node
  positions; `NaN` is in both components or neither; a window never exceeds
  its request; a missing step is an error, not an empty window.
- **Lane — the pyramid**, default `go test`: a constant field is constant at
  every level; the analytic rotation's direction holds within tolerance at
  every level and across the seam; the vector mean is never longer than the
  scalar mean, and equals it for a uniform field; a field of alternating sign
  does not alias into a uniform flow; a masked field's coast neither advances
  nor retreats by more than a cell per level.
- **Lane — the layer**, default `go test`: the same seed and tick count give
  the same positions, however the ticks are split across frames; a particle on
  the solid rotation stays on its circle within a bound over its maximum age;
  an easterly component moves a particle toward +x and a northerly one toward
  −y on the canvas; on-screen pace is the same at two zooms; a late reply to a
  superseded request is dropped.
- **Lane — the scene.** `scripts/dev/scene.sh` over the package's
  `scenes/flow-on-map.scene.md` (ADR-0248): the gallery demo on a headless
  host, asserting that trails are painted, that one request serves the first
  view, that a pan inside the margin asks for nothing, and that the readout's
  scalar mean is never the shorter; then the capture.
- **Lane — captures.** The screenshot tour: the `flowonmap` demo under its
  fixed-tick, synchronous mode. The Rust check lane for the host's draw.
- **What would fail.** A pyramid that averages magnitude and direction instead
  of components, a forward-Euler integrator, a step per frame, a
  projection-unaware direction or a remembered sign, a global random source,
  a wrapped regional field, or a segment draw that drops or reorders colours
  shows up as a red test or a changed capture.
- **Gap.** Nothing gates the frame cost between runs of the M5 trial; the
  animation's smoothness and the pulsing and density defects are judged by
  eye; and no test can tell that a source's components were rotated
  correctly from real data — only that a synthetic fixture was.

## Status

Accepted 2026-09-19.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-09-19 — M5: the opcode was needed, the mesh stays, the cap comes down

The [trial](../trials/flow-particles-frame-cost/README.md) ran once, on one
low-power machine that was not idle, one launch per cell; its §0 is the claim
and states what that is worth. What this record takes from it:

- **O1 stays rejected, now on a measurement.** Painting a `paintLine` per
  trail segment costs the Go side about 1 µs a segment on both hosts — 44 ms a
  frame at 5 000 particles against 6 to 10 ms for one `paintSegments` — and
  writes 32 bytes a segment against 20. The arm remains in the layer as
  `Options.LinePerSegment`, for the trial only.
- **SD5's host draw is settled: the mesh is the default.** On the
  CPU-rasterizer host the mesh rasterises in about a third of the time of the
  same segments as feathered line shapes; on the wgpu host the two do not
  differ. `tessellated()` stays a hint, for a GPU host that wants the
  antialiasing.
- **The default cap on the particle count comes down from 20 000 to 10 000**,
  and the default density stays. On the machine measured, the shipped arm's
  stages pass a 30 Hz tick between 10 000 and 20 000 particles on the GPU
  host, where the limit is the Go side and the host's dispatch and not the
  GPU; a CPU-rasterizer host holds the tick to about 5 000.
- **The largest cost on the Go side is not the layer's.** About 60 % of it is
  FFFI slice marshalling, an element per write; the trial files it as a
  finding against the runtime and this ADR does not decide it. It would move
  every arm, the shipped one most.
- **Still unknown:** the desktop host, and what an animating layer costs a
  remote viewer on the mesh lane.

## References

- [Drawing a large vector field on a map — what the state of the art does, and what it had to fix](../adr-background-work/vector-field-flow-visualization-survey.md)
  — the clean-room survey behind SD1–SD4 and the verification plan.
- ADR-0204 — the map, its `Projector`, and the overlay pipeline.
- ADR-0228 — hosted rendering: one canvas, more than one guest.
- ADR-0224 — graphview on the painter lane.
- ADR-0149 §SD3, §SD5 — one opcode per series; large rasters go to a texture.
- ADR-0058 — the scrolling texture, the precedent O4 would follow.
- ADR-0096 — play's viewport-keyed raster, the pattern for a SQL-backed source.
- ADR-0205, ADR-0128 — the CPU-rasterizer host and the mesh lane.
- ADR-0215 — `CGO_ENABLED=0` for shipped artifacts.
- [graph-on-a-map](../howto/graph-on-a-map.md).
