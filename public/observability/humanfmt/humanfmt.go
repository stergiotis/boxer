// Package humanfmt renders metric quantities as compact human-readable strings
// for the runtime-dashboard apps (imzrt, imztop), which each previously carried
// a verbatim copy of Bytes (ADR-0061 SD13, same rationale as the SlidingWindow
// lift).
package humanfmt

import "fmt"

// Bytes renders a byte count with a binary (IEC) unit suffix — B, KiB, MiB, GiB,
// TiB. Precision widens with magnitude (whole KiB, one MiB decimal, two for
// GiB/TiB) so dense dashboard cells stay compact while large values keep
// resolution.
func Bytes(n uint64) (s string) {
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
