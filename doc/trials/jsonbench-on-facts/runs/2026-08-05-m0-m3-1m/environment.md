---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Environment — run 2026-08-05-m0-arm-a

Captured at run time per the trial protocol
([README §6](../../README.md)). Hostnames and filesystem paths outside the
ClickHouse store are deliberately omitted.

| Facet | Value |
| --- | --- |
| boxer commit | `5a065288` (working tree carried unrelated uncommitted changes; none in the trial's paths) |
| ClickHouse | 26.7.2.59, official build |
| Server timezone | **Europe/Zurich** — see the Q3 caveat below |
| CPU | AMD Ryzen AI MAX+ PRO 395 w/ Radeon 8060S — 1 socket, 16 cores, 32 threads, 1 NUMA node |
| Memory | 93 GiB total, ~51 GiB free / ~79 GiB available at run start |
| Storage | NVMe SSD (`ROTA=0`), LUKS/dm-crypt, ext-family filesystem; ClickHouse store on the same volume, 277.96 GiB free of 951.27 GiB |
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

1. **No cold runs.** Upstream drops the OS page cache
   (`sync; echo 3 | sudo tee /proc/sys/vm/drop_caches`) once before each
   query's three tries, making try 1 the cold measurement. This workstation
   has no passwordless `sudo`, so the run used `DROP_CACHES=0`: all three
   tries ran against a warm page cache. **The cold column is absent from
   this run**, not merely noisy. Hot numbers (`min(try2, try3)`, per
   upstream's own reduction) are unaffected and are the comparable figure.

   *Resolved after this run.* A scoped grant
   (`NOPASSWD: /usr/bin/tee /proc/sys/vm/drop_caches`) was added on the same
   day and verified against the exact command `measure.sh` issues. Runs from
   the next one on can set `DROP_CACHES=1` and carry a real cold column; this
   run cannot be retrofitted with one.
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
