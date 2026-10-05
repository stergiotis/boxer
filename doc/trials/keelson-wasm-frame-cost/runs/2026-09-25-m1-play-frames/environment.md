---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Environment

- date: 2026-09-25T20:26:45Z
- what: M1 — real play frames counted by `fffi2stub tee` between the Go host and the CPU-rasterizer headless client, driven by the scene runner; two M1 scenes (scenes/ in this trial) and the whole maintained play tour (apps/play/scenes, 63 of 80 scenes runnable here)
- build: boxer f99f0a30 with the trial's uncommitted additions; headless-soft client built the same day; scene runner from the tree
- cpu: AMD Custom APU 0932, 8 threads (a handheld-class APU), not idle
- cpufreq: governor powersave, driver amd-pstate-epp
- clickhouse: the local server; scenes needing anchor.facts were skipped (table empty here)
- files: scenes.tsv (one row per scene: messages and bytes per frame over the scene's frames), opcodes/<scene>.tsv (messages and bytes per frame by opcode), m1_play_table_*.frames.tsv (per frame, the two M1 scenes)
