package play

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/analytics/stats/distsql"
	"github.com/stergiotis/boxer/public/analytics/stats/letterval"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/boxenplot"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/ecdf"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// play_dist_panel.go is the ADR-0161 Distribution dock tab: a result set
// carrying the distribution contract rendered as comparison-first
// distribution views (ECDF + bands, shift function, letter-value boxen,
// histogram when supplied).
//
// The contract is named columns rather than detection (ADR-0122 §SD1 lineage):
// `series`, `n`, `ps`, `qs` required; moments, `hist_lo`/`hist_hi`/`hist_w`
// and `estimator` optional — the column names and the grid rules live in
// public/analytics/stats/distsql, shared with the descriptiveStatistics
// macro (M1). Acceptance is schema-only and cheap; the grid VALUES are
// validated at fold time, and a bad row rejects the whole result loudly
// (never a silently empty plot).
//
// The selected series is the shift view's baseline (ADR-0161 §SD5): a chip
// click publishes the ordinary row-cursor selection — this is a main-result
// panel, so the row IS a result row and Detail follows.

const (
	// distMaxSeries bounds the fold. Overlaid distribution plots stop being
	// readable long before this; the excess is counted in the status line
	// rather than silently dropped.
	distMaxSeries = 32
	// distBandAlpha is the simultaneous band level (1-α = 95%).
	distBandAlpha = 0.05
	// distMaxBandsAll: bands paint on every series up to this many series;
	// beyond, only the selected series carries its band (ADR-0161 §SD5).
	distMaxBandsAll = 3
	// distPlotHeight is the PREFERRED plot-box height; both dimensions follow
	// the pane when the pane is smaller (one-frame lag on the probe, fine for
	// a stable dock).
	//
	// It was a FIXED height until 2026-08-06, and that was a bug — the same one
	// the Chart tab carried (ADR-0172): implot draws the x tick labels along
	// the BOTTOM of the plot box, so a box taller than its pane loses them, and
	// the part of the y range below the clip reads as missing data rather than
	// as a cropped view. In an applet window (~900×660, and not resizable out
	// of) a 420pt box had barely half a pane to sit in. The surrounding
	// ScrollArea does not rescue it: implot captures the wheel while the
	// pointer is over the plot (ADR-0140), so the reader has to move off the
	// chart before the labels can be scrolled to.
	distPlotHeight = 420
	distPlotMinW   = 480
	// distPaneSlack keeps the box off the pane's edge.
	distPaneSlack = 8
)

// distPlotMinH floors the box at the height below which implot clips its own x
// tick labels: the gutters come out of the box, so a box under that leaves the
// layout taller than the canvas and the bottom gutter — the tick labels and
// the axis title under them — is what the canvas cuts. Read from the widget
// rather than guessed, because a floor under it clips while the pane still
// looks roomy.
//
// It is not a readability floor and must not be raised into one: a floor set
// where a view stops being COMFORTABLE would overshoot a small pane and clip
// the labels this sizing exists to keep. Three of the four views label both
// axes, which is the deeper gutter, so all four take that figure — 76pt at the
// time of writing.
var distPlotMinH = implot.MinBoxHeight(false, true, true, 1)

// distPaneProbeSalt namespaces the pane probe's r21 slot; threading it through
// the instance's id stack makes it window-unique, so two playgrounds size their
// own plot.
const distPaneProbeSalt uint64 = 0xd1570d15791b0001

// distClaim is the panel's channel claim: resolved column indices (-1 when
// an optional is absent) plus the row to highlight from the selection signal.
type distClaim struct {
	seriesCol, nCol, psCol, qsCol    int
	nNullCol                         int
	xMinCol, xMaxCol                 int
	meanCol, sdCol, skewCol, kurtCol int
	histLoCol, histHiCol, histWCol   int
	estimatorCol                     int
	selRow                           int64
}

// distSeries is one folded row of the contract.
type distSeries struct {
	label        string
	n, nNull     int64
	ps, qs       []float64
	xMin, xMax   float64
	haveExtremes bool
	hist         [3][]float64 // lo, hi, w — all or none
	estimator    string
}

func (inst *distSeries) degenerate() bool {
	return len(inst.qs) == 0 || !(inst.qs[len(inst.qs)-1] > inst.qs[0])
}

// DistDriver owns the Distribution tab state: the folded model, its cache
// key, and the panel-local view + baseline state.
type DistDriver struct {
	ids *c.WidgetIdStack

	series     []distSeries
	foldErr    string // data-level reject (grid rules); whole-result, loud
	sharedGrid bool   // every series carries the identical ps grid
	haveHist   bool   // every series carries the histogram triplet
	truncated  int64

	forExecuted     time.Time
	forSchema       *arrow.Schema
	pendingExecuted time.Time

	view     int // 0 ecdf, 1 shift, 2 boxen, 3 histogram
	selected int // selected series index = the shift baseline

	// paneW/paneH is the last good answer from the pane probe. The probe
	// reports nothing on the frame a hidden tab comes back — and this tab is
	// Lazy — so sizing off the miss would flash the plot to its floor every
	// time the reader switches to it. The last good answer is held instead.
	paneW, paneH float32
}

func NewDistDriver(ids *c.WidgetIdStack) (inst *DistDriver) {
	return &DistDriver{ids: ids}
}

func (inst *DistDriver) noteExecuted(t time.Time) { inst.pendingExecuted = t }

// distPanel is the PanelI face. Acceptance is schema-only — the grid rules
// need the data and run in the fold (data-dependent work belongs in Render).
type distPanel struct {
	driver *DistDriver
}

func (inst distPanel) ID() PanelID { return "dist" }

func (inst distPanel) Channels() []ChannelSpec {
	return []ChannelSpec{{ID: chMain, Required: true, Label: "distributions"}}
}

func (inst distPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if schema == nil {
		reason = "Run a query to see distributions."
		return
	}
	k, reason := resolveDistColumns(schema)
	if reason != "" {
		return
	}
	k.selRow, _ = readSelection(sig)
	claim = k
	return
}

func (inst distPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	main := filled[chMain]
	k, ok := main.Claim.(distClaim)
	if !ok {
		return
	}
	inst.driver.render(main.Rec, main.Rec.Schema(), k, emit)
}

// resolveDistColumns applies the ADR-0161 §SD1 contract to a schema. Pure and
// schema-only; every reject carries the reason the pane paints in place.
func resolveDistColumns(schema *arrow.Schema) (k distClaim, reason string) {
	k = distClaim{seriesCol: -1, nCol: -1, psCol: -1, qsCol: -1, nNullCol: -1,
		xMinCol: -1, xMaxCol: -1, meanCol: -1, sdCol: -1, skewCol: -1, kurtCol: -1,
		histLoCol: -1, histHiCol: -1, histWCol: -1, estimatorCol: -1, selRow: -1}
	grids := map[string]*int{distsql.ColPs: &k.psCol, distsql.ColQs: &k.qsCol,
		distsql.ColHistLo: &k.histLoCol, distsql.ColHistHi: &k.histHiCol, distsql.ColHistW: &k.histWCol}
	scalars := map[string]*int{distsql.ColXMin: &k.xMinCol, distsql.ColXMax: &k.xMaxCol,
		distsql.ColMean: &k.meanCol, distsql.ColSd: &k.sdCol, distsql.ColSkew: &k.skewCol, distsql.ColKurt: &k.kurtCol}
	for ci, f := range schema.Fields() {
		switch {
		case f.Name == distsql.ColSeries:
			k.seriesCol = ci
		case f.Name == distsql.ColN:
			if !isKanbanCountType(f.Type) {
				return k, fmt.Sprintf("Column `%s` must be an integer count; it is %s. count(col) yields one.", distsql.ColN, f.Type)
			}
			k.nCol = ci
		case f.Name == distsql.ColNNull:
			if isKanbanCountType(f.Type) {
				k.nNullCol = ci
			}
		case f.Name == distsql.ColEstimator:
			k.estimatorCol = ci
		default:
			if dst, isGrid := grids[f.Name]; isGrid {
				if !isFloatListType(f.Type) {
					return k, fmt.Sprintf("Column `%s` must be Array(Float64); it is %s.", f.Name, f.Type)
				}
				*dst = ci
				continue
			}
			if dst, isScalar := scalars[f.Name]; isScalar {
				*dst = ci
			}
		}
	}
	if k.seriesCol < 0 || k.nCol < 0 || k.psCol < 0 || k.qsCol < 0 {
		return k, distContractHint(k)
	}
	histPresent := 0
	for _, ci := range []int{k.histLoCol, k.histHiCol, k.histWCol} {
		if ci >= 0 {
			histPresent++
		}
	}
	if histPresent != 0 && histPresent != 3 {
		return k, fmt.Sprintf("The histogram triplet is all-or-none: name `%s`, `%s` and `%s` together.",
			distsql.ColHistLo, distsql.ColHistHi, distsql.ColHistW)
	}
	return k, ""
}

// distContractHint is the reject shown when required columns are absent — it
// names the contract and a query shape that satisfies it.
func distContractHint(k distClaim) string {
	var missing []string
	for _, m := range []struct {
		col  int
		name string
	}{{k.seriesCol, distsql.ColSeries}, {k.nCol, distsql.ColN}, {k.psCol, distsql.ColPs}, {k.qsCol, distsql.ColQs}} {
		if m.col < 0 {
			missing = append(missing, "`"+m.name+"`")
		}
	}
	return fmt.Sprintf("Distributions need %s columns (ADR-0161): one row per series, "+
		"e.g. SELECT 'latency' AS series, count(x) AS n, [0.25,0.5,0.75] AS ps, quantilesTDigest(0.25,0.5,0.75)(x) AS qs FROM t "+
		"— or write descriptiveStatistics(x) once the macro lands.",
		strings.Join(missing, ", "))
}

func isFloatListType(dt arrow.DataType) bool {
	switch t := dt.(type) {
	case *arrow.ListType:
		return isFloatScalarType(t.Elem())
	case *arrow.LargeListType:
		return isFloatScalarType(t.Elem())
	case *arrow.FixedSizeListType:
		return isFloatScalarType(t.Elem())
	}
	return false
}

func isFloatScalarType(dt arrow.DataType) bool {
	return dt.ID() == arrow.FLOAT64 || dt.ID() == arrow.FLOAT32
}

// distListFloats reads one Array(Float*) cell as []float64.
func distListFloats(col arrow.Array, row int64) (out []float64, ok bool) {
	if row < 0 || int(row) >= col.Len() || col.IsNull(int(row)) {
		return nil, false
	}
	var vals arrow.Array
	var start, end int64
	switch a := col.(type) {
	case *array.List:
		start, end = a.ValueOffsets(int(row))
		vals = a.ListValues()
	case *array.LargeList:
		start, end = a.ValueOffsets(int(row))
		vals = a.ListValues()
	case *array.FixedSizeList:
		start, end = a.ValueOffsets(int(row))
		vals = a.ListValues()
	default:
		return nil, false
	}
	out = make([]float64, 0, end-start)
	switch v := vals.(type) {
	case *array.Float64:
		for i := start; i < end; i++ {
			out = append(out, v.Value(int(i)))
		}
	case *array.Float32:
		for i := start; i < end; i++ {
			out = append(out, float64(v.Value(int(i))))
		}
	default:
		return nil, false
	}
	return out, true
}

// render folds the result (cached), draws the honesty line, the view and
// series selectors, and the active view; publishes chip clicks as the shared
// selection.
func (inst *DistDriver) render(rec arrow.RecordBatch, schema *arrow.Schema, k distClaim, emit SignalEmitterI) {
	inst.rebuild(rec, schema, k)
	if inst.foldErr != "" {
		for rt := range c.RichTextLabel(inst.foldErr) {
			rt.Small().Weak()
		}
		return
	}
	if len(inst.series) == 0 {
		for rt := range c.RichTextLabel("The query returned no rows, so there is no distribution to draw.") {
			rt.Small().Weak()
		}
		return
	}
	// Follow the shared selection: the selected series is also the shift
	// baseline, so the rest of the dock and this panel agree on focus.
	if k.selRow >= 0 && k.selRow < int64(len(inst.series)) {
		inst.selected = int(k.selRow)
	}
	if inst.selected >= len(inst.series) {
		inst.selected = 0
	}

	dens := styletokens.DensityFromEnv()
	c.Label(inst.statusLine()).Send()
	c.AddSpace(styletokens.GapInline(dens))
	inst.renderSelectors(emit, k)
	c.AddSpace(styletokens.GapItems(dens))

	// Seq-keyed pane probe, window-unique through the instance's id stack (one
	// frame behind). NOT CaptureAvailableSize: one process-wide slot the
	// frame's last capture wins, and play's Detail pane renders after every
	// body tab — so with a temporal row selected the plot was drawn at the
	// width of the narrow side column.
	//
	// Emitted HERE, after the chrome above the plot and before the plot
	// itself: the rect is the room left for the NEXT widget, so a probe placed
	// after the plot would size the plot against its own output.
	if availW, availH, ok := c.CapturePaneSize(inst.ids.PrepareHighEntropy(distPaneProbeSalt).Derive()); ok {
		inst.paneW, inst.paneH = availW, availH
	}
	w := float32(distPlotMinW)
	if inst.paneW > distPlotMinW {
		w = inst.paneW - distPaneSlack
	}
	// One view is drawn at a time, so all four take the same box.
	h := inst.plotHeight()
	switch inst.view {
	case 1:
		inst.renderShift(w, h)
	case 2:
		inst.renderBoxen(w, h)
	case 3:
		inst.renderHistogram(w, h)
	default:
		inst.renderEcdf(w, h)
	}
}

// plotHeight is the plot box's height: the preferred one, or the pane when the
// pane is shorter. A box taller than its pane is clipped at the bottom, taking
// the x tick labels with it (see distPlotHeight).
func (inst *DistDriver) plotHeight() (h float32) {
	h = distPlotHeight
	if inst.paneH > 0 && inst.paneH-distPaneSlack < h {
		h = inst.paneH - distPaneSlack
	}
	return max(h, distPlotMinH)
}

// renderSelectors draws the view chips and the series chips. A series chip
// click publishes the row selection (baseline follows it).
func (inst *DistDriver) renderSelectors(emit SignalEmitterI, k distClaim) {
	shiftOK := len(inst.series) >= 2 && inst.sharedGrid
	views := []struct {
		name string
		ok   bool
	}{{"ECDF", true}, {"Shift", shiftOK}, {"Boxen", true}, {"Histogram", inst.haveHist}}
	for range c.HorizontalTop().KeepIter() {
		for i, v := range views {
			if !v.ok {
				continue
			}
			sel := inst.view == i
			if c.Button(inst.ids.PrepareSeq(uint64(0xd150+i)), c.Atoms().Text(v.name).Keep()).
				Selected(sel).FrameWhenInactive(false).Frame(true).SendResp().HasPrimaryClicked() {
				inst.view = i
			}
		}
		if !shiftOK && len(inst.series) >= 2 {
			for rt := range c.RichTextLabel("(shift needs one shared ps grid)") {
				rt.Small().Weak()
			}
		}
	}
	for range c.HorizontalTop().KeepIter() {
		for i, s := range inst.series {
			for range c.IdScope(inst.ids.PrepareSeq(uint64(0xd200 + i))) {
				sel := i == inst.selected
				if c.Button(inst.ids.PrepareStr("chip"), c.Atoms().BeginRichText("● ").Size(10).End().Text(s.label).Keep()).
					Selected(sel).FrameWhenInactive(false).Frame(true).SendResp().HasPrimaryClicked() {
					inst.selected = i
					if int64(i) != k.selRow {
						emit.Emit(signalSelection, int64(i))
					}
				}
			}
		}
	}
}

func (inst *DistDriver) renderEcdf(w float32, h float32) {
	for p := range implot.Scoped(inst.ids, "##play-dist-ecdf", w, h) {
		p.SetupAxes("value", "F(x)", implot.AxisFlagsNone, implot.AxisFlagsNone)
		for i := range inst.series {
			s := &inst.series[i]
			if s.degenerate() {
				continue
			}
			r := ecdf.New().SeriesName(s.label).
				EcdfStroke(distSeriesColor(i), 1.6).
				Alpha(distBandAlpha)
			if len(inst.series) <= distMaxBandsAll || i == inst.selected {
				r = r.BandFill(distSeriesFill(i))
				_ = r.RenderGridPreview(p, s.qs, s.ps, int(s.n))
			} else {
				r.RenderGridCurveOnly(p, s.qs, s.ps)
			}
		}
	}
}

// renderShift draws Δ(p) = Q_s(p) − Q_baseline(p) per non-baseline series,
// with the conservative α/2+α/2 combined band (ADR-0161 §SD5) built by
// inverting each series' DKW F-band through its grid oracle.
func (inst *DistDriver) renderShift(w float32, h float32) {
	base := &inst.series[inst.selected]
	baseOracle, err := distsql.NewGridOracle(base.ps, base.qs, base.n)
	if err != nil {
		return
	}
	epsBase := distsql.DkwEpsilon(base.n, distBandAlpha/2)
	for p := range implot.Scoped(inst.ids, "##play-dist-shift", w, h) {
		p.SetupAxes("p", "Δ value vs "+base.label, implot.AxisFlagsNone, implot.AxisFlagsNone)
		for i := range inst.series {
			s := &inst.series[i]
			if i == inst.selected || s.degenerate() {
				continue
			}
			o, oerr := distsql.NewGridOracle(s.ps, s.qs, s.n)
			if oerr != nil {
				continue
			}
			eps := distsql.DkwEpsilon(s.n, distBandAlpha/2)
			ps := s.ps
			delta := make([]float64, len(ps))
			lo := make([]float64, len(ps))
			hi := make([]float64, len(ps))
			for j, pv := range ps {
				delta[j] = s.qs[j] - base.qs[j]
				lo[j] = o.Quantile(pv-eps) - baseOracle.Quantile(pv+epsBase)
				hi[j] = o.Quantile(pv+eps) - baseOracle.Quantile(pv-epsBase)
			}
			label := fmt.Sprintf("%s (W1 %.4g)", s.label, distsql.Wasserstein1(ps, s.qs, base.qs))
			p.ShadedBetween(label, ps, lo, hi)
			p.Line(label, ps, delta)
		}
	}
}

func (inst *DistDriver) renderBoxen(w float32, h float32) {
	for p := range implot.Scoped(inst.ids, "##play-dist-boxen", w, h) {
		// Setup before the first item, or the plot locks it out and drops both
		// calls at log level — which leaves the categorical argument axis to the
		// default numeric locator, labelling the gaps between the columns.
		positions := make([]float64, 0, len(inst.series))
		labels := make([]string, 0, len(inst.series))
		for i := range inst.series {
			positions = append(positions, float64(i))
			labels = append(labels, inst.series[i].label)
		}
		p.SetupAxisTicks(implot.AxisX1, positions, labels)
		p.SetupAxes("", "value", implot.AxisFlagsNone, implot.AxisFlagsNone)
		for i := range inst.series {
			s := &inst.series[i]
			if s.degenerate() {
				continue
			}
			o, err := distsql.NewGridOracle(s.ps, s.qs, s.n)
			if err != nil {
				continue
			}
			levels := letterval.Levels(o, distsql.GridMaxDepth(s.ps, s.n))
			var extremes []float64
			if s.haveExtremes {
				extremes = []float64{s.xMin, s.xMax}
			}
			boxenplot.New("play-dist-boxen").SeriesName(s.label).
				Render(p, float64(i), levels, extremes, letterval.BudgetFor(levels).Each)
		}
	}
}

// renderHistogram draws the optional server-side histogram triplet as a
// density step (height = weight / width — variable-width bins mislead
// otherwise; ADR-0161 §SD5).
func (inst *DistDriver) renderHistogram(w float32, h float32) {
	for p := range implot.Scoped(inst.ids, "##play-dist-hist", w, h) {
		p.SetupAxes("value", "density", implot.AxisFlagsNone, implot.AxisFlagsNone)
		for i := range inst.series {
			s := &inst.series[i]
			lo, hi, wt := s.hist[0], s.hist[1], s.hist[2]
			if len(lo) == 0 {
				continue
			}
			xs := make([]float64, 0, 2*len(lo))
			ys := make([]float64, 0, 2*len(lo))
			for b := range lo {
				d := wt[b] / (hi[b] - lo[b])
				xs = append(xs, lo[b], hi[b])
				ys = append(ys, d, d)
			}
			p.Shaded(s.label, xs, ys, 0)
		}
	}
}

// rebuild folds the result into the series model, keyed on (executed,
// schema). A row violating the grid rules rejects the whole result with the
// row named — the ADR's loud-reject contract at the data level.
func (inst *DistDriver) rebuild(rec arrow.RecordBatch, schema *arrow.Schema, k distClaim) {
	if inst.series != nil && schema == inst.forSchema && inst.pendingExecuted.Equal(inst.forExecuted) {
		return
	}
	inst.forSchema = schema
	inst.forExecuted = inst.pendingExecuted
	inst.foldErr = ""
	inst.truncated = 0

	rows := rec.NumRows()
	if rows > distMaxSeries {
		inst.truncated = rows - distMaxSeries
		rows = distMaxSeries
	}
	series := make([]distSeries, 0, rows)
	for row := int64(0); row < rows; row++ {
		s := distSeries{label: formatCell(rec, k.seriesCol, row)}
		if nv, ok := numericCellValue(rec.Column(k.nCol), row); ok {
			s.n = int64(nv)
		}
		var okPs, okQs bool
		s.ps, okPs = distListFloats(rec.Column(k.psCol), row)
		s.qs, okQs = distListFloats(rec.Column(k.qsCol), row)
		if !okPs || !okQs {
			inst.reject(row, s.label, "ps/qs cell is NULL or not a float array")
			return
		}
		if err := distsql.ValidateSeries(s.ps, s.qs); err != nil {
			inst.reject(row, s.label, err.Error())
			return
		}
		if k.nNullCol >= 0 {
			if nv, ok := numericCellValue(rec.Column(k.nNullCol), row); ok {
				s.nNull = int64(nv)
			}
		}
		if k.xMinCol >= 0 && k.xMaxCol >= 0 {
			lo, okLo := numericCellValue(rec.Column(k.xMinCol), row)
			hi, okHi := numericCellValue(rec.Column(k.xMaxCol), row)
			if okLo && okHi {
				s.xMin, s.xMax, s.haveExtremes = lo, hi, true
			}
		}
		if k.histLoCol >= 0 {
			lo, ok1 := distListFloats(rec.Column(k.histLoCol), row)
			hi, ok2 := distListFloats(rec.Column(k.histHiCol), row)
			wt, ok3 := distListFloats(rec.Column(k.histWCol), row)
			if ok1 && ok2 && ok3 {
				if err := distsql.ValidateHist(lo, hi, wt); err != nil {
					inst.reject(row, s.label, err.Error())
					return
				}
				s.hist = [3][]float64{lo, hi, wt}
			}
		}
		if k.estimatorCol >= 0 {
			s.estimator = formatCell(rec, k.estimatorCol, row)
		}
		series = append(series, s)
	}
	inst.series = series
	inst.sharedGrid = true
	inst.haveHist = len(series) > 0
	for i := range series {
		if !slices.Equal(series[i].ps, series[0].ps) {
			inst.sharedGrid = false
		}
		if len(series[i].hist[0]) == 0 {
			inst.haveHist = false
		}
	}
}

// reject records a data-level fold rejection; series stays non-nil so the
// cache key holds and the message does not re-fold every frame.
func (inst *DistDriver) reject(row int64, label, why string) {
	inst.series = []distSeries{}
	inst.foldErr = fmt.Sprintf("Row %s (series %q) violates the distribution contract: %s. The panel draws nothing until every row is valid.",
		humanize.Comma(int64(row)), label, why)
}

func (inst *DistDriver) statusLine() string {
	var b strings.Builder
	var totalN, totalNull int64
	estimators := make([]string, 0, 2)
	for i := range inst.series {
		s := &inst.series[i]
		totalN += s.n
		totalNull += s.nNull
		e := s.estimator
		if e == "" {
			e = "unlabelled"
		}
		if !slices.Contains(estimators, e) {
			estimators = append(estimators, e)
		}
	}
	fmt.Fprintf(&b, "%d series · Σn %s", len(inst.series), humanize.Comma(int64(totalN)))
	if totalNull > 0 {
		fmt.Fprintf(&b, " (nulls %s)", humanize.Comma(int64(totalNull)))
	}
	fmt.Fprintf(&b, " · estimator: %s", strings.Join(estimators, ", "))
	fmt.Fprintf(&b, " · band: DKW %.0f%% preview", (1-distBandAlpha)*100)
	if slices.Contains(estimators, "unlabelled") || !slices.Contains(estimators, "exact-hf7") {
		b.WriteString(" (excludes sketch error)")
	}
	if !inst.sharedGrid && len(inst.series) > 1 {
		b.WriteString(" · grids differ across series")
	}
	if inst.truncated > 0 {
		fmt.Fprintf(&b, " · %s more series not shown (cap %d — GROUP BY coarser)",
			humanize.Comma(int64(inst.truncated)), distMaxSeries)
	}
	return b.String()
}

func distSeriesColor(i int) color.Color {
	return color.Hex(styletokens.QualitativeCycle(i).AsHex())
}

// distSeriesFill is the band fill: the series colour at low alpha.
func distSeriesFill(i int) color.Color {
	return color.Hex((styletokens.QualitativeCycle(i).AsHex() &^ 0xff) | 0x30)
}

// renderDistTab is the dock-tab entry (spec.Render): frame plumbing around
// the PanelI dispatch, mirroring the Kanban tab minus its lanes channel.
func (inst *PlayApp) renderDistTab(rec arrow.RecordBatch, schema *arrow.Schema, loading bool, err error, executed time.Time) {
	if loading && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		inst.renderResultsFailed()
		return
	}
	if rec == nil {
		for rt := range c.RichTextLabel("Run a query emitting the distribution contract (`series`, `n`, `ps`, `qs` — ADR-0161) to see distributions.") {
			rt.Small().Weak()
		}
		return
	}
	inst.distDriver.noteExecuted(executed)
	inputs := map[ChannelID]channelInput{
		chMain: {node: inst.resolvedTabNode("dist"), rec: rec, schema: schema, sig: inst.frameSig},
	}
	reject := dispatchPanel(distPanel{driver: inst.distDriver}, inputs, inst.sigEmit)
	if reject != "" {
		for rt := range c.RichTextLabel(reject) {
			rt.Small().Weak()
		}
	}
}
