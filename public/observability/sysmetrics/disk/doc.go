//go:build llm_generated_opus47

// Package disk samples filesystem mounts and per-block-device I/O state.
//
// Two complementary views land in each [Snapshot]:
//
//   - [Mount] describes one /proc/self/mounts entry: device, mountpoint,
//     filesystem type, and the [Capacity] computed via `statvfs(3)`.
//   - [BlockDevice] describes one /sys/class/block/{name}/stat counter
//     line: cumulative-read-rate, cumulative-write-rate, and the
//     io_ticks-derived busy percent. Multiple Mounts that share a block
//     device (bind mounts, multiple partitions, etc.) reference the
//     same BlockDevice via [Mount.BlockName].
//
// The collector is stateful: I/O rate fields are derived from the prior
// [Sample]'s sector counters and io_ticks. The first call after [New]
// reports zero rates; subsequent calls report deltas / wall-clock.
//
// Provenance: btop src/linux/btop_collect.cpp:2210-2521. The "real fs"
// filter uses /proc/filesystems; ZFS pool I/O via
// /proc/spl/kstat/zfs/{pool}/io is deferred to a follow-up milestone.
//
// # See also
//
//   - doc/adr/0019-observability-sysmetrics-linux-collector.md (M2).
package disk
