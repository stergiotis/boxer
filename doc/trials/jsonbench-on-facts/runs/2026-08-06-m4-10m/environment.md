---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Environment — run 2026-08-06-m4-10m

Captured at run time per the trial protocol
([README §6](../../README.md)). Hostnames and filesystem paths outside the
ClickHouse store are deliberately omitted.

| Facet | Value |
| --- | --- |
| boxer commit | `5a065288` plus this trial's uncommitted work; the tree carried unrelated uncommitted changes in `apps/play` |
| ClickHouse | 26.7.2.59, official build |
| Server timezone | **Europe/Zurich** — see the Q3 caveat below |
| CPU | AMD Ryzen AI MAX+ PRO 395 w/ Radeon 8060S — 1 socket, 16 cores, 32 threads, 1 NUMA node |
| Memory | 93 GiB total |
| Storage | NVMe SSD (`ROTA=0`), LUKS/dm-crypt, ext-family filesystem; ClickHouse store on the same volume, ~262 GiB free of 951.27 GiB after the run |
| OS | Fedora Linux 44, kernel 7.1.6-201.fc44.x86_64 |
| `max_threads` | `auto(32)` |
| `max_memory_usage` | 60,000,000,000 (60 GB) |
| `use_query_cache` | 0 |
| `max_bytes_before_external_group_by` | 0 |

## Table settings the upstream DDL asks for

The pinned DDL sets four MergeTree serialization settings with the comment
"planned to be default soon". On 26.7.2.59 all four are **already the server
defaults**, so the DDL is a no-op with respect to them:

| Setting | Default on this server | Requested by pinned DDL |
| --- | --- | --- |
| `object_serialization_version` | `v3` | `v3` |
| `dynamic_serialization_version` | `v3` | `v3` |
| `object_shared_data_serialization_version` | `advanced` | `advanced` |
| `object_shared_data_serialization_version_for_zero_level_parts` | `map_with_buckets` | `map_with_buckets` |

`enable_json_type` is likewise `1` by default; the pinned load path passes it
explicitly and this run kept that.

## Deviations from the pinned run discipline

1. **Cold runs are measured.** A scoped grant
   (`NOPASSWD: /usr/bin/tee /proc/sys/vm/drop_caches`) is in place, so this
   run used `DROP_CACHES=1` and reproduces upstream's procedure exactly: the
   page cache is dropped once before each query's three tries, cold = try 1,
   hot = min(try 2, try 3). This is the first run of this trial with a real
   cold column; the 1M run has none and the two are not comparable on that
   axis.
2. **Server timezone is Europe/Zurich, not UTC.** Q3 buckets by
   `toHour()`, which is server-timezone-dependent, so this run's Q3 *result
   rows* are shifted relative to the published upstream results. Runtimes
   remain comparable, and every arm in this trial runs on this same server,
   so cross-arm Q3 comparison is internally sound.
3. **System `clickhouse-client`, not a downloaded `./clickhouse`.** The
   pinned `install.sh` downloads a binary into the working directory; this
   run used the already-installed 26.7.2.59 build.

Deviations 1 and 2 are properties of the machine, not of the toolbelt, and
are recorded as protocol deviations rather than findings.

## Query vocabulary

Unlike the 1M run, the facts arms' queries use the leeway query vocabulary:
the `LEEWAY_VALUE_BY_TAG_EQUAL` / `LEEWAY_LU_MEMB_IDX_TO_VAL_IDX` UDFs (already
present on this server) and ADR-0162's `chpack` pack (`coGather`,
`raggedStarts`), installed for this run via `jsonbench chpack` — pack version
1, 16 functions. `SELECT leewayPackVersion()` reports 1.

This changes what the facts arms measure: the open-coded form the 1M run used
is ~3× slower on Q1, so **the two tiers' arm-B latencies are not comparable
with each other**. Arm A is unaffected.
