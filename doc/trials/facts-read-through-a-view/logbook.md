---
type: reference
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

# Facts read through a view — logbook

Chronological, append-only record of runs of the
[facts-read-through-a-view](./README.md) trial, per the
[directory convention](../README.md). Newest entry last. Each entry's raw
evidence lives in its own `./runs/<YYYY-MM-DD-slug>/` directory.

## 2026-11-21 — M0, first run — a keyed carrier prunes as the table does; an expression for the timestamp keeps only the partition

- **Build under test:** the commit `environment.md` names; ClickHouse
  26.9.13.
- **Environment:** AMD Ryzen AI Max+ Pro 395, 32 threads, 122 GiB memory,
  local NVMe, Linux 7.0; a throwaway server on the same machine, which was
  busy with other work during the run (load average 6 before, 21 after).
- **Attempted:** M0 as the README states it: 2,000,000 log rows and 365
  heartbeats, four arms, six reads, five repetitions.
- **Findings:**
  - H1 confirmed: in arm `view-keyed` the one-day read selects one part and
    two granules, as arm `table` does (`plans.txt`).
  - H2 confirmed: in arm `view-computed-ts` the same read selects one part
    — the partition key, written with the view's expression, matches — and
    all 28 of its granules for the tenant.
  - A read by row id alone selects every part and every granule in all
    four arms, the facts table included.
  - Positive maturity: the facts store's reader and the record-store
    executor ran unchanged against a view over the `chstoretest` carrier;
    the harness needed no SQL of its own for the store's reads.
  - Method: without clearing the query condition cache, the second
    repetition of a by-id read reported one part and two marks. The first
    smoke run showed it; the protocol clears the cache before every
    repetition since.
- **Solution size:** `harness/view_test.go`, `measure.sh`.
- **Results:** `cost.tsv`; the durations in it were taken under load and
  are not a result.
- **Run dir:** [./runs/2026-11-21-first-run/](./runs/2026-11-21-first-run/)
