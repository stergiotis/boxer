//go:build llm_generated_opus48

// Package psi samples Linux Pressure Stall Information (PSI) from
// /proc/pressure/{cpu,memory,io} — the share of wall-time tasks spent stalled
// on each resource, averaged over 10/60/300 s windows. PSI is the kernel's
// "is this resource the bottleneck" signal: unlike utilisation it distinguishes
// a resource that is merely busy from one that is contended.
//
// A kernel built without CONFIG_PSI (or booted with psi=0) has no
// /proc/pressure tree; [Collector.Sample] then returns a Snapshot with
// Available=false rather than an error, so callers degrade gracefully.
//
// # See also
//
//   - doc/adr/0019-observability-sysmetrics-linux-collector.md
package psi
