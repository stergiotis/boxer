package widgets

import (
	"fmt"
	"iter"
	"math/rand/v2"
	"time"

	"github.com/dustin/go-humanize"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline/layout"
)

// =============================================================================
// Timeline demo — calendar-axis interval widget + rug strip of point events.
// Renders a synthetic three-day LLM-session log (lane-packed by provider)
// with ~150 synthetic git commits overlaid as the top rug strip. Above the
// timeline: a selection card reflecting the current Timeline.Selection.
// Below: a capped-5 ring of recent click events so the demo doubles as
// proof that clicks resolve to specific events / buckets.
// =============================================================================

const recentClicksCap = 5

type timelineDemoState struct {
	tl           *timeline.Timeline
	annotations  []*layout.Annotation
	recentClicks []string
}

func (s *timelineDemoState) pushClick(line string) {
	s.recentClicks = append([]string{line}, s.recentClicks...)
	if len(s.recentClicks) > recentClicksCap {
		s.recentClicks = s.recentClicks[:recentClicksCap]
	}
}

func init() {
	registry.Register(registry.Demo{
		Name:        "timeline",
		Category:    "Layout & widgets",
		Title:       "timeline (interval lanes + rug)",
		Stage:       [2]float32{1200, 720},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindUX,
		Description: "Calendar-axis timeline with greedy lane-packed interval bars (LLM sessions) over three days and a top rug strip of ~150 synthetic git commits. Click a bar or bucket to select; click again or off-target to clear.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			s := &timelineDemoState{}
			s.annotations = makeAnnotationFixture()
			s.tl = timeline.New(ids, "timeline-demo", makeTimelineFixture(),
				timeline.WithContainerWidth(1180),
				timeline.WithPointEvents(makeCommitFixture()),
				timeline.WithAnnotations(s.annotations),
				timeline.WithBackgroundBands(composeBandProducers(weekendBands, officeHoursBands)),
				timeline.WithNowLine(true),
				timeline.WithOnSelection(func(sel timeline.SelectionInfo) {
					s.pushClick(formatSelectionClickLine(sel))
				}))
			state = s
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			s := state.(*timelineDemoState)
			c.Label("Timeline visualization — LifeLines-style mixed point + interval events on a calendar axis (ADR-0043).").Send()
			c.Label("Top rug strip: ~150 PCG-seeded synthetic git commits over 3 days. Lane bars: 20 LLM sessions in three provider rows (claude / gpt / gemini).").Send()
			c.Label("Annotation flags at the very top mark deploys/alerts/releases (6 sample markers); click a flag or its dashed line to select. The alert + hotfix pair sits too close for one row, so those flags stagger into a second row instead of overlapping.").Send()
			c.Label("Background bands (iter.Seq, computed each frame from the view range): muted weekend shade + warm office-hours overlay 09–17. Bright vertical line = now.").Send()
			c.Label("Hover for tooltip · click to select (outline + card below) · Ctrl+scroll over a session zooms anchored at the cursor.").Send()
			s.tl.Render()
			c.Separator().Send()
			renderStrongLabel("Selection")
			c.Label(formatSelectionCard(s.tl.Selection())).Send()
			c.Label(formatCursorReadout(s.tl)).Send()
			c.Separator().Send()
			renderStrongLabel("Annotations panel (sibling widget — click to drive timeline selection)")
			renderSiblingAnnotationPanel(ids, s)
			c.Separator().Send()
			renderStrongLabel("Recent clicks (newest first)")
			if len(s.recentClicks) > 0 {
				for _, line := range s.recentClicks {
					c.Label("  " + line).Send()
				}
			} else {
				c.Label("  (no clicks yet — click an interval bar, rug bucket, or annotation flag to populate)").Send()
			}
		},
	})
}

// renderStrongLabel paints a bold text label as a section header in the
// below-timeline info area. Uses Atoms+Strong rather than plain Label so
// the typography distinguishes section breaks from data rows.
func renderStrongLabel(text string) {
	atoms := c.Atoms()
	for rt := range atoms.StyledText(text) {
		rt.Strong()
	}
	c.LabelAtoms(atoms.Keep()).Send()
}

// formatCursorReadout returns a single-line readout of the time under
// the cursor, paired with humanize.Time for a relative "X ago / in X"
// phrasing so the absolute timestamp is grounded in something the eye
// can parse without subtracting dates. Returns a placeholder when the
// cursor isn't over the timeline data area.
func formatCursorReadout(tl *timeline.Timeline) (text string) {
	t, ok := tl.CursorTime()
	if !ok {
		text = "Cursor: (not over timeline data area)"
		return
	}
	text = fmt.Sprintf("Cursor: %s  (%s)",
		t.Format("2006-01-02 15:04:05 MST"),
		humanize.Time(t))
	return
}

func formatSelectionCard(sel timeline.SelectionInfo) (text string) {
	switch sel.Kind {
	case timeline.SelectionInterval:
		ev := sel.Interval
		hint := ev.LaneHint
		if hint == "" {
			hint = "(unhinted)"
		}
		text = fmt.Sprintf("Selected interval:    %s  %s – %s  (%v, intensity %.2f)",
			hint,
			ev.AsFromTime().Format("2006-01-02 15:04"),
			ev.AsToTime().Format("15:04"),
			time.Duration(ev.DurationMS())*time.Millisecond,
			ev.Intensity)
	case timeline.SelectionBucket:
		b := sel.Bucket
		text = fmt.Sprintf("Selected bucket:      %s  %d event(s), Σintensity %.2f",
			time.UnixMilli(b.StartMS).UTC().Format("2006-01-02 15:04:05"),
			b.Count, b.SumIntensity)
	case timeline.SelectionAnnotation:
		a := sel.Annotation
		text = fmt.Sprintf("Selected annotation:  #%d  %s  —  %s",
			a.Number,
			a.AsTime().Format("2006-01-02 15:04:05"),
			a.Label)
	default:
		text = "Selection: (none)  —  click an interval bar, rug bucket, or annotation flag to select"
	}
	return
}

// formatSelectionClickLine formats a SelectionInfo as a one-line entry
// for the demo's "Recent clicks" ring buffer. SelectionNone covers the
// click-miss / click-same gestures that clear the previous selection.
func formatSelectionClickLine(sel timeline.SelectionInfo) (line string) {
	switch sel.Kind {
	case timeline.SelectionInterval:
		ev := sel.Interval
		hint := ev.LaneHint
		if hint == "" {
			hint = "(unhinted)"
		}
		line = fmt.Sprintf("interval  %s  %s – %s  (%v, i=%.2f)",
			hint,
			ev.AsFromTime().Format("01-02 15:04"),
			ev.AsToTime().Format("15:04"),
			time.Duration(ev.DurationMS())*time.Millisecond,
			ev.Intensity)
	case timeline.SelectionBucket:
		b := sel.Bucket
		line = fmt.Sprintf("bucket    %s  %d event(s)  Σi=%.2f",
			time.UnixMilli(b.StartMS).UTC().Format("01-02 15:04:05"),
			b.Count, b.SumIntensity)
	case timeline.SelectionAnnotation:
		a := sel.Annotation
		line = fmt.Sprintf("annot #%-2d %s  %s",
			a.Number,
			a.AsTime().Format("01-02 15:04"),
			a.Label)
	default:
		line = "(selection cleared)"
	}
	return
}

// renderSiblingAnnotationPanel demonstrates the cross-widget linking
// pattern. A SelectableLabel per annotation reflects the timeline's
// current selection (driven by Timeline.Selection() → SelectionAnnotation)
// and, on click, calls Timeline.SelectAnnotationByNumber to drive
// selection state in the OTHER direction. In a real app this panel would
// be a separate widget on a separate panel/window; here it lives in the
// demo for visual proximity.
func renderSiblingAnnotationPanel(ids *c.WidgetIdStack, s *timelineDemoState) {
	sel := s.tl.Selection()
	for _, a := range s.annotations {
		if a == nil {
			continue
		}
		isSelected := sel.Kind == timeline.SelectionAnnotation && sel.Annotation == a
		text := fmt.Sprintf("  #%d  %s  %s",
			a.Number,
			a.AsTime().Format("01-02 15:04"),
			a.Label)
		if c.SelectableLabel(ids.PrepareSeq(uint64(0xA1100+uint64(a.Number))), isSelected, text).
			SendResp().HasPrimaryClicked() {
			s.tl.SelectAnnotationByNumber(a.Number)
		}
	}
}

// makeTimelineFixture returns a synthetic three-day LLM-session log used by
// the timeline demo. Three LaneHint categories ("claude", "gpt", "gemini")
// keep one row per provider; intentional within-row overlap exercises the
// hint-pin "caller-asserted invariant" rule (vs. greedy-auto packing).
func makeTimelineFixture() (events []*layout.IntervalEvent) {
	base := time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC).UnixMilli()
	day := int64(24 * time.Hour / time.Millisecond)
	hour := int64(time.Hour / time.Millisecond)
	minute := int64(time.Minute / time.Millisecond)

	type spec struct {
		offsetMS  int64
		durMS     int64
		hint      string
		intensity float32
	}
	specs := []spec{
		{0, 45 * minute, "claude", 0.30},
		{50 * minute, 30 * minute, "claude", 0.55},
		{2 * hour, 15 * minute, "gpt", 0.20},
		{2*hour + 30*minute, 90 * minute, "claude", 0.70},
		{4*hour + 30*minute, 20 * minute, "gemini", 0.40},
		{5 * hour, 75 * minute, "claude", 0.60},
		{6*hour + 30*minute, 25 * minute, "gpt", 0.80},
		{8 * hour, 40 * minute, "gemini", 0.50},

		{day + 4*hour, 60 * minute, "claude", 0.90},
		{day + 4*hour + 30*minute, 35 * minute, "gpt", 0.60},
		{day + 5*hour + 30*minute, 80 * minute, "claude", 0.40},
		{day + 7*hour, 25 * minute, "gemini", 0.30},
		{day + 7*hour + 30*minute, 50 * minute, "gpt", 0.70},
		{day + 9*hour, 110 * minute, "claude", 1.00},

		{2*day + hour, 30 * minute, "claude", 0.40},
		{2*day + 2*hour, 60 * minute, "claude", 0.55},
		{2*day + 4*hour, 20 * minute, "gpt", 0.20},
		{2*day + 5*hour, 45 * minute, "gemini", 0.65},
		{2*day + 6*hour, 90 * minute, "claude", 0.85},
		{2*day + 8*hour, 30 * minute, "gpt", 0.45},
	}

	events = make([]*layout.IntervalEvent, 0, len(specs))
	for _, s := range specs {
		events = append(events, &layout.IntervalEvent{
			FromMS:    base + s.offsetMS,
			ToMS:      base + s.offsetMS + s.durMS,
			LaneHint:  s.hint,
			Intensity: s.intensity,
		})
	}
	return
}

// =============================================================================
// Background band producers — demonstrate the iter.Seq pattern. Each
// producer walks the view range once per frame and yields the bands that
// intersect it; no slice is materialised.
// =============================================================================

const (
	weekendBandColor     uint32 = 0x1f202450 // muted dark-gray, low alpha
	officeHoursBandColor uint32 = 0xf0c97014 // warm tint, very low alpha
)

// weekendBands shades every Saturday + Sunday across the visible window.
// Walks day by UTC midnight, yielding one full-day band per weekend day.
func weekendBands(viewMinMS, viewMaxMS int64) iter.Seq[layout.BackgroundBand] {
	return func(yield func(layout.BackgroundBand) bool) {
		dayMS := int64(24 * time.Hour / time.Millisecond)
		t0 := time.UnixMilli(viewMinMS).UTC()
		startMS := time.Date(t0.Year(), t0.Month(), t0.Day(), 0, 0, 0, 0, time.UTC).UnixMilli()
		for t := startMS; t < viewMaxMS; t += dayMS {
			wd := time.UnixMilli(t).UTC().Weekday()
			if wd == time.Saturday || wd == time.Sunday {
				if !yield(layout.BackgroundBand{
					FromMS: t,
					ToMS:   t + dayMS,
					Color:  weekendBandColor,
					Label:  wd.String(),
				}) {
					return
				}
			}
		}
	}
}

// officeHoursBands shades 09:00–17:00 UTC on every weekday inside the
// visible window. Walks day by day; emits nothing for weekends (which
// weekendBands already covers).
func officeHoursBands(viewMinMS, viewMaxMS int64) iter.Seq[layout.BackgroundBand] {
	return func(yield func(layout.BackgroundBand) bool) {
		dayMS := int64(24 * time.Hour / time.Millisecond)
		hourMS := int64(time.Hour / time.Millisecond)
		t0 := time.UnixMilli(viewMinMS).UTC()
		startMS := time.Date(t0.Year(), t0.Month(), t0.Day(), 0, 0, 0, 0, time.UTC).UnixMilli()
		for t := startMS; t < viewMaxMS; t += dayMS {
			wd := time.UnixMilli(t).UTC().Weekday()
			if wd == time.Saturday || wd == time.Sunday {
				continue
			}
			if !yield(layout.BackgroundBand{
				FromMS: t + 9*hourMS,
				ToMS:   t + 17*hourMS,
				Color:  officeHoursBandColor,
				Label:  "office hours",
			}) {
				return
			}
		}
	}
}

// composeBandProducers concatenates N producers into one. Bands paint in
// the order they're yielded, so earlier producers paint *below* later ones
// (weekend behind office-hours, in the demo's setup).
func composeBandProducers(producers ...timeline.BackgroundBandProducer) timeline.BackgroundBandProducer {
	return func(viewMinMS, viewMaxMS int64) iter.Seq[layout.BackgroundBand] {
		return func(yield func(layout.BackgroundBand) bool) {
			for _, p := range producers {
				for band := range p(viewMinMS, viewMaxMS) {
					if !yield(band) {
						return
					}
				}
			}
		}
	}
}

// makeAnnotationFixture returns six Grafana-style annotations spread
// across the three-day window: one deploy, one alert, one hotfix, one
// release, one incident-resolve, one config-change. PaletteIdx values fan
// across the BatlowS qualitative palette so the flags read as visually
// distinct. The hotfix lands 30 minutes after the alert — too close for
// side-by-side flags at the demo's default zoom — so the pair exercises
// the flag-row stagger (zoom in to watch them rejoin one row).
func makeAnnotationFixture() (out []*layout.Annotation) {
	base := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC).UnixMilli()
	day := int64(24 * time.Hour / time.Millisecond)
	hour := int64(time.Hour / time.Millisecond)
	minute := int64(time.Minute / time.Millisecond)

	type spec struct {
		offsetMS int64
		palette  int32
		label    string
	}
	specs := []spec{
		{8 * hour, 0, "Deploy v2.4.1"},
		{day + 3*hour, 2, "Alert: API latency spike"},
		{day + 3*hour + 30*minute, 5, "Hotfix v2.4.2 rollout"},
		{day + 8*hour, 4, "Release notes published"},
		{2*day + 2*hour, 6, "Incident resolved"},
		{2*day + 10*hour, 8, "Config change: rate limit"},
	}
	out = make([]*layout.Annotation, 0, len(specs))
	for i, sp := range specs {
		out = append(out, &layout.Annotation{
			TMS:        base + sp.offsetMS,
			Number:     int32(i + 1),
			PaletteIdx: sp.palette,
			Label:      sp.label,
		})
	}
	return
}

// makeCommitFixture returns ~150 synthetic git-commit timestamps spread
// across the same three-day window the LLM sessions cover. Burstier in
// working hours (9–12, 14–18 UTC) so the rug strip has visible density
// peaks; quiet at night. Deterministic via a fixed PCG seed.
func makeCommitFixture() (points []*layout.PointEvent) {
	const (
		nCommits     = 150
		dayCount     = 3
		workHourFrom = 9
		workHourTo   = 18
		lunchHour    = 13
	)
	base := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC).UnixMilli()
	day := int64(24 * time.Hour / time.Millisecond)
	hour := int64(time.Hour / time.Millisecond)

	rng := rand.New(rand.NewPCG(0xC0FFEE, 0xB16B00B5))
	points = make([]*layout.PointEvent, 0, nCommits)
	for range nCommits {
		dayIdx := int64(rng.IntN(dayCount))
		hr := workHourFrom + rng.IntN(workHourTo-workHourFrom)
		if hr == lunchHour && rng.IntN(3) == 0 {
			hr = workHourFrom + rng.IntN(workHourTo-workHourFrom)
		}
		minOffset := int64(rng.IntN(60 * 60 * 1000))
		tms := base + dayIdx*day + int64(hr)*hour + minOffset
		points = append(points, &layout.PointEvent{
			TMS:       tms,
			Intensity: 0.4 + rng.Float32()*0.5,
		})
	}
	return
}
