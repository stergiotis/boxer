---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Arm A at the 1M tier — summary

The upstream ClickHouse JSON entry, run locally as this trial's reference.
Environment in [`environment.md`](./environment.md); raw evidence under
[`arm-a/`](./arm-a/). Pin and run discipline in
[`../../upstream/PIN.md`](../../upstream/PIN.md).

Arm A was run **twice** end to end, the second time entirely through the
committed [`arm-a.sh`](../../arm-a.sh) including a fresh drop-and-reload. Both
runs are reported: the spread between them is this machine's run-to-run noise,
and it is not small relative to the numbers being measured.

## Load

| | |
| --- | --- |
| Rows | 1,000,000 |
| Source | `file_0001.json.gz`, 135,176,827 B gzipped / 480,778,277 B raw |
| Ingest wall clock | 3.67 s (run 1), 3.74 s (run 2) — client-side, excludes gunzip |
| Client peak RSS | 1.06 GB (run 1), 1.04 GB (run 2) |
| Parts / marks | 1 part, 124 marks — a single insert block |

## Size

Identical to the byte across both runs.

| Metric | This run | Published `m6i.8xlarge`, CH 25.11 | Delta |
| --- | --- | --- | --- |
| `total_size` (bytes on disk) | 102,099,450 | 98,534,792 | +3.6 % |
| `data_size` (compressed) | 102,074,732 | 98,424,457 | +3.7 % |
| `index_size` (PK + marks) | 24,579 | 110,328 | −77.7 % |
| uncompressed columnar | 674,515,642 | not published | — |

The index is far smaller here because the whole tier landed in one part
(124 marks). Compression against the columnar uncompressed size is 6.6×;
against the raw JSON input, 4.7×.

## Query runtimes

Seconds. `hot = min(try2, try3)` per the upstream reduction; the **cold column
is absent by construction** on this machine — see `environment.md` §
Deviations.

| Q | Run 1 tries | Run 1 hot | Run 2 tries | Run 2 hot | Published hot | Peak memory |
| --- | --- | --- | --- | --- | --- | --- |
| Q1 | 0.005 / 0.004 / 0.004 | **0.004** | 0.006 / 0.005 / 0.006 | **0.005** | 0.004 | 2.3 MB |
| Q2 | 0.027 / 0.022 / 0.028 | **0.022** | 0.042 / 0.035 / 0.030 | **0.030** | 0.022 | 31 MB |
| Q3 | 0.011 / 0.013 / 0.012 | **0.012** | 0.012 / 0.011 / 0.010 | **0.010** | 0.012 | 49 MB |
| Q4 | 0.018 / 0.016 / 0.037 | **0.016** | 0.019 / 0.015 / 0.014 | **0.014** | 0.017 | 13 MB |
| Q5 | 0.019 / 0.018 / 0.039 | **0.018** | 0.015 / 0.015 / 0.015 | **0.015** | 0.019 | 13 MB |

## M0 gate

The gate is *relative query ordering consistent with published results*, not
absolute reproduction. Ordering by hot runtime:

- Run 1: Q1 < Q3 < Q4 < Q5 < Q2
- Run 2: Q1 < Q3 < Q4 < Q5 < Q2
- Published: Q1 < Q3 < Q4 < Q5 < Q2

**Identical, and stable across both runs — the gate passes.** Absolute hot
numbers land within roughly ±40 % of the published `m6i.8xlarge` figures on
different hardware and a newer engine (26.7.2.59 vs 25.11), which is closer
than the gate asks for.

Two caveats on the numbers themselves. At 1M every query completes in tens of
milliseconds, so per-query timer granularity and scheduler noise are a
material fraction of each measurement — run 1's Q4 and Q5 third tries
(0.037 s, 0.039 s) are more than double their own second tries. And this is a
shared workstation, not a quiesced benchmark host. **The 1M tier is a smoke
test for the harness, not a source of load-bearing latency numbers**; the
protocol already treats 10M as the development tier and 100M as the real run,
and this run supports that.

## Pruning baseline

`EXPLAIN indexes=1` for all five queries is in
[`arm-a/explain.txt`](./arm-a/explain.txt), alongside `EXPLAIN PIPELINE`.
At 1M in a single part, arm A's sort key
(`kind, commit.operation, commit.collection, did, ts`) reads
123/123 granules for Q1 — an unfiltered `GROUP BY` has nothing to prune — so
the interesting pruning comparison against arms B and C belongs at a tier with
more than one part.

Query result rows are in [`arm-a/query-results.txt`](./arm-a/query-results.txt)
and are the correctness oracle the facts arms must match — with the standing
exception of Q3, whose hour buckets follow the server timezone
(Europe/Zurich here).
