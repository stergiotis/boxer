---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Environment — 2026-10-02-first-run

Written by hand: this run preceded `measure.sh`, which ran the same tests the
same way for the repeat run.

- boxer commit: d7518efc (the Map pane and portolan unmodified)
- ClickHouse: 26.8.1.1939 — auto(8) max_threads
- Tables: planes_mercator 1385478 rows, planes_mercator_sample10 138714 rows, planes_mercator_sample100 13885 rows
- Go: go1.27.0 linux/amd64
- CPU: AMD Custom APU 0932, 8 threads
- Memory: 14 GiB total, about 3.5 GiB available, swap in use
- Kernel: Linux 6.11.11-valve29-1-neptune-611-g2dcfaf4df7ac
- Load average before the run: 5.86 3.66 3.17 (other sessions active)
- Load average after the run: 4.23 3.75 3.25
