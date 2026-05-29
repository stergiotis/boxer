//go:build llm_generated_opus47

// Package imztop is a btop-style desktop resource monitor built on
// ImZero2 + egui2, consuming the in-repo `observability/sysmetrics`
// data layer. Wired as the imzero2 demo subcommand `appCode == 7`.
//
// The package is read-only against sysmetrics. There is no process
// write-side (kill / nice / signal) by design — see ADR-0020 SD11.
//
// # Architecture
//
// One sampler goroutine owns a *sysmetrics.Bundle and ticks at
// SamplerOptions.UpdateInterval (default 1 s). Each tick it calls
// Bundle.Sample, appends the result to per-series ring buffers, then
// publishes a fresh PublishedSnapshot via atomic.Pointer. The egui
// frame loop reads the latest snapshot via atomic.Load and re-slices
// stable ring backing memory — no allocation on the hot path.
//
// # See also
//
//   - doc/adr/0020-imzero2-imztop-resource-monitor.md — accepted
//     decision and milestone plan.
//   - public/observability/sysmetrics/ — data source.
package imztop
