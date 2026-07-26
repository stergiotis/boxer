package play

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

// play_progress.go — what play DISPLAYS of a run's live progress.
//
// The wire half of ADR-0115 plane A moved out: reading
// `X-ClickHouse-Progress` lines off a still-open response-header block is
// the ClickHouse HTTP engine's business, and it lives with that engine's
// adapter (queryengine/chserver). What is left here is the lane selection
// and the one-line rendering, which is play's.
//
// The ticks arrive as [runstream.Progress] — the five counters both progress
// producers can actually see. That is not a coincidence: the frame contract
// picked that shape so an in-band tick and a tick polled from
// system.processes by somebody who never held the connection are the same
// value, and this renderer cannot tell them apart.

// activeProgress returns the live tick of the lane the result panels
// observe — the intermediate lane when an intermediate node is observed
// (mirroring activeSnapshot's selection without issuing a demand), the
// `main` lane otherwise. Render-thread-only.
func (inst *PlayApp) activeProgress() (p runstream.Progress, fresh bool) {
	split := inst.currentSplit
	if inst.observedNode != "" && inst.observedNode != split.Sink && len(split.Nodes) > 0 {
		return inst.intermediateLane.progressView()
	}
	return inst.graph.MainProgress()
}

// formatProgressLine renders one tick for the status bar and the loading
// empty-state: rows (with a percentage when the server knows the total),
// bytes read, peak memory, elapsed.
func formatProgressLine(p runstream.Progress) string {
	var b strings.Builder
	b.WriteString(humanCount(p.ReadRows))
	if p.TotalRowsToRead > 0 {
		fmt.Fprintf(&b, " / %s rows (%d%%)", humanCount(p.TotalRowsToRead),
			min(100, p.ReadRows*100/p.TotalRowsToRead))
	} else {
		b.WriteString(" rows")
	}
	b.WriteString(" · ")
	b.WriteString(humanBytes(p.ReadBytes))
	b.WriteString(" read")
	if p.MemoryUsage > 0 {
		b.WriteString(" · mem ")
		b.WriteString(humanBytes(p.MemoryUsage))
	}
	if p.ElapsedNs > 0 {
		b.WriteString(" · ")
		b.WriteString((time.Duration(p.ElapsedNs) * time.Nanosecond).Round(100 * time.Millisecond).String())
	}
	return b.String()
}

// humanCount renders a row count with K/M/B suffixes (counts, unlike
// bytes, conventionally use decimal thousands).
func humanCount(n uint64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	default:
		return strconv.FormatUint(n, 10)
	}
}
