package play

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/analytics/stats/distsql"
	"github.com/stergiotis/boxer/public/analytics/stats/letterval"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// The Distribution pane as an agent reads and sets it (ADR-0270, update of
// 2026-10-05): per series its count, quantiles read off its grid, the
// width of the band the ECDF view draws and the Wasserstein-1 distance to
// the baseline the Shift view draws, with letter values on request; which
// views the result admits and which one is drawn. The quantiles and
// distances are computed in the query from the copied grids, with the
// pane's own functions. The baseline is the selected series, so it moves
// with set_signal('selection', row); the view buttons go through
// set_dist_options.

const (
	opGetDist        = "get_dist"
	opSetDistOptions = "set_dist_options"
	opsResDist       = distPaneId
	distPaneId       = "dist"
)

// distViewNames is the pane's views in its view index order.
var distViewNames = []string{"ecdf", "shift", "boxen", "histogram"}

// distReadingPs is where get_dist reads each series' quantiles.
var distReadingPs = []float64{0.05, 0.25, 0.5, 0.75, 0.95}

// GetDistArgs is get_dist's argument.
type GetDistArgs struct {
	LetterValues bool `json:",omitzero" desc:"add each series' letter values, the levels the Boxen view draws"`
}

// DistViewOption is one view of the pane.
type DistViewOption struct {
	Name      string `desc:"ecdf, shift, boxen or histogram"`
	Available bool   `desc:"whether this result admits the view"`
	WhyNot    string `json:",omitzero" desc:"why the result does not admit it"`
}

// DistQuantile is one quantile read off a series' grid.
type DistQuantile struct {
	P     float64 `desc:"the probability"`
	Value float64 `desc:"the value at p, interpolated linearly between the grid's points; a p outside the grid reads as the grid's end value"`
}

// DistLetterValue is one letter-value level.
type DistLetterValue struct {
	Depth     int32   `desc:"1 is the median, 2 the fourths, 3 the eighths, and so on"`
	LowerP    float64 `desc:"the lower probability, 2^-depth"`
	UpperP    float64 `desc:"the upper probability, 1 - 2^-depth"`
	Lower     float64 `desc:"the value at the lower probability"`
	Upper     float64 `desc:"the value at the upper probability"`
	TailCount int64   `desc:"the expected count of observations beyond each end of the level"`
}

// DistSeriesReading is one series of the pane.
type DistSeriesReading struct {
	Label     string `desc:"the series' label, from its series column"`
	Row       int32  `desc:"the result row the series is; set_signal('selection', row) makes it the baseline"`
	N         int64  `desc:"its count, from n"`
	Nulls     int64  `json:",omitzero" desc:"its null count, from n_null"`
	Estimator string `json:",omitzero" desc:"the estimator that made its grid, from estimator; a sketch's error is not in the bands"`
	// Degenerate series are skipped by every view.
	Degenerate bool               `json:",omitzero" desc:"true when every quantile is the same value; the views skip it"`
	Min        *float64           `json:",omitzero" desc:"its smallest value, from x_min, when the result carries it"`
	Max        *float64           `json:",omitzero" desc:"its largest value, from x_max, when the result carries it"`
	GridPoints int32              `desc:"how many (p, value) points its grid has"`
	GridFrom   float64            `desc:"the grid's first probability"`
	GridTo     float64            `desc:"the grid's last probability"`
	Quantiles  []DistQuantile     `desc:"its values at p = 0.05, 0.25, 0.5, 0.75 and 0.95, read off its grid as the views read it"`
	DkwEpsilon *float64           `json:",omitzero" desc:"the half-width, in probability, of the 95% DKW simultaneous band the ECDF view draws; it excludes sketch error"`
	W1         *float64           `json:",omitzero" desc:"the Wasserstein-1 distance to the baseline over the shared grid, as the Shift view's legend gives it; the tails beyond the grid add nothing, so it underestimates; absent for the baseline and when the grids differ"`
	Letters    []DistLetterValue  `json:",omitzero" desc:"its letter values, when asked for"`
	Histogram  *DistHistogramNote `json:",omitzero" desc:"its histogram triplet's extent, when the result carries one"`
}

// DistHistogramNote is the extent of a series' histogram triplet.
type DistHistogramNote struct {
	Bins int32   `desc:"how many bins"`
	From float64 `desc:"the first bin's lower edge"`
	To   float64 `desc:"the last bin's upper edge"`
}

// DistReading is get_dist's result.
type DistReading struct {
	Drawn       PaneDraw            `desc:"which draw this is of, its status line, and why it drew nothing when it did not"`
	View        string              `desc:"the view the pane draws: ecdf, shift, boxen or histogram"`
	Views       []DistViewOption    `json:",omitzero" desc:"each view and whether this result admits it"`
	Baseline    string              `json:",omitzero" desc:"the selected series, which the Shift view compares the others to"`
	BaselineRow int32               `desc:"the baseline's result row"`
	SharedGrid  bool                `desc:"whether every series carries the same ps grid; the Shift view and W1 need it"`
	Histogram   bool                `desc:"whether every series carries the histogram triplet; the Histogram view needs it"`
	SeriesShown int32               `desc:"series drawn"`
	SeriesCut   int64               `json:",omitzero" desc:"rows past the 32 series the pane draws; GROUP BY coarser to see them"`
	Series      []DistSeriesReading `json:",omitzero" desc:"the drawn series in row order"`
}

// SetDistOptionsArgs is set_dist_options' argument; a field left out keeps
// the person's setting.
type SetDistOptionsArgs struct {
	View *string `json:",omitzero" desc:"ecdf, shift, boxen or histogram; one get_dist lists as available"`
}

// distFoldView is the copied part of a fold, immutable once built. The
// series' grids are the driver's own slices: a fold allocates them and
// never writes them again.
type distFoldView struct {
	series     []distSeries
	sharedGrid bool
	haveHist   bool
	truncated  int64
}

// distOpsView is what get_dist reads.
type distOpsView struct {
	fold     *distFoldView
	view     int
	selected int
}

// distView is the Distribution's view for the snapshot.
func (inst *PlayApp) distView() (v distOpsView) {
	d := inst.distDriver
	if d.series != nil && (inst.paneViews.dist == nil || inst.paneViews.distGen != d.foldGen) {
		inst.paneViews.dist = &distFoldView{series: slices.Clone(d.series), sharedGrid: d.sharedGrid,
			haveHist: d.haveHist, truncated: d.truncated}
		inst.paneViews.distGen = d.foldGen
	}
	return distOpsView{fold: inst.paneViews.dist, view: d.view, selected: d.selected}
}

// distViewAvailability is which views a fold admits, by the pane's rules.
func distViewAvailability(series int, sharedGrid bool, haveHist bool) (out []DistViewOption) {
	out = make([]DistViewOption, len(distViewNames))
	for i, name := range distViewNames {
		out[i] = DistViewOption{Name: name, Available: true}
	}
	if series < 2 || !sharedGrid {
		out[1].Available = false
		out[1].WhyNot = "the Shift view needs two or more series on one shared ps grid"
	}
	if !haveHist {
		out[3].Available = false
		out[3].WhyNot = "the Histogram view needs hist_lo, hist_hi and hist_w on every series"
	}
	return
}

// distReading is get_dist.
func distReading(sn *opsSnap, in GetDistArgs) (out DistReading, err error) {
	d, readable, err := paneDrawOf(sn, distPaneId)
	if err != nil {
		return
	}
	v := sn.paneViews.dist
	out = DistReading{Drawn: d, View: distViewNames[min(max(v.view, 0), len(distViewNames)-1)]}
	if !readable || v.fold == nil || len(v.fold.series) == 0 {
		return
	}
	f := v.fold
	out.Views = distViewAvailability(len(f.series), f.sharedGrid, f.haveHist)
	out.SharedGrid, out.Histogram = f.sharedGrid, f.haveHist
	out.SeriesShown, out.SeriesCut = int32(len(f.series)), f.truncated
	base := min(max(v.selected, 0), len(f.series)-1)
	out.Baseline, out.BaselineRow = opsLabel(f.series[base].label), int32(base)
	out.Series = make([]DistSeriesReading, 0, len(f.series))
	for i := range f.series {
		out.Series = append(out.Series, distSeriesReading(&f.series[i], i, &f.series[base], i != base && f.sharedGrid, in.LetterValues))
	}
	return
}

// distSeriesReading reads one series off its grid with the functions the
// views draw with.
func distSeriesReading(s *distSeries, row int, base *distSeries, withW1 bool, letters bool) (r DistSeriesReading) {
	r = DistSeriesReading{Label: opsLabel(s.label), Row: int32(row), N: s.n, Nulls: s.nNull,
		Estimator: opsLabel(s.estimator), Degenerate: s.degenerate(), GridPoints: int32(len(s.ps))}
	if s.haveExtremes {
		r.Min, r.Max = finite(s.xMin), finite(s.xMax)
	}
	if lo := s.hist[0]; len(lo) > 0 {
		r.Histogram = &DistHistogramNote{Bins: int32(len(lo)), From: lo[0], To: s.hist[1][len(lo)-1]}
	}
	if len(s.ps) == 0 {
		return
	}
	r.GridFrom, r.GridTo = s.ps[0], s.ps[len(s.ps)-1]
	r.DkwEpsilon = finite(distsql.DkwEpsilon(s.n, distBandAlpha))
	if withW1 {
		r.W1 = finite(distsql.Wasserstein1(s.ps, s.qs, base.qs))
	}
	o, err := distsql.NewGridOracle(s.ps, s.qs, s.n)
	if err != nil {
		return
	}
	r.Quantiles = make([]DistQuantile, 0, len(distReadingPs))
	for _, p := range distReadingPs {
		if q := finite(o.Quantile(p)); q != nil {
			r.Quantiles = append(r.Quantiles, DistQuantile{P: p, Value: *q})
		}
	}
	if letters && !r.Degenerate {
		for _, l := range letterval.Levels(o, distsql.GridMaxDepth(s.ps, s.n)) {
			if finite(l.LowerValue) == nil || finite(l.UpperValue) == nil {
				continue
			}
			r.Letters = append(r.Letters, DistLetterValue{Depth: int32(l.Depth), LowerP: l.LowerQ, UpperP: l.UpperQ,
				Lower: l.LowerValue, Upper: l.UpperValue, TailCount: l.TailCount})
		}
	}
	return
}

// setOptions is set_dist_options on the driver. A view the last fold does
// not admit is refused with the pane's reason; before anything has folded,
// a known view is kept for when it does.
func (inst *DistDriver) setOptions(in SetDistOptionsArgs) (err error) {
	if in.View == nil {
		return noOptionsRefusal(distPaneId, "view")
	}
	i := slices.Index(distViewNames, strings.ToLower(strings.TrimSpace(*in.View)))
	if i < 0 {
		return app.RefuseOperation("no view " + strconv.Quote(*in.View) + "; a view is " + strings.Join(distViewNames, ", "))
	}
	if len(inst.series) > 0 {
		if opt := distViewAvailability(len(inst.series), inst.sharedGrid, inst.haveHist)[i]; !opt.Available {
			return app.RefuseOperation(opt.WhyNot)
		}
	}
	inst.view = i
	return
}

// requestOptions is a view button's click: through set_dist_options when
// play's launcher routes it, directly otherwise.
func (inst *DistDriver) requestOptions(in SetDistOptionsArgs) {
	if inst.onOptions != nil {
		inst.onOptions(in)
		return
	}
	_ = inst.setOptions(in)
}

// distOptionsDigest is the dist resource: the view.
func distOptionsDigest(p *PlayApp) string {
	if p.distDriver == nil {
		return ""
	}
	return "view=" + strconv.Itoa(p.distDriver.view)
}

func addDistOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	addPaneOps(s, paneOpsSpec[DistReading, GetDistArgs, SetDistOptionsArgs]{
		pane:     distPaneId,
		resource: "the Distribution pane's options: the view it draws",
		digest:   distOptionsDigest,
		get:      opGetDist,
		getSummary: "read what the Distribution pane last drew: per series its count, quantiles, band width and " +
			"Wasserstein-1 distance to the baseline, optionally its letter values; the views the result admits and the one drawn",
		set:        opSetDistOptions,
		setSummary: "set the Distribution pane's view: ecdf, shift, boxen or histogram",
		gesture:    "the view buttons above the distributions",
		follows: []string{"the pane draws the view from its next frame; the result is not rerun",
			"the Shift view's baseline is the selected series: set_signal('selection', row) moves it"},
		read:  distReading,
		apply: func(p *PlayApp, in SetDistOptionsArgs) error { return p.distDriver.setOptions(in) },
	})
}
