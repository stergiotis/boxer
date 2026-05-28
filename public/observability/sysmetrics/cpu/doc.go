//go:build llm_generated_opus47

// Package cpu samples Linux CPU state — totals, per-core utilization,
// load averages, frequency, RAPL power, and the cgroup-effective cpuset
// — into a single [Snapshot] per sample.
//
// The collector is stateful: utilization percentages and watts are derived
// from /proc/stat and /sys/class/powercap deltas against the prior call.
// The first call after [New] therefore reports zero utilization and zero
// watts; subsequent calls report meaningful values.
//
// CPU temperatures are intentionally NOT exposed here. Wire a separate
// `sensors.Collector` (or use the root `sysmetrics.Bundle`) to read
// hwmon thermal data alongside CPU metrics.
//
// Provenance: btop src/linux/btop_collect.cpp (Cpu namespace, line ~410
// onwards). The Linux subset only — macOS / BSD branches are not mirrored.
//
// # Usage
//
//	c, err := cpu.New(cpu.Options{})
//	if err != nil { /* ... */ }
//	for {
//	    snap, err := c.Sample(ctx)
//	    if err != nil { /* ... */ }
//	    fmt.Println(snap.TotalPercent, "%")
//	    time.Sleep(time.Second)
//	}
//
// # See also
//
//   - doc/adr/0019-observability-sysmetrics-linux-collector.md (M1).
package cpu
