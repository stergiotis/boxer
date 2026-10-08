package play

import (
	"math"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/trendsmooth"
)

// The Series pane as an agent reads and sets it (ADR-0270, update of
// 2026-10-05): the time axis and its Δt class with the pane's finding and
// scaffold, each lane's range, the smoothing and envelope settings, and —
// while the buffer names `scores` / `spans` — the overlays: the detector's
// caption, the flagged spans with the verdict recorded for each, and the
// measured readout. The spans are readable; adjudicating them stays the
// person's (ADR-0270 §SD5, restated in the update). The smoothing stepper
// goes through set_series_options; the smoothing and envelope boxes are
// bound, so the write-back records the person's change.

const (
	opGetSeries        = "get_series"
	opSetSeriesOptions = "set_series_options"
	opsResSeries       = seriesPaneId
	seriesPaneId       = "series"

	// opsSeriesMaxSpans bounds the spans get_series lists.
	opsSeriesMaxSpans = 100
)

// SeriesLaneReading is one drawn lane.
type SeriesLaneReading struct {
	Label string   `desc:"the lane's column"`
	Nulls int32    `json:",omitzero" desc:"null values; the line breaks there and nothing is filled in"`
	Min   *float64 `json:",omitzero" desc:"its smallest value; absent when every value is null"`
	Max   *float64 `json:",omitzero" desc:"its largest value"`
	Last  *float64 `json:",omitzero" desc:"its value at the last timed row, when that is not null"`
}

// SeriesGridReading is the Δt classification.
type SeriesGridReading struct {
	Class       string  `desc:"regular, regular with gaps, irregular, not ordered by time, or too short to classify"`
	Step        string  `json:",omitzero" desc:"the median interval between samples, as an INTERVAL would spell it"`
	StepSeconds float64 `json:",omitzero" desc:"the median interval in seconds"`
	Gaps        int32   `json:",omitzero" desc:"intervals beyond tolerance; lines break there and analysis runs per segment"`
}

// SeriesFinding is the pane's data-quality finding and its scaffold.
type SeriesFinding struct {
	Text     string `desc:"what the pane says about the grid"`
	Action   string `desc:"the label of the pane's scaffold button: add ORDER BY, add WITH FILL or add GROUP BY"`
	Scaffold string `desc:"the SQL that button inserts, with the measured step filled in and the current query to go where it says; adapt it and apply it with set_sql"`
}

// SeriesSpanReading is one flagged extent of the spans channel.
type SeriesSpanReading struct {
	From    string `desc:"its start, UTC"`
	To      string `desc:"its end, UTC"`
	Label   string `json:",omitzero" desc:"its label from the spans CTE"`
	Verdict string `json:",omitzero" desc:"the verdict recorded for it on this input: confirmed or false_alarm; empty when none is"`
}

// SeriesOverlays is what the `scores` and `spans` channels drew.
type SeriesOverlays struct {
	Caption      string              `json:",omitzero" desc:"the pane's caption under the score plot: the detector, its window, whether it is causal, what the baseline curve is"`
	Detector     string              `json:",omitzero" desc:"the detector behind the score lane, when a client node made it"`
	Window       int32               `json:",omitzero" desc:"the detector's window"`
	Causal       bool                `desc:"whether each score uses only earlier samples"`
	Baseline     bool                `desc:"whether the moving-average residual baseline is drawn beside the score"`
	BaselineWhy  string              `json:",omitzero" desc:"why no baseline is drawn"`
	WarmUp       int32               `json:",omitzero" desc:"positions with no score"`
	ScoreMax     *float64            `json:",omitzero" desc:"the highest score"`
	ScoreMaxAt   string              `json:",omitzero" desc:"when the highest score falls, UTC"`
	Spans        []SeriesSpanReading `json:",omitzero" desc:"the flagged extents, at most 100"`
	SpansCut     int32               `json:",omitzero" desc:"extents past the 100 listed"`
	SpansSkipped int32               `json:",omitzero" desc:"span rows the pane skipped: an unknown colour token, or an end before its start"`
	Readout      string              `json:",omitzero" desc:"the pane's measured readout of the detector against the baseline on the confirmed spans, with its caveats"`
}

// SeriesReading is get_series' result.
type SeriesReading struct {
	Drawn       PaneDraw            `desc:"which draw this is of, its status line, and why it drew nothing when it did not"`
	X           string              `json:",omitzero" desc:"the time column"`
	Points      int32               `json:",omitzero" desc:"timed rows drawn"`
	From        string              `json:",omitzero" desc:"the first time, UTC"`
	To          string              `json:",omitzero" desc:"the last time, UTC"`
	Untimed     int64               `json:",omitzero" desc:"rows without a time, left out"`
	Lanes       []SeriesLaneReading `json:",omitzero" desc:"the drawn lanes, at most 12"`
	LanesHidden int32               `json:",omitzero" desc:"numeric columns past the 12 lanes the pane draws"`
	Grid        *SeriesGridReading  `json:",omitzero" desc:"the Δt classification"`
	Finding     *SeriesFinding      `json:",omitzero" desc:"the pane's finding when the grid is worth acting on"`
	Smoothing   bool                `desc:"whether the smoothed trend is drawn over the raw line; drawing only, every reading and analysis uses the raw values"`
	HalfWidth   int32               `desc:"the smoothing kernel's half-width, in samples"`
	Envelope    bool                `desc:"whether a long series is drawn as a per-pixel min/max envelope; drawing only, extremes are kept"`
	DrawnPoints int32               `json:",omitzero" desc:"samples the last draw plotted, when the envelope thinned them"`
	SourcePts   int32               `json:",omitzero" desc:"samples the envelope thinned from"`
	Overlays    *SeriesOverlays     `json:",omitzero" desc:"the score and span overlays, when the buffer names a scores or spans CTE"`
	LaneNotes   []string            `json:",omitzero" desc:"what the status line says about the scores and spans lanes: a query that failed or is still running"`
}

// SetSeriesOptionsArgs is set_series_options' argument; a field left out
// keeps the person's setting.
type SetSeriesOptionsArgs struct {
	Smoothing *bool  `json:",omitzero" desc:"draw the smoothed trend over the raw line; drawing only, it changes no value an analysis or a reading uses"`
	HalfWidth *int32 `json:",omitzero" desc:"the smoothing kernel's half-width in samples, from the kernel's minimum to 60; the refusal names the range"`
	Envelope  *bool  `json:",omitzero" desc:"draw a long series as a per-pixel min/max envelope, which keeps every extreme; false draws every sample"`
}

// seriesFoldView is the copied part of a fold, immutable once built.
type seriesFoldView struct {
	x           string
	points      int32
	from, to    string
	untimed     int64
	lanes       []SeriesLaneReading
	lanesHidden int32
	grid        SeriesGridReading
	finding     *SeriesFinding
}

// seriesOverlayKey identifies what the score lane's walk depends on: the
// lane's input (the score record, the detector call and window) and the
// series fold the baseline is computed from.
type seriesOverlayKey struct {
	scoresGen uint64
	foldGen   uint64
}

// seriesOverlayFold is the part of the overlays that walks the score lane.
type seriesOverlayFold struct {
	caption    string
	warm       int32
	scoreMax   *float64
	scoreMaxAt string
}

// seriesOpsView is what get_series reads.
type seriesOpsView struct {
	fold      *seriesFoldView
	overlays  *SeriesOverlays
	smoothing bool
	halfWidth int32
	envelope  bool
	drawn     int32
	sourced   int32
	notes     []string
}

// foldView copies the driver's last series fold.
func (inst *SeriesDriver) foldView() (v *seriesFoldView) {
	v = &seriesFoldView{x: opsLabel(inst.tLabel), points: int32(len(inst.t)), untimed: inst.skippedRows,
		lanesHidden: int32(inst.droppedLane)}
	if n := len(inst.t); n > 0 {
		v.from, v.to = formatEpochMS(int64(inst.t[0]*1000)), formatEpochMS(int64(inst.t[n-1]*1000))
	}
	v.grid = SeriesGridReading{Class: inst.grid.class.String(), Gaps: int32(inst.grid.gaps)}
	if inst.grid.medianSec > 0 {
		v.grid.Step, v.grid.StepSeconds = formatSeriesStep(inst.grid.medianSec), inst.grid.medianSec
	}
	if hint, action, sql := inst.gridFinding(); hint != "" {
		v.finding = &SeriesFinding{Text: hint, Action: action, Scaffold: sql}
	}
	v.lanes = make([]SeriesLaneReading, 0, len(inst.lanes))
	for i := range inst.lanes {
		l := &inst.lanes[i]
		lo, hi := math.Inf(1), math.Inf(-1)
		for j, val := range l.vals {
			if l.valid[j] {
				lo, hi = math.Min(lo, val), math.Max(hi, val)
			}
		}
		r := SeriesLaneReading{Label: opsLabel(l.label), Nulls: int32(l.nulls), Min: finite(lo), Max: finite(hi)}
		if n := len(l.vals); n > 0 && l.valid[n-1] {
			r.Last = finite(l.vals[n-1])
		}
		v.lanes = append(v.lanes, r)
	}
	return
}

// overlayFold walks the score lane once: the caption counts its warm-up,
// and the highest score is read off it.
func (inst *SeriesDriver) overlayFold() (f *seriesOverlayFold) {
	sc := &inst.scores
	f = &seriesOverlayFold{caption: inst.seriesOverlayCaption()}
	for _, w := range sc.warm {
		if w {
			f.warm++
		}
	}
	best, at := math.Inf(-1), -1
	for i, s := range sc.score {
		if i < len(sc.warm) && sc.warm[i] {
			continue
		}
		if !math.IsNaN(s) && s > best {
			best, at = s, i
		}
	}
	if at >= 0 {
		f.scoreMax = finite(best)
		if at < len(sc.t) {
			f.scoreMaxAt = formatEpochMS(int64(sc.t[at] * 1000))
		}
	}
	return
}

// overlays is the overlay part of the reading. The spans and the readout
// are copied each snapshot (at most 100 spans, and the verdicts change
// under the person's clicks); the score lane is walked once per lane.
func (inst *PlayApp) seriesOverlays(d *SeriesDriver) (o *SeriesOverlays) {
	sc := &d.scores
	if len(sc.t) == 0 && len(d.spans) == 0 && d.spansSkipped == 0 {
		return nil
	}
	o = &SeriesOverlays{SpansSkipped: int32(d.spansSkipped)}
	if len(sc.t) > 0 {
		key := seriesOverlayKey{scoresGen: d.scoresGen, foldGen: d.foldGen}
		if inst.paneViews.seriesOverlay == nil || inst.paneViews.seriesOverlayKey != key {
			inst.paneViews.seriesOverlay, inst.paneViews.seriesOverlayKey = d.overlayFold(), key
		}
		f := inst.paneViews.seriesOverlay
		o.Caption, o.WarmUp, o.ScoreMax, o.ScoreMaxAt = f.caption, f.warm, f.scoreMax, f.scoreMaxAt
		o.Detector, o.Window, o.Causal = opsLabel(sc.detector), sc.window, sc.causal
		o.Baseline, o.BaselineWhy = len(sc.baseline) > 0, sc.baselineWhy
	}
	n := min(len(d.spans), opsSeriesMaxSpans)
	o.SpansCut = int32(len(d.spans) - n)
	for i := range d.spans[:n] {
		s := &d.spans[i]
		r := SeriesSpanReading{From: formatEpochMS(int64(s.from * 1000)), To: formatEpochMS(int64(s.to * 1000)), Label: opsLabel(s.label)}
		if v := d.labels[tsLabelKey{fromMS: int64(s.from * 1000), toMS: int64(s.to * 1000)}]; v != tsVerdictNone {
			r.Verdict = v.String()
		}
		o.Spans = append(o.Spans, r)
	}
	if d.haveRead {
		o.Readout = seriesReadoutLine(d.readout)
	}
	return
}

// seriesView is the Series' view for the snapshot.
func (inst *PlayApp) seriesView() (v seriesOpsView) {
	d := inst.seriesDriver
	if d.t != nil && (inst.paneViews.series == nil || inst.paneViews.seriesGen != d.foldGen) {
		inst.paneViews.series, inst.paneViews.seriesGen = d.foldView(), d.foldGen
	}
	v = seriesOpsView{fold: inst.paneViews.series, overlays: inst.seriesOverlays(d),
		smoothing: d.smooth.On, halfWidth: d.smooth.HalfWidth(), envelope: d.decimate}
	if d.decimate && d.sourced > d.drawn && d.drawn > 0 {
		v.drawn, v.sourced = int32(d.drawn), int32(d.sourced)
	}
	if len(d.auxNotes) > 0 {
		v.notes = make([]string, 0, len(d.auxNotes))
		for _, n := range d.auxNotes {
			v.notes = append(v.notes, truncateBytes(n, opsStatusMaxBytes))
		}
	}
	return
}

// seriesReading is get_series.
func seriesReading(sn *opsSnap, _ appops.None) (out SeriesReading, err error) {
	d, readable, err := paneDrawOf(sn, seriesPaneId)
	if err != nil {
		return
	}
	v := sn.paneViews.series
	out = SeriesReading{Drawn: d, Smoothing: v.smoothing, HalfWidth: v.halfWidth, Envelope: v.envelope}
	if !readable || v.fold == nil {
		return
	}
	f := v.fold
	out.X, out.Points, out.From, out.To, out.Untimed = f.x, f.points, f.from, f.to, f.untimed
	out.Lanes, out.LanesHidden, out.Finding = f.lanes, f.lanesHidden, f.finding
	g := f.grid
	out.Grid = &g
	out.DrawnPoints, out.SourcePts = v.drawn, v.sourced
	out.Overlays, out.LaneNotes = v.overlays, v.notes
	return
}

// seriesHalfWidthRange is the half-widths trendsmooth accepts.
func seriesHalfWidthRange() (lo int32, hi int32) {
	s := trendsmooth.New()
	s.SetHalfWidth(0)
	return s.HalfWidth(), trendsmooth.MaxHalfWidth
}

// setOptions is set_series_options on the driver; a half-width outside
// the kernel's range is refused, not clamped, so the caller learns the
// range.
func (inst *SeriesDriver) setOptions(in SetSeriesOptionsArgs) (err error) {
	if in.Smoothing == nil && in.HalfWidth == nil && in.Envelope == nil {
		return noOptionsRefusal(seriesPaneId, "smoothing", "half_width", "envelope")
	}
	if in.HalfWidth != nil {
		if lo, hi := seriesHalfWidthRange(); *in.HalfWidth < lo || *in.HalfWidth > hi {
			return app.RefuseOperation("half_width is " + strconv.Itoa(int(lo)) + " to " + strconv.Itoa(int(hi)) + " samples")
		}
	}
	if in.Smoothing != nil {
		inst.smooth.On = *in.Smoothing
	}
	if in.HalfWidth != nil {
		inst.smooth.SetHalfWidth(*in.HalfWidth)
	}
	if in.Envelope != nil {
		inst.decimate = *in.Envelope
	}
	return
}

// requestOptions is the smoothing stepper's click: through
// set_series_options when play's launcher routes it, directly otherwise.
func (inst *SeriesDriver) requestOptions(in SetSeriesOptionsArgs) {
	if inst.onOptions != nil {
		inst.onOptions(in)
		return
	}
	_ = inst.setOptions(in)
}

// seriesOptionsDigest is the series resource: smoothing, its half-width and
// the envelope, compared across the write-back.
func seriesOptionsDigest(p *PlayApp) string {
	d := p.seriesDriver
	if d == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("smooth=" + strconv.FormatBool(d.smooth.On))
	b.WriteString("|hw=" + strconv.Itoa(int(d.smooth.HalfWidth())))
	b.WriteString("|envelope=" + strconv.FormatBool(d.decimate))
	return b.String()
}

func addSeriesOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	addPaneOps(s, paneOpsSpec[SeriesReading, appops.None, SetSeriesOptionsArgs]{
		pane:     seriesPaneId,
		resource: "the Series pane's options: smoothing, its half-width and the envelope",
		digest:   seriesOptionsDigest,
		get:      opGetSeries,
		getSummary: "read what the Series pane last drew: the time range, the Δt class with the pane's finding and scaffold SQL, " +
			"each lane's range, the smoothing and envelope, and the score and span overlays with each span's recorded verdict",
		set:        opSetSeriesOptions,
		setSummary: "set the Series pane's smoothing, its half-width, or the envelope; drawing only",
		gesture:    "the smoothing stepper above the series",
		follows: []string{"the pane draws with the new settings from its next frame; the result is not rerun",
			"verdicts on spans stay the person's: no operation records one"},
		read:  seriesReading,
		apply: func(p *PlayApp, in SetSeriesOptionsArgs) error { return p.seriesDriver.setOptions(in) },
	})
}
