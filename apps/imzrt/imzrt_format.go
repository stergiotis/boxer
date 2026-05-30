package imzrt

import (
	"fmt"

	"github.com/stergiotis/boxer/public/observability/humanfmt"
)

const bytesPerMiB = 1024 * 1024

// mib converts a byte count to MiB. History series are stored in MiB so plot
// axes can be labelled "MiB" without per-frame scaling.
func mib(b uint64) (v float64) {
	v = float64(b) / bytesPerMiB
	return
}

// humanBytes renders a byte count with a binary unit suffix. It delegates to
// the shared formatter lifted out of imzrt + imztop per ADR-0061 SD13.
func humanBytes(n uint64) (s string) {
	return humanfmt.Bytes(n)
}

// humanDuration renders a duration given in seconds with an adaptive unit, so GC
// pauses (typically µs–ms) read naturally.
func humanDuration(sec float64) (s string) {
	switch {
	case sec <= 0:
		s = "0"
	case sec < 1e-6:
		s = fmt.Sprintf("%.0f ns", sec*1e9)
	case sec < 1e-3:
		s = fmt.Sprintf("%.1f µs", sec*1e6)
	case sec < 1:
		s = fmt.Sprintf("%.2f ms", sec*1e3)
	default:
		s = fmt.Sprintf("%.2f s", sec)
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
