---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** A capture of one host on one day.

# Environment — 2026-08-19 M0 run

- ClickHouse: 26.7.3.19, server timezone Europe/Zurich
- rclone: rclone v1.74.3
- Go: go1.26.5-X:nodwarf5 linux/amd64
- repo commit under test: deb4be09
- build tags: boxer_enable_profiling,goexperiment.jsonv2
- CPU: 32 logical cores, AMD RYZEN AI MAX+ PRO 395 w/ Radeon 8060S
- memory: 93 GiB
- storage: ClickHouse data on the host's local filesystem
- blake3: lukechampine.com/blake3 v1.4.1

Server settings that the checks read:
   ┌─name─────────────────────────────────┬─value───┐
1. │ min_compress_block_size              │ 65536   │
2. │ max_compress_block_size              │ 1048576 │
3. │ max_insert_block_size                │ 1048449 │
4. │ join_use_nulls                       │ 0       │
5. │ lightweight_mutation_projection_mode │ throw   │
   └──────────────────────────────────────┴─────────┘
   ┌─name───────────────────┬─value─┐
1. │ merge_with_ttl_timeout │ 14400 │
2. │ ttl_only_drop_parts    │ 0     │
   └────────────────────────┴───────┘
