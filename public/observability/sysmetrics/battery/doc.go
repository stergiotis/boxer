//go:build llm_generated_opus47

// Package battery samples battery and AC-adapter state from
// /sys/class/power_supply.
//
// The collector is stateless: each [Sample] is a fresh enumeration of
// power_supply entries, classifying them as batteries or AC adapters and
// reading the kernel-exposed scalars. Batteries with `present=0` are
// skipped; entries that are neither Battery, UPS, nor Mains are ignored.
//
// Provenance: btop src/linux/btop_collect.cpp:805-1006 (Battery::get_battery).
// Two simplifications versus upstream:
//
//   - We expose all batteries in the [Snapshot] rather than picking one
//     to display. Consumers select via [BatteryStatus.Name].
//   - AC adapters are surfaced as a separate slice instead of folded into
//     the per-battery state. The kernel's `Mains` device type is the
//     authoritative source; older btop heuristics (AC0/online, AC/online
//     under the battery dir) are not mirrored.
//
// # Unit handling
//
// Some kernels report battery state in energy units (Wh in microWattHours
// — `energy_now`, `energy_full`, `power_now`) and others in charge units
// (Ah in microAmpHours — `charge_now`, `charge_full`, `current_now` +
// `voltage_now`). [Sample] handles both by computing percent from
// whichever ratio is available and prefers the kernel-provided
// `capacity` when present (most accurate; vendor-tuned).
package battery
