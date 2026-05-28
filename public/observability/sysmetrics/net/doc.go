//go:build llm_generated_opus47

// Package net samples per-interface network statistics — link state,
// IPv4/IPv6 addresses, MAC, and rx/tx byte counters with derived rates —
// from /sys/class/net and the standard library's [net.Interfaces]
// (a thin wrapper over getifaddrs(3)).
//
// The collector is stateful: rate fields are derived from byte-counter
// deltas vs. the prior [Sample] call. The first call after [New] reports
// the cumulative byte counters with rates of zero; subsequent calls
// report meaningful per-second rates.
//
// Provenance: btop src/linux/btop_collect.cpp:2676-2867 (Net::collect).
// Counter rollover handling at btop_collect.cpp:2760-2767 — when a
// kernel-exposed counter wraps (some virtual NICs expose a 32-bit
// rx_bytes), the rollover is folded into a per-interface offset so
// the cumulative total continues to grow monotonically.
//
// # Usage
//
//	c := net.New(net.Options{})
//	for {
//	    snap, err := c.Sample(ctx)
//	    if err != nil { /* ... */ }
//	    for _, ifc := range snap.Interfaces {
//	        fmt.Printf("%s rx=%d B/s tx=%d B/s\n", ifc.Name, ifc.RxBytesPerSec, ifc.TxBytesPerSec)
//	    }
//	    time.Sleep(time.Second)
//	}
//
// # See also
//
//   - doc/adr/0019-observability-sysmetrics-linux-collector.md (M2).
package net
