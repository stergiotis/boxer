---
type: reference
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

# Image upload cost — logbook

Chronological, append-only record of runs of the
[image upload cost](./README.md) trial, per the
[directory convention](../README.md). Newest entry last. Each entry's raw
evidence lives in its own `./runs/<YYYY-MM-DD-slug>/` directory.

## 2026-11-14 — M0, both headless hosts, five arms, five sides — the cost is the carriage, and half of that is one read loop

- **Build under test:** boxer at `136c90877` (first run, profiles) and
  `e08d80422` (repeats; three commits later, none touching the path), plus
  this trial's files and the `imagebench` demo, uncommitted; Go 1.27.3,
  rustc 1.96.1, egui 0.36.2, wgpu 30.0.1. Both headless clients rebuilt from
  the tree before the first run. The working tree was shared with another
  session committing to other packages; the scene runner was built once per
  invocation for that reason.
- **Environment:** a 16-core, 32-thread desktop-class AMD APU, 122 GiB,
  Linux 7.0, governor `powersave`. **Not idle**: load average 2–7 over the
  runs, other sessions building and testing.
- **Attempted:** the matrix once, 2 hosts × 5 arms × 5 sides, one launch per
  cell ([first-run](./runs/2026-11-14-first-run/results.tsv)); six cells per
  host three times ([repeats](./runs/2026-11-14-repeats/results.tsv)); `perf`
  on the client of five cells, user space only
  ([profiles](./runs/2026-11-14-profiles/)); the Go mapping benchmark, five
  runs ([map-bench.txt](./runs/2026-11-14-first-run/map-bench.txt)); the
  `still` check on both hosts
  ([cpu](./runs/2026-11-14-first-run/still-flip-cpu.txt),
  [wgpu](./runs/2026-11-14-first-run/still-flip-wgpu.txt)).
- **Hypotheses:** H1 roughly — 1.9 to 3.3 ms per MiB across 4 to 256 MiB,
  the single texture dearer per MiB at 64 MiB and above. H2 refuted — the two
  hosts agree within the spread of launches, and the wgpu texture write is
  4–5 % of its client's user samples. H3 refuted — tiles cost no more at 4
  and 16 MiB and about 40 % less at 64 MiB; the single texture's client spent
  38 % of its CPU time in the kernel against 6 % for the tiled cell. H4
  confirmed up to 8192². H5 confirmed.
- **Findings:**
  - **[broken imzero2/hosts / functional-suitability.functional-correctness / S2]**
    on the cpu host a texture replaced in place under an otherwise unchanged
    frame is not redrawn: live frames and a capture show the old pixels for
    12 s, and the new ones once the image moves by a point; the wgpu host
    redraws at once (evidence: first-run/still-flip-cpu.txt,
    first-run/still-flip-wgpu.txt)
  - **[pain imzero2/framed-render-pipeline / performance-efficiency.time-behaviour / S2]**
    the host reads a `u32` slice argument one element at a time, and that
    read is 48–61 % of the client's user-space samples while an image is
    replaced every frame (evidence: profiles/*.client.inclusive.perf.txt,
    `read_plain_u32h`)
  - **[pain imzero2/framed-render-pipeline / performance-efficiency.resource-utilization / S3]**
    between the pipe and the texture an upload is copied into a `Vec<u32>`,
    a `Vec<Color32>` (12–15 % of samples), the export mirror (2–5 %), the
    mesh lane's texture store when a carrier is attached (14–18 %) and the
    rasteriser's own format (7–11 %, cpu host); one texture of 64 MiB
    allocates and frees each of these per frame, which is where its kernel
    time goes (evidence: profiles/cpu-reupload-s4096.bench.txt against
    profiles/cpu-tiles512-s4096.bench.txt)
  - **[pain imzero2/headless-driver-and-scenes / usability.operability / S3]**
    `scripts/dev/scene.sh` rebuilds the runner on every call, so a matrix of
    launches follows a shared tree as it moves: the first attempt at the
    matrix stopped at its thirteenth cell on another session's compile
    error in an unrelated package (evidence: none kept; the run was
    discarded and `measure.sh` now builds once)
  - **[pain measuring/trials / usability.operability / S4]** with
    `perf_event_paranoid=2` a call-graph recording needs
    `-e cycles:u --user-callchains` or it opens no event, and two recorders
    share one locked-memory budget, so the Go process's profile needs a
    smaller buffer; the earlier trial's script assumes a lower setting
    (evidence: profile-cell.sh)
  - **[pain imzero2/framed-render-pipeline → proposed:imzero2-frame-metrics / functional-suitability.functional-correctness / S3]**
    the headless raster statistic does not move with the upload on either
    host — 0.1–0.2 ms (cpu) and 0.5–0.6 ms (wgpu) from no image to 256 MiB a
    frame — so it cannot be used to cost a texture; on the cpu host that is
    the staleness above, on the wgpu host the texture write lies outside the
    sampled span (evidence: first-run/results.tsv, `raster_p50_us`)
  - positive: **imzero2/headless-driver-and-scenes** — fifty launches of a
    gallery demo driven by generated scenes, no failed cell once the runner
    was pinned; **imzero2/framed-render-pipeline** — a 256 MiB slice in one
    frame crossed the pipe and became a texture on both hosts without a limit
    being hit.
- **Solution size:** the demo, 320 lines; `measure.sh`, `profile-cell.sh`,
  `still_flip_check.sh`, one scene, and a 250-line Go benchmark. No change
  to the path under test.
- **Results:** the three run directories; no facts written.
- **Outcome:** §0. Replacing 16 MiB a frame is about what 30 Hz allows on
  this machine; the Go mapping is a small part of a recipe change; the wire
  format is not what costs, the host's handling of it is; and the cpu host
  does not redraw a texture replaced in place.
- **Run dirs:** [first-run](./runs/2026-11-14-first-run/),
  [repeats](./runs/2026-11-14-repeats/), [profiles](./runs/2026-11-14-profiles/).

## 2026-11-14 — M3, the slice read in blocks and the texture revision in the cpu rasteriser's key — tiled uploads cost about half, and the cpu host redraws

- **Build under test:** boxer at `21da7d0cb` with, uncommitted, the trial's
  files and two changes to the host: `read_plain_*h` in `fffi/io.rs` read a
  slice through a 64 KiB block instead of one element at a time, and the
  vendored software rasteriser keys a cached primitive on a per-texture
  revision that every texture delta bumps. Toolchains and pins as the entry
  above; all four clients rebuilt.
- **Environment:** the same machine, busier: load average 19 falling to 3
  over the matrix (it started as the client builds finished), 2.5–9 over the
  repeats.
- **Attempted:** the entry above again — the matrix once
  ([after-host-fixes](./runs/2026-11-14-after-host-fixes/results.tsv)), the
  six cells per host three times
  ([repeats](./runs/2026-11-14-after-host-fixes-repeats/results.tsv)), the
  five profiles
  ([profiles](./runs/2026-11-14-after-host-fixes-profiles/)), the `still`
  check on both hosts
  ([cpu](./runs/2026-11-14-after-host-fixes/still-flip-cpu.txt),
  [wgpu](./runs/2026-11-14-after-host-fixes/still-flip-wgpu.txt)).
- **Findings:**
  - resolved: the element-wise slice read — 4–7 % of the client's user-space
    samples, from 48–61 %; tiled cells at 47–63 % of their earlier cycle on
    wgpu at 16 and 64 MiB (evidence:
    after-host-fixes-profiles/*.client.inclusive.perf.txt, the two repeats)
  - resolved: the cpu host's stale texture — the frame shows the second
    pattern after the swap with nothing else changed (evidence:
    after-host-fixes/still-flip-cpu.txt); the unit test that was meant to
    guard it, `softraster::tests::texture_mutated_in_place_still_repaints`,
    passed before the change because an `Area`'s first passes change the
    mesh by themselves, and now settles the frame first
  - **[pain imzero2/framed-render-pipeline / performance-efficiency.resource-utilization / S3]**
    with the read gone the cycle is copies: `Color32` conversion 25–33 % of
    the client's user-space samples, the mesh lane's texture store 19–35 %,
    and for one large texture a kernel share of 35–57 % of the client's CPU
    time, from buffers of the texture's size allocated and freed per frame
    (evidence: after-host-fixes-profiles/*.bench.txt and
    *.client.inclusive.perf.txt)
  - **[pain imzero2/headless-driver-and-scenes / usability.operability / S3]**
    the repeats did not start the first time: the runner's one build per
    invocation still meets a shared tree, and another session's edit did not
    compile at that minute (evidence: none kept; the run was started again)
  - positive: **measuring/trials** — the same scripts measured the path
    before and after a change within the day, which is what the protocol is
    for.
- **Solution size:** `fffi/io.rs` +60 lines and three tests; the vendored
  rasteriser +28 lines, recorded in its `VENDORING.md`; one test rewritten in
  `softraster.rs`; the browser host's digest refreshed.
- **Results:** the three run directories; no facts written.
- **Outcome:** §0's first two claims and its last. 16 MiB a frame as tiles
  is 17 ms on the wgpu host and 22 ms on the cpu host; one texture stays
  dearer than tiles, by its allocations.
- **Run dirs:** [after-host-fixes](./runs/2026-11-14-after-host-fixes/),
  [after-host-fixes-repeats](./runs/2026-11-14-after-host-fixes-repeats/),
  [after-host-fixes-profiles](./runs/2026-11-14-after-host-fixes-profiles/).
