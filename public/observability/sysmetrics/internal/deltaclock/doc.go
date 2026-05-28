//go:build llm_generated_opus47

// Package deltaclock helps collectors compute delta-rate metrics from
// monotonically-increasing counters sampled over time. It exists so that
// each domain collector (cpu /proc/stat ticks, net rx_bytes, RAPL
// energy_uj, disk I/O sectors) does not re-implement the same
// "previous-vs-current with rollover" arithmetic.
//
// Two surfaces are exposed:
//
//   - [Diff] / [RatePerSecond] — pure functions for one-shot calculation.
//   - [Counter] — a stateful holder that remembers the previous sample.
//
// Counter rollover is configurable per [Counter] (or per [Diff] call) via
// rolloverMax. Pass 0 to disable rollover handling and clamp to zero on a
// backward step (the conservative default for 64-bit kernel counters).
// Pass math.MaxUint32 for the 32-bit /sys/class/net rx_bytes counters
// btop's net_collect handles at src/linux/btop_collect.cpp:2760.
package deltaclock
