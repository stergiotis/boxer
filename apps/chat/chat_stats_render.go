package chat

import (
	"context"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/ecdf"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

const (
	// statsPanelW is the Statistics panel's width; the plots fill it.
	statsPanelW float32 = 380
	statsPlotH  float32 = 150

	tipStats     = "Token and answer statistics of this window's conversations, kept while the window is open."
	tipOpenStats = "Publish the statistics as the ad-hoc datasets chat_turns and chat_calls, and open play on them. A later press republishes."
)

var (
	atomsStats     = c.Atoms().Text(icons.PhChartLine + " Statistics").Keep()
	atomsOpenStats = c.Atoms().Text(icons.PhTable + " Open in play").Keep()
)

// renderStatsToggle is the bar's Statistics button.
func (inst *App) renderStatsToggle() {
	if !inst.advanced {
		return
	}
	for range c.HoverText(tipStats).KeepIter() {
		if c.Button(inst.ids.PrepareStr("stats-toggle"), atomsStats).Selected(inst.showStats).SendResp().HasPrimaryClicked() {
			inst.showStats = !inst.showStats
		}
	}
}

// renderStatsPanel is the Statistics panel, beside the transcript.
func (inst *App) renderStatsPanel() {
	if !inst.advanced || !inst.showStats {
		return
	}
	for range c.PanelRightInside(inst.ids.PrepareStr("stats")).ExactSize(statsPanelW).KeepIter() {
		for range c.ScrollArea().Vscroll(true).Hscroll(false).KeepIter() {
			inst.renderStats()
		}
	}
}

func (inst *App) renderStats() {
	s := &inst.stats
	turns, answered, calls, in, out := s.totals()
	for rt := range c.RichTextLabel("Statistics") {
		rt.Heading()
	}
	if turns == 0 {
		for rt := range c.RichTextLabel("No turn yet: the statistics fill in as the model answers.") {
			rt.Weak()
		}
		return
	}
	c.Label(strconv.Itoa(turns) + " turns · " + strconv.Itoa(answered) + " answered · " + strconv.Itoa(calls) + " model calls").Send()
	c.Label("tokens: " + strconv.FormatInt(in, 10) + " in · " + strconv.FormatInt(out, 10) + " out").Send()
	w := statsPanelW - 30
	inst.renderEcdf(0, "answer time", "seconds from send to answer", s.answerSeconds(), w)
	inst.renderEcdf(1, "output tokens", "output tokens per model call", s.callTokens(true), w)
	inst.renderEcdf(2, "input tokens", "input tokens per model call", s.callTokens(false), w)
	c.Separator().Send()
	busy := inst.handover.Snapshot().State == bgjob.StateRunning
	for range c.HoverText(tipOpenStats).KeepIter() {
		if c.Button(inst.ids.PrepareStr("stats-open"), atomsOpenStats).SendResp().HasPrimaryClicked() && !busy {
			inst.openStats()
		}
	}
	if busy {
		c.Label("publishing…").Send()
	} else if inst.handoverNote != "" {
		for rt := range c.RichTextLabel(inst.handoverNote) {
			rt.Small().Weak()
		}
	}
}

// renderEcdf draws one empirical distribution with its confidence band
// once the band is ready; until then the curve alone.
func (inst *App) renderEcdf(i int, name string, xLabel string, sorted []float64, w float32) {
	for rt := range c.RichTextLabel(name) {
		rt.Strong()
	}
	if len(sorted) < 2 {
		for rt := range c.RichTextLabel("needs two values to draw, has " + strconv.Itoa(len(sorted))) {
			rt.Small().Weak()
		}
		return
	}
	if sorted[0] == sorted[len(sorted)-1] {
		for rt := range c.RichTextLabel("all " + strconv.Itoa(len(sorted)) + " values are " + strconv.FormatFloat(sorted[0], 'g', 4, 64) + ": no spread to draw") {
			rt.Small().Weak()
		}
		return
	}
	in := ecdf.Input{Style: ecdf.Style{SeriesName: name}, Sorted: sorted, Band: ecdf.BandNone}
	if ecdf.BandReady(in.Style, len(sorted)) {
		in.Band = ecdf.BandExact
	} else {
		ecdf.EnsureBandJob(inst.bandKeys[i], nil, len(sorted), in.Style)
	}
	p := implot.Begin(inst.ids, "##chat-ecdf-"+strconv.Itoa(i), w, statsPlotH)
	p.SetupAxes(xLabel, "fraction at or below", implot.AxisFlagsNone, implot.AxisFlagsNone)
	p.IncludeY(0)
	p.IncludeY(1)
	_ = ecdf.Paint(p, in)
	p.End()
}

// openStats publishes a copy of the statistics and opens play on it, off
// the render thread.
func (inst *App) openStats() {
	bus, pubs, snap := inst.bus, inst.pubs, inst.stats.clone()
	if bus == nil {
		inst.handoverNote = "no bus: Open in play needs the app runtime"
		return
	}
	inst.handoverNote = ""
	inst.handover.Start(nil, bgjob.Spec{Kind: "chat-stats-handover", Title: "open the chat statistics in play"},
		func(ctx context.Context) (note *string, err error) {
			n, err := openStatsInPlay(bus, pubs, snap)
			if err != nil {
				return
			}
			note = &n
			return
		})
}

// drainHandover lands the handover's outcome.
func (inst *App) drainHandover() {
	if note, _, ok := inst.handover.TakeResult(); ok {
		inst.handoverNote = *note
	} else if snap := inst.handover.Snapshot(); snap.State == bgjob.StateFailed {
		inst.handover.Invalidate()
		inst.handoverNote = "could not open in play"
		if snap.Err != nil {
			inst.handoverNote += ": " + snap.Err.Error()
		}
	}
}
