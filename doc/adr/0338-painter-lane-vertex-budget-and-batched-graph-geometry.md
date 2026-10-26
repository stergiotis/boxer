---
type: adr
status: accepted
date: 2026-10-13
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-13
---

# ADR-0338: A vertex budget for the painter lane, and graph geometry sent as batches

## Context

A frame's tessellated geometry reaches the GPU through egui-wgpu, which keeps
every vertex of the frame in one buffer and grows that buffer by doubling.
wgpu refuses a buffer larger than the device's `max_buffer_size` with a
validation error, and treats the error as fatal. A frame with enough geometry
therefore does not draw slowly — it ends the host process. Because the buffer
doubles from its previous size, the request can cross the limit while the
frame itself needs only a little more than half of it.

Nothing upstream bounds a frame's geometry. The painter lane (ADR-0149) turns
one Go opcode into as many epaint shapes as the caller asks for, and a widget
fed by a query has no natural ceiling. The case that surfaced it was the play
Graphview tab (ADR-0227) drawing a located graph inside a portolan map
(ADR-0228): one feathered disc per node, one hairline ring per pinned node,
one line and one polygon per directed edge. On one machine (2026-10-13, an
informal sweep, not a trial) a graph of 50 000 nodes and 80 000 edges drew,
and one of 100 000 and 160 000 asked for a 304 MB vertex buffer against a
256 MiB limit and the host exited. Long before that the frame was slow: the
host's tessellation of the per-shape geometry was the largest single cost of
the frame at every size measured.

Two questions follow. How should the host keep any frame — whatever widgets
drew it, a map and the graph it hosts together — under the device's limit?
And how should a widget that draws many small shapes spend fewer vertices,
and degrade when even that is too many?

## Decision

### SD1 — The interpreter holds the painter lane to a vertex budget

Every `PaintCmd` is estimated before it is realised, against a per-frame
budget of vertices. A command that would take the frame past the budget is
not drawn: a batch command (markers, segments, triangles, rects) is cut to the
items that fit, and any other command is skipped whole. Clip bookkeeping and
sense regions cost nothing and are never held back. Each cut or skip counts
once in the frame's skip count.

The budget is a quarter less than half the device's `max_buffer_size`,
expressed in vertices. Half, because a doubling growth from just under half
overshoots; a quarter less, for the geometry egui draws itself — text,
frames, widgets — which the estimate does not see. The estimate follows
epaint's tessellation closely enough to bound it, and is not exact.

A capture replay (ADR-0281 §SD5) is not held to the budget: it rasterises on
the CPU, apart from the GPU frame.

### SD2 — The host asks the adapter for its largest buffer

The wgpu hosts — desktop and headless — request the adapter's own
`max_buffer_size` instead of wgpu's 256 MiB default, and size the budget
from the device they get. A host without a wgpu device (the CPU rasteriser,
the browser tab) keeps the budget wgpu's default limit implies, which every
device meets.

### SD3 — Go reads the budget back

A fetcher, `fetchPaintBudget`, reports the previous frame's estimated
vertices, the budget and the skip count, with the one-frame lag of
`fetchFrameMetrics`. The state manager records it in the frame metrics. The
runtime status line shows a warning while frames are being cut, so missing
geometry is visible as missing rather than read as data.

### SD4 — Batched opcodes for many small shapes

Two additions to the painter lane let a widget send a layer of small shapes
as one opcode the host draws as one untextured mesh:

- `paintMarkers(...).mesh()` draws a circle batch as one mesh: a fan per disc
  with a segment count that follows the radius, and `outline(col, width)`
  as a ring around it. Only the outermost edge is feathered, over one pixel.
  A transparent fill draws the ring alone, so a batch of ring markers costs
  no fans.
- `paintTriangles(xs, ys, cols)` draws one filled triangle per index with a
  colour each, unfeathered — a layer of arrow heads in one opcode.

They follow ADR-0249 §SD5's `paintSegments`: the opcode promises the
geometry, not feathered antialiasing, and the mesh is the default.

### SD5 — Graphview sends its geometry as batches and culls what is off the canvas

Graphview draws straight edges as one `paintSegments` per stroke width, with
widths quantised to a quarter pixel so continuously weighted edges still share
a few batches; arrow heads as one `paintTriangles`; node discs as one mesh
marker batch per colour and radius; pinned rings as one ring-marker batch per
radius. Curves, loops, and edges the selection or hover recolours or that
carry a label are drawn on their own, over the batches. An edge whose
endpoints' box, widened by its stroke, head, bulge or loop, misses the canvas
is not sent, and neither is a node whose disc misses it.

### SD6 — Graphview degrades by decoration before data

Graphview holds its own geometry to a vertex budget: `Options.VertexBudget`,
or by default a share of the host's budget from SD3. Past it the picture
drops, in order, the pinned rings, then the arrow heads, then draws a 1-in-k
sample of the edges, k chosen to fit. The sample is a function of the edge
index, so the same edges stay drawn from frame to frame. `PaintStats` reports
what was drawn, culled and dropped, and play's status line names it.

The share shrinks at once while the host reports frames past its budget
(SD3), and recovers slowly once it does not. That is how a graph hosted in a
map — where the map's own layers share the frame and the graph cannot see
their cost — settles under the host's line instead of leaving SD1 to cut the
map's or the graph's geometry at an arbitrary point.

## Alternatives

- **Only raise the device limit (SD2 alone).** Moves the ceiling, does not
  remove it; a WebGL2 tab cannot raise it at all. Kept as a complement.
- **A budget in each widget only.** A widget cannot see what else is on the
  frame — a hosted graph does not know the map's cost — and every widget
  would have to carry one. Kept as SD6 for the graceful half; the safety net
  is the host's.
- **Guard between tessellation and upload.** The headless host could drop
  primitives after tessellating; the desktop host cannot, because eframe
  tessellates and uploads internally. Estimating per `PaintCmd` works on
  every host.
- **Thin nodes as well as edges.** Dropping data points is the last thing a
  reader should lose; SD6 leaves the nodes to SD1's cut, which the status
  line reports.

## Consequences

### Positive

- A frame past the device's buffer limit draws with geometry missing and
  says so, instead of ending the host.
- A graph's frame costs a few opcodes per layer rather than one or two per
  node and per edge, and the host tessellates meshes it builds directly
  rather than one feathered path per shape.

### Negative

- Mesh discs, segments and triangles are not feathered except on a disc's
  outer edge: arrow heads and edges are aliased where epaint's shapes were
  not.
- The estimate is approximate. An estimate below the real cost lets a frame
  cross the half-buffer line; the margin in SD1 is what absorbs it.
- A coarse picture is a different picture. A thinned edge set does not show
  every edge, and the status line is the only place that says so.

### Neutral

- Drawing order within the graph changes: edges in batches, then nodes, then
  rings, with recoloured and labelled edges over the batches rather than in
  declaration order among the others.

## Status

Accepted 2026-10-13.

## References

- ADR-0149 — the painter lane.
- ADR-0224 — the Graphview widget.
- ADR-0227 — the play Graphview tab.
- ADR-0228 — hosted canvas rendering (a graph inside a portolan map).
- ADR-0249 §SD5 — `paintSegments`, the batched-mesh precedent.
- ADR-0281 §SD5 — capture replay.

## Updates

### 2026-10-26 — renumbered from 0329 to 0338

This record and the imztop operations catalog were both filed as 0329 on
2026-10-13. The operations catalog was filed first and keeps the number; this
record takes the next free one. Citations of `ADR-0329` that concern the
painter lane's vertex budget or batched graph geometry, in code and in docs
committed before this date, mean this record.
