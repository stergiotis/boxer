---
type: reference
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

# Audio lane playout — logbook

Chronological, append-only record of runs of the
[audio lane playout](./README.md) trial, per the
[directory convention](../README.md). Newest entry last. Each entry's raw
evidence lives in its own `./runs/<YYYY-MM-DD-slug>/` directory.

## 2026-11-14 — M0, loopback — nine arms, no underrun in any

- **Build under test:** the working tree that was committed afterwards as
  boxer `200a0d42a`; the run's `env.txt` names the commit that tree stood
  on when it was built. Chromium 157 (snap), headless.
- **Environment:** AMD Ryzen AI Max+ Pro 395, 32 threads, 123 GiB, Linux
  7.0; browser, Go side and headless client on the one machine, loopback.
  Details in the run's `env.txt`.
- **Attempted:** the protocol's nine arms, 20 s each: four targets on each
  carrier and the busy tab. The arms were run twice within the hour, the
  first time with the runner in an earlier form; only the second pass is
  kept. Both had no underrun; the leads of several arms differed by 10 to
  20 ms between the two.
- **Findings:**
  - **[missing headless-driver-and-scenes → proposed:browser-page-driving /
    functional-completeness / S3]** nothing puts input into a real browser
    page, so the workload is started by a seed variable and the browser's
    autoplay rule is switched off by a flag; the gesture path the lane has
    for a real person is not exercised (evidence: `env.txt`, browser flags).
  - **[pain headless-driver-and-scenes / operability / S4]** the carrier
    serves its page from the binary, so a probe cannot be placed in it; the
    headless arms serve a composed copy from the tab binary's dev server and
    point it at the carrier with `?ws=` (evidence: `measure.sh`).
  - The run leaned on the tab bundle and its dev server's report sink
    without incident.
- **Solution size:** `measure.sh` and one harness file, the page probe.
- **Results:** `results.jsonl` in the run directory; none in the facts
  store.
- **Run dir:** [`./runs/2026-11-14-first/`](./runs/2026-11-14-first/)
