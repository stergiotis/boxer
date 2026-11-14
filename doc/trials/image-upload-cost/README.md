---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

# Image upload cost — a texture-path trial

## 0 The claim, and how to cite it

**On one machine (2026-11-14), with the host reading the pixel slice in
blocks, replacing an RGBA8 image through `paintImage` on every frame as 512 px
tiles cost 5.6 ms per cycle for 4 MiB, 17 ms for 16 MiB and 58 ms for 64 MiB
on the wgpu headless host, and 7.9, 22 and 64 ms on the CPU rasteriser
(medians of three launches per cell): about 0.9 to 2 ms per MiB.** One
texture of the same size cost more at every size — 6.9, 23 and 136 ms on
wgpu, 10.5, 33 and 171 ms on cpu — and 1024 tiles of 256 px in a frame cost
no more than 256 tiles of 512 px did (one launch each). A texture that is resident and not replaced
costs nothing measurable per frame at any side up to 8192. The first upload
of a texture, in one frame: about 40 ms for 2048², 160–220 ms for 4096²,
0.55–0.6 s for 8192² (one launch each).

**The block read is what changed, and this trial is its before and after.**
With the host reading the slice one element at a time, as it did when the
trial started, the same tiled cells took 9.4, 33 and 123 ms on wgpu and 9.9,
34 and 128 ms on cpu, and 48–61 % of the client process's user-space samples
were that read (`read_plain_u32h`); with the block read it is 4–7 %, and what
is left is copies — 25–33 % the conversion to `Color32`, 19–35 % the mesh
lane's own copy of the texture, which exists because a carrier is attached,
9–21 % the software rasteriser's copy and redraw (cpu host), 8–11 % the wgpu
texture write (wgpu host).

**Turning samples into those pixels in Go is small beside the upload**: for a
3840 × 2160 viewport (33 MiB of RGBA), a 16-bit lookup took about 3.5 ms on
one core and 0.6 ms on sixteen goroutines, and a float32 window with a table
lookup about 20 ms and 2.2 ms (medians of five benchmark runs, plain loops,
synthetic sources).

**The CPU rasteriser used not to redraw a texture replaced in place under an
otherwise unchanged frame, and now does.** Before the change the frame showed
the old pixels for the 12 s the check waited and the new ones as soon as the
image was moved by one point
([before](./runs/2026-11-14-first-run/still-flip-cpu.txt)); with the
texture's revision in the rasteriser's cache key it shows them at once
([after](./runs/2026-11-14-after-host-fixes/still-flip-cpu.txt)). The wgpu
host always did.

**What this trial does not say.**

- **Not a desktop figure and not a browser-tab figure.** Both hosts are
  headless and ran under the scene runner, whose carrier hashes every frame
  and keeps the mesh lane's texture store; the desktop host has neither, and
  in a tab the pixels cross two wasm modules and a mesh-lane message instead of
  a pipe. Neither was measured.
- **Not the cost of a GPU texture upload.** The wgpu host's
  `Queue::write_texture` is 8–11 % of its client's user-space samples after
  the change and was 4–5 % before. A reader quoting a per-MiB figure from
  here as what the GPU costs has the wrong stage.
- **Not a like-for-like comparison of the two hosts.** Before the change the
  cpu host did not redraw the replaced texture, so its cells measured
  receiving and storing pixels only and the two hosts agreed; after it, the
  cpu host rasterises what it receives and is the dearer of the two. Its
  "before" figures are therefore low by the redraw it skipped.
- **Not a whole profile.** `perf` sampled user space only
  (`perf_event_paranoid=2`). The kernel's share of the client's CPU time over
  the profiled windows, after the change, was 20–23 % for the tiled cells,
  about 35 % for one 16 MiB texture and 57 % for one 64 MiB texture, where
  every frame allocates and frees buffers of that size; that share is absent
  from the percentages above, and it is most of why one texture costs more
  than tiles.
- **Not a frame rate below 4 ms.** The cycle is floored by the host's
  cadence, 240 Hz here; every cell of 1 MiB sits on that floor, so the trial
  resolves nothing smaller.
- **Not a tuned mapping.** The Go kernels are the plainest loops that do the
  job, the float source is uniform noise of which a fifth clips, and an
  `asinh` evaluated per pixel with `math.Asinh` (133 ms on one core for the
  viewport) is in the results as the direct way, not the recommended one.
- **Not an idle machine, and one machine.** A 16-core desktop-class APU under
  the `powersave` governor, with other sessions building and testing: load
  average 2–7 during the runs before the change and 2.5–9 during those
  after. Of the twelve cells launched three times, the launches agreed within
  9 % in nine cells before the change and within 17 % in eight after it; in
  each set two cells had one launch about 50 % slower than the other two.
  The full matrices are one launch per cell. Not reviewed, not replicated.

**If you need a number**, take it from a run's `results.tsv` with its host,
arm and side: the cells launched three times
[after](./runs/2026-11-14-after-host-fixes-repeats/results.tsv) and
[before](./runs/2026-11-14-repeats/results.tsv) the change, the whole matrix
[after](./runs/2026-11-14-after-host-fixes/results.tsv) and
[before](./runs/2026-11-14-first-run/results.tsv); the mapping figures from
[map-bench.txt](./runs/2026-11-14-first-run/map-bench.txt); where a cycle
goes from the profiles
[after](./runs/2026-11-14-after-host-fixes-profiles/) and
[before](./runs/2026-11-14-profiles/). **No figure from this trial travels
without its arm, its host, and which side of the change it was measured on.**

## 1 Question and scope

An image viewer that keeps its samples in Go and maps them to RGBA8 there
changes what is on screen by shipping pixels: a new window or colour map is a
new texture. The only path for that is the `image` / `paintImage` opcode,
whose pixel slice travels in-band inside the frame
([the IDL](../../../public/thestack/imzero2/egui2/definition/egui2_definition_d_painter.go)),
and the tree held no measurement of it. This trial asks:

1. **What does replacing an image cost, per frame, by size** — on the Go
   side, on the pipe, in the host — and what is the most that can be replaced
   at an interactive rate?
2. **Does the same area cost more as tiles than as one texture**, and how
   many tiles can a frame carry?
3. **Where does the cycle go**, and which part of it is the path's design and
   which an accident of its implementation?
4. **What does the Go side's mapping add** to an upload it has to precede?

The numbers lead; the toolbelt question is the side-product. Runs append to
the [logbook](./logbook.md).

In scope: `paintImage` on the CPU-rasterizer host (ADR-0205) and the wgpu
headless host, five sides from 512 to 8192, the Go mapping kernels natively.
Out of scope: the desktop host, the browser tab, the `image` widget opcode
(same cache and upload code, a different draw), `scrollingTexture`, decoding,
and any change to the path.

## 2 The workload

The `imagebench` gallery demo
([egui2_hl_imagebench_demo.go](../../../public/thestack/imzero2/egui2/demo/apps/widgets/egui2_hl_imagebench_demo.go)):
one 512 × 512 canvas and an RGBA8 image of side 512, 1024, 2048, 4096 or 8192
drawn into it — 1, 4, 16, 64 or 256 MiB of pixels. The canvas is the same in
every cell, so what the host draws covers the same area whatever the image's
side. Two buffers of the cell's size hold two patterns and an upload
alternates between them, so consecutive versions differ in content.

A **cell** is one host, one arm, one side, in a launch of its own. The demo
asks for a repaint every frame, drops a warm-up and records a window whose
length falls with the side (120 frames at 512 and 1024, 60, 30, and 12 at
8192). The frame after a cell is selected builds the buffers and uploads
nothing, so the first upload is a frame by itself and is reported separately.

The Go mapping kernels
([harness/map_test.go](./harness/map_test.go)) turn a 512² tile and a
3840 × 2160 viewport of 8-bit, 16-bit and float32 samples into RGBA8: a table
lookup for the integer types; for float32 a window onto a palette, the same
through a 4096-entry table that holds the transfer function, and an `asinh`
per pixel; on 1, 4 and 16 goroutines.

## 3 System under test and arms

| Arm | What a frame ships |
| --- | --- |
| `reupload` | the whole image as one texture, a new version every frame: **what a display-recipe change on a resident image costs with no tiling** |
| `tiles512`, `tiles256` | the same pixels as a grid of 512 px or 256 px textures, every one a new version every frame |
| `resident` | the image once, then an empty slice: the frame between two changes |
| `off` | the canvas with no image: the floor |
| `still` | the image once, and a second pattern to the same texture id 150 frames after `flip`, with nothing else in the frame changing — **a correctness check, not a measurement**; `nudge` then moves the image by one point |

Hosts: `cpu` is `headless_soft`, `wgpu` is `headless_wgpu`, both built from
the tree under test and both at 240 Hz, the host's maximum cadence.

Standing hypotheses, written before the first run: H1 the cost is linear in
the payload; H2 the two hosts differ, the wgpu host paying a texture write the
cpu host does not; H3 tiles cost more than one texture, by a per-texture
overhead; H4 a resident texture is free; H5 a texture replaced in place under
an unchanged frame goes stale on the cpu host (ADR-0205 records the hazard as
unobserved).

## 4 Method

- **Environment.** Recorded per run in `environment.txt`: CPU, memory, kernel,
  governor, toolchains, the pinned egui and wgpu, the commit, load average at
  start and end.
- **Per cell**, from the demo's labels: `emit` (the Go side of the image
  alone — the `paintImage` calls, which include writing the slice to the pipe
  and so wait on the host reading it), and for the whole frame `render`,
  `sync`, `total`, the host's `interpret` time and the bytes written, all of
  which describe the frame before the one that records them; `period`, the
  wall time between two entries of the demo, which with a repaint every frame
  is the whole cycle; and the first upload's `total`, `interpret` and
  `period`. From the host's log: its raster statistics.
- **`period` is the figure for "what a frame costs".** `interpret` is wall
  time that includes the wait on the pipe and overlaps `emit`; the stages do
  not add up, and the profiles, not the columns, say where time goes.
- **Runner.** `measure.sh` builds the scene runner once per invocation and
  resumes a partial run; `profile-cell.sh` profiles one cell on a profiling
  build of a host; `still_flip_check.sh` runs the `still` scene and reads the
  host's dumps of live frames.
- **Reporting.** Raw per-cell output under `runs/<slug>/raw/`, one row per
  launch in `results.tsv`. The tables are small and stay in the run
  directories; nothing is restated here beyond §0.

## 5 Findings ledger

Findings follow the [directory convention](../README.md). Pre-registered
candidates: a size limit somewhere on the way to 256 MiB in one frame; the
element-wise read on the host; the copies around an upload; the staleness of
H5. The first did not occur — 8192² crossed in one frame on both hosts.

## 6 Milestone cut

- **M0 — the two headless hosts and the Go kernels.** Done 2026-11-14.
- **M3 — after a change to the path.** Done 2026-11-14 for two changes to the
  host: the slice read in blocks, and the texture's revision in the cpu
  rasteriser's cache key. Any later change that moves the figures reruns the
  matrix the same way.
- **M1 — the browser tab.** The same cells with Go and the host as wasm
  modules in one worker; the harness of the
  [wasm frame-cost trial](../keelson-wasm-frame-cost/README.md) is the
  starting point.
- **M2 — the desktop host.** No carrier, a presented surface.

## 7 Open questions

1. What the remaining copies cost one by one — the `Color32` conversion, the
   export mirror, the mesh lane's store — and whether an upload needs all of
   them when no carrier is attached and nothing is exported.
2. Whether one large texture can reuse its buffers between frames, which is
   where its kernel time goes.
3. Whether the staleness also held for the `image` widget opcode and for
   `scrollingTexture`; neither was exercised, and both go through the same
   rasteriser cache.
4. Whether the slow launches were the machine or the path; a quiet machine
   would say.

Related: [ADR-0205](../../adr/0205-imzero2-cpu-rasterized-pixel-host.md) (the cpu
host and the staleness it recorded), [ADR-0149](../../adr/0149-implot-core-port-painter-lane.md)
(`paintImage`), [ADR-0204](../../adr/0204-leaflet-map-core-port.md) (tiles
through `paintImage`).
