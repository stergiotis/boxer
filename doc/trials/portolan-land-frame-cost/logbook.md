---
type: reference
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

# Portolan land, frame cost — logbook

Chronological, append-only record of runs of the
[portolan land frame-cost](./README.md) trial, per the
[directory convention](../README.md). Newest entry last. Each entry's raw
evidence lives in its own `./runs/<YYYY-MM-DD-slug>/` directory.

## 2026-10-03 — M0, both hosts, three arms, four views — the frame is mostly invariant work

- **Build under test:** boxer at `48c860787` plus this trial's files; Go
  1.27.0, egui 0.35.0. The second and third runs and the profiles ran from a
  worktree at that commit, because the main checkout carried another
  session's uncommitted work that did not build at the time; the clients
  were built from the main checkout with identical Rust sources.
- **Environment:** a handheld-class AMD custom APU, 8 hardware threads, 16 GB
  shared memory, Linux 6.11. **Not idle**: load average 2–8 during the
  first run, 2–8 during the repeats, other sessions compiling and testing.
- **Attempted:** the Go benchmark per view and arm with CPU and allocation
  profiles; the in-host matrix once
  ([first-run](./runs/2026-10-03-first-run/results.tsv)) and three times
  ([repeats](./runs/2026-10-03-repeats/results.tsv)), 2 hosts × 3 arms × 4 views, one
  launch per cell; `perf` on the cpu host's client and the Go host for the
  three arms at `world-z0`
  ([profiles](./runs/2026-10-03-profiles/host-client-world-z0-fill.perf.txt), the script beside them).
- **Hypotheses:** H1 confirmed — the Go side is projection (about half of
  the layer) and scratch allocation (94 % of 820 kB a frame from two
  helpers), the FFFI encoding 6–8 %. H2 confirmed — at `world-z0` the
  `fill` arm's host work exceeds `nofill`'s by the ear clip, about two
  thirds of the client's dispatch samples; it is the largest host-side cost
  of the layer. H3 undecided — the raster statistics do not see the map.
- **Findings:** the competence vault is not in the working tree, so every
  finding anchors at the toolbelt root with a proposed slug.
  - **[pain boxer-toolbelt → proposed:imzero2-frame-metrics / functional-suitability.functional-correctness / S2]**
    `LastInterpretNs` is wall time including the wait on the pipe, so on the
    lockstep protocol it carries the Go side's time too; a trial reading it
    as host dispatch double-counts (evidence: repeats/results.tsv,
    `world-z0/nofill`, against profiles/host-client-world-z0-nofill.perf.txt)
  - **[pain boxer-toolbelt → proposed:imzero2-headless-raster-stats / functional-suitability.functional-correctness / S3]**
    the raster statistics' sampled frame does not contain the bench's canvas:
    triangles at `world-z0/fill` equal the `off` arm's
  - **[pain boxer-toolbelt → proposed:imzero2-scene-runner / performance-efficiency.resource-utilization / S3]**
    a scene run carries a per-frame BLAKE3 of the framebuffer
    (`WsCarrier::on_frame`, 11–30 % of the client's samples) and an AV1
    encoder process; both load every whole-frame figure of every trial run
    through the scene runner
  - **[pain boxer-toolbelt → proposed:imzero2-scene-runner / usability.operability / S3]**
    a scene document whose trace fence is not closed runs no steps and
    passes; a long `sleep` is reaped by the driver, so a profile window has
    to be several short ones
  - **[pain boxer-toolbelt → proposed:imzero2-scene-runner / usability.operability / S3]**
    the client staleness check compares file times, so a fresh worktree at
    the client's own commit is refused as newer
- **Outcome:** §0's first claim. The invariant work is projection and
  allocation in Go and triangulation in the host; the wire is not where the
  cost is.

## 2026-10-03 — the plane cache and scratch reuse, Go benchmark — the Go side is a quarter of what it was

- **Build under test:** the main checkout's portolan and landoverlay with the
  fix (the atlas projected once into the CRS's plane, `Projector.PlanePolygon`
  / `PlanePolyline`, the overlay's scratch on the `Map`), against the same
  package from the worktree at `48c860787`.
- **Environment:** as above; load average 17–26 — very busy.
- **Attempted:** `BenchmarkPaint`, old and new binaries interleaved, five
  rounds of one second each per cell.
- **Results (medians, ms per frame, old → new):** `world-z0/fill` 9.12 →
  2.18; `europe-z2.6/fill` 3.65 → 0.59; `alps-z5/fill` 0.75 → 0.16;
  `swiss-z8/fill` 0.33 → 0.08; `world-z0/nofill` 16.5 → 7.1, the rest of
  which is Douglas–Peucker and Cohen–Sutherland per frame. Allocation at
  `world-z0/fill` 820 kB → 16 kB a frame; bytes and messages unchanged.
- **Equivalence:** the paint stream is byte-identical to the build before at
  all eight view × arm cells, and the convex and polyline paths fed every
  atlas ring are identical too (hashed in the same tree, the fix's files
  swapped for `HEAD`'s). `TestPlanePathPaintsWhatTheDegreePathPaints` holds
  the line; replacing the world copy's offset with zero fails it.
- **Outcome:** §0's second claim. The in-host matrix after the fix is the
  next entry.

## 2026-10-03 — the plane cache in the host, partial — a filled frame is now bound by the host's triangulation

- **Build under test:** a worktree at `48c860787` with the fix's files and
  this trial's, which is `4ba2b1036`'s code; clients as before.
- **Environment:** as above; load average 5–14.
- **Attempted:** the matrix with `REPEATS=2`. **Stopped after six cells**,
  all on the cpu host: `/tmp`, a tmpfs shared with other sessions, filled up
  and the seventh cell's scene runner could not write. The run directory
  holds the six cells and the failed one's message.
- **Results (cpu host, medians, ms; before = the repeats run, after = one
  launch):** Go draw at `world-z0/fill` 6.85 → 4.81, `europe-z2.6/fill`
  3.04 → 1.66, `alps-z5/fill` 0.53 → 0.21, `swiss-z8/fill` 0.29 → 0.14;
  interpret at `world-z0/fill` 14.80 → 14.66, `europe-z2.6/fill` 5.76 →
  5.38; interpret at `world-z0/nofill` 9.61 → 3.63, `europe-z2.6/nofill`
  4.70 → 1.63. Bytes per frame unchanged in every cell.
- **Reading:** where the Go side bounded the frame (`nofill`) the frame got
  about 2.6 to 2.9 times faster in the host. Where the host's own work does
  (`fill` at continent and world zoom), the frame did not move: the ear clip
  of the concave rings is what it waits on now. The Go draw in the host is
  about twice the benchmark's after the fix; one launch under load, not yet
  separated from noise.
- **Findings:**
  - **[pain boxer-toolbelt → proposed:imzero2-scene-runner / reliability.fault-tolerance / S3]**
    a full `/tmp` fails a cell with "No space left on device" and the script
    goes on to the next; the run degrades to a partial matrix without
    stopping
- **Outcome:** §0's second claim, qualified. The next step for a filled map
  is the host's triangulation, which is not a Go-side change. Repeat the
  matrix with room on `/tmp`.
