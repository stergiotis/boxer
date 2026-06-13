// Package sensors enumerates Linux temperature sensors via the /sys/class/
// hwmon subsystem. Each TempReading is one hwmon temp*_input file paired
// with its label, critical threshold, and a label-based hint of whether
// the reading is a CPU package or per-core temperature.
//
// The package returns every hwmon temperature sensor discovered, including
// per-disk SMART temperatures (NVMe, SATA). Consumers wanting CPU-only
// readings filter on KindCPUPackage / KindCPUCore.
//
// Provenance: btop src/linux/btop_collect.cpp:470-598 (Cpu::get_sensors).
// Two simplifications versus upstream:
//   - we do not canonicalize hwmon paths via symlink resolution. In
//     practice the same physical hwmon never appears twice under
//     /sys/class/hwmon, so the deduplication step is unnecessary.
//   - the /sys/devices/platform/coretemp.0/hwmon and
//     /sys/class/thermal/thermal_zone fallbacks btop reaches for when
//     hwmon is empty are deferred to a later milestone — modern kernels
//     surface coretemp via /sys/class/hwmon directly.
//
// # See also
//
//   - doc/adr/0019-observability-sysmetrics-linux-collector.md (M1).
package sensors
