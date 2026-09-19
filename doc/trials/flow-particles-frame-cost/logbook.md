---
type: reference
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

# Flow particles, frame cost — logbook

Chronological, append-only record of runs of the
[flow-particles frame-cost](./README.md) trial, per the
[directory convention](../README.md). Newest entry last. Each entry's raw
evidence lives in its own `./runs/<YYYY-MM-DD-slug>/` directory.

## 2026-09-19 — M0, both hosts, three arms — the batched opcode was needed; the mesh draw stays the default

- **Build under test:** boxer at the commit that adds this entry, over
  `5c5d4a1d`; Go 1.27.0, rustc 1.96.1, egui 0.35.0. An edit to the layer
  landed between the second and third cells (see the environment file).
- **Environment:** a handheld-class AMD custom APU, 8 hardware threads,
  integrated GPU, 16 GB shared memory, Linux 6.11. **Not idle**: other work, including a compile,
  ran during the run.
- **Attempted:** M0 — 2 hosts × (3 arms × 5 particle counts + the floor), one
  launch per cell, through the scene runner. All 32 cells completed.
- **Hypotheses:** H1 confirmed — `line-per-segment` is five to seven times
  `segments-mesh` on the Go side at every count on both hosts, and about 1 µs
  per segment. H2 confirmed for rasterisation on the cpu host (about three
  times) and refuted for the wgpu host (no difference); the tessellation half
  of H2 is undecided, because the column that would decide it is not usable.
- **Findings:** the competence vault is not in the working tree, so every
  finding anchors at the toolbelt root with a proposed slug.
  - **[pain boxer-toolbelt → proposed:fffi2-slice-marshalling / performance-efficiency.time-behaviour / S2]**
    a slice argument is marshalled one element per `io.Writer.Write`, which is
    about 60 % of the Go side of a frame that is mostly slices (evidence:
    runs/2026-09-19-first-run/environment.md, the profile)
  - **[pain boxer-toolbelt → proposed:imzero2-scene-runner / usability.operability / S3]**
    a scene's `read` step binds a value and nothing hands it back to the
    caller; the run reads its numbers out of a `tree` step's listing on
    stdout, which clips a long value, so the demo publishes three short labels
    (evidence: measure.sh)
  - **[broken boxer-toolbelt → proposed:imzero2-scene-runner / reliability / S3]**
    an unfiltered-by-role `tree` step timed out "waiting for the carrier" once
    while the demo animated 5 000 particles; with `"role":"label"` it has not
    recurred — not reproduced, not understood (evidence: none kept)
  - **[broken boxer-toolbelt → proposed:imzero2-drive-verbs / functional-suitability.functional-correctness / S3]**
    `set_value` on an egui slider changed nothing; no scene in the tree uses
    the verb. The demo takes its count from buttons instead (evidence:
    measure.sh, the `n=` buttons)
  - **[pain boxer-toolbelt → proposed:imzero2-headless-raster-stats / functional-suitability.functional-appropriateness / S3]**
    raster statistics are cumulative over a launch, so a cell costs a launch
    (about 25 s of mounting the gallery), and the tessellation median
    disagrees between hosts for identical work (evidence: results.tsv,
    `tess_p50_us`)
  - **[note boxer-toolbelt → proposed:imzero2-frame-metrics]** the frame
    metrics of ADR-0062 (dispatch time, bytes written) and
    `IMZERO2_HEADLESS_RASTER_STATS` were enough to split a frame's cost across
    the Go side, the boundary and the host with no new instrumentation
  - **[note boxer-toolbelt → proposed:imzero2-scene-runner]** the scene runner
    carried a 32-launch matrix across two host builds with a generated scene
    per cell and no port, process or teardown handling in the script
- **Solution size:** `measure.sh` 120 lines; the `flowbench` demo 190 lines;
  `Options.LinePerSegment` 8 lines in the layer; `BenchmarkDraw` 35 lines.
- **Results:** [results.tsv](./runs/2026-09-19-first-run/results.tsv) — 32
  rows, raw microseconds and bytes. Not loaded as facts (README §4).
- **Run dir:** [runs/2026-09-19-first-run/](./runs/2026-09-19-first-run/)

## 2026-09-19 — governor comparison, a subset four times — the governor does not matter here; the two headline differences repeat

- **Build under test:** boxer `961d98c8`; Go 1.27.0, rustc 1.96.1, egui 0.35.0.
- **Environment:** as the first run, **not idle** — the desktop gallery ran
  the `flowonmap` demo throughout and a browser was open; load average 2.7 to
  7.1 at the start of a round.
- **Attempted:** whether the CPU frequency governor moves the medians, and
  with it a first look at the spread between launches: both hosts,
  `segments-mesh` at 5 000 and 20 000 particles, `line-per-segment` at 5 000,
  the floor; `performance` and `powersave` interleaved, two rounds each.
  All 40 launches completed.
- **Outcome:** no governor effect. In every cell the two `performance`
  launches and the two `powersave` launches overlap; the spread between
  launches of one cell is about a tenth of its median, the governor's effect
  is inside it, and under this load the cores were near their top frequency
  under either. Across the four launches of a cell: `line-per-segment` at
  5 000 particles costs the Go side 48 to 56 ms against 6.4 to 8.2 ms for
  `segments-mesh`, on either host; on the cpu host the per-line arm's
  feathered shapes rasterise in 41 to 43 ms against 14.6 to 16.3 ms for the
  mesh; on the wgpu host both rasterise in 3 to 4 ms.
- **Findings:**
  - **[pain boxer-toolbelt → proposed:imzero2-frame-metrics / performance-efficiency.time-behaviour / S4]**
    the host's opcode dispatch at 20 000 particles in the mesh arm is bimodal
    between launches on the wgpu host — 13.4 ms once, 24.9 to 25.9 ms three
    times — and the first run's cpu-host figure for the same cell, 11.8 ms,
    sits in the low mode against 23.2 to 26.5 ms here; not understood
    (evidence: runs/2026-09-19-governor/results.tsv)
  - **[pain boxer-toolbelt → proposed:portolan-landoverlay / performance-efficiency.time-behaviour / S3]**
    under an animating layer the land overlay re-projects and re-fills every
    country every frame, which is between three tenths and a half of the Go
    side of the `flowonmap` demo — as much as the flow layer (evidence:
    runs/2026-09-19-governor/environment.md, the live profile)
  - **[note boxer-toolbelt → proposed:fffi2-slice-marshalling]** the live
    profile repeats the first run's finding on a real host: slice marshalling
    is about four fifths of the flow layer's Go side, and the particle
    simulation a small part of it
- **Solution size:** none; `measure.sh` as committed.
- **Results:** [results.tsv](./runs/2026-09-19-governor/results.tsv) — 32
  rows with the governor and the round as columns; the floor, launched twice
  a round, is kept once.
- **Run dir:** [runs/2026-09-19-governor/](./runs/2026-09-19-governor/)
