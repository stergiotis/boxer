package play

import (
	"fmt"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/hmi/progressbar"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// play_progress.go — what play MAKES of a run's live progress, and what it
// shows of it.
//
// The wire half of ADR-0115 plane A moved out: reading
// `X-ClickHouse-Progress` lines off a still-open response-header block is
// the ClickHouse HTTP engine's business, and it lives with that engine's
// adapter (queryengine/chserver). What is left here is the lane selection,
// the estimation, and the rendering, which are play's.
//
// The ticks arrive as [runstream.Progress] — the five counters both progress
// producers can actually see. That is not a coincidence: the frame contract
// picked that shape so an in-band tick and a tick polled from
// system.processes by somebody who never held the connection are the same
// value, and this renderer cannot tell them apart.
//
// A tick on its own says how far the server has got, not how long the rest
// will take. [progressTracker] folds the ticks into a
// [progressbar.Estimator] (Holt's double exponential smoothing plus display
// damping — the estimator the CLI progress bar uses) so the counters gain a
// smoothed rate and a non-oscillating ETA. One tracker per OBSERVED LANE,
// driven once per frame: the app's follows `main`/the observed intermediate,
// and a panel that runs its own node on its own [nodeLane] (ADR-0097 SD5) keeps
// its own — see renderLaneProgress. Every display site reads the resulting
// [progressView] rather than the raw tick.

// progressView is one frame's answer about the run the panels are waiting
// for: the raw tick plus what the estimator makes of it. The zero value
// means "nothing in flight", which is what every display site checks.
type progressView struct {
	// fresh mirrors the lane's own gate: a tick has landed for a run that
	// is still going. Everything below is meaningless without it.
	fresh bool
	p     runstream.Progress
	// knownTotal says the server reported a total to divide by. Without one
	// there is no fraction and no ETA — only rows, bytes and a rate.
	knownTotal bool
	fraction   float32 // 0..1 for the bar
	percent    uint64  // 0..100, integer-exact for the text line
	// rate is the estimator's smoothed level in rows/s. Zero until two
	// ticks have landed (or when the smoothing dips below zero on a stall).
	rate float64
	// eta is the DAMPED estimate: decreases pass through, small increases
	// are suppressed, large ones break through — see progressbar's
	// EXPLANATION.md. etaValid is false while the estimator warms up.
	eta      time.Duration
	etaValid bool
}

// progressTracker turns the active lane's ticks into a [progressView]. It is
// render-thread-only state and holds one estimator, re-anchored whenever the
// run underneath it changes: a different lane, a row count that went
// backwards, or a run clock that restarted. Without that re-anchor the
// previous run's level and trend bias the new run's first estimates.
type progressTracker struct {
	est  *progressbar.Estimator
	lane string
	// anchor + useElapsed define the estimator's clock. The server's own
	// ElapsedNs is preferred over wall time: it is the clock the counters
	// were sampled against, so a frame that re-reads an unchanged tick
	// contributes dt=0 (which the estimator skips) instead of a spurious
	// zero-rate sample. Endpoints that report no elapsed fall back to wall
	// time, where the same protection comes from the changed-tick gate.
	anchor      time.Time
	useElapsed  bool
	lastRows    uint64
	lastElapsed uint64
	tracking    bool
}

// observe folds one frame's tick into the estimator and returns what the
// display sites should draw. Call it exactly once per frame: the damped ETA
// is stateful, and feeding it twice with the same remaining work is
// harmless only by accident of the damping rule.
func (inst *progressTracker) observe(now time.Time, lane string, p runstream.Progress, fresh bool) (v progressView) {
	if !fresh {
		// The lane gates its tick off when the run lands or is superseded;
		// drop the tracking so the next run re-anchors rather than
		// continuing this one's level.
		inst.tracking = false
		return
	}
	if inst.est == nil {
		inst.est = progressbar.NewEstimator()
	}
	switch {
	case !inst.tracking || lane != inst.lane ||
		p.ReadRows < inst.lastRows || p.ElapsedNs < inst.lastElapsed:
		// A new run under the same fresh gate: rows or the run clock went
		// backwards, or the observed lane changed.
		inst.tracking = true
		inst.lane = lane
		inst.anchor = now
		inst.useElapsed = p.ElapsedNs > 0
		inst.est.Reset(inst.sampleTime(now, p), int64(p.ReadRows))
	case p.ReadRows != inst.lastRows || p.ElapsedNs != inst.lastElapsed:
		// A genuinely new tick. Re-reading the same one (the common case —
		// ticks land every ~250 ms, frames every ~16 ms) must not be folded
		// in as a zero-rate sample.
		inst.est.Update(inst.sampleTime(now, p), int64(p.ReadRows))
	}
	inst.lastRows, inst.lastElapsed = p.ReadRows, p.ElapsedNs

	v.fresh = true
	v.p = p
	if rate := inst.est.SmoothedRate(); rate > 0 {
		v.rate = rate
	}
	if p.TotalRowsToRead > 0 {
		v.knownTotal = true
		done := min(p.ReadRows, p.TotalRowsToRead)
		v.percent = done * 100 / p.TotalRowsToRead
		v.fraction = float32(float64(done) / float64(p.TotalRowsToRead))
		v.eta, v.etaValid = inst.est.EstimateETA(float64(p.TotalRowsToRead - done))
	}
	return
}

// sampleTime is the estimator's clock for one tick — see progressTracker's
// anchor/useElapsed.
func (inst *progressTracker) sampleTime(now time.Time, p runstream.Progress) time.Time {
	if inst.useElapsed {
		return inst.anchor.Add(time.Duration(p.ElapsedNs) * time.Nanosecond)
	}
	return now
}

// activeProgress returns the live tick of the lane the result panels
// observe — the intermediate lane when an intermediate node is observed
// (mirroring activeSnapshot's selection without issuing a demand), the
// `main` lane otherwise. The lane id is the tracker's re-anchor witness.
// Render-thread-only.
func (inst *PlayApp) activeProgress() (lane string, p runstream.Progress, fresh bool) {
	split := inst.currentSplit
	if inst.observedNode != "" && inst.observedNode != split.Sink && len(split.Nodes) > 0 {
		p, fresh = inst.intermediateLane.progressView()
		return string(inst.observedNode), p, fresh
	}
	p, fresh = inst.graph.MainProgress()
	return string(mainNodeID), p, fresh
}

// syncProgress folds this frame's tick into the tracker. Called once from
// Render, before any display site reads inst.frameProgress.
func (inst *PlayApp) syncProgress() {
	lane, p, fresh := inst.activeProgress()
	inst.frameProgress = inst.progress.observe(time.Now(), lane, p, fresh)
}

// formatProgressLine renders one tick for the status bar and the loading
// empty-state: rows (with a percentage when the server knows the total),
// bytes read, the smoothed rate, the damped ETA, peak memory, elapsed.
func formatProgressLine(v progressView) string {
	p := v.p
	var b strings.Builder
	b.WriteString(countAtAGlance(p.ReadRows))
	if v.knownTotal {
		fmt.Fprintf(&b, " / %s rows (%d%%)", countAtAGlance(p.TotalRowsToRead), v.percent)
	} else {
		b.WriteString(" rows")
	}
	b.WriteString(" · ")
	b.WriteString(humanize.IBytes(p.ReadBytes))
	b.WriteString(" read")
	if v.rate >= 1 {
		b.WriteString(" · ")
		b.WriteString(formatRate(v.rate))
	}
	if v.etaValid {
		b.WriteString(" · ETA ")
		b.WriteString(progressbar.FormatETA(v.eta))
	}
	if p.MemoryUsage > 0 {
		b.WriteString(" · mem ")
		b.WriteString(humanize.IBytes(p.MemoryUsage))
	}
	if p.ElapsedNs > 0 {
		b.WriteString(" · ")
		b.WriteString((time.Duration(p.ElapsedNs) * time.Nanosecond).Round(100 * time.Millisecond).String())
	}
	return b.String()
}

// formatProgressBrief is the short form for the top bar, where the toolbar
// has no room for the full line: the percentage the bar cannot legibly carry
// itself, plus the ETA — or the rate while the ETA warms up (or forever, on a
// query whose total the server never reports).
func formatProgressBrief(v progressView) string {
	var b strings.Builder
	if v.knownTotal {
		fmt.Fprintf(&b, "%d%%", v.percent)
	}
	tail := ""
	switch {
	case v.etaValid:
		tail = "ETA " + progressbar.FormatETA(v.eta)
	case v.rate >= 1:
		tail = formatRate(v.rate)
	case !v.knownTotal:
		tail = countAtAGlance(v.p.ReadRows) + " rows"
	}
	if tail != "" {
		if b.Len() > 0 {
			b.WriteString(" · ")
		}
		b.WriteString(tail)
	}
	return b.String()
}

// formatProgressStrip is the pane strip's line: the numbers a reader of a
// pane that is about to be replaced wants, without the memory/elapsed tail
// the status bar carries.
func formatProgressStrip(v progressView) string {
	var b strings.Builder
	b.WriteString(countAtAGlance(v.p.ReadRows))
	if v.knownTotal {
		b.WriteString(" / ")
		b.WriteString(countAtAGlance(v.p.TotalRowsToRead))
	}
	b.WriteString(" rows")
	if v.rate >= 1 {
		b.WriteString(" · ")
		b.WriteString(formatRate(v.rate))
	}
	if v.etaValid {
		b.WriteString(" · ETA ")
		b.WriteString(progressbar.FormatETA(v.eta))
	}
	return b.String()
}

// Bar footprints. The toolbar's is compact enough to sit beside Cancel
// without pushing the rest of the strip around; the pane and empty-state
// bars can afford more. All three are explicit because egui's ProgressBar
// otherwise claims the whole available width, which inside a Horizontal
// means everything to its right.
const (
	topBarProgressWidth  = 150
	paneProgressWidth    = 190
	loadingProgressWidth = 260
)

// renderProgressBar emits the bar itself. With a total to divide by it is
// determinate; without one it is an animated zero-progress bar — egui draws a
// minimum-width pill with a rotating arc, which reads as "running, no idea
// how far" rather than as "0% done".
//
// Deliberately no ShowPercentage: egui paints the bar's own text at the
// bar's LEFT edge, which is exactly where the fill pill sits early in a run,
// so "1%" renders as a blob followed by "%" (seen live). The percentage
// rides the adjacent label instead, where it is legible at every value.
func renderProgressBar(v progressView, width float32) {
	if v.knownTotal {
		c.ProgressBar(v.fraction).DesiredWidth(width).Send()
		return
	}
	c.ProgressBar(0).DesiredWidth(width).Animate(true).Send()
}

// renderTopBarProgress is the always-visible half of the display (the
// placement decision of 2026-07-28): a bar beside Cancel, so an in-flight run
// is legible from any tab and whether or not the panes already hold a
// result. Emitted inside the top bar's Horizontal.
//
// It draws the OBSERVED lane's tick, while the Cancel it sits beside acts on
// `main`. The two are the same lane in every ordinary flow — a Run resets the
// observed node to the new sink — and diverge only if an intermediate is
// picked from the Graph view mid-run, where both readings still mean "a query
// is running".
func (inst *PlayApp) renderTopBarProgress() {
	v := inst.frameProgress
	if !v.fresh {
		// No tick yet (a run that just started, or an endpoint that streams
		// none — chlocal, mocks): the spinner beside us is the whole signal.
		return
	}
	renderProgressBar(v, topBarProgressWidth)
	diagWeak(formatProgressBrief(v))
}

// renderPaneProgressStrip is the in-pane half: a slim bar above a result
// pane that is being replaced. It exists because a re-run over a populated
// pane used to be invisible there — the pane kept showing the previous
// result with nothing to say a new one was on the way, and the empty-state
// spinner (renderResultsLoading) is reached only when there is no result at
// all.
//
// numbers is false for a pane bound to another node's lane (slice 6c): that
// pane is waiting on a lane the tracker is not following, so the honest
// strip is the indeterminate bar alone.
func (inst *PlayApp) renderPaneProgressStrip(numbers bool) {
	v := inst.frameProgress
	pad := styletokens.PaddingTight(inst.density)
	for range c.Horizontal().KeepIter() {
		if numbers && v.fresh {
			renderProgressBar(v, paneProgressWidth)
			diagWeak(formatProgressStrip(v))
		} else {
			renderProgressBar(progressView{}, paneProgressWidth)
			diagWeak("executing…")
		}
	}
	c.AddSpace(pad)
	c.Separator().Send()
	c.AddSpace(pad)
}

// renderLaneProgress is renderTopBarProgress for a panel that runs its own node
// on its own [nodeLane] (ADR-0097 SD5) rather than observing `main`: the same
// spinner + Cancel + bar + numbers, against the panel's lane instead of the
// app's. Such a panel used to have none of it — its query ran, and could only
// be waited out.
//
// Emitted INSIDE the caller's existing control row, like the top bar's, and NOT
// as a row of its own. A row that comes and goes with the run changes the
// panel's height, and a panel that sizes a query off its own viewport then
// re-keys its demand on the change: the Map's fetch superseded itself a debounce
// after starting, and hiding the row on Cancel started a fresh one — a Cancel
// undone by its own disappearance (seen live, 2026-08-05). Beside a control that
// is always there, the row height is pinned and nothing moves.
//
// v is what the CALLER's tracker made of this frame's tick; the app's tracker
// follows a different lane and cannot stand in for it. Returns true on the frame
// Cancel is clicked — what cancelling means is the caller's, since the lane is
// the caller's.
func renderLaneProgress(ids *c.WidgetIdStack, cancelID string, v progressView) (cancelled bool) {
	c.Spinner().Size(14).Send()
	if c.Button(ids.PrepareStr(cancelID), c.Atoms().Text("Cancel").Keep()).
		SendResp().HasPrimaryClicked() {
		cancelled = true
	}
	if v.fresh {
		renderProgressBar(v, paneProgressWidth)
		diagWeak(formatProgressStrip(v))
		return
	}
	// No tick yet — a run that just started, or an endpoint that streams none
	// (chlocal, mocks). The animated bar reads as "running, no idea how far",
	// which is all that is known.
	renderProgressBar(progressView{}, paneProgressWidth)
	return
}

// laneStats is the accounting of the run that produced what a lane-owning
// panel is currently drawing. It fills the slot renderLaneProgress occupies
// while a run is in flight, so the row reads as one story: the counters climb
// during, and settle into what the finished run cost.
//
// valid is false when no result has landed or the last one errored — the same
// no-latch discipline the panels apply to the lane error, mirrored from the
// view every demand rather than kept until something overwrites it.
type laneStats struct {
	valid    bool
	readRows uint64
	returned int64
	elapsed  time.Duration
}

// statsFromLane lifts one demand's served result into [laneStats].
//
// `returned` is the record's own row count rather than the summary's
// ResultRows: it is what the panel actually received, so it holds on an
// endpoint that reports no summary at all (chlocal, mocks) and cannot disagree
// with what is on screen.
func statsFromLane(view laneView) (out laneStats) {
	if view.key == "" || view.err != nil {
		return
	}
	out.valid = true
	out.readRows = view.summary.ReadRows
	out.elapsed = view.elapsed
	if view.rec != nil {
		out.returned = view.rec.NumRows()
	}
	return
}

// formatLaneStats renders one landed run: rows read, rows returned, the
// throughput those imply, and the wall clock.
//
// The clock is the lane's, not the server's ElapsedNs — it is what the person
// waited, and it is the clock play's own status bar reports — so the
// throughput is end-to-end rather than the server's internal rate. Counts are
// exact (this is a statistics readout, where the digits are the point); only
// the rate is abbreviated, where a magnitude is all anyone reads.
func formatLaneStats(s laneStats) string {
	var b strings.Builder
	b.WriteString(humanize.Comma(int64(s.readRows)))
	b.WriteString(" rows read · ")
	b.WriteString(humanize.Comma(s.returned))
	b.WriteString(" returned")
	if s.elapsed <= 0 {
		return b.String()
	}
	if rate := formatRowRate(s.readRows, s.elapsed); rate != "" {
		b.WriteString(" · ")
		b.WriteString(rate)
	}
	b.WriteString(" · ")
	b.WriteString(s.elapsed.Round(time.Millisecond).String())
	return b.String()
}

// formatRowRate is play's one spelling of a rows-per-second figure — an SI
// magnitude, since at a glance only the order matters — shared by the panel
// readout and the status bar so a run reads the same in both. Empty when there
// is no measurable rate: an unclocked run, or one too fast to divide.
func formatRowRate(rows uint64, elapsed time.Duration) string {
	if elapsed <= 0 {
		return ""
	}
	rate := float64(rows) / elapsed.Seconds()
	if rate < 1 {
		return ""
	}
	// SIWithDigits pads a bare magnitude with the space its (empty) unit would
	// have taken; trimming keeps "500 rows/s" from doubling it.
	return strings.TrimSpace(humanize.SIWithDigits(rate, 1, "")) + " rows/s"
}

// countAtAGlance spells a count the way a number READ WHILE IT MOVES wants to
// be spelled: an SI magnitude, because at a tick every 250 ms the digits past
// the first are noise. Its counterpart is humanize.Comma, which play uses
// wherever a count is read after the fact and the digits ARE the point — the
// status bar's landed line, the panel stats readout, the run detail. One
// vocabulary (go-humanize), two registers, chosen by how the number is read.
//
// SI rather than the "3.5B rows" a data person might say: the same lines carry
// SI rates and IEC bytes, so B-for-billion was the odd spelling out — and G is
// 10^9 in every locale, which B is not.
func countAtAGlance(n uint64) string {
	return strings.TrimSpace(humanize.SIWithDigits(float64(n), 1, ""))
}

// formatRate is play's one spelling of a rows-per-second figure, shared by the
// live formatters and the landed readouts so a run reads the same during and
// after. Always a glance magnitude: nobody reads the units digit of a rate.
func formatRate(rate float64) string {
	return strings.TrimSpace(humanize.SIWithDigits(rate, 1, "")) + " rows/s"
}
