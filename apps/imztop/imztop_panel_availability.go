package imztop

import (
	"fmt"
	"iter"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/sysmreplay"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline"
	tllayout "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline/layout"
)

// The availability strip (ADR-0197 §SD9): a timeline showing where stored
// history is, brushed to pick the replay window.
//
// It replaces the window-length combo M4 shipped. A combo asks the user to name
// a range without telling them where any data is — which is how replaying a
// host the tee never covered produced a correct, useless answer and no way to
// find a covered stretch except to jog and look.
//
// The widget is per-window: pan, zoom and the brush are view state, and two
// imztop windows may reasonably look at different stretches while sharing one
// replay session. The coverage behind it is process-wide, because it is the
// same question about the same host.

// availabilityContextSpan is how much history the strip shows when a session
// opens: wide enough to find a gap, narrow enough that the first query is
// cheap. The user pans and zooms from there.
const availabilityContextSpan = 6 * time.Hour

// ensureAvailability builds this window's timeline on first use.
//
// The brush callback is the whole point of the widget being here: a completed
// gesture seeks the shared session, which is what makes the strip a range
// control rather than a picture.
func (inst *App) ensureAvailability() (tl *timeline.Timeline) {
	if inst.availability != nil {
		return inst.availability
	}
	now := time.Now().UTC()
	tl = timeline.New(inst.ids, "imztop-availability", nil,
		timeline.WithContainerWidth(900),
		timeline.WithRange(now.Add(-availabilityContextSpan), now),
		timeline.WithNowLine(true),
		// Local, to agree with every other time imztop prints. The widget
		// defaults to UTC; leaving it there put the axis two hours off the
		// transport row's readout of the same instant, which reads as a bug
		// long before it reads as a zone.
		timeline.WithTimeZone(time.Local),
		timeline.WithBackgroundBands(availabilityBands),
		// The load layer: one rug mark per preview bin, tinted by how busy the
		// box was. It is what makes the strip answer "when was something
		// happening" rather than only "when was anything recorded".
		timeline.WithIntensityEncoding(true),
		timeline.WithBrush(func(r timeline.BrushRange, ok bool) {
			if !ok {
				return // a click clears the brush; it does not cancel the window
			}
			if session := ActiveReplay(); session != nil {
				session.SeekWindow(
					time.UnixMilli(r.FromMS).UTC(),
					time.UnixMilli(r.ToMS).UTC())
			}
		}),
		timeline.WithVisuals(func(v *timeline.Visuals) {
			// No lanes are attached, so the lane area is dead space; the
			// smallest the widget will draw is what this strip wants. The rug
			// keeps a real height because it is not decoration here — it
			// carries the load preview (§SD9), and a zero-height rug silently
			// drops it.
			v.LaneHeight = 8
			v.RugStripH = 16
			v.RugGap = 2
		}))
	inst.availability = tl
	return
}

// availabilityBands is the widget's band producer: one band per contiguous
// stretch of stored history, clipped to the visible range.
//
// It reads the slice a completed query left behind and never queries itself.
// The producer runs inside the frame, and a database read there would stall it.
func availabilityBands(viewMinMS, viewMaxMS int64) iter.Seq[tllayout.BackgroundBand] {
	return func(yield func(tllayout.BackgroundBand) bool) {
		runs, _, _, _ := coverageSnapshot()
		for _, r := range runs {
			if r.EndMS <= viewMinMS || r.StartMS >= viewMaxMS {
				continue
			}
			if !yield(tllayout.BackgroundBand{
				FromMS: r.StartMS,
				ToMS:   r.EndMS,
				Color:  availabilityBandColor,
				Label:  fmt.Sprintf("stored history — %d rows", r.Rows),
			}) {
				return
			}
		}
	}
}

// previewEvents turns the loaded preview into the rug's point events.
//
// Intensity is the metric normalised to 0..1, which is what the rug's colormap
// takes. CPU busy percent is already bounded, so the normalisation is a
// division rather than a scan for the range — a scan would make the strip's
// colours mean something different every time the window moved.
//
// A point carries no label of its own; the widget's hover tooltip reads the
// rug's density and intensity, and the availability band underneath already
// names the stretch.
func previewEvents() (points []*tllayout.PointEvent) {
	src := previewSnapshot()
	if len(src) == 0 {
		return
	}
	points = make([]*tllayout.PointEvent, 0, len(src))
	for _, p := range src {
		points = append(points, &tllayout.PointEvent{
			TMS:       p.StartMS,
			Intensity: float32(min(max(p.Value/100.0, 0), 1)),
		})
	}
	return
}

// availabilityBandColor shades a covered stretch. The band channel takes a
// packed RGBA literal, and the alpha is low on the widget's own advice: a band
// is context under everything else and must not compete with it.
const availabilityBandColor uint32 = 0x4c9f7038

// renderAvailability draws the strip and keeps its coverage fresh.
//
// The refresh is driven from the view the widget reported last frame rather
// than from the window being replayed: the user pans the strip to look
// somewhere else, and that is exactly when new coverage is needed.
func (inst *App) renderAvailability(st ReplayStatus, snap *PublishedSnapshot) {
	tl := inst.ensureAvailability()

	if from, to, ok := tl.ViewRange(); ok {
		ensureCoverage(ActiveReplaySource(), from, to)
	} else {
		// Before the first Render there is no view to ask about; seed the
		// query from the range the widget was constructed with so the first
		// frame after it has something to draw.
		now := time.Now().UTC()
		ensureCoverage(ActiveReplaySource(), now.Add(-availabilityContextSpan), now)
	}

	// Refresh the rug from whatever the last background pass loaded. The points
	// are rebuilt rather than mutated so the widget never sees a half-updated
	// slice; at a few hundred bins that is cheaper than any sharing scheme.
	tl.SetPoints(previewEvents())

	// Mirror the session's window onto the brush so the strip shows what is
	// being replayed, including a window the jog buttons set rather than a
	// gesture here, and mark where playback has got to inside it.
	if session := ActiveReplay(); session != nil {
		inst.mirrorReplayWindow(tl, session.Window())
		mirrorReplayPosition(tl, session)
	} else {
		tl.ClearPlayhead()
	}

	// No min-height floor here. A floor is a scroll-host device — it gives a
	// widget a finite rect inside an unbounded vscroll — and this host is a
	// bounded top panel. Forcing one grew the panel to the floor and stretched
	// the transport row's vertical separators down the whole window with it.
	// The timeline's height is content-driven, so it needs no help.
	tl.Render()
	inst.renderAvailabilityLegend(st, snap)
}

// mirrorReplayWindow reflects the session's range onto this strip's brush, and
// brings the strip to it when something else moved it.
//
// The brush is set on every frame rather than on change: it is idempotent, and
// it is what puts the range back after a stray click cleared it — clearing the
// brush does not cancel the window, so the strip must not go on claiming
// nothing is being replayed.
//
// Panning the view is the opposite, and must fire only on the edge. A jog can
// step the window clean out of the visible span, where a brush paints nothing
// and the button reads as having done nothing at all; but a view that
// re-centred every frame could not be panned away from at all, which is the
// first thing a user does after picking a range.
func (inst *App) mirrorReplayWindow(tl *timeline.Timeline, w sysmreplay.Window) {
	if w.From.IsZero() || w.To.IsZero() {
		return // unbounded: there is no range to brush
	}
	fromMS, toMS := w.From.UnixMilli(), w.To.UnixMilli()
	tl.SetBrush(fromMS, toMS)
	if fromMS == inst.availabilityBrushFromMS && toMS == inst.availabilityBrushToMS {
		return
	}
	inst.availabilityBrushFromMS, inst.availabilityBrushToMS = fromMS, toMS
	followBrushedWindow(tl, fromMS, toMS)
}

// mirrorReplayPosition marks where playback has got to.
//
// The brush says which stretch is being replayed; this says which instant
// inside it is on screen. The transport row states the same thing as text
// ("at 10:19:51"), which answers it only for someone already reading that
// row — on the strip it is a position among the bands and the load rug, which
// is where the question is actually asked.
//
// Cleared rather than left standing when the session has no position: that is
// what a seek leaves behind (ADR-0197 §SD12 resets it), and a mark held over
// from the previous range would point at a moment nothing is showing.
func mirrorReplayPosition(tl *timeline.Timeline, session *ReplaySampler) {
	at, ok := session.Position()
	if !ok {
		tl.ClearPlayhead()
		return
	}
	tl.SetPlayhead(at.UnixMilli())
}

// followBrushedWindow pans the strip so a newly-picked window is visible.
func followBrushedWindow(tl *timeline.Timeline, fromMS, toMS int64) {
	viewFrom, viewTo, ok := tl.ViewRange()
	if !ok {
		return // nothing rendered yet; the constructor's range stands
	}
	newFromMS, newToMS, move := followRange(
		viewFrom.UnixMilli(), viewTo.UnixMilli(), fromMS, toMS)
	if !move {
		return
	}
	tl.SetRange(time.UnixMilli(newFromMS).UTC(), time.UnixMilli(newToMS).UTC())
}

// followRange decides where a strip currently showing [viewFromMS, viewToMS]
// should look so that [fromMS, toMS] is on it. move is false when the view
// already contains the window, or when neither is a usable span.
//
// The zoom is kept: the user chose it to look at this much history at a time,
// and a jog is a step along the axis rather than a request to reframe it. It
// widens only when the window would not fit — and then to three times the
// window, so the range lands with context on both sides rather than filling
// the strip edge to edge.
func followRange(viewFromMS, viewToMS, fromMS, toMS int64) (newFromMS, newToMS int64, move bool) {
	if toMS <= fromMS || viewToMS <= viewFromMS {
		return
	}
	if fromMS >= viewFromMS && toMS <= viewToMS {
		return
	}
	spanMS := max(viewToMS-viewFromMS, 3*(toMS-fromMS))
	midMS := fromMS + (toMS-fromMS)/2
	newFromMS, newToMS, move = midMS-spanMS/2, midMS+spanMS/2, true
	return
}

// renderAvailabilityLegend says what the bands mean and what the strip is for,
// and reports a coverage query that failed — a strip with no bands otherwise
// reads as "no history" whether the query found none or never ran.
func (inst *App) renderAvailabilityLegend(st ReplayStatus, snap *PublishedSnapshot) {
	for range c.HorizontalTop().KeepIter() {
		runs, _, _, err := coverageSnapshot()
		switch {
		case err != nil:
			for rt := range c.RichTextLabelColored(colorHot, colorBgClear,
				"availability unknown — the coverage query failed") {
				rt.Strong()
			}
		case len(runs) == 0:
			for rt := range c.RichTextLabelColored(colorWarn, colorBgClear,
				fmt.Sprintf("no stored history for %s in this span", st.Host)) {
				rt.Weak()
			}
		default:
			c.Label(fmt.Sprintf("shaded = stored history (%d stretch(es)) · caret = where playback is · drag the strip under the axis to pick a window", len(runs))).Send()
		}
		inst.renderRangeNote()
	}
}

// renderRangeNote says what a long range is doing.
//
// It changed meaning when §SD6 closed. Before decimation the plots simply
// showed the tail of an over-long range and the note was a warning; now the
// range is sampled to fit, so the note reports a resolution rather than a loss
// — every frame shown is a real recorded one, there are just fewer of them.
//
// The measured-not-estimated form stays for the case decimation does not cover:
// a range the fold is already showing in full needs no note at all.
func (inst *App) renderRangeNote() {
	src := ActiveReplaySource()
	if src == nil {
		return
	}
	stored, shown, ok := src.Decimation()
	if !ok {
		return
	}
	c.AddSpace(inst.spaceOuter())
	for rt := range c.RichTextLabelColored(colorWarn, colorBgClear,
		fmt.Sprintf("range sampled to fit — %d of %d stored bundles, one per bin", shown, stored)) {
		rt.Weak()
	}
}

// defaultHistorySlots mirrors the sliding window's capacity: SamplerOptions
// defaults of a 10-minute window at a 1 s interval.
//
// It is the decimation budget (ADR-0197 §SD6, closed by §SD11) — how many
// bundles a replay may put on screen, and therefore how many bins a long range
// is sampled into. Stated here rather than read from the fold because a fold
// built with other options would want its own answer, and nothing yet builds
// one.
const defaultHistorySlots = 600

// availabilityWindowLabel formats a session window for the transport row.
func availabilityWindowLabel(w sysmreplay.Window) (s string) {
	if w.From.IsZero() || w.To.IsZero() {
		return "unbounded"
	}
	return fmt.Sprintf("%s → %s", w.From.Local().Format("15:04:05"), w.To.Local().Format("15:04:05"))
}
