---
type: how-to
audience: engineer with a specific task
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** The recipes here are the ones the
> `graphonmap` gallery demo runs, and each number quoted was measured against
> the live APIs; the page itself has not been reviewed.

# How to draw a graph on a map

You have nodes with coordinates — sites, sensors, stations — and edges between
them, and you want the graph over a basemap with the map still panning and
zooming and the nodes still hoverable, clickable and draggable.

One canvas does it: [portolan](../../public/thestack/imzero2/egui2/widgets/portolan/)
owns it, [graphview](../../public/thestack/imzero2/egui2/widgets/graphview/)
paints and picks inside it
([ADR-0228](../adr/0228-hosted-canvas-rendering-and-graph-widget-sharing.md)).
Read [egui2_hl_graphonmap_demo.go](../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_graphonmap_demo.go)
beside this page; it is the whole thing working.

## 1 The frame, in three calls

A host applies input at the top of its frame and offers its paint slot at the
bottom, so the guest reads the pointer *before* the map does and paints *after*
it. That is why hosting is two calls and not one.

```go
canvas, area := m.Handles()
claim := gv.HostedInput(graphview.HostCanvas{
    Canvas: canvas, Area: area, W: w, H: h,
    Camera: m.View().CameraAt(refZoom, origin), // §2
})
m.SetPointerVeto(claim.Pointer)                 // the map stands down this frame

m.Render(w, h, func(p portolan.Projector) {
    gv.SetHostCamera(p.CameraAt(refZoom, origin)) // §4 — the view moved since
    gv.HostedPaint(nodes, edges)
})

for _, ev := range gv.Events() { /* as after Render */ }
```

`claim.Pointer` is the whole arbitration rule: **a gesture that starts on a
node is the graph's, everything else is the map's.** The map is told, not
asked, so it needs one boolean and no knowledge of its guest. It stops a new
drag, box zoom or double-click zoom; a gesture already in flight finishes, and
the wheel, the pinch and the keyboard stay the map's in every case.

The guest emits no canvas, no sense region and no background, so the graph over
the tiles is transparent by construction.

## 2 Pick the right world, and it is not always the same one

World units are the graph's coordinate space. There are two ways to set them up
and **the choice is not free**.

| | Identity camera over layer points | The map's camera at a fixed reference zoom |
| --- | --- | --- |
| world units | canvas pixels | projected pixels at `refZoom` |
| pin with | `p.ToCanvas(ll)`, every frame | `v.ProjectAt(ll, refZoom) - origin`, once |
| camera | `camera.Camera{Zoom: 1}` | `v.CameraAt(refZoom, origin)` |
| node `Radius` | already a screen size | divide by the camera's zoom |
| use when | **every node is located** | **a layout runs** |

Under the identity camera a layout solves in screen space while the geometry it
solves against rescales with the view: graphview's ideal edge length comes from
the canvas area and is a constant, where in the demo the Zürich–Bern separation
runs from 114 px at zoom 7 to 12,699 px at zoom 14. Unlocated nodes are yanked
about as you zoom. A world fixed at a reference zoom has one equilibrium, so the
layout settles once and a camera change does not even wake it.

**Measure the fixed world from a local origin.** Projected coordinates are large
absolute numbers — Zürich is 19,713 px from the antimeridian at zoom 7.2 — and
that costs twice: float32 spends its mantissa on the magnitude (about 2 px of
error at zoom 18, none from a local origin at any zoom), and graphview seats a
node with no placed neighbour in a box at the *world* origin, which under a
whole-world projection is in the Pacific. `CameraAt(refZoom, origin)` with
`origin := v.ProjectAt(somewhereNearTheData, refZoom)` fixes both.

## 3 Nodes the data never located

Declare the ones with coordinates `Pinned`; declare the rest with none. The
force step lays them out among the pinned ones, which ADR-0224 §SD10 gives for
free — a declared pin is fixed and everything else still feels it. Under the
fixed world their retained position is already geographic, so nothing needs
anchoring between frames.

Two knobs matter. `ForceParams.KScale` brings the ideal edge length, derived
from the canvas area, to the same order as the spread of the pinned nodes at
`refZoom` — 0.5 in the demo. And `PauseOnSettle` stops the step once it has
converged, which it can do because the world no longer changes with the view.

## 4 The camera the paint uses is not the camera the pick used

The map's handlers run at the top of its `Render`, so by the time your paint
slot runs its view has already moved. `SetHostCamera` with the *current*
camera, inside the slot, or the graph is a frame behind what the map drew and
slides against it under a pan. The pick keeps the older camera on purpose: it
belongs to the frame the pointer was over.

## 5 A basemap without a tile server

[`portolan/landoverlay`](../../public/thestack/imzero2/egui2/widgets/portolan/landoverlay/)
fills land and strokes country borders from the `worldmap` atlas through the
same projector, so a `NoTiles` map still shows geography:

```go
layer := &landoverlay.Layer{}          // keep it; it reuses its buffers
atlas, _ := worldmap.LoadAtlas()
// inside the overlay callback, before the graph:
layer.Draw(p, atlas, landoverlay.DefaultStyle())
```

The outlines are 110m Natural Earth: a basemap at country-and-continent zooms,
visibly a polygon past about zoom 8. `Style.NoFill` strokes them without
filling — a look of its own, and the first thing to try when a fill misbehaves,
since the fill and the stroke take different paths through the vector pipeline.

## 6 The aura legend must be external

A legend row stamped from the overlay slot sits under the host's own drag
region and can never be clicked. Set `AuraLegendExternal`, take the rows from
`gv.AuraLegendItems()`, and paint them in a canvas you own — a side panel — with
the shared `legend` package, toggling with `HideAura` / `ShowAura`
(ADR-0224 §SD15).

## 7 What bites

- **The graph slides against the tiles when you pan.** You did not call
  `SetHostCamera` in the paint slot (§4).
- **Unlocated nodes fly about as you zoom.** You are on the identity camera
  with a layout running (§2).
- **Nodes are the wrong size, and the error grows with zoom.** Under the fixed
  world `Radius` is a world unit; divide by the camera's zoom.
- **A node arrives from off-screen and creeps in.** Raw projected coordinates
  without a local origin (§2).
- **The map pans out from under a node drag.** You did not pass
  `claim.Pointer` to `SetPointerVeto`.
- **`FitNow` / `FitNodes` / `SetCamera` do nothing.** They are overruled every
  frame in hosted mode by design; move the *host's* view instead —
  `FitBounds` over the unprojected node box, from `gv.Bounds()` and
  `v.Unproject`.

## 8 Not covered

Clustering nodes by their distance on the map — Ogma's `addGeoClustering` — is
a distance predicate over whatever grouping stage `nav` grows, and `nav` has no
grouping yet ([the gap analysis](../adr-background-work/graph-viewer-gap-analysis-cytoscape-ogma.md)
§5 items 1–3). Nothing about it is geographic beyond the predicate, so it is
noted here and owned there.

## References

- [ADR-0228](../adr/0228-hosted-canvas-rendering-and-graph-widget-sharing.md) —
  the hosted-canvas seam, the claim protocol, and the world and camera choices
  of §2 and §4.
- [ADR-0224](../adr/0224-graphview-go-graph-widget-painter-lane.md) — the
  widget: pins (§SD10), the fade pair (§SD14), the aura legend (§SD15).
- [ADR-0204](../adr/0204-leaflet-map-core-port.md) — the map, its projector
  hook, and the overlay pipeline's one deliberate departure from Leaflet.
- The `graphonmap` demo in the widget gallery, which runs every recipe above.
