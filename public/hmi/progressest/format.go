package progressest

import (
	"fmt"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
)

func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%02ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// FormatETA formats an ETA duration with reduced precision at larger magnitudes.
// Under 10 minutes: full resolution. 10–60 min: nearest minute. Over 1 hour:
// nearest 5 minutes. Coarser labels reduce perceived wait (Harrison et al. 2007).
func FormatETA(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Seconds())
	switch {
	case total < 60:
		return fmt.Sprintf("%ds", total)
	case total < 600:
		return fmt.Sprintf("%dm%02ds", total/60, total%60)
	case total < 3600:
		m := (total + 30) / 60
		return fmt.Sprintf("~%dm", m)
	default:
		h := total / 3600
		m := ((total % 3600) + 150) / 300 * 5
		if m >= 60 {
			h++
			m = 0
		}
		if m > 0 {
			return fmt.Sprintf("~%dh%02dm", h, m)
		}
		return fmt.Sprintf("~%dh", h)
	}
}

func FormatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// FormatRemaining is the ETA as a readout clause: "<1s left", "2m05s left",
// "~15m left" — FormatETA's precision with the sub-second case spelled out
// rather than shown as "0s".
func FormatRemaining(d time.Duration) string {
	if d < time.Second {
		return "<1s left"
	}
	return FormatETA(d) + " left"
}

// FormatRate spells a rate for a readout that moves: IEC bytes for the unit
// "bytes" ("18 MiB/s"), otherwise an SI magnitude and the unit ("1.2 k rows/s",
// "240 items/s", "0.4 frames/s"; "1.2 k/s" without a unit). Only the magnitude
// matters at a glance, so digits past the first decimal are dropped. Empty for
// a rate that is not positive.
func FormatRate(rate float64, unit string) string {
	if rate <= 0 {
		return ""
	}
	if unit == "bytes" {
		return humanize.IBytes(uint64(rate)) + "/s"
	}
	var mag string
	if rate < 1 {
		// SIWithDigits would spell 0.4 as "400 m" — milli-items.
		mag = fmt.Sprintf("%.1f", rate)
	} else {
		// SIWithDigits pads a bare magnitude with the space its (empty) unit
		// would have taken; trimming keeps "500 rows/s" from doubling it.
		mag = strings.TrimSpace(humanize.SIWithDigits(rate, 1, ""))
	}
	if unit == "" {
		return mag + "/s"
	}
	return mag + " " + unit + "/s"
}
