---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Environment — run 2026-08-06-jsonmap-100m

Captured at run time per the trial protocol
([README §6](../../README.md)). Hostnames and filesystem paths outside the
ClickHouse store are deliberately omitted.

| Facet | Value |
| --- | --- |
| ClickHouse | 26.7.2.59, official build |
| Server timezone | **Europe/Zurich** — see the Q3 caveat below |
| CPU | 16 cores / 32 threads, 1 socket |
| Memory | 93 GiB total |
| Storage | NVMe SSD, LUKS/dm-crypt; ClickHouse store on the same volume |
| OS | Fedora Linux 44, kernel 7.1.6 |
| `max_threads` | `auto(32)` |
| `max_memory_usage` | 60,000,000,000 (60 GB) |
| `use_query_cache` | 0 |
| `max_bytes_before_external_group_by` | 0 |

Identical to the [10M run](../2026-08-06-m4-10m/environment.md) on every facet
above, so this run's numbers are comparable with that one's.

## What is under test

The **canonical leeway JSON mapping** (`mapping.LoadJsonMapping`), not
`boxer.facts`. This is a new arm, not a re-run of arms A–E:

| | facts arm (B–E) | this arm |
| --- | --- | --- |
| schema | `boxer.facts` | `mapping.LoadJsonMapping` |
| membership | Ref-shaped; path demoted to the parameter channel | `MixedLowCardVerbatim` — path verbatim on `lmv` |
| memberships per attribute | 2 (path ref + params ref) | 1 |
| sections | array-valued (`len` lane) | scalar |
| path resolution in SQL | `LEEWAY_VALUE_BY_TAG_EQUAL` / `LEEWAY_LIST_BY_TAG_EQUAL` over `RAGGED_PARENT_IDS` | `value[indexOf(lmv, path)]` |
| UDFs installed | `chpack` + the `LEEWAY_*` read-back family | **none** |
| nulls | no section; dropped | section exists, **deliberately left empty**; dropped and counted |
| key | `ORDER BY ts` | `ORDER BY tuple()` |

`ORDER BY tuple()` matches the reference controls A0/A00 rather than arm A: a
store holding a mixture of document shapes cannot sort on paths most of its
rows do not carry (README §4). Re-keying is a separate lever and was not
exercised in this run.

## Deviations from the pinned run discipline

1. **Cold runs are measured** where stated — the scoped
   `NOPASSWD: /usr/bin/tee /proc/sys/vm/drop_caches` grant is in place, so
   `DROP_CACHES=1` reproduces upstream's procedure: cache dropped once before
   each query's three tries, cold = try 1, hot = min(try 2, try 3).
2. **Server timezone is Europe/Zurich, not UTC**, so Q3's hour buckets are
   shifted relative to upstream's published rows. Cross-arm comparison on this
   server is unaffected.
3. **The ingest is sharded.** The 10M run's logbook entry names the
   single-process ingest as the thing to parallelise first; this run splits the
   corpus across 8 processes writing to one table. The string/symbol routing is
   **pinned** rather than sampled per shard — it is a corpus-level judgement,
   and shards inferring it independently could place the same path in different
   sections. The pinned list is the one the sampler produced from file 1, which
   is also what the 10M table was built with.
4. **No comparison table was loaded.** This run measures what the canonical
   mapping costs and what it can express; it does not re-measure ClickHouse's
   native JSON type. Ratios against arms A/A0/A00/B are taken from the
   [10M run](../2026-08-06-m4-10m/) (as summarised in
   [README §5](../../README.md)) and are therefore **cross-tier**
   wherever this run reports 100M numbers — they are labelled where used.
