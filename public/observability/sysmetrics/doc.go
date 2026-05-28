//go:build llm_generated_opus47

// Package sysmetrics is the entry point for the Linux system metrics
// collector tree. Subpackages cover one metric domain each (cpu, mem,
// sensors and — in later milestones — disk, net, proc, battery, container,
// gpu/...). This root package only hosts shared types; callers most often
// import the per-domain packages directly.
//
// The collector layer is non-interactive: no TUI, no global config, no
// logger singleton. It mirrors the Linux subset of btop's data-gathering
// layer (Apache-2.0, ../../../../../contrib/btop/) without taking any of
// btop's UI or runtime architecture.
//
// # See also
//
//   - doc/adr/0019-observability-sysmetrics-linux-collector.md — accepted
//     decision and milestone plan.
//   - doc/observability/sysmetrics/REFERENCE.md — public-API reference.
package sysmetrics
