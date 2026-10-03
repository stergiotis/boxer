---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

# Portolan land, frame cost — a painter-lane trial

## 0 The claim, and how to cite it

**On one machine (2026-10-03), a frame of the landoverlay at world view
(`world-z0`, four copies of the 110m atlas, `fill` as shipped) cost the Go
side about 7 ms and the host about 7 ms more of its own work, and most of
both was work that does not change between frames:** on the Go side the
Mercator projection of every vertex and the scratch slices around it (half
and a fifth of the layer's samples), on the host the ear-clip triangulation
of every concave ring, four times per ring at zoom 0 (about two thirds of the
host's dispatch CPU in a sampling profile). The FFFI encoding and the pipe
were about 5 % of the frame; batching the per-ring messages is not where the
cost is. The cost falls with the zoom: about 1 ms in total at `alps-z5`.

**Projecting the atlas once into the CRS's plane and reusing the overlay's
scratch (the Go-only fix that followed) took the Go side of `fill` from
9.1 to 2.2 ms at `world-z0` and 3.7 to 0.6 ms at `europe-z2.6`, medians of
five interleaved benchmark rounds, with a byte-identical paint stream**
(`TestPlanePathPaintsWhatTheDegreePathPaints`). The host's triangulation is
untouched by it, and in the host that is what a filled frame then waits on:
at `world-z0/fill` the frame's interpret time stayed at about 14.7 ms while
`world-z0/nofill`, which the Go side bounds, fell from 9.6 to 3.6 ms (one
launch per cell after the fix, cpu host — see the logbook).

**What this trial does not say.**

- **Not a frame rate.** The `interpret_*` column is the host's wall time
  for the frame's opcodes, which includes waiting on the pipe while the Go
  side is still producing them; at `world-z0/nofill` it equals the Go draw
  time while the host's profile shows little work. The host's own cost
  above is `interpret − go_draw − off`, an estimate that assumes the two
  sides never overlap.
- **Not a rasterisation figure.** The headless raster statistics do not see
  the map: their triangle count at `world-z0/fill` equals the `off` arm's,
  and rasterise is flat across arms. They are in the results and unused.
- **Not an idle machine.** Load average ranged from about 2 to over 20
  during the runs, other sessions compiling and testing. A cell's three
  launches differ by up to a third; the claims rest on differences of three
  times and more, which held in every launch and on both hosts.
- **Not the scene harness's overhead.** The scene carrier hashes every
  frame (BLAKE3, 11–30 % of the client's samples) and an AV1 encoder runs
  beside it; both are in every whole-frame figure and neither is the
  layer's.

**If you need a number**, take it from a run's `results.tsv` — the
[repeats](./runs/2026-10-03-repeats/results.tsv) for the cells, the
[profiles](./runs/2026-10-03-profiles/host-client-world-z0-fill.perf.txt) for where a frame goes — with its
view, arm and host.

## 1 Question and scope

The landoverlay draws static vector geometry — the Natural Earth 110m
admin-0 outlines of the worldmap atlas — on every frame of a portolan map,
in immediate mode: every ring is projected, clipped and encoded, one paint
opcode per ring, and the host decodes and, for a concave ring, triangulates
it again. Nothing of it survives a frame on either side of the FFFI
boundary. A design that would let it survive — a retained paint list the
host replays under a transform — is under discussion, and this trial is its
evidence. It asks:

1. **Where does a frame of static vector geometry spend its time?** Go side
   (cull, projection, clip, encoding), the host's dispatch (decoding, the
   concave triangulation), tessellation, rasterisation — per view.
2. **What part of that is invariant across frames** and so removable by
   retaining it: what a retained list could save, against what batching the
   per-ring messages could.

The numbers lead; the toolbelt question is the side-product.

In scope: the landoverlay over a `NoTiles` map, four views, on the
CPU-rasterizer host (ADR-0205) and the wgpu headless host. Out of scope: the
desktop host, the mesh lane to a remote viewer (ADR-0128), raster tiles,
any other overlay.

## 2 The workload

The `landbench` gallery demo
([egui2_hl_landbench_demo.go](../../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_landbench_demo.go)):
one `NoTiles` portolan map, 960 × 600, with one `landoverlay.Layer` over the
vendored atlas in `DefaultStyle`, at one of four static views:

| View | Centre | Zoom | What it exercises |
| --- | --- | --- | --- |
| `world-z0` | 20° N, 0° | 0 | the whole atlas, and the copies a wide pane shows of a 256 px world |
| `europe-z2.6` | 35° N, 5° E | 2.6 | a continent, most rings whole |
| `alps-z5` | 46.5° N, 9° E | 5 | a few dozen countries, most of each ring off screen |
| `swiss-z8` | 46.8° N, 8.2° E | 8 | a handful of countries, past where 110m geometry is useful |

A **cell** is one host, one arm, one view. The demo drops 90 frames after a
control changes, then records 300, asking for a frame every 1/30 s.

## 3 System under test and arms

| Arm | What it paints |
| --- | --- |
| `fill` | outer rings as `paintPolygonFilled` with a stroke — concave, so triangulated by the host — and holes as polylines: **the system as shipped** |
| `nofill` | every ring as `paintPolyline` (`Style.NoFill`): the same geometry with no fill, so no triangulation; clipped and simplified in Go |
| `off` | the same frame with no layer: the floor to subtract from whole-frame figures |

Hosts: `cpu` — `headless_soft`; `wgpu` — `headless_wgpu`.

Hypotheses, written before the first run:

- **H1** — The Go side is dominated by work that does not change between
  frames of a static view: the Mercator projection and the allocation around
  it, not the FFFI encoding.
- **H2** — On the host, the `fill` arm's dispatch exceeds `nofill`'s by the
  concave triangulation, which is the largest host-side cost of the layer at
  `world-z0`.
- **H3** — Rasterisation is not the limit on the wgpu host at any view.

## 4 Method

Three instruments, each answering what the others cannot:

- **The Go benchmark**, `BenchmarkPaint` in
  [landoverlay](../../../public/thestack/imzero2/egui2/widgets/portolan/landoverlay/bench_test.go):
  the layer's frame against a channel that counts messages and bytes and
  discards them, per view and arm, with CPU and allocation profiles. No host.
- **The in-host cells**, by [measure.sh](./measure.sh), flowbench's method:
  one launch per cell through the scene runner; `go_draw_*` timed around
  `Layer.Paint`; `interpret_*` and `written_p50_bytes` from the frame
  metrics (ADR-0062), whole frame; `raster_*`, `tess_p50_us`, `tris` from
  `IMZERO2_HEADLESS_RASTER_STATS`, whole launch.
- **Sampling profiles of the host process**, for what `interpret_*` lumps
  together: `perf record` attached to the cpu host's client during a cell,
  built with line tables and frame pointers into a target directory of its
  own so the measured binaries stay the release build.

Percentiles, not means. One launch per cell. Environment in each run's
`env.txt`; hardware without hostnames or paths.

## 5 Findings ledger

In the [logbook](./logbook.md).

## 6 Milestone cut

- **M0 — one run, one machine.** Both hosts, three arms, four views, one
  launch per cell; the Go benchmark with profiles; one host profile of the
  `fill` arm at `world-z0`. Gate: §0 can say where a frame goes per view and
  what part of it is invariant.
- **M1 — repeats** on an idle machine.
- **M2 — a pan.** The same views with the camera moving every frame, which a
  retained design must be measured under.

Related: [ADR-0149](../../adr/0149-implot-core-port-painter-lane.md),
[ADR-0204](../../adr/0204-leaflet-map-core-port.md),
[ADR-0012](../../adr/0012-imzero2-collapsible-retained-bodies.md),
[the flow-particles trial](../flow-particles-frame-cost/README.md), whose
harness this one copies.
