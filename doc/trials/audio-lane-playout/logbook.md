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

## 2026-11-14 — M0, loopback, first pass — withdrawn

- **Build under test:** the working tree that was committed afterwards as
  boxer `200a0d42a`. Chromium 157 (snap), headless.
- **Environment:** AMD Ryzen AI Max+ Pro 395, 32 threads, 123 GiB, Linux
  7.0; browser, Go side and headless client on the one machine, loopback.
- **Attempted:** nine arms, 20 s each: four targets on each carrier and a
  tab at a continuous cadence.
- **Outcome:** withdrawn after a review of the code and of this trial. The
  page then placed a late chunk behind a nearly empty schedule, which hides
  a stream running dry; the playhead figure measured the unevenness of
  reports, not a distance; the lead was sampled on one phase of the report
  cycle; the busy arm carried no load. The protocol's §5 has each.
- **Run dir:** removed with its figures.

## 2026-11-14 — M0, loopback — eight arms, none ran dry

- **Build under test:** boxer `6dc61235b` with the uncommitted fixes that
  followed the review on top; the binaries were built by the run and their
  digests are in its `env.txt`. Chromium 157 (snap), headless.
- **Environment:** as above. Details in the run's `env.txt`.
- **Attempted:** the protocol's eight arms, 20 s each. A pass an hour
  earlier, with the lane's floor at 50 ms, is what moved the floor to 80 ms
  (protocol §5); its rows are not kept.
- **Findings:**
  - **[missing headless-driver-and-scenes → proposed:browser-page-driving /
    functional-completeness / S3]** nothing puts input into a real browser
    page, so the workload is started by a seed variable and the browser's
    autoplay rule is switched off by a flag; the input path the lane has
    for a real person is not exercised (evidence: `env.txt`, browser flags).
  - **[pain headless-driver-and-scenes / operability / S4]** the carrier
    serves its page from the binary, so a probe cannot be placed in it; the
    headless arms serve a composed copy from the tab binary's dev server and
    point it at the carrier with `?ws=` (evidence: `env.txt`, the `tabserve`
    digest).
  - The run leaned on the tab bundle and its dev server's report sink
    without incident.
- **Solution size:** `measure.sh` and one harness file, the page probe.
- **Results:** `results.jsonl` in the run directory; none in the facts
  store.
- **Outcome:** no arm ran dry; the arms that asked for 40 ms ran at the
  80 ms floor.
- **Run dir:** [`./runs/2026-11-14-loopback/`](./runs/2026-11-14-loopback/)
