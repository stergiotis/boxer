package progressbar

import (
	"fmt"
	"time"
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
		m := ((total%3600)+150)/300*5
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
