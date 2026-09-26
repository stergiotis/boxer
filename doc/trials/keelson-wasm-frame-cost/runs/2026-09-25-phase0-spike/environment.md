---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Environment

Written by hand: the run was stopped by the operator's tooling during the
Firefox arms, after every other arm had completed, so measure.sh never
reached its own environment step. Values are those the script would have
recorded, read from the same machine right after the run.

- date: 2026-09-25 (run started about 05:40 UTC)
- boxer commit: 410fdf8b, with the trial's own uncommitted additions in the tree
  (rust/fffi2stub, the wasmspike command, the deferred-flush channel option)
- go: go1.27.0
- rustc: 1.96.1 (rust/fffi2stub/rust-toolchain.toml)
- node: v23.4.0
- chromium: Ungoogled Chromium 144.0.7559.96 (flatpak, headless=new, --disable-gpu)
- firefox: Mozilla Firefox 146.0.1 (flatpak, headless) — started, loaded nothing
  the report server saw, no result; rows dropped from results.tsv
- cpu: AMD Custom APU 0932, 8 threads (a handheld-class APU)
- memory: 14 GiB
- kernel: 6.11.11-valve29-1-neptune-611-g2dcfaf4df7ac
- load: not recorded at start; the machine carried other sessions throughout
  (a memory-pressure stop ended the run), so treat every figure as an upper
  bound taken on a loaded machine
- settings: SCENES="gallery labels" ROWS=1700 FRAMES=300 WARMUP=30 TARGETS="wasip1 js" BROWSERS="chromium firefox" STAGE=1024x600
- clocks: browser arms read time inside the worker, where performance.now is
  coarsened to 100 µs; their columns are rounded accordingly
- cpufreq: governor powersave, driver amd-pstate-epp (read after the run; the run was not made under the performance governor)
