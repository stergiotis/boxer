// Package goruntime reads the running process's own Go runtime metrics
// (runtime/metrics) into a caller-owned [Snapshot]. It is the portable
// counterpart to the Linux-only sysmetrics collector: the same Bundle-style
// "fill a snapshot" shape (see [Collector.Read]) over the platform-independent
// runtime/metrics surface — heap occupancy, GC accounting, scheduler state.
//
// Portability: the collector carries no platform or feature build tags. It runs
// on every GOOS/GOARCH the Go toolchain supports.
//
// Version tolerance: the curated metric set is intersected with metrics.All()
// at construction, so a metric that a given Go release does not expose is simply
// not requested and its [Snapshot] field stays zero. [Snapshot.Missing] reports
// how many curated metrics were absent.
//
// Observer effect: this collector measures the process it runs in. [Collector.Read]
// is built to perturb that process as little as possible — it reuses its sample
// buffer and the histogram slice backings, allocating nothing in steady state,
// and it reads exclusively through runtime/metrics (which, unlike
// runtime.ReadMemStats, does not stop the world). See ADR-0061 for the wider
// dashboard this feeds.
package goruntime
