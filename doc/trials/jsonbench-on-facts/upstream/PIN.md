---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# JSONBench upstream — the pin

What the [jsonbench-on-facts](../README.md) trial reuses from upstream, pinned
so a later run is comparable with an earlier one.

- **Repository:** `ClickHouse/JSONBench` on GitHub
- **Pinned commit:** `e6c7c98dc766394d51f7d506a3dd2b5d51165d70` (2026-03-08),
  `main` at the 2026-08-05 fetch
- **Licence:** CC BY-NC-SA 4.0

## Licence — why nothing is vendored

The protocol's M0 says "vendor the ClickHouse DDL, queries, and run
discipline". Upstream is CC BY-NC-SA 4.0 and boxer is MIT; copying upstream
files into this tree would carry a ShareAlike and NonCommercial obligation the
repository does not otherwise have. **No upstream file is copied here.**

Reproducibility is preserved by pinning instead: [`fetch-pin.sh`](./fetch-pin.sh)
clones the repository, checks out the commit, and verifies the SHA-256 of every
file the trial depends on. A run that fetches a different byte sequence fails
loudly rather than silently drifting. The recorded hashes are:

| File | SHA-256 (prefix) |
| --- | --- |
| `clickhouse/ddl.sql` | `fe77b96d…` |
| `clickhouse/queries.sql` | `86c590b7…` |
| `clickhouse/run_queries.sh` | `724f79eb…` |
| `clickhouse/load_data.sh` | `424ae911…` |
| `clickhouse/create_and_load.sh` | `27b44e14…` |
| `clickhouse/data_size.sh` | `479269db…` |
| `clickhouse/index_size.sh` | `642cf17d…` |
| `clickhouse/total_size.sh` | `698e57b3…` |
| `clickhouse/index_usage.sh` | `8b8db92f…` |
| `download_data.sh` | `d5140009…` |
| `clickhouse/results/m6i.8xlarge_bluesky_1m.json` | `c8fed02a…` |

Full hashes live in `fetch-pin.sh`, which is what actually enforces them.
Measured *numbers* from published results are facts and are quoted freely in
the [logbook](../logbook.md).

## The table, as read off the pin

One table, one `JSON`-typed column (`clickhouse/ddl.sql`). Its type hints
carry upstream's cardinality decisions, so the string-vs-symbol question for
the facts mapping can be read off rather than inferred from sample data:

- **`max_dynamic_paths = 0`** — every path outside the five hinted ones lands
  in the JSON type's shared data; only the backbone gets dedicated columns.
- **Typed paths:** `kind`, `commit.operation`, `commit.collection` are
  `LowCardinality(String)`; `did` is a plain `String` — *not*
  dictionary-encoded, at millions of distinct users; `time_us` is `UInt64`.
- **`ORDER BY`** is exactly the five hinted paths, cardinality-ascending:
  `kind`, `commit.operation`, `commit.collection`, `did`,
  `fromUnixTimestamp64Micro(time_us)` — the clustered index of the reference
  entry, and the shape arm D would be compared against.
- The column carries `CODEC(ZSTD(1))` plus four MergeTree serialization
  settings; the M0 run's environment note found all four to be server
  defaults on 26.7.2.59 already.

## Run discipline, as read off the pin

The protocol §2 noted that repetition counts and the cold-cache procedure live
in the run scripts rather than the README. Read off the pinned scripts:

- **Repetitions: `TRIES=3`** per query (`clickhouse/run_queries.sh`).
- **Cold-cache procedure:** once per query, *before* its three tries —
  `sync` then `echo 3 > /proc/sys/vm/drop_caches` via `sudo`
  (`clickhouse/run_queries.sh`). Requires root.
- **Cold and hot are a reduction over the triple, not separate runs**
  (`index.html`, the `selectRun` function): **cold = try 1**;
  **hot = min(try 2, try 3)** — a minimum, not a median. This answers the
  protocol's open question §9.4 on "hot = min or median of N" from upstream
  rather than by choice.
- **Timings and memory** come from `clickhouse client --time --memory-usage
  --format=Null --progress 0`, which prints seconds then bytes on stderr.
- **Sizes** come from `system.parts` over active parts:
  `total_size = sum(bytes_on_disk)`, `data_size = sum(data_compressed_bytes)`,
  `index_size = sum(primary_key_size) + sum(marks_bytes)`.
- **Pruning evidence** is `EXPLAIN indexes=1`; physical plans are
  `EXPLAIN PIPELINE`.
- **Load:** one `INSERT … FORMAT JSONAsObject` per gzipped file, with
  `min_insert_block_size_rows = 1000000, min_insert_block_size_bytes = 0`.
  Upstream retries once with `input_format_allow_errors_*` relaxed if the
  first attempt fails.
- **Dataset:** `file_%04g.json.gz` from the ClickHouse public-datasets S3
  bucket; tier N = files 1..N, one million events each. The 1M file is
  135,176,827 bytes gzipped and 480,778,277 bytes raw.

## Queries, as verified against the pin

The protocol §2 table was compiled before the pin and is re-verified here.
All five statements are as described: Q1 is an unfiltered `GROUP BY` on
`data.commit.collection`; Q2 adds `WHERE data.kind = 'commit' AND
data.commit.operation = 'create'` and `uniqExact(data.did)`; Q3 buckets by
`toHour(fromUnixTimestamp64Micro(data.time_us))` with an `IN [...]` array
literal over three collections; Q4 and Q5 filter to
`app.bsky.feed.post`, group by `data.did::String`, and take `LIMIT 3` on
`min(ts)` ascending and on
`date_diff('milliseconds', min(ts), max(ts))` descending respectively.

The three translation hazards §2 pre-registered are all present in the pinned
text: the `IN ['…','…','…']` array literal (Q3), the `::String` cast (Q4, Q5),
and `date_diff` with a string unit (Q5). `toHour`'s server-timezone dependency
is likewise confirmed.
