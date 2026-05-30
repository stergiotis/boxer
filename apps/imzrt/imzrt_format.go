//go:build llm_generated_opus48

package imzrt

import "fmt"

const bytesPerMiB = 1024 * 1024

// mib converts a byte count to MiB. History series are stored in MiB so plot
// axes can be labelled "MiB" without per-frame scaling.
func mib(b uint64) (v float64) {
	v = float64(b) / bytesPerMiB
	return
}

// humanBytes renders a byte count with a binary unit suffix. Copy of imztop's
// helper (apps/imztop/imztop_panel_mem.go) — see SD13's duplication note for the
// same rationale applied to SlidingWindow.
func humanBytes(n uint64) (s string) {
	const (
		kib = 1 << 10
		mib = 1 << 20
		gib = 1 << 30
		tib = 1 << 40
	)
	switch {
	case n >= tib:
		s = fmt.Sprintf("%.2f TiB", float64(n)/float64(tib))
	case n >= gib:
		s = fmt.Sprintf("%.2f GiB", float64(n)/float64(gib))
	case n >= mib:
		s = fmt.Sprintf("%.1f MiB", float64(n)/float64(mib))
	case n >= kib:
		s = fmt.Sprintf("%.0f KiB", float64(n)/float64(kib))
	default:
		s = fmt.Sprintf("%d B", n)
	}
	return
}

// humanCount renders a cardinal count (goroutines, objects) with a decimal
// suffix so wide values stay compact in the dense top bar.
func humanCount(n uint64) (s string) {
	switch {
	case n >= 1_000_000_000:
		s = fmt.Sprintf("%.1fG", float64(n)/1e9)
	case n >= 1_000_000:
		s = fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 10_000:
		s = fmt.Sprintf("%.1fk", float64(n)/1e3)
	default:
		s = fmt.Sprintf("%d", n)
	}
	return
}
