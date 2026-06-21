// Package sysmsnap holds the system-metrics data types — the per-domain
// snapshot structs, the [BundleSnapshot] union, the static CPU [Topology], and
// the [Domain] enum — with no collector code.
//
// It is the data half of the sysmetrics split (ADR-0090 SD6): the /proc- and
// /sys-reading collectors live under public/observability/sysmetrics/<domain>
// and depend on this package for their result types, while consumers (the
// sysmetricsbus codec/consumer and apps/imztop) depend on this package alone.
// Importing a metric type therefore no longer drags in the collectors, so a
// pure subscriber holds no system-state capability.
//
// Nothing here reads the filesystem or imports a collector; the package is a
// zero-dependency vocabulary leaf (stdlib only) and is WASM-clean on every
// target.
package sysmsnap
