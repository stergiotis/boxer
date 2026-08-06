package play

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colorscale"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// play_chart_panel.go is the ADR-0172 Chart dock tab: the plain chart play had
// no carrier for. Series draws a value against a TIME axis, Distribution a
// quantile contract, Projection an embedding — none of them draws a category
// against a count, or a number against a number, or a two-key grid against a
// third value, which is what an ordinary GROUP BY produces.
//
// The claim is NAMED columns (ADR-0122 §SD1), bare, and it has two readings
// discriminated by whether a numeric `z` is present (§SD1):
//
//   - LANES — `x` plus every other numeric column, each one a series whose
//     legend label IS its column name; an optional `series` column splits the
//     rows into groups. Both the wide idiom (a GROUP BY with several
//     aggregates) and the long one (a GROUP BY with a grouping key) reach a
//     result directly, and neither converts to the other in a line of SQL.
//     `x` is OPTIONAL here: with no `x` column the rows are numbered 1, 2, 3 …
//     in the order the query returned them (§SD2), which is the one order a
//     result always has.
//   - GRID — `x`, `y` and `z`: one row per cell, drawn as a heatmap over the
//     distinct keys. Both keys must be real columns — a row number cannot
//     stand in for one, since it would put every row in its own column.
//
// The names are claimed BY NAME, not by type: `series` is never a lane even
// when it is numeric, and neither is `x`. A column named `y` is an ordinary
// lane in the lanes reading, which is what makes `SELECT a AS x, b AS y` draw
// with no ceremony at all.
//
// What the panel refuses to do carries as much weight as what it draws. It
// never fills a hole: a NULL becomes NaN, and the port already breaks the
// polyline at NaN, skips the marker and skips the bar (ADR-0163 §SD2's rule,
// inherited). It never invents a cell: a repeated (x, y) pair rejects the
// whole result rather than letting last-write-wins fabricate a matrix. And it
// never sorts a categorical axis: the query's row order is the only order a
// string has (§SD2).

// The claimed column names (§SD1). Everything else numeric is a lane.
const (
	chartColX      = "x"
	chartColY      = "y"
	chartColZ      = "z"
	chartColSeries = "series"
)

const (
	// chartMaxRows bounds the fold. Rows past it are counted in the status
	// line rather than silently dropped — this panel truncates where the
	// Series tab decimates (ADR-0172 §SD6).
	chartMaxRows = 100_000
	// chartMaxSeries bounds the drawn (group × lane) pairs. Past two dozen
	// overlaid series a shared axis stops being readable, and the qualitative
	// palette has wrapped twice over.
	chartMaxSeries = 24
	// chartMaxCells bounds the grid. Unlike the row cap this one REJECTS
	// rather than truncates: half a heatmap is not a smaller heatmap.
	chartMaxCells = 40_000
	// chartMaxTickLabels is the most categorical ticks worth labelling; past
	// it the labels overplot into an unreadable band, so they are dropped and
	// the status line says so.
	chartMaxTickLabels = 40
	// chartClickTol is the selection radius as a fraction of the viewport,
	// measured in axis-normalised space so it means the same at any zoom.
	chartClickTol = 0.05
	// chartFitMargin pads the auto-fit by this fraction of each axis's data
	// span. The port fits tight, as upstream does, which puts the extreme
	// sample exactly ON the plot border — a scatter marker there is drawn
	// half outside, and the tallest bar merges into the frame. The margin
	// goes through IncludeX/IncludeY rather than pinned limits, so a
	// double-click refit and a legend toggle still take the ordinary path.
	chartFitMargin = 0.05
	// chartPlotHeight is the PREFERRED plot-box height; both dimensions follow
	// the pane when the pane is smaller.
	//
	// It used to be fixed, as it is in the Distribution and Series tabs, and
	// that is a bug those tabs share: a box taller than its pane is clipped at
	// the BOTTOM, which is exactly where implot draws the x tick labels. In an
	// applet window (~900×660, and not resizable out of) a 380pt box in a
	// ~255pt pane lost every category label, and the part of the y range below
	// the clip read as missing bars — a top-N ranking looked like it had
	// dropped two thirds of its rows and moved its baseline. The pane's
	// ScrollArea does not save it either: implot captures the wheel while the
	// pointer is over the plot (ADR-0140), so the labels cannot be scrolled to
	// without first moving the pointer off the chart.
	chartPlotHeight = 380
	chartPlotMinW   = 480
	// chartPaneSlack keeps the box off the pane's edge.
	chartPaneSlack = 8
	// chartColorbarH is the colorscale legend's box under a heatmap, tall
	// enough for the gradient AND its tick labels. The grid reading takes it
	// out of the plot rather than adding it below, so both readings occupy the
	// same vertical budget and the legend does not land under the fold.
	chartColorbarH = 44
	// chartColorbarMaxW keeps the legend visibly shorter than the plot it
	// annotates — a gradient the full width of a wide pane reads as a second
	// chart rather than as a key.
	chartColorbarMaxW = 640
)

// chartPlotMinH floors the box at the height below which implot clips its own
// x tick labels. There are TWO ways a pane-sized plot loses them and this is
// the second: a box taller than its pane is cut by the pane, and a box shorter
// than this is cut by its own canvas, because the gutters come out of the box
// height rather than sitting outside it. The original 80 was picked as "far
// below comfortable" without knowing where that second bound was; it happened
// to clear it. Reading it from the widget removes the coincidence.
//
// It is not a readability floor and must not be raised into one — a floor set
// where a chart stops being COMFORTABLE would overshoot a small pane and clip
// the labels this sizing exists to keep. Both axes are labelled here, the
// deeper gutter configuration: 76pt at the time of writing.
var chartPlotMinH = implot.MinBoxHeight(false, true, true, 1)

// chartPaneProbeSalt namespaces the pane probe's register slot; threading it
// through the instance id stack makes it window-unique, so two playgrounds
// size their own plot. NOT CaptureAvailableSize — that is one process-wide
// slot the frame's last capture wins (the Distribution tab's lesson).
const chartPaneProbeSalt uint64 = 0xc4a271e0b3d50001

// chartReadingE is which of §SD1's two readings a schema satisfied.
type chartReadingE uint8

const (
	chartReadingLanes chartReadingE = iota
	chartReadingGrid
)

// chartAxisE is how a key column projects onto an axis (§SD2).
type chartAxisE uint8

const (
	// chartAxisCategorical: distinct values in FIRST-APPEARANCE order take
	// positions 0, 1, 2, … A string has no intrinsic order, so the query's
	// ORDER BY is the only order it has and sorting would override the author.
	chartAxisCategorical chartAxisE = iota
	chartAxisNumeric
	chartAxisTemporal
	// chartAxisRow: there is NO `x` column, so the rows are numbered 1, 2, 3 …
	// ascending in the order the query returned them — the one order every
	// result has. It is 1-based because Detail names the same row `row 1 / N`,
	// so the number on the axis is the number a click leads to. When a `series`
	// column splits the rows the numbering restarts inside each group, which is
	// what makes the groups overlay on a shared abscissa the way a real key
	// would; the status line says so, because that alignment is positional and
	// not something the result measured.
	chartAxisRow
)

// chartMarkE is the drawn mark (§SD3). Which are offered is decided by the
// resolved types; which is picked is the reader's, seeded by the same types.
type chartMarkE uint8

const (
	chartMarkLine chartMarkE = iota
	chartMarkBar
	chartMarkScatter
	chartMarkHeatmap
)

func (inst chartMarkE) String() (name string) {
	switch inst {
	case chartMarkBar:
		name = "Bar"
	case chartMarkScatter:
		name = "Scatter"
	case chartMarkHeatmap:
		name = "Heatmap"
	default:
		name = "Line"
	}
	return
}

// chartClaim is the panel's channel claim: the reading, the resolved column
// indices (-1 when absent) and their axis kinds, plus the row to highlight
// from the selection signal.
type chartClaim struct {
	reading   chartReadingE
	xCol      int
	xAxis     chartAxisE
	yCol      int // grid reading only
	yAxis     chartAxisE
	zCol      int // grid reading only
	seriesCol int // lanes reading only; -1 when the result carries none
	laneCols  []int
	selRow    int64
}

// chartLane is one drawn series of the lanes reading. barXs is xs shifted by
// the series' dodge offset, precomputed at fold time so a 100k-point bar chart
// does not reallocate every frame.
type chartLane struct {
	label     string
	xs, barXs []float64
	ys        []float64
	rows      []int64
	nulls     int
	// minY is what gates the log chip: +Inf when the lane carries no value at
	// all, which leaves the gate alone — there is nothing for a log axis to
	// drop.
	minY float64
}

// chartGrid is the folded grid reading. values and rows are row-major in
// DISPLAY order — row 0 is the TOP, which is implot's Heatmap orientation
// contract, so the LAST y key is row 0. A hole is NaN in values and -1 in rows.
type chartGrid struct {
	xLabels, yLabels []string
	values           []float64
	rows             []int64
	nRows, nCols     int
	vmin, vmax       float64
	holes            int
}

// ChartDriver owns the Chart tab state: the folded model, its cache key, and
// the panel-local mark and scale choices.
type ChartDriver struct {
	ids *c.WidgetIdStack

	reading    chartReadingE
	lanes      []chartLane
	grid       chartGrid
	catLabels  []string // the categorical x axis's ticks, in position order
	xName      string
	yName      string
	zName      string
	xAxis      chartAxisE
	xPerSeries bool    // the implicit abscissa restarts inside each `series`
	barSlot    float64 // the x-slot width a grouped bar chart divides (§SD4)
	logOK      bool    // every drawn value is strictly positive
	// The folded extents across every lane, which the fit margin is computed
	// from. Held on the driver rather than per lane because the margin is a
	// property of the plot, not of one series.
	xMin, xMax, yMin, yMax float64

	foldErr       string // data-level reject; whole-result, loud
	truncated     int64
	droppedSeries int
	points        int
	foldedOnce    bool

	forExecuted     time.Time
	forSchema       *arrow.Schema
	pendingExecuted time.Time

	mark     chartMarkE
	markSet  bool // the reader picked; otherwise the default follows the data
	logScale bool

	// paneW/paneH is the last good answer from the pane probe. The probe
	// reports nothing on the frame a hidden tab comes back — and this tab is
	// Lazy — so sizing off the miss would flash the plot to its floor every
	// time the reader switches to it. The last good answer is held instead.
	paneW, paneH float32

	// cm is the heatmap's colormap, kept across folds so the colorscale legend
	// beside it stays bound to one Config (the widget's documented idiom); the
	// fold mutates its range in place.
	cm    *colormap.Config
	cbar  *colorscale.ColorScale
	cmLog bool
}

func NewChartDriver(ids *c.WidgetIdStack) (inst *ChartDriver) {
	return &ChartDriver{ids: ids, cm: colormap.NewConfig(colormap.Viridis8, 0, 1)}
}

func (inst *ChartDriver) noteExecuted(t time.Time) { inst.pendingExecuted = t }

// chartPanel is the PanelI face. Acceptance is schema-only and cheap — the
// duplicate-cell rule and the key interning need the data and run in the fold.
type chartPanel struct {
	driver *ChartDriver
}

func (inst chartPanel) ID() PanelID { return "chart" }

func (inst chartPanel) Channels() []ChannelSpec {
	return []ChannelSpec{{ID: chMain, Required: true, Label: "chart"}}
}

func (inst chartPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if schema == nil {
		reason = "Run a query with at least one numeric column to draw a chart."
		return
	}
	k, reason := resolveChartColumns(schema)
	if reason != "" {
		return
	}
	k.selRow, _ = readSelection(sig)
	claim = k
	return
}

func (inst chartPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	main := filled[chMain]
	k, ok := main.Claim.(chartClaim)
	if !ok {
		return
	}
	inst.driver.render(main.Rec, main.Rec.Schema(), k, emit)
}

// resolveChartColumns applies the §SD1 contract to a schema. Pure and
// schema-only; every reject names what THIS result carries, so the message
// says what is wrong here rather than restating the contract.
func resolveChartColumns(schema *arrow.Schema) (k chartClaim, reason string) {
	k = chartClaim{xCol: -1, yCol: -1, zCol: -1, seriesCol: -1, selRow: -1}
	for ci, f := range schema.Fields() {
		switch f.Name {
		case chartColX:
			k.xCol = ci
		case chartColY:
			k.yCol = ci
		case chartColZ:
			k.zCol = ci
		case chartColSeries:
			k.seriesCol = ci
		}
	}
	// A `z` column is the grid discriminator: with it, `x` and `y` are the
	// cell keys and `z` is the value. It is claimed by name, so a `z` that
	// cannot BE a cell value is a mistake worth naming rather than ignoring.
	if k.zCol >= 0 {
		if !isNumericType(schema.Field(k.zCol).Type) {
			reason = fmt.Sprintf("Column `z` is a heatmap's cell value and must be numeric; it is %s. "+
				"Cast it, or rename it if this result is not a grid.", schema.Field(k.zCol).Type)
			return
		}
		// Both keys must be REAL columns. The lanes reading numbers the rows
		// when `x` is absent, and that substitution is wrong here rather than
		// merely unhelpful: one cell key per row puts every row in a column of
		// its own, which is a diagonal, not a grid.
		if k.yCol < 0 || k.xCol < 0 {
			reason = "A heatmap needs `x`, `y` and `z` — the two cell keys and the cell value " +
				"(ADR-0172), e.g. `SELECT toHour(ts) AS x, toDayOfWeek(ts) AS y, count() AS z " +
				"FROM t GROUP BY x, y`. This result has `z` but no " + chartMissingGridKeys(k) + ". " +
				"A grid key is never filled in by the row number: every row would be its own cell."
			return
		}
		k.reading = chartReadingGrid
		k.xAxis = chartAxisFor(schema.Field(k.xCol).Type)
		k.yAxis = chartAxisFor(schema.Field(k.yCol).Type)
		return
	}

	// The lanes reading. `x` is optional: with no column claiming it the rows
	// are numbered in the order the query returned them (§SD2), so an ordinary
	// `SELECT count() AS n … ORDER BY n DESC` draws as the ranking it is.
	if k.xCol >= 0 {
		k.xAxis = chartAxisFor(schema.Field(k.xCol).Type)
	} else {
		k.xAxis = chartAxisRow
	}
	for ci, f := range schema.Fields() {
		if ci == k.xCol || ci == k.seriesCol {
			continue // claimed by name — never a lane, whatever its type
		}
		if isNumericType(f.Type) {
			k.laneCols = append(k.laneCols, ci)
		}
	}
	if len(k.laneCols) == 0 {
		if k.xCol >= 0 {
			reason = "A chart needs at least one numeric column besides `x` to draw against it. " +
				"Every number in the result becomes a series labelled by its own column name. " +
				chartShapeHint(schema)
			return
		}
		// Without `x` the abscissa is free — the rows number themselves — so
		// what is missing is the value, and saying THAT is the difference
		// between a message about the query and one about the contract.
		reason = "A chart needs at least one numeric column to draw. With no `x` column the rows " +
			"number themselves 1, 2, 3 … in the order the query returned them, but there is still " +
			"nothing to draw against that. " + chartShapeHint(schema)
		return
	}
	k.reading = chartReadingLanes
	return
}

// chartMissingGridKeys names which cell keys a `z`-carrying result is short of,
// so the reject is about this result rather than about the contract.
func chartMissingGridKeys(k chartClaim) (missing string) {
	switch {
	case k.xCol < 0 && k.yCol < 0:
		return "`x` and no `y`"
	case k.xCol < 0:
		return "`x`"
	}
	return "`y`"
}

// chartShapeHint names what the result actually carries (the Series tab's
// idiom), so a reject is about this query rather than about the contract.
func chartShapeHint(schema *arrow.Schema) (hint string) {
	names := make([]string, 0, schema.NumFields())
	for _, f := range schema.Fields() {
		names = append(names, "`"+f.Name+"` "+f.Type.String())
	}
	if len(names) == 0 {
		return "This result has no columns."
	}
	return "This result has " + strings.Join(names, ", ") + "."
}

// chartAxisFor decides how a key column projects onto an axis (§SD2). The
// temporal test comes first: a timestamp is numeric underneath, and stringifying
// one into a category would sort by its text.
func chartAxisFor(dt arrow.DataType) (axis chartAxisE) {
	switch {
	case isSeriesTemporalType(dt):
		axis = chartAxisTemporal
	case isNumericType(dt):
		axis = chartAxisNumeric
	default:
		axis = chartAxisCategorical
	}
	return
}

// chartKeyer interns the distinct values of a key column. Slots are handed out
// in first-appearance order; finish then applies §SD2's ordering rule and
// returns the provisional→final remap.
type chartKeyer struct {
	axis    chartAxisE
	index   map[string]int
	labels  []string
	sortKey []float64
}

func newChartKeyer(axis chartAxisE) (inst *chartKeyer) {
	return &chartKeyer{axis: axis, index: make(map[string]int, 32)}
}

func (inst *chartKeyer) add(label string, num float64) (slot int) {
	if s, seen := inst.index[label]; seen {
		return s
	}
	slot = len(inst.labels)
	inst.index[label] = slot
	inst.labels = append(inst.labels, label)
	inst.sortKey = append(inst.sortKey, num)
	return
}

// finish orders the keys: ascending for a numeric or temporal key, which has an
// intrinsic order and arrives shuffled from a GROUP BY without an ORDER BY;
// first-appearance for anything else, which does not.
func (inst *chartKeyer) finish() (labels []string, remap []int) {
	n := len(inst.labels)
	ord := make([]int, n)
	for i := range ord {
		ord[i] = i
	}
	if inst.axis != chartAxisCategorical {
		sort.SliceStable(ord, func(a, b int) bool { return inst.sortKey[ord[a]] < inst.sortKey[ord[b]] })
	}
	labels = make([]string, n)
	remap = make([]int, n)
	for final, prov := range ord {
		labels[final] = inst.labels[prov]
		remap[prov] = final
	}
	return
}

// chartKeyLabel is a key cell as its axis label. NULL reads as ∅ rather than
// as the empty string, so an absent key is a visible tick and not a blank one.
func chartKeyLabel(rec arrow.RecordBatch, col int, row int64) (label string) {
	label = formatCell(rec, col, row)
	if label == "" {
		label = "∅"
	}
	return
}

// chartKeySort is the ordering value behind a key label: the number itself for
// a numeric key, epoch seconds for a temporal one, and an ignored zero for a
// categorical one (which finish leaves in first-appearance order).
func chartKeySort(rec arrow.RecordBatch, col int, row int64, axis chartAxisE) (v float64) {
	switch axis {
	case chartAxisNumeric:
		v, _ = numericCellValue(rec.Column(col), row)
	case chartAxisTemporal:
		if ms, ok := temporalCellMS(rec.Column(col), int(row), false); ok {
			v = float64(ms) / 1000
		}
	}
	return
}

// render folds the result (cached on the Distribution tab's key), draws the
// status line, the mark chips and the plot, and publishes a click as the
// ordinary row cursor.
func (inst *ChartDriver) render(rec arrow.RecordBatch, schema *arrow.Schema, k chartClaim, emit SignalEmitterI) {
	inst.rebuild(rec, schema, k)
	if inst.foldErr != "" {
		for rt := range c.RichTextLabel(inst.foldErr) {
			rt.Small().Weak()
		}
		return
	}
	if inst.points == 0 {
		for rt := range c.RichTextLabel("The query returned no rows, so there is nothing to chart.") {
			rt.Small().Weak()
		}
		return
	}

	dens := styletokens.DensityFromEnv()
	c.Label(inst.statusLine()).Send()
	c.AddSpace(styletokens.GapInline(dens))
	inst.renderChips()
	c.AddSpace(styletokens.GapItems(dens))

	// Seq-keyed pane probe, window-unique through the instance id stack (one
	// frame behind). Emitted HERE, after the chrome above the plot and before
	// the plot itself: the rect is the room left for the NEXT widget, so a
	// probe placed after the plot would size the plot against its own output.
	if availW, availH, ok := c.CapturePaneSize(inst.ids.PrepareHighEntropy(chartPaneProbeSalt).Derive()); ok {
		inst.paneW, inst.paneH = availW, availH
	}
	w := float32(chartPlotMinW)
	if inst.paneW > chartPlotMinW {
		w = inst.paneW - chartPaneSlack
	}
	if inst.reading == chartReadingGrid {
		inst.renderGrid(w, inst.plotHeight(), k, emit)
		return
	}
	inst.renderLanes(w, inst.plotHeight(), k, emit)
}

// activeMark resolves the mark to draw: the reader's pick when it is still
// available for this result, the type-seeded default otherwise (§SD3).
func (inst *ChartDriver) activeMark() (mark chartMarkE) {
	avail := inst.availableMarks()
	if inst.markSet && slices.Contains(avail, inst.mark) {
		return inst.mark
	}
	return avail[0]
}

// availableMarks is the chip row, most-appropriate first — the head of the
// slice is also the default (§SD3).
func (inst *ChartDriver) availableMarks() (marks []chartMarkE) {
	if inst.reading == chartReadingGrid {
		return []chartMarkE{chartMarkHeatmap}
	}
	if inst.xAxis == chartAxisCategorical {
		// Bars answer "how big is each category"; Line and Scatter stay
		// available because with the ticks labelled a line across categories
		// is exactly as meaningful as the row order it follows.
		return []chartMarkE{chartMarkBar, chartMarkLine, chartMarkScatter}
	}
	// The implicit row number takes the continuous default with the numeric
	// axis, which is Line: with no key to label them, what a numbered result
	// shows is the SHAPE of an ordered sequence — a ranking curve, most often —
	// and a hundred thousand bars one pixel apart is a filled rectangle.
	return []chartMarkE{chartMarkLine, chartMarkBar, chartMarkScatter}
}

// renderChips draws the mark selector and the log toggle. The log control is
// absent — not disabled — when a non-positive value is present: a log axis over
// a zero draws a picture of nothing, and an absent affordance is cheaper to
// understand than a plot that silently drops points.
//
// The marks are chips because they are a mutually-exclusive set, and the log
// scale is a CHECKBOX because it is an independent boolean. A pressed-button
// state is too weak a signal to read a boolean off — the control looked inert
// even while it worked, which is the report that moved it.
func (inst *ChartDriver) renderChips() {
	active := inst.activeMark()
	for range c.HorizontalTop().KeepIter() {
		for i, m := range inst.availableMarks() {
			if c.Button(inst.ids.PrepareSeq(uint64(0xc4a0+i)), c.Atoms().Text(m.String()).Keep()).
				Selected(m == active).FrameWhenInactive(false).Frame(true).SendResp().HasPrimaryClicked() {
				inst.mark, inst.markSet = m, true
			}
		}
		if inst.logOK {
			label := "log y"
			if inst.reading == chartReadingGrid {
				label = "log colour"
			}
			c.Checkbox(inst.ids.PrepareStr("chart-log"), inst.logScale, label).
				SendRespVal(&inst.logScale)
		}
	}
}

// applyFitMargin widens the auto-fit so an extreme sample is drawn inside the
// plot rather than on its border. Two carve-outs, both about not asserting
// something the data does not:
//
//   - a bar chart keeps its BASELINE. Bars grow from zero and the port already
//     fits it, so padding below would float the bars above the axis and read
//     as a truncated scale. Only the free end is padded.
//   - a log axis pads MULTIPLICATIVELY. Subtracting a linear margin from the
//     minimum of a log axis walks toward — or past — zero, which has no
//     position on it. logOK guarantees every value is positive, so dividing
//     keeps it there.
func (inst *ChartDriver) applyFitMargin(p *implot.Plot, mark chartMarkE) {
	if math.IsInf(inst.xMin, 0) || math.IsInf(inst.yMin, 0) {
		return // nothing numeric was folded
	}
	if mark != chartMarkBar {
		// Bars already extend the x fit by half a bar either side.
		padX := chartAxisPad(inst.xMin, inst.xMax)
		p.IncludeX(inst.xMin - padX)
		p.IncludeX(inst.xMax + padX)
	}
	if inst.logScale {
		p.IncludeY(inst.yMax * (1 + chartFitMargin))
		p.IncludeY(inst.yMin / (1 + chartFitMargin))
		return
	}
	lo := inst.yMin
	if mark == chartMarkBar {
		lo = math.Min(lo, 0)
	}
	padY := chartAxisPad(lo, inst.yMax)
	p.IncludeY(inst.yMax + padY)
	if mark != chartMarkBar || lo < 0 {
		p.IncludeY(lo - padY)
	}
}

// chartAxisPad is one axis's margin: a fraction of the span, falling back to a
// value derived from the magnitude when the span is degenerate — a constant
// series would otherwise be fitted into a zero-height range.
func chartAxisPad(lo float64, hi float64) (pad float64) {
	if span := hi - lo; span > 0 {
		return span * chartFitMargin
	}
	return math.Max(math.Abs(hi)*chartFitMargin, 1)
}

// plotHeight is the plot box's height: the preferred one, or the pane when the
// pane is shorter. A box taller than its pane is clipped at the bottom, taking
// the x tick labels with it (see chartPlotHeight).
func (inst *ChartDriver) plotHeight() (h float32) {
	h = chartPlotHeight
	if inst.paneH > 0 && inst.paneH-chartPaneSlack < h {
		h = inst.paneH - chartPaneSlack
	}
	return max(h, chartPlotMinH)
}

// renderLanes draws the lanes reading under the active mark. Every Setup call
// precedes the first item, which is where the port stops accepting them.
func (inst *ChartDriver) renderLanes(w float32, h float32, k chartClaim, emit SignalEmitterI) {
	mark := inst.activeMark()
	for p := range implot.Scoped(inst.ids, "##play-chart-lanes", w, h) {
		if inst.xAxis == chartAxisTemporal {
			p.SetupAxisScale(implot.AxisX1, implot.ScaleTime)
		}
		if inst.logScale {
			p.SetupAxisScale(implot.AxisY1, implot.ScaleLog10)
		}
		if inst.xAxis == chartAxisCategorical && len(inst.catLabels) <= chartMaxTickLabels {
			pos := make([]float64, len(inst.catLabels))
			for i := range pos {
				pos[i] = float64(i)
			}
			p.SetupAxisTicks(implot.AxisX1, pos, inst.catLabels)
		}
		p.SetupAxes(inst.xName, inst.yName, implot.AxisFlagsNone, implot.AxisFlagsNone)

		for i := range inst.lanes {
			l := &inst.lanes[i]
			switch mark {
			case chartMarkBar:
				p.Bars(l.label, l.barXs, l.ys, inst.barWidth())
			case chartMarkScatter:
				p.Scatter(l.label, l.xs, l.ys, implot.MarkerCircle, 2.5)
			default:
				p.Line(l.label, l.xs, l.ys)
			}
		}
		inst.applyFitMargin(p, mark)
		inst.echoSelection(p, k, mark)
		inst.publishClick(p, k, emit)
	}
}

// echoSelection marks the point the shared row cursor names, so the selection
// reads in both directions: a click here moves Detail, and a row selected
// anywhere else is findable in the plot.
func (inst *ChartDriver) echoSelection(p *implot.Plot, k chartClaim, mark chartMarkE) {
	if k.selRow < 0 {
		return
	}
	for i := range inst.lanes {
		l := &inst.lanes[i]
		for j, row := range l.rows {
			if row != k.selRow || math.IsNaN(l.ys[j]) {
				continue
			}
			x := l.xs[j]
			if mark == chartMarkBar {
				x = l.barXs[j]
			}
			// A label the legend already carries merges into that series'
			// entry and palette slot rather than adding a row of its own.
			p.Scatter(l.label, []float64{x}, []float64{l.ys[j]}, implot.MarkerCross, 7)
			return
		}
	}
}

// renderGrid draws the grid reading as a heatmap plus the colorscale legend
// bound to the same Config — without it the cells carry no readable magnitude.
func (inst *ChartDriver) renderGrid(w float32, h float32, k chartClaim, emit SignalEmitterI) {
	g := &inst.grid
	inst.cm.DataMin, inst.cm.DataMax = g.vmin, g.vmax
	if !(inst.cm.DataMax > inst.cm.DataMin) {
		// One distinct value across every cell: widen so the colormap has a
		// range to normalise into rather than dividing by zero.
		inst.cm.DataMin, inst.cm.DataMax = g.vmin-0.5, g.vmin+0.5
	}
	inst.cm.Scale = colormap.ScaleLinearE
	if inst.logScale && inst.logOK {
		inst.cm.Scale = colormap.ScaleLogE
	}
	// The colorbar comes out of the same budget as the plot, so the pair
	// occupies exactly what the lanes reading does and the legend never lands
	// under the pane's fold.
	for p := range implot.Scoped(inst.ids, "##play-chart-grid", w, max(h-chartColorbarH, chartPlotMinH)) {
		if len(g.xLabels) <= chartMaxTickLabels {
			p.SetupAxisTicks(implot.AxisX1, chartCellCenters(len(g.xLabels)), g.xLabels)
		}
		if len(g.yLabels) <= chartMaxTickLabels {
			p.SetupAxisTicks(implot.AxisY1, chartCellCenters(len(g.yLabels)), g.yLabels)
		}
		p.SetupAxes(inst.xName, inst.yName, implot.AxisFlagsNone, implot.AxisFlagsNone)
		p.Heatmap(inst.zName, g.values, g.nRows, g.nCols, inst.cm,
			0, 0, float64(g.nCols), float64(g.nRows))
		// Declared AFTER the heatmap so it paints over it — Custom items keep
		// upstream's call-order z-model.
		inst.renderGridHover(p)
		inst.publishClick(p, k, emit)
	}
	if inst.cbar == nil || inst.cmLog != inst.cm.IsLog() {
		// The ticker is chosen at construction from the Config's scale, so a
		// scale change needs a fresh legend rather than a re-render.
		inst.cbar = colorscale.New(inst.ids, "play-chart-cbar", inst.cm,
			colorscale.WithSize(min(w, chartColorbarMaxW), chartColorbarH))
		inst.cmLog = inst.cm.IsLog()
	}
	inst.cbar.Render()
}

// renderGridHover outlines the cell under the pointer and annotates it with
// both keys and the value.
//
// A heatmap gets no useful highlight from the built-in one, and the reason is
// structural rather than an oversight: the port expresses legend hover as a
// stroke-weight multiplier, and a heatmap has no stroke to thicken — it is one
// legend entry standing for the whole grid, so there is nothing for hovering it
// to single out. The unit a reader is actually pointing at is the CELL, so that
// is what answers.
//
// A cell no row filled says so rather than reporting a value, which is the same
// distinction its transparent fill makes: "nothing was observed here" is not a
// measurement of zero.
func (inst *ChartDriver) renderGridHover(p *implot.Plot) {
	hx, hy, ok := p.HoverPlotPos()
	if !ok {
		return
	}
	g := &inst.grid
	col, key := int(math.Floor(hx)), int(math.Floor(hy))
	if col < 0 || col >= g.nCols || key < 0 || key >= g.nRows {
		return
	}
	v := g.values[(g.nRows-1-key)*g.nCols+col]
	x0, y0 := float64(col), float64(key)
	accent := styletokens.AccentStrong.AsHex()
	// Anonymous (label ""): the outline is chrome, not a series, so it takes
	// no legend row and no palette slot. Custom items do not contribute to
	// auto-fit, so a hover cannot move the axes.
	p.Custom("", func(dc implot.DrawCtx) {
		c.PaintRectStroke(dc.T.PxX(x0), dc.T.PxY(y0+1), dc.T.PxX(x0+1), dc.T.PxY(y0),
			0, color.Hex(accent), styletokens.StrokeRegular).Send()
	})
	readout := fmt.Sprintf("%s = %s · %s = %s · %s = %s",
		inst.xName, g.xLabels[col], inst.yName, g.yLabels[key], inst.zName, chartNum(v))
	if math.IsNaN(v) {
		readout = fmt.Sprintf("%s = %s · %s = %s · no row",
			inst.xName, g.xLabels[col], inst.yName, g.yLabels[key])
	}
	p.Annotation(x0+0.5, y0+1, 0, -4, accent, true, readout)
}

// chartCellCenters places one tick at the middle of each ordinal cell.
func chartCellCenters(n int) (pos []float64) {
	pos = make([]float64, n)
	for i := range pos {
		pos[i] = float64(i) + 0.5
	}
	return
}

// barWidth is one series' bar width inside its x slot (§SD4): the slot keeps a
// fifth of itself as the gap between neighbouring groups.
func (inst *ChartDriver) barWidth() (w float64) {
	n := len(inst.lanes)
	if n == 0 {
		return 0
	}
	return inst.barSlot * 0.8 / float64(n)
}

// publishClick turns a click into the ordinary row cursor (§SD5). The rows of
// this panel ARE result rows, so Detail follows the same selection the Table
// tab writes.
func (inst *ChartDriver) publishClick(p *implot.Plot, k chartClaim, emit SignalEmitterI) {
	cx, cy, ok := p.Clicked()
	if !ok {
		return
	}
	row, found := inst.rowAt(p, cx, cy)
	if !found || row == k.selRow {
		return
	}
	emit.Emit(signalSelection, row)
}

// rowAt resolves a plot-space click to a result row, reading the axis spans the
// click was made against off the plot. The two readings' geometry lives in
// cellRowAt / nearestRow, which are pure and testable without a rendered plot.
func (inst *ChartDriver) rowAt(p *implot.Plot, cx float64, cy float64) (row int64, ok bool) {
	if inst.reading == chartReadingGrid {
		return inst.cellRowAt(cx, cy)
	}
	x0, x1, okX := p.AxisRangePrev(implot.AxisX1)
	y0, y1, okY := p.AxisRangePrev(implot.AxisY1)
	if !okX || !okY {
		return 0, false
	}
	return inst.nearestRow(cx, cy, x1-x0, y1-y0)
}

// cellRowAt is the grid reading's hit test: simply the cell under the pointer,
// undoing the display flip (row 0 is the top; key index 0 is the bottom).
func (inst *ChartDriver) cellRowAt(cx float64, cy float64) (row int64, ok bool) {
	g := &inst.grid
	col, key := int(math.Floor(cx)), int(math.Floor(cy))
	if col < 0 || col >= g.nCols || key < 0 || key >= g.nRows {
		return 0, false
	}
	row = g.rows[(g.nRows-1-key)*g.nCols+col]
	return row, row >= 0
}

// nearestRow is the lanes reading's hit test: the nearest declared point with
// each axis divided by its visible span, so "nearest" means nearest ON SCREEN
// at any zoom and on any aspect ratio — a raw plot-space distance would snap to
// whichever axis happens to carry the larger numbers. Past chartClickTol of the
// viewport nothing is picked: a click on empty space must not drag the whole
// dock's row cursor to some far-off point.
func (inst *ChartDriver) nearestRow(cx float64, cy float64, spanX float64, spanY float64) (row int64, ok bool) {
	if !(spanX > 0) || !(spanY > 0) {
		return 0, false
	}
	bars := inst.activeMark() == chartMarkBar
	best := math.Inf(1)
	for i := range inst.lanes {
		l := &inst.lanes[i]
		xs := l.xs
		if bars {
			xs = l.barXs
		}
		for j := range xs {
			if math.IsNaN(xs[j]) || math.IsNaN(l.ys[j]) {
				continue
			}
			dx, dy := (xs[j]-cx)/spanX, (l.ys[j]-cy)/spanY
			if d := dx*dx + dy*dy; d < best {
				best, row, ok = d, l.rows[j], true
			}
		}
	}
	if best > chartClickTol*chartClickTol {
		return 0, false
	}
	return row, ok
}

// rebuild folds the result into the drawing model, keyed on (executed, schema
// pointer). A contract violation in the DATA rejects the whole result with the
// offending row named — never a silently partial picture.
func (inst *ChartDriver) rebuild(rec arrow.RecordBatch, schema *arrow.Schema, k chartClaim) {
	if inst.foldedOnce && inst.forSchema == schema && inst.pendingExecuted.Equal(inst.forExecuted) {
		return
	}
	inst.forSchema = schema
	inst.forExecuted = inst.pendingExecuted
	inst.foldedOnce = true
	inst.foldErr = ""
	inst.truncated = 0
	inst.droppedSeries = 0
	inst.points = 0
	inst.lanes = nil
	inst.catLabels = nil
	inst.grid = chartGrid{}
	inst.xMin, inst.yMin = math.Inf(1), math.Inf(1)
	inst.xMax, inst.yMax = math.Inf(-1), math.Inf(-1)
	inst.reading = k.reading
	inst.xAxis = k.xAxis
	inst.xPerSeries = k.xCol < 0 && k.seriesCol >= 0
	// With no `x` the axis title says what the numbers on it are. It is not
	// backticked anywhere it is printed as a column name would be, because
	// there is no such column to look for in the result.
	switch {
	case k.xCol >= 0:
		inst.xName = schema.Field(k.xCol).Name
	case inst.xPerSeries:
		inst.xName = "row in series"
	default:
		inst.xName = "row"
	}

	rows := rec.NumRows()
	if rows > chartMaxRows {
		inst.truncated = rows - chartMaxRows
		rows = chartMaxRows
	}
	if k.reading == chartReadingGrid {
		inst.foldGrid(rec, schema, k, rows)
		return
	}
	inst.foldLanes(rec, schema, k, rows)
}

// foldLanes builds one chartLane per (group, lane) pair. The group order is the
// groups' first appearance, so the legend reads in the order the query emitted.
func (inst *ChartDriver) foldLanes(rec arrow.RecordBatch, schema *arrow.Schema, k chartClaim, rows int64) {
	groups := []string{""}
	groupIdx := map[string]int{"": 0}
	if k.seriesCol >= 0 {
		groups, groupIdx = groups[:0], make(map[string]int, 8)
		for row := range rows {
			g := chartKeyLabel(rec, k.seriesCol, row)
			if _, seen := groupIdx[g]; !seen {
				groupIdx[g] = len(groups)
				groups = append(groups, g)
			}
		}
	}

	type slot struct {
		group int
		col   int
	}
	slots := make([]slot, 0, len(groups)*len(k.laneCols))
	for gi := range groups {
		for _, col := range k.laneCols {
			slots = append(slots, slot{group: gi, col: col})
		}
	}
	if len(slots) > chartMaxSeries {
		inst.droppedSeries = len(slots) - chartMaxSeries
		slots = slots[:chartMaxSeries]
	}

	// One legend entry per label: the port merges same-label items into one
	// palette slot, so a duplicate column name must be disambiguated or two
	// distinct lanes would silently become one.
	taken := make(map[string]bool, len(slots))
	inst.lanes = make([]chartLane, len(slots))
	slotsOfGroup := make([][]int, len(groups))
	for si, s := range slots {
		name := schema.Field(s.col).Name
		label := name
		switch {
		case k.seriesCol < 0:
		case len(k.laneCols) == 1:
			label = groups[s.group]
		default:
			label = groups[s.group] + " · " + name
		}
		for n := 2; taken[label]; n++ {
			label = fmt.Sprintf("%s (%d)", name, n)
		}
		taken[label] = true
		inst.lanes[si] = chartLane{label: label, minY: math.Inf(1)}
		slotsOfGroup[s.group] = append(slotsOfGroup[s.group], si)
	}
	inst.yName = ""
	if len(inst.lanes) == 1 {
		inst.yName = inst.lanes[0].label
	}

	var keyer *chartKeyer
	if k.xAxis == chartAxisCategorical {
		keyer = newChartKeyer(chartAxisCategorical)
	}
	// One ordinal counter per group, for the implicit abscissa of a result with
	// no `x` (§SD2). Numbering restarts inside each `series` rather than running
	// once over the whole result, so the groups OVERLAY the way they would on a
	// real key — a single global counter would lay them out end to end, which is
	// a picture of the row order rather than of the groups. It costs one
	// int64 per group, so it is allocated whether or not the axis needs it.
	ordinals := make([]int64, len(groups))
	group := 0
	for row := range rows {
		if k.seriesCol >= 0 {
			group = groupIdx[chartKeyLabel(rec, k.seriesCol, row)]
		}
		if group >= len(slotsOfGroup) {
			continue
		}
		ordinals[group]++
		x := inst.foldX(rec, k, row, keyer, ordinals[group])
		if !math.IsNaN(x) {
			inst.xMin, inst.xMax = math.Min(inst.xMin, x), math.Max(inst.xMax, x)
		}
		for _, si := range slotsOfGroup[group] {
			l := &inst.lanes[si]
			y, ok := numericCellValue(rec.Column(slots[si].col), row)
			if !ok {
				y, l.nulls = math.NaN(), l.nulls+1
			} else {
				l.minY = math.Min(l.minY, y)
				inst.yMin, inst.yMax = math.Min(inst.yMin, y), math.Max(inst.yMax, y)
			}
			l.xs = append(l.xs, x)
			l.ys = append(l.ys, y)
			l.rows = append(l.rows, row)
			inst.points++
		}
	}
	if keyer != nil {
		// Categorical keys never remap: finish leaves them in the
		// first-appearance order the fold already assigned positions from.
		inst.catLabels, _ = keyer.finish()
	}

	inst.logOK = len(inst.lanes) > 0
	for i := range inst.lanes {
		if !(inst.lanes[i].minY > 0) {
			inst.logOK = false
		}
	}
	inst.barSlot = inst.computeBarSlot()
	inst.applyBarOffsets()
}

// foldX projects one x cell onto the axis §SD2 chose for it. ordinal is the
// row's 1-based position within its own series, used only when the result
// carries no `x` column at all.
func (inst *ChartDriver) foldX(rec arrow.RecordBatch, k chartClaim, row int64, keyer *chartKeyer, ordinal int64) (x float64) {
	switch k.xAxis {
	case chartAxisRow:
		return float64(ordinal)
	case chartAxisNumeric:
		v, ok := numericCellValue(rec.Column(k.xCol), row)
		if !ok {
			return math.NaN()
		}
		return v
	case chartAxisTemporal:
		ms, ok := temporalCellMS(rec.Column(k.xCol), int(row), false)
		if !ok {
			return math.NaN()
		}
		return float64(ms) / 1000
	}
	return float64(keyer.add(chartKeyLabel(rec, k.xCol, row), 0))
}

// computeBarSlot is the x span one group of bars divides (§SD4): 1 on a
// categorical axis, and the smallest positive gap between distinct x values on
// a continuous one — the widest bars that cannot overlap their neighbours.
func (inst *ChartDriver) computeBarSlot() (slot float64) {
	// A categorical axis divides one, and so does the implicit row number:
	// consecutive ordinals ARE a gap of one, so taking the short way out skips
	// sorting every point of a hundred-thousand-row result to rediscover it.
	if inst.xAxis == chartAxisCategorical || inst.xAxis == chartAxisRow {
		return 1
	}
	xs := make([]float64, 0, inst.points)
	for i := range inst.lanes {
		for _, x := range inst.lanes[i].xs {
			if !math.IsNaN(x) {
				xs = append(xs, x)
			}
		}
	}
	if len(xs) < 2 {
		return 1
	}
	sort.Float64s(xs)
	slot = math.Inf(1)
	for i := 1; i < len(xs); i++ {
		if d := xs[i] - xs[i-1]; d > 0 && d < slot {
			slot = d
		}
	}
	if math.IsInf(slot, 1) {
		return 1 // every x identical
	}
	return slot
}

// applyBarOffsets precomputes the dodged x of every point, so a bar chart of a
// hundred thousand points does not rebuild its abscissa every frame.
func (inst *ChartDriver) applyBarOffsets() {
	n := len(inst.lanes)
	if n == 0 {
		return
	}
	w := inst.barWidth()
	for i := range inst.lanes {
		l := &inst.lanes[i]
		off := (float64(i)+0.5)*w - 0.4*inst.barSlot
		l.barXs = make([]float64, len(l.xs))
		for j, x := range l.xs {
			l.barXs[j] = x + off
		}
	}
}

// foldGrid pivots the long (x, y, z) form into the dense matrix implot's
// Heatmap takes, row 0 at the TOP. A repeated cell rejects the whole result:
// last-write-wins would fabricate a matrix the query never asked for.
func (inst *ChartDriver) foldGrid(rec arrow.RecordBatch, schema *arrow.Schema, k chartClaim, rows int64) {
	inst.yName = schema.Field(k.yCol).Name
	inst.zName = schema.Field(k.zCol).Name
	xk, yk := newChartKeyer(k.xAxis), newChartKeyer(k.yAxis)
	provX := make([]int, rows)
	provY := make([]int, rows)
	zs := make([]float64, rows)
	zArr := rec.Column(k.zCol)
	for row := range rows {
		provX[row] = xk.add(chartKeyLabel(rec, k.xCol, row), chartKeySort(rec, k.xCol, row, k.xAxis))
		provY[row] = yk.add(chartKeyLabel(rec, k.yCol, row), chartKeySort(rec, k.yCol, row, k.yAxis))
		z, ok := numericCellValue(zArr, row)
		if !ok {
			z = math.NaN()
		}
		zs[row] = z
	}
	xLabels, xRemap := xk.finish()
	yLabels, yRemap := yk.finish()
	nCols, nRows := len(xLabels), len(yLabels)
	if nCols*nRows > chartMaxCells {
		inst.foldErr = fmt.Sprintf("This grid would be %s × %s = %s cells, past the %s the panel draws "+
			"(ADR-0172). Aggregate the keys coarser — a heatmap that dense is a texture, not a reading.",
			humanize.Comma(int64(nCols)), humanize.Comma(int64(nRows)),
			humanize.Comma(int64(nCols*nRows)), humanize.Comma(chartMaxCells))
		return
	}

	g := chartGrid{xLabels: xLabels, yLabels: yLabels, nRows: nRows, nCols: nCols,
		vmin: math.Inf(1), vmax: math.Inf(-1)}
	g.values = make([]float64, nCols*nRows)
	g.rows = make([]int64, nCols*nRows)
	for i := range g.values {
		g.values[i] = math.NaN()
		g.rows[i] = -1
	}
	for row := range rows {
		dc := xRemap[provX[row]]
		dr := nRows - 1 - yRemap[provY[row]] // row 0 = the top = the LAST key
		idx := dr*nCols + dc
		if g.rows[idx] >= 0 {
			inst.foldErr = fmt.Sprintf("Row %s repeats the cell (x = %s, y = %s), which row %s already filled. "+
				"A heatmap needs one row per cell, so the panel draws nothing until that holds — "+
				"`GROUP BY x, y` with an aggregate over `z` collapses the duplicates.",
				humanize.Comma(row), xLabels[dc], yLabels[nRows-1-dr], humanize.Comma(g.rows[idx]))
			return
		}
		g.values[idx] = zs[row]
		g.rows[idx] = row
		if !math.IsNaN(zs[row]) {
			g.vmin, g.vmax = math.Min(g.vmin, zs[row]), math.Max(g.vmax, zs[row])
		}
		inst.points++
	}
	for _, r := range g.rows {
		if r < 0 {
			g.holes++
		}
	}
	if math.IsInf(g.vmin, 1) {
		g.vmin, g.vmax = 0, 1 // every cell NULL
	}
	inst.grid = g
	inst.logOK = g.vmin > 0
}

// statusLine reports the shape actually drawn — including what was capped and,
// for a grid, that its cells are ORDINAL. A heatmap over the distinct keys of a
// GROUP BY is a matrix, and reading equal cell widths as a numeric spacing is
// exactly the mistake the line exists to prevent (§SD2).
func (inst *ChartDriver) statusLine() string {
	var b strings.Builder
	if inst.reading == chartReadingGrid {
		g := &inst.grid
		fmt.Fprintf(&b, "%s × %s cells over the distinct `%s` and `%s` (equal width — not to numeric scale)",
			humanize.Comma(int64(g.nCols)), humanize.Comma(int64(g.nRows)), inst.xName, inst.yName)
		fmt.Fprintf(&b, " · z %s – %s", chartNum(g.vmin), chartNum(g.vmax))
		if g.holes > 0 {
			fmt.Fprintf(&b, " · %s cells empty", humanize.Comma(int64(g.holes)))
		}
		if len(g.xLabels) > chartMaxTickLabels || len(g.yLabels) > chartMaxTickLabels {
			fmt.Fprintf(&b, " · tick labels dropped past %d keys", chartMaxTickLabels)
		}
	} else {
		var nulls int
		for i := range inst.lanes {
			nulls += inst.lanes[i].nulls
		}
		fmt.Fprintf(&b, "%d series · %s points · x: ", len(inst.lanes),
			humanize.Comma(int64(inst.points)))
		switch inst.xAxis {
		case chartAxisRow:
			// Said out loud, because the abscissa is the panel's and not the
			// query's: nothing in the result measured it. The per-series form
			// says the extra part — that row 3 of one group is drawn beside row
			// 3 of another because they are both third, not because anything
			// says they belong together.
			if inst.xPerSeries {
				b.WriteString("the row number inside each `series` (implicit — the result has no `x`)")
			} else {
				b.WriteString("the row number, in result order (implicit — the result has no `x`)")
			}
		case chartAxisCategorical:
			fmt.Fprintf(&b, "`%s` (%s categories, in row order)", inst.xName,
				humanize.Comma(int64(len(inst.catLabels))))
			if len(inst.catLabels) > chartMaxTickLabels {
				fmt.Fprintf(&b, " · tick labels dropped past %d", chartMaxTickLabels)
			}
		case chartAxisTemporal:
			fmt.Fprintf(&b, "`%s` (time, UTC)", inst.xName)
		default:
			fmt.Fprintf(&b, "`%s`", inst.xName)
		}
		if nulls > 0 {
			fmt.Fprintf(&b, " · %s nulls (the line breaks; nothing is interpolated)", humanize.Comma(int64(nulls)))
		}
		if inst.droppedSeries > 0 {
			fmt.Fprintf(&b, " · %d more series not shown (cap %d — GROUP BY coarser)",
				inst.droppedSeries, chartMaxSeries)
		}
	}
	if inst.truncated > 0 {
		fmt.Fprintf(&b, " · %s more rows not read (cap %s)",
			humanize.Comma(inst.truncated), humanize.Comma(chartMaxRows))
	}
	return b.String()
}

// chartNum formats a magnitude for the status line at a readable precision.
func chartNum(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return humanize.Comma(int64(v))
	}
	return strconv.FormatFloat(v, 'g', 4, 64)
}

// renderChartTab is the dock-tab entry (spec.Render): frame plumbing around the
// PanelI dispatch, mirroring the Distribution tab's.
func (inst *PlayApp) renderChartTab(rec arrow.RecordBatch, schema *arrow.Schema, loading bool, err error, executed time.Time) {
	if loading && rec == nil {
		inst.renderResultsLoading()
		return
	}
	if err != nil && rec == nil {
		inst.renderResultsFailed()
		return
	}
	if rec == nil {
		for rt := range c.RichTextLabel("Run a query with at least one numeric column — plus an `x` to draw " +
			"it against, or `x`, `y` and `z` for a heatmap (ADR-0172) — to see a chart.") {
			rt.Small().Weak()
		}
		return
	}
	inst.chartDriver.noteExecuted(executed)
	inputs := map[ChannelID]channelInput{
		chMain: {node: inst.resolvedTabNode("chart"), rec: rec, schema: schema, sig: inst.frameSig},
	}
	reject := dispatchPanel(chartPanel{driver: inst.chartDriver}, inputs, inst.sigEmit)
	if reject != "" {
		for rt := range c.RichTextLabel(reject) {
			rt.Small().Weak()
		}
	}
}
