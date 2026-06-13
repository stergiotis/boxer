// Package proc samples the Linux process table by walking
// /proc/[pid]/{comm,cmdline,stat,statm,status} and yielding one [Info]
// per visible process.
//
// The collector is stateful: per-process CPU percentages are derived
// from the prior [Sample]'s utime+stime delta vs. wall-clock. The first
// call after [New] reports zero CPU%; subsequent calls produce
// meaningful per-CPU rates (a process pegging one core reads as 100%).
//
// Both [Collector.Sample] and [Collector.All] eagerly snapshot the
// entire /proc tree before returning — early break out of the iterator
// does not corrupt prior-tick state. Memory cost is bounded by the live
// process count (typically <1000 processes / a few hundred KB of
// pre-sized slices).
//
// Provenance: btop src/linux/btop_collect.cpp:2872-3296 (Proc::collect).
// Three simplifications versus upstream:
//
//   - We do not chase the /proc/[pid]/stat "RSS looks wrong, re-read
//     statm" fallback. Modern kernels report sane RSS in stat; the
//     fallback only fires when totalMem is known up-front (a TUI
//     concept).
//   - No "dead process" preservation. btop keeps zombies for one frame;
//     we drop them as soon as /proc no longer lists them. A consumer
//     that needs zombie tracking can diff successive snapshots.
//   - No process tree / sort / filter helpers. Consumers build trees
//     from [Info.PID]/[Info.PPID] downstream.
//
// # See also
//
//   - doc/adr/0019-observability-sysmetrics-linux-collector.md (M3).
package proc
