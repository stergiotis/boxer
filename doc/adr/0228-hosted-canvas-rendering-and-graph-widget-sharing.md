---
type: adr
status: proposed
date: 2026-09-12
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Phases 1 and 2 of §SD7 are built;
> the record is kept current with them, as a pre-acceptance ADR is. The
> measurements in §Context were taken against the live `portolan` and
> `graphview` APIs on the date above.

# ADR-0228: one canvas, two widgets — hosted rendering, and what the two graph widgets should share

## Context

Two questions have been waiting on each other.

**Geo.** [ADR-0224 §SD12](./0224-graphview-go-graph-widget-painter-lane.md)'s
gap analysis and the
[Cytoscape/Ogma follow-on](../adr-background-work/graph-viewer-gap-analysis-cytoscape-ogma.md)
§3.9 both arrived at a graph drawn on a basemap, and both deferred it the same
way: "an injectable camera, a contract between two widgets, which wants its own
ADR". That framing turns out to be wrong about which part is hard.

[portolan](../../public/thestack/imzero2/egui2/widgets/portolan/)'s scale
function is `256·2^zoom`, a pure multiplier, so `Project(ll, z)` factors as
`ProjectAt(ll, z0) · ZoomScaleAt(z, z0)` and the container point is that minus
`PixelOrigin()`. That is exactly graphview's camera, `screen = world · zoom +
pan`. Checked against both live APIs at zooms 0, 3, 7, 11, 14 and 18 over six
spread points, the worst disagreement was **0.49 px**, which is Leaflet's own
half-pixel `.Round()` in `LatLngToLayerPoint` and nothing else. **There is no
projection to inject.** Three lines derive one camera from the other.

A second measurement decides how a caller should feed positions. Declaring
*global* projected pixels as world units costs float32: over a city-sized
spread the worst error is 0.03 px at zoom 12, 0.48 px at 16, **1.92 px at 18
and 7.62 px at 20** — `NodeSpec.PinX/PinY` and the retained positions are
`float32`. Declaring **layer points** instead — projected relative to
`PixelOrigin()`, recomputed per frame, camera identity — measures **0.0000 px
at every zoom**, because the numbers stay viewport-sized. The second recipe is
the one to document.

What actually blocks the composition is **input**. graphview and portolan each
end their render the same way: paint, then
`PaintSenseRegion("…-area", 0, 0, w, h)`, then `PaintCanvas("…-canvas", w, h)`.
Emission order is hit-test priority — the ADR-0149 M7 lesson §SD3 already
records — and each emits its drag-owning region last *on purpose*. Stack the
two and one of them receives nothing.

The substrate is friendlier than that makes it sound. `PaintCanvas` is emitted
**last and drains the paint commands before it**, so painting into another
widget's canvas is simply *not emitting a canvas* — a slot portolan already
exposes as `Render(w, h, overlay func(Projector))` and which
[h3overlay](../../public/thestack/imzero2/egui2/widgets/portolan/h3overlay/)
already occupies. And graphview picks Go-side over struct-of-arrays positions
from the canvas's R24 cursor row (§SD3) rather than with a region per node,
which means it needs *registers*, not regions — the property that makes it
hostable at all.

**Sharing.** ADR-0224's O4 asked whether graphview and
[`layeredgraph/view`](../../public/thestack/imzero2/egui2/widgets/layeredgraph/view/)
should share a painter core, and deferred it to "once graphview has
stabilised". It is two questions wearing one number, and they have different
answers; §SD6 separates them.

## Design space (QOC)

**Question.** How does a painter-lane widget paint and pick inside a canvas
another widget owns?

**Options.**

- **O1** — Hosted rendering: split canvas *ownership* from painting and
  picking. The guest paints into the current canvas and reads the host's
  registers.
- **O2** — An injectable camera only: the guest keeps its own canvas, the host
  supplies the transform. The phrasing the gap analyses reached for.
- **O3** — The guest emits its own sense regions into the host's canvas.
- **O4** — A composite widget per pair that owns the canvas and drives both.

**Criteria.**

- **C1** — One pointer, one unambiguous owner per gesture.
- **C2** — No change to the common case, a widget that owns its canvas.
- **C3** — Works for a second guest and a second host.
- **C4** — Testable headless.
- **C5** — Cost.

**Assessment.**

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | −− | −  | ++ |
| C2 | ++ | ++ | ++ | −− |
| C3 | ++ | −  | −  | −− |
| C4 | +  | +  | +  | +  |
| C5 | +  | ++ | +  | −− |

O2 does not address the problem it was proposed for: two canvases are still
two hit tests, and whichever is emitted later takes the pointer whole. O3 is
defeated by the ordering the hosts rely on — a host emits its drag region last
so it wins, so a guest's regions, emitted from the overlay slot before it,
lose; a post-area slot would fix that but makes every host carry a region
ordering contract, and it does not serve graphview, which wants registers.
O4 is a new widget per pair of widgets.

## Decision

**SD1 — A hosted render is the widget minus the canvas.** `View.Render` keeps
its signature and its canvas; a second entry point paints into whatever canvas
is current and reads its input from a handle the host names:

```go
type HostCanvas struct {
    Canvas, Area widgethandle.WidgetHandle // the host's, for the registers
    W, H         float32                   // the host's canvas size
    Camera       camera.Camera             // the transform to draw with
}
```

The transform is the shared camera of §SD5 rather than three floats, which
is what phase 1 made possible and what a host hands out directly
(`portolan.Projector.Camera`).

The guest emits no `PaintCanvas`, no area region and no background — the
background is the host's `PaintCanvas(...).Background(...)`, which the guest
does not call, so a hosted graph over tiles is transparent by construction
rather than by a flag. It pushes its own clip inside the host's, which is the
nesting `implot` already does.

**SD2 — Input is claimed before the host handles it, so hosting is two
phases.** A host applies the previous frame's input at the top of its frame
and calls the overlay slot at the bottom; one call cannot both claim a gesture
and paint with the result. So:

```go
canvas, area := m.Handles()
claim := gv.HostedInput(graphview.HostCanvas{Canvas: canvas, Area: area, W: w, H: h, Camera: cm})
m.SetPointerVeto(claim.Pointer)        // the host stands down this frame
m.Render(w, h, func(p portolan.Projector) {
    gv.HostedPaint(nodes, edges)       // paints into the map's canvas
})
```

`HostClaim` also names the node under the pointer, which a host has no use
for but a caller often does. The veto stops a *new* drag, box zoom or
double-click zoom; a gesture already in flight is the host's to finish, and
the wheel, the pinch and the keyboard are the host's in every case. One
consequence of reading the pointer before the declaration: the guest's pick
runs against the previous frame's geometry — the frame the pointer was
actually over — so a node first declared in the next frame becomes pickable
one frame later than it would under `Render`.

`HostedInput` runs the pick and the gesture logic and reports what it took;
`HostedPaint` reconciles, lays out and paints. The claim is the whole
arbitration rule: **a gesture that starts on a node is the guest's, everything
else is the host's**, and the host is told rather than asked, so a host needs
one setter and no knowledge of its guest. `View.Events` is read after
`HostedPaint` as after `Render`.

**SD3 — In hosted mode the camera is the host's and the guest owns no view
gestures.** `HostCanvas` carries the transform; `Options.NoZoomAndPan` is
implied, and a camera the guest moved would disagree with what the host drew.
`SetCamera`, `FitNow` and `FitNodes` are therefore not refused with an error
but *overruled*: `HostedInput` reinstalls the host's transform every frame and
a hosted paint clears the fit latch, so they simply have no lasting effect.
That is one rule rather than a set of guards, and it cannot be got wrong by a
caller who sets one of them from shared code. A consumer that wants "fit the
graph" in hosted mode moves the *host's* view — `FitBounds` over the
unprojected node box, which `Bounds()` and `View.Unproject` give it
(ADR-0224 §SD14).

**SD3a — A hosted declaration may mix located and unlocated nodes, and
needs nothing new to.** A node the data places is declared `Pinned` at its
projected layer point; a node the data joins to but never locates is declared
without one, and the force step lays it out among the pinned ones. That falls
out of ADR-0224 §SD10 — a declared pin is fixed and everything else still
feels it — and it is Ogma's `geo.runLayout` ("get the coordinates for the
nodes that don't have them") arriving for free rather than as a feature.

**Which camera the guest takes then stops being a free choice, and this is
the correction the demo forced.** Two recipes were on offer in §Context, and
the first reading recommended layer points universally:

- *Identity camera over layer points.* World units are canvas pixels. Exact at
  every zoom, and world-unit sizes — the node radius — are already screen
  sizes. **Right when every node is located**, because nothing has to be laid
  out.
- *The host's camera at a fixed reference zoom.* World units are projected
  pixels at that zoom — the same geometry whatever the host is showing.
  **Right as soon as a layout runs.**

Under the identity camera a layout is solving in screen space while the
geometry it solves against rescales with the view. graphview's ideal edge
length is derived from the canvas area, so it is a constant: in the demo, 174
px, against a Zürich–Bern separation that runs from 114 px at zoom 7 to 12,699
px at zoom 14. The unlocated nodes are yanked toward or flung away from their
neighbours as the view moves, and once one escapes, its edges are the long
clipped ones that gave the symptom away. A world fixed at a reference zoom
removes the problem rather than damping it: the layout sees one geometry, it
settles once, and a camera change is not one of the conditions that wakes a
settled simulation.

It also removes the anchoring question entirely. An unlocated node's retained
world position *is* geographic under the fixed world, so nothing needs
unprojecting after the paint and projecting back before the next declaration —
which was this record's first answer, and was both more code and, through
Leaflet's whole-pixel rounding of layer points, a source of jitter near
equilibrium.

What the fixed world costs is that world-unit sizes scale with the view: a
radius must be divided by the camera's zoom to stay a constant size on screen.
That is the caller's one-liner, and it is not a reason to solve in screen
space.

**Measure the fixed world from a local origin, not from the projection's
corner.** `View.CameraAt(refZoom, origin)` takes one, and pinning at
`ProjectAt(ll, refZoom) - origin` is the other half. Projected coordinates are
large absolute numbers — Zürich is 19,713 pixels from the antimeridian at zoom
7.2 — and two things go wrong when they are used raw. float32 spends its
mantissa on the magnitude, which is the ceiling §Context measured and which a
local origin removes rather than defers: the same data within a few hundred
units of zero stays sub-pixel at every zoom. And graphview's placement seats a
node with no placed neighbour in a box at the *world* origin, which under a
whole-world projection is somewhere in the Pacific; the node then crawls back
across the map at `MaxStep` a frame. The second failure was what the demo
actually showed, and it was half a widget defect besides — ADR-0224's dated
update of this date has the rest.

**SD3b — The paint takes a later camera than the pick.** A host applies input
at the top of its frame and offers its paint slot at the bottom, so by the
time the guest paints, the host's view has already moved. `SetHostCamera`
refreshes the transform between the two phases: the pick keeps the camera of
the frame the pointer was over, the paint takes the one being drawn.  Without
it the graph is a frame behind the host's own drawing and slides against it
under a pan — which is the second half of the same symptom.

**SD4 — What hosted mode gives up is named, not discovered.** The wheel
belongs to the host, so the guest neither zooms nor reads `GetCanvasWheel`.
Rectangle selection stays available, because it is a drag the claim can take.
The aura legend was the third item on this list and is no longer: a row's
sense region emitted from the overlay slot would sit under the host's area
region and never be clicked, so **a hosted view sets `AuraLegendExternal`**
(ADR-0224 §SD15) and the caller paints the rows from `AuraLegendItems` in a
canvas it owns, toggling with `HideAura` and `ShowAura`. That mode was built
ahead of this record rather than being left as a cost of hosting.

**SD5 — The camera type is extracted; portolan converts rather than
consumes.** graphview's unexported `camera` — the affine, `fit`, `zoomAround`
and the limits — becomes a small shared type under `widgets/`, and
`layeredgraph/view`, which re-derives the same arithmetic inline from its fit
and its `ViewState`, consumes it too. portolan keeps Leaflet's own view model,
which carries a CRS, `zoomSnap` and wrapping that the shared type has no
business holding; it gains one method that produces the shared camera from the
current view, which is the three lines §Context measured. The shared type is
what the two graph widgets have genuinely in common; the map is a *source* of
one, not a consumer.

**SD6 — The two graph widgets do not merge; two things they duplicate are
shared, and layeredgraph's prerequisite is a picking change.** ADR-0224's O4
is split:

- *Should the widgets merge?* **No, and this closes O4's first half.** The
  layout sources differ (host-side Graphviz over a finished `Layout` against a
  per-frame simulation the widget owns), the picking postures differ (a sense
  region per node against a Go-side pick over struct-of-arrays), and the node
  vocabularies differ (labelled boxes against discs with donuts and auras).
  Merging them is a redesign of the static widget for the live one's benefit,
  which is what O4's own assessment said and is still true.
- *What should they share?* The camera (§SD5) and the host seam (§SD1–SD4).
  graphview is the first guest because §SD3's Go-side pick already reads
  registers. **layeredgraph cannot be a guest until its picking changes**: a
  sense region per node is emitted into the host's canvas before the host's
  area region and loses, so hosting it means giving it the same Go-side pick
  over its node boxes. That change stands on its own — it also removes the
  `ui.interact`-per-node cost ADR-0224 §SD3 weighed and rejected — and it is
  the prerequisite, not a consequence.

A shared *painter* core — the node and edge drawing itself — stays deferred,
with the trigger unchanged: a consumer that needs one drawing in both
postures. Nothing in the tree asks for that today, and the two vocabularies
would have to converge first.

**SD7 — Order of work.** Four phases, each landing on its own and each useful
without the next:

1. ~~**Extract the camera** (§SD5)~~ — **done 2026-09-12**, ahead of the rest
   of this record. `widgets/camera` holds the type; graphview's unexported
   camera is gone and layeredgraph/view composes its fit, its user zoom about
   the canvas centre and its pan through it; portolan gained `View.Camera`
   and `Projector.Camera`. Two things the work turned up: layeredgraph's
   transform had no test at all, so the conversion is pinned by a property
   test against the arithmetic it replaced; and the float32 cost of the map
   conversion is dominated by the **pan**, not the world point — the pixel
   origin reaches about 35 million at zoom 18 and quantises to about two
   pixels on its own, which is why `View.Camera`'s doc sends a caller who
   needs exact placement there to layer points instead.
2. ~~**The host seam** (§SD1–SD4)~~ — **done 2026-09-12**. `HostCanvas`,
   `HostClaim`, `HostedInput` and `HostedPaint` in graphview; `Map.Handles`
   and `Map.SetPointerVeto` in portolan; the `graphonmap` gallery demo. That
   demo first shipped with tiles off, for a capture that needs no tile
   server, and the choice was wrong: a basemap is the point of drawing a
   graph over one, and a `NoTiles` map is a grey rectangle that shows nothing
   about the composition. Tiles are on with a toggle, as in the portolan
   demo. `Render` was split into
   `reconcileAndPlace` and `stepAndPaint` first, so both paths do the same
   thing to the graph and differ only in where the input comes from and
   whether the camera may move.
3. **A Go-side pick for `layeredgraph/view`**, replacing the region per node.
   *2–3 days.* It makes layeredgraph the second guest and pays for itself in
   the interact cost.
4. **Geo conveniences**, none of which need the widget: the layer-point recipe
   as a how-to, and `addGeoClustering` as a distance predicate on whatever
   grouping stage `nav` grows. *Deferred to grouping.*

**SD8 — Home and provenance.** The shared camera under
`widgets/` beside the other shared widget packages; `HostCanvas` and the
hosted entry points in graphview, since the guest defines what it needs and a
host satisfies it with handles it already has. The two-phase shape is this
design's own; the claim-before-handle rule is the ordinary one for nested
pointer handling and is not taken from Leaflet or from egui.

## Alternatives

- **O2 — an injectable camera and two canvases.** Killed for C1: the camera
  was never the obstacle (measured, §Context), and two canvases leave the
  pointer with one owner chosen by emission order rather than by the gesture.
- **O3 — the guest emits sense regions into the host's canvas.** Killed for
  C1/C3: hosts emit their drag region last deliberately, so a guest's regions
  lose; a post-area slot would work but puts an ordering contract in every
  host and serves a picking posture graphview does not have.
- **O4 — a composite widget per pair.** Killed for C2/C3: a third widget for
  every pair, each re-deriving both.
- **One call instead of two (§SD2).** Rejected: the host applies input before
  the overlay slot runs, so a single hosted call claims a gesture one frame
  after the host has already acted on it. The lag is exactly the bug the claim
  exists to prevent.
- **The host asks the guest through a callback** rather than being told.
  Rejected: it couples the host to the guest's type, where a boolean veto
  couples it to nothing.
- **Merging graphview and `layeredgraph/view`** (ADR-0224 O4). Killed, §SD6.
- **Global projected pixels as world units.** Rejected for the float32
  measurement in §Context: correct to about zoom 16, visibly wrong at street
  level. Layer points cost one projection per node per frame and measure
  exact.

## Consequences

### Positive

- A graph on a basemap with node hover, pick and drag, and the map keeping its
  own drag, wheel, pinch, box zoom and keyboard — which B1, the camera-writing
  loop, cannot offer.
- The seam is general: any painter-lane widget that picks from registers can
  be a guest, and any that owns a canvas can be a host.
- ADR-0224's O4 is answered rather than deferred again, and the half that is
  worth doing is separated from the half that is not.

### Negative

- graphview grows a second entry point and a mode in which several of its
  methods are refused; that is API surface whose only purpose is composition.
- layeredgraph's picking change is a real change to a working widget, made for
  a composition nobody has asked for yet — which is why it is phase 3 and not
  phase 1.

### Neutral

- portolan gains one setter and no knowledge of graphview.
- The camera extraction is invisible to every consumer of both widgets.
- The aura legend's external mode (ADR-0224 §SD15) landed ahead of this
  record, so §SD4 costs one line of caller setup rather than a feature.

## Verification plan

- **Camera (phase 1).** *Done.* graphview's camera tests moved with the type
  and gained round-trip and limits cases; portolan carries four: the
  factorisation in float64 against `LatLngToContainerPoint` at zooms 0–18
  within Leaflet's rounding, the float32 camera against that factorisation
  within a tolerance that names both quantisation terms, a sub-pixel
  assertion below zoom 14, and the layer-point recipe losing nothing at any
  zoom. layeredgraph's transform, which had no test, is pinned by a property
  test against the arithmetic it replaced.
- **Host seam (phase 2).** *Done.* A hosted scene scripts registers on
  handles the view does not own and asserts: a pointer over a node claims and
  names it while empty canvas does not; a claim holds for every frame of a
  node drag, including the one that ends it, and is released after; a
  Shift-drag claims the rectangle where a plain background drag does not; the
  host's camera is reinstalled every frame so a `SetCamera` or a `FitNow`
  between frames does not survive; a background drag and a wheel leave the
  guest's camera alone; and the legend's rows are published but never stamped.
  That the guest emits no canvas is asserted by counting messages through the
  scene channel: a hosted render must send strictly fewer than `Render` for
  the same declaration.
- **What would fail.** A wrong claim shows as the map panning while a node is
  dragged, which the scene test catches; a wrong camera shows as the graph
  sliding against the tiles while zooming, which the conversion test catches.

## Status

Proposed 2026-09-12, and awaiting review. Phases 1 and 2 of §SD7 are built
and are recorded above as they landed; phase 3 — the Go-side pick that
`layeredgraph/view` needs before it can be a guest — is not, and has no
consumer asking for it.

## References

- [ADR-0224](./0224-graphview-go-graph-widget-painter-lane.md) — the widget,
  its Go-side pick (§SD3), its camera (§SD4), the deferred O4, and §SD14's
  bounds readers.
- [ADR-0204](./0204-leaflet-map-core-port.md) — portolan, its overlay slot and
  its handler set.
- [ADR-0069](./0069-imzero2-layeredgraph-widget.md) — `layeredgraph/view` and
  its region-per-node picking.
- [ADR-0149](./0149-implot-core-port-painter-lane.md) — the painter-lane port
  doctrine and the M7 emission-order lesson §SD1 rests on.
- [graph-viewer-gap-analysis-cytoscape-ogma.md](../adr-background-work/graph-viewer-gap-analysis-cytoscape-ogma.md)
  §3.9 — the geo-mode reading this record answers.
