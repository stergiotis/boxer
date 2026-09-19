---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** One run on one machine that was not
> idle; §0 says what that is worth. Do not cite as authoritative.

# Flow particles, frame cost — a painter-lane trial

## 0 The claim, and how to cite it

**On one machine and one launch per cell, painting a flow layer's trails as
one `paintLine` opcode per segment costs the Go side about 1 µs per segment —
44 ms a frame at 5 000 particles (43 000 segments), more than a 30 Hz tick
allows — where one `paintSegments` for the layer costs 6 to 10 ms for the same
frame; the batched opcode was needed.** The per-line arm also writes 32 bytes
per segment across the FFFI boundary against 20, and its opcode dispatch in
the host is one and a third to nearly three times as long. These hold on both
hosts, because none of it is the host's rasteriser.

**On the CPU-rasterizer host, drawing the batch as one untextured mesh
rasterises in about a third of the time of the same segments as feathered
line shapes — 13.5 ms against 37.7 ms per frame at 5 000 particles, 49 ms
against 146 ms at 20 000; on the wgpu host the two do not differ** (5.2 ms
against 5.4 ms at 20 000). The mesh stays the default draw, and
`tessellated()` is for a GPU host that wants the antialiasing, where it is
free.

| At 5 000 particles, ~43 000 segments, medians per frame | `segments-mesh` | `segments-tessellated` | `line-per-segment` — the baseline decided against |
| --- | --- | --- | --- |
| Go side of the layer, cpu host / wgpu host | 6.2 ms / 10.4 ms | 7.3 ms / 9.1 ms | 43.8 ms / 44.3 ms |
| Bytes written, whole frame | 868 kB | 870 kB | 1 389 kB |
| Host opcode dispatch, whole frame, cpu / wgpu (floor 0.8 ms) | 5.6 ms / 9.0 ms | 5.3 ms / 6.8 ms | 8.6 ms / 10.2 ms |
| Rasterise, whole frame, **cpu host** (floor 3.1 ms) | 13.5 ms | 37.7 ms | 37.8 ms |
| Rasterise, whole frame, **wgpu host** (floor 3.0 ms) | 3.5 ms | 3.4 ms | 3.4 ms |

**What this trial does not say.**

- **Not "the flow layer costs 44 ms a frame."** That figure is the
  `line-per-segment` arm, a paint path that exists only so this trial could
  measure what the ADR decided against.
- **Not a frame rate.** The rows are different stages of one frame, some for
  the layer alone and some for the whole frame with the gallery around it;
  they were not measured as a sum and overlap in time across two processes.
  What they do support: on this machine the shipped arm's stages add to about
  25 ms at 5 000 particles on the cpu host and to about 50 ms at 10 000, so
  on a CPU rasteriser of this class a 30 Hz tick holds to roughly 5 000
  particles; on the wgpu host the Go side and the dispatch are the limit, not
  the GPU, and they pass 33 ms between 10 000 and 20 000.
- **Not a statement about tessellation cost.** The hosts' `tess_p50_us`
  column disagrees between the two hosts for identical work (3.0 ms against
  11.7 ms at 20 000 particles in the mesh arm) and is not monotonic in the
  count. It is in the results and is not used for any claim here.
- **Not the layer's own efficiency.** About 60 % of the Go side's time in
  every arm is FFFI slice marshalling, one element per write (see the
  logbook). A marshaller that writes a slice at once would move every Go-side
  figure in this table, the shipped arm's most.
- **Not the desktop host, and not a remote viewer.** Neither was measured.
- **Not reviewed, and replicated only in part.** One machine — a low-power
  APU — that was not idle; one launch per cell in the table above. A second
  run repeated five of the cells four times each: launches of one cell differ
  by about a tenth of the median, the CPU frequency governor makes no
  difference inside that, and both claims above held in every launch (the
  [logbook](./logbook.md) has the ranges). Differences between neighbouring
  counts of one arm are inside that spread — the wgpu mesh arm's Go side
  reads 10.4 ms at 5 000 particles in the table and 6.4 to 6.7 ms in the
  repeats. The claims rest on differences of three to seven times, not on
  those.
- **Not the dispatch figure at 20 000 particles.** It is bimodal between
  launches — about 12 ms or about 25 ms for the same cell — for a reason not
  found.

**If you need a number**, take it from the first run's
[results.tsv](./runs/2026-09-19-first-run/results.tsv) or the repeats'
[results.tsv](./runs/2026-09-19-governor/results.tsv), which hold raw
microseconds and bytes per cell. **No figure from this trial travels without
the pair of arms it compares and the host it was measured on.**

## 1 Question and scope

[ADR-0249](../../adr/0249-vector-fields-on-the-map-particles-over-a-batched-segment-opcode.md)
draws a vector field as particle trails and took two decisions on arithmetic
alone, leaving this trial (its M5) to measure them:

1. **Was a batched opcode needed?** The ADR added `paintSegments` — every
   trail of a layer in one opcode — because a `paintLine` per trail segment
   looked too costly at tens of thousands of segments a frame. Nothing had
   measured it.
2. **How should a host draw the batch?** As one untextured mesh, a quad per
   segment, which is what it does; or as a feathered epaint line shape per
   segment, which the opcode's `tessellated()` hint asks for. The ADR left the
   default to this trial.

The numbers lead here, which is the less usual weighting for this directory.
The toolbelt question is the side-product: what it took to measure a frame's
cost across the Go side, the FFFI boundary and two hosts with what the tree
already had, and what got in the way. Findings follow the
[directory convention](../README.md); runs append to the
[logbook](./logbook.md).

In scope: the per-frame cost of the flow layer against particle count, by
paint arm, on the CPU-rasterizer host (ADR-0205) and the wgpu headless host.
Out of scope: the desktop (eframe) host; the mesh lane to a remote viewer
(ADR-0128), whose cost the ADR records as unknown; the simulation's own
scaling beyond what the Go-side figure includes; any field other than the
analytic one.

## 2 The workload

The `flowbench` gallery demo
([egui2_hl_flowbench_demo.go](../../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_flowbench_demo.go)):
one `NoTiles` portolan map, 960 × 600, with one `flowoverlay` layer over the
analytic jet-and-vortices field (`vectorfield.Swirl`) served through the
in-memory pyramid at half a degree. Layer options are the defaults — a
12-tick trail, a 30 Hz tick — except the particle count, which the demo pins
through `MaxParticles` under a density no canvas reaches. The gallery window
shows the top of the demo, so part of the map is clipped; the clip is the
same in every cell.

A **cell** is one host, one arm, one particle count. The demo drops 90 frames
after a control changes, then records 300.

## 3 System under test and arms

| Arm | What it paints | What it is |
| --- | --- | --- |
| `segments-mesh` | one `paintSegments`, drawn as one untextured mesh | the system as shipped |
| `segments-tessellated` | one `paintSegments` with the `tessellated()` hint: an epaint line shape per segment | the alternative host draw |
| `line-per-segment` | one `paintLine` opcode per trail segment (`Options.LinePerSegment`) | **the baseline the ADR decided against** — it exists for this trial and is not a configuration anyone should run |
| *floor* | zero particles: the same frame with no layer on it | what to subtract from whole-frame figures |

Hosts: `cpu` — `headless_soft`, the CPU rasterizer; `wgpu` — `headless_wgpu`.

Standing hypotheses, written before the first run: (H1) `line-per-segment`
costs several times `segments-mesh` on the Go side and in dispatch, growing
with the count; (H2) `segments-tessellated` costs more than `segments-mesh` in
tessellation on both hosts and more in rasterisation on `cpu`, because a
feathered line is about three times the triangles.

## 4 Method

- **One launch per cell.** The hosts' raster statistics are cumulative over a
  launch, so a cell that shared a launch with another would share its
  percentiles. The demo starts at zero particles for the same reason: frames
  of a configuration nobody asked for would be in the statistics.
- **What is recorded per cell**, by
  [measure.sh](./measure.sh), into `results.tsv` with the raw lines beside it:
  - `go_draw_*` — wall time around `Layer.Draw` on the frame goroutine: the
    simulation's ticks, building the batch, encoding it. The layer alone.
  - `interpret_*` — the host's dispatch of the frame's opcodes, from the frame
    metrics (ADR-0062). **The whole frame**, gallery chrome included.
  - `written_p50_bytes` — bytes written across the FFFI boundary. The whole
    frame.
  - `raster_*`, `tess_p50_us` — the host's tessellation and rasterisation per
    frame (`IMZERO2_HEADLESS_RASTER_STATS`), and `tris`, the triangles of the
    last sampled frame. The whole frame, **and the whole launch**: the
    percentiles include the seconds before the demo was opened. With 390
    frames of the cell against a few dozen before it, the median is the
    cell's; the 90th percentile is less clean and the mean is not used.
- **Percentiles, not means.** A frame's cost is bounded below by its work and
  unbounded above by whatever else the machine did.
- **Environment.** Both hosts built from the tree under test; an otherwise
  idle machine. Hardware, toolchains and what else was running go in the
  logbook entry — never hostnames or paths.
- **Idiom rule.** The cells run through the scene runner (ADR-0248) and the
  demo's own controls. No environment variable, flag or code path exists for
  the trial beyond `Options.LinePerSegment` and the `flowbench` demo.
- **Numbers are data.** `results.tsv` holds raw microseconds and bytes and no
  ratio. This trial's results are a few dozen rows of frame timings about the
  renderer itself, not domain facts; they stay in the run directory and are
  not loaded into the facts store.

## 5 Findings ledger

Pre-registered candidates: the scene runner having no way to hand a value it
read back to the caller; FFFI slice marshalling cost; the hosts' raster
statistics being cumulative per launch.

Findings are in the [logbook](./logbook.md).

## 6 Milestone cut

- **M0 — one run, one machine.** Both hosts, three arms, five counts, one
  launch per cell. Gate: §0 can state, with its conditions, whether the
  batched opcode was needed and which host draw to default to.
- **M1 — repeats.** `REPEATS=5` on an idle machine, so that a cell has a
  spread and not a single launch. Descope-able while M0's differences are
  large against any plausible noise.
- **M2 — the mesh lane.** Bytes per frame to a remote viewer, which the ADR
  lists as unknown.

## 7 Open questions

1. Whether the Go side's figure is dominated by the layer or by FFFI slice
   marshalling (see the logbook's first entry), and so how much of it a
   marshaller that writes a slice in one call would remove for every arm.
2. Whether the desktop host differs from the headless wgpu host enough to
   matter; it shares the tessellator and the renderer.

Related: [ADR-0249](../../adr/0249-vector-fields-on-the-map-particles-over-a-batched-segment-opcode.md),
[ADR-0205](../../adr/0205-imzero2-cpu-rasterized-pixel-host.md),
[ADR-0248](../../adr/0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md),
[the survey behind ADR-0249](../../adr-background-work/vector-field-flow-visualization-survey.md).
