---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-09-11 as the substrate
> for [ADR-0224 §SD11](../adr/0224-graphview-go-graph-widget-painter-lane.md)
> (graphview auras). Nothing here is a decision. Provenance: no library source
> was read or decompiled. The inputs were (a) the vendor's public settings
> reference for `auras` and `auras.style`, (b) the published example's own
> configuration and data, and (c) controlled renders driven through the
> vendor's documentation page, which hosts the library under its own licence
> and offers an "edit and run" tryout. Renders used a static layout,
> transparent nodes, no shadows and opaque aura fills, and the drawn pixels
> were measured. Every number below is a measurement in device pixels at zoom
> 1 unless marked "model", from one browser on one machine on the compile
> date. The constants are observations about the reference, not values the
> port must adopt.

# ZoomCharts NetChart "auras" — behavioural analysis

## 1 Question and scope

NetChart draws a translucent, blob-shaped "aura" around every group of nodes
that share an aura id, with a legend whose entries toggle the group. The
question was how the aura shape is computed, precisely enough to re-derive
the behaviour for the graphview widget without reading the vendor's code.
Legend behaviour is taken from the reference documentation and was not
measured.

## 2 Per-node kernel

Each node emanates a radially symmetric scalar field that is 1 near the
centre and falls linearly to 0. With node screen radius `r` and the
`intensity` setting `i` (default 6):

    r' = r + c            c ≈ 5 px, a screen-pixel constant not scaled by zoom
    d0 = r' / 2           plateau: f = 1 for d ≤ d0
    R  = i · r'           f = 0 for d ≥ R
    f(d) = (R − d) / (R − d0)   for d0 < d < R

Evidence, r = 30, i = 6, cell size 1 (model: d0 = 17.6, R = 211.2):

| drawLimit | measured radius | model |
|---|---|---|
| 0.999 | 17.9 | 17.8 |
| 0.9 | 36.5 | 37.0 |
| 0.8 | 56.5 | 56.3 |
| 0.5 | 114.5 | 114.4 |
| 0.2 | 172.5 | 172.5 |
| 0.05 | 201.5 | 201.5 |
| 0.005 | 210.5 | 210.2 |

Scaling checks: R / i ≈ 35.2, 20.2 and 65.2 for r = 30, 15 and 60 (so c ≈
5.2); i = 3 and 12 scale R proportionally; zoom 0.5 and 2 reproduce the
r = 15 and r = 60 results, so the kernel works on the node's on-screen radius
plus a constant pad. The node's line width and shadow do not enter. The
plateau-plus-linear-ramp profile is what a two-stop radial gradient produces.

## 3 Accumulation within an aura

Fields of nodes in the same aura combine as a complementary product
("screen" compositing), not as a sum and not as a maximum:

    F_k(p) = 1 − Π_{n ∈ aura k} (1 − f_n(p))

Evidence: two r = 30 nodes at drawLimit 0.8. A union of the single-node
discs would separate past 113 px; the pair stayed connected at 200 px and
separated by 250 px (the screen model separates between 222 and 250; a sum
would stay connected past 250). Bridge half-widths at the midpoint measured
91.5 / 72.5 / 28.5 px for separations 100 / 150 / 200; the screen model gives
91.4 / 72.3 / 29.3. A node listed in several auras contributes its kernel to
each of them.

## 4 Grid, threshold, ownership

- The field is evaluated on a grid of `cellSize` screen pixels anchored to
  the canvas origin and sampled at cell centres (drawn extents fall at
  cellSize/2 modulo cellSize for cell sizes 10, 20 and 40).
- A cell belongs to aura k when F_k ≥ `drawLimit` (default 0.8).
- `overlap = true`: every aura keeps all its cells; shapes are drawn
  independently and blend by alpha, larger `zIndex` on top.
- `overlap = false`: a cell goes to the single aura with the largest F_k. At
  every measured boundary — unequal radii, three nodes against one, three
  auras — the two competing accumulated fields agreed within 0.003. Exact
  ties (a node in two auras, coincident nodes) go to the lexicographically
  smallest aura id regardless of node order, style order or zIndex.

An earlier reading of the same renders suggested a non-local rule; it was an
artefact of the chart recentring its view on the node bounding box when the
radii differ. Positions taken from the chart's own node-dimension query
removed it.

## 5 Outline and paint

- The outline is a closed curve through the centres of the boundary cells,
  smoothed. At cell size 1 a lone node yields a circle; at cell size 10 (the
  example's setting) the same node yields a rounded square about 100 px wide
  where the iso-contour is a 113 px circle; at cell size 40 the shape follows
  the handful of included cells. No interpolation along cell edges is done.
- The shape is filled with the aura's fill colour, stroked with its line
  style (a 6 px stroke centred on the outline widened a 113 px shape to
  119 px) and given a canvas shadow, which is the soft glow in the example.
- Auras are drawn beneath nodes and links, ordered by zIndex.

## 6 Legend (reference documentation, not measured)

Legend items are the auras with `showInLegend`; clicking one hides the nodes
of that aura, after which the field is recomputed from the remaining visible
nodes. A legend group id changes the toggle semantics to exclusive.

## 7 What a re-derivation needs

1. One alpha grid per aura over the viewport at the cell size.
2. Per visible node of aura k: a radial ramp, 1 inside an inner radius,
   linear to 0 at the outer radius, accumulated as a = a + f − a·f.
3. Mask_k = grid ≥ drawLimit; without overlap, keep only cells where k is
   the argmax over auras, ties to the smallest id.
4. Trace the mask boundary into closed polygons, smooth, fill and stroke per
   style in zIndex order, before nodes.

Where the port departs from this — interpolated contours instead of
cell-centre outlines, its own constants — is recorded in the ADR, not here.
