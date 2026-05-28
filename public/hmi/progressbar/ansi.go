//go:build llm_generated_opus47

package progressbar

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

// ANSI/TTY rendering for Bar. Kept in a separate file so the core Bar state
// (counter, estimator, lifecycle) stays free of terminal concerns and can be
// reused from non-CLI contexts such as the egui2 demo.
//
// Robustness notes:
//   - Terminal width is polled (not signal-driven) at each render via
//     term.GetSize on stderr. Cheap at 250 ms cadence; no SIGWINCH wiring.
//   - Every TTY emit ends with \033[K to erase any residue from a previously
//     longer render, in place of the old trailing-space padding hack.
//   - Every line is rune-truncated to termWidth-1 before emit so the output
//     never wraps (the -1 headroom avoids right-edge auto-wrap quirks).
//   - Bar width adapts to terminal width: ~1/3 of the screen, clamped
//     [minBarWidth, maxBarWidth].
//
// A terminal resize mid-render may leave one frame of stale content on rows
// the previous wider render wrapped onto; the next render is clean.
// "Robust, not pretty" — we don't track+erase the wrapped region.

const (
	maxBarWidth  = 30
	minBarWidth  = 5
	spinnerChars = `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`
	ansiClearEOL = "\x1b[K"
)

func stderrIsTTY() bool {
	fd := os.Stderr.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

// queryTermWidth returns the current terminal column count for stderr, or 0
// on non-TTY / query failure. Called each render tick — cheap, no caching.
func queryTermWidth() (cols int) {
	cols, _, err := term.GetSize(int(os.Stderr.Fd()))
	if err != nil || cols <= 0 {
		return 0
	}
	return cols
}

// barWidthFor picks an adaptive bar width. termWidth=0 means unknown / non-TTY,
// in which case we use the default max.
func barWidthFor(termWidth int) (w int) {
	if termWidth <= 0 {
		return maxBarWidth
	}
	w = termWidth / 3
	if w > maxBarWidth {
		w = maxBarWidth
	}
	if w < minBarWidth {
		w = minBarWidth
	}
	return w
}

// truncateToWidth returns a rune-truncated prefix of s that fits within
// termWidth-1 cells (using rune count as a cell-width approximation — correct
// for ASCII + our Unicode block/braille chars; off by up to 2 for CJK runes
// that callers pass through DetailFunc). Width 0 means unknown → no truncation.
func truncateToWidth(s string, termWidth int) string {
	if termWidth <= 0 {
		return s
	}
	limit := termWidth - 1
	if limit <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

// emitInline writes \r<truncated>\033[K so the cursor returns to column 0,
// the payload prints within the terminal width, and any stale residue to the
// right of it (from a previous longer render) is cleared.
func emitInline(inst *Bar, line string, termWidth int) {
	line = truncateToWidth(line, termWidth)
	_, _ = fmt.Fprint(inst.w, "\r", line, ansiClearEOL)
}

// renderANSI draws the current bar state to inst.w. On a TTY it uses a
// carriage-return inline update with Unicode block chars + spinner; off TTY
// it emits throttled plain lines.
func (inst *Bar) renderANSI(n int64, elapsed time.Duration, detail string) {
	runes := []rune(spinnerChars)
	spinIdx := int(elapsed.Milliseconds()/80) % len(runes)
	spinner := string(runes[spinIdx])

	termWidth := 0
	barW := maxBarWidth
	if inst.isTTY {
		termWidth = queryTermWidth()
		barW = barWidthFor(termWidth)
	}

	if inst.total <= 0 {
		inst.renderIndeterminateANSI(n, elapsed, spinner, detail, termWidth, barW)
	} else {
		inst.renderDeterminateANSI(n, elapsed, spinner, detail, termWidth, barW)
	}
}

func (inst *Bar) renderIndeterminateANSI(n int64, elapsed time.Duration, spinner string, detail string, termWidth int, barW int) {
	if !inst.isTTY {
		if n%100 == 0 {
			line := fmt.Sprintf("%d %s  %s", n, inst.label, FormatDuration(elapsed))
			if detail != "" {
				line += "  " + detail
			}
			_, _ = fmt.Fprintln(inst.w, line)
		}
		return
	}

	pos := int(elapsed.Milliseconds()/120) % (barW * 2)
	if pos >= barW {
		pos = barW*2 - pos - 1
	}
	var bar strings.Builder
	for i := 0; i < barW; i++ {
		dist := pos - i
		if dist < 0 {
			dist = -dist
		}
		switch {
		case dist == 0:
			bar.WriteString("█")
		case dist == 1:
			bar.WriteString("▓")
		case dist == 2:
			bar.WriteString("░")
		default:
			bar.WriteString(" ")
		}
	}

	line := fmt.Sprintf("%s [%s] %d %s  %s",
		spinner, bar.String(), n, inst.label, FormatDuration(elapsed))
	if detail != "" {
		line += "  " + detail
	}
	emitInline(inst, line, termWidth)
}

func (inst *Bar) renderDeterminateANSI(n int64, elapsed time.Duration, spinner string, detail string, termWidth int, barW int) {
	_ = elapsed
	pct := float64(n) / float64(inst.total) * 100

	if !inst.isTTY {
		if n%500 == 0 || n == inst.total {
			line := fmt.Sprintf("[%d/%d] %.0f%%", n, inst.total, pct)
			if detail != "" {
				line += "  " + detail
			}
			_, _ = fmt.Fprintln(inst.w, line)
		}
		return
	}

	filled := int(float64(barW) * pct / 100)
	if filled > barW {
		filled = barW
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barW-filled)

	etaStr := "—"
	remaining := float64(inst.total) - float64(n)
	eta, valid := inst.eta.EstimateETA(remaining)
	if valid {
		etaStr = FormatETA(eta)
	}

	line := fmt.Sprintf("%s %s %5.1f%% %d/%d", spinner, bar, pct, n, inst.total)
	if detail != "" {
		line += "  " + detail
	}
	line += "  ETA " + etaStr
	emitInline(inst, line, termWidth)
}

// finalizeLineLocked emits the trailing newline after the bar's last in-place
// render. Caller must hold inst.writeMu.
func (inst *Bar) finalizeLineLocked() {
	if inst.isTTY {
		_, _ = fmt.Fprintln(inst.w)
	}
}
