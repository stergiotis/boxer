package leewaywidgets

import (
	"math"
	"slices"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colorscale"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// ChartMarkE is how a ChartModel's values are drawn.
type ChartMarkE uint8

const (
	ChartMarkBar ChartMarkE = iota
	ChartMarkLine
	ChartMarkScatter
	ChartMarkHeatmap
)

// ChartSortE orders the categories.
type ChartSortE uint8

const (
	// ChartSortNone keeps batch order.
	ChartSortNone ChartSortE = iota
	// ChartSortAscending and ChartSortDescending order categories by the
	// first series' value, missing values last.
	ChartSortAscending
	ChartSortDescending
)

// ChartOptions are the encoding choices a ChartModel is drawn under — the
// settings the Experiments chart sink declares (ADR-0257, proposed, §SD2).
type ChartOptions struct {
	Mark ChartMarkE
	// Transpose puts series on the category axis and categories in series:
	// entities as series, memberships along x.
	Transpose bool
	Legend    bool
	Sort      ChartSortE
	// Colormap is the heatmap's palette.
	Colormap []uint32
}

// chartMaxTickLabels is where tick labels stop being readable at any
// plausible width; past it the axis carries none, as play's chart panel does.
const chartMaxTickLabels = 40

// ChartView draws ChartModels. It keeps the heatmap's colormap and colour
// scale across frames: the scale binds to the config at construction.
type ChartView struct {
	ids  *c.WidgetIdStack
	cm   *colormap.Config
	cbar *colorscale.ColorScale
	pal  []uint32
}

// NewChartView returns a view drawing on ids.
func NewChartView(ids *c.WidgetIdStack) *ChartView {
	return &ChartView{ids: ids}
}

// oriented is a model as drawn: transposed and sorted per the options.
type oriented struct {
	cats   []string
	series []string
	vals   [][]float64 // [series][cat]
}

func orient(m *ChartModel, o ChartOptions) (r oriented) {
	r = oriented{cats: m.Categories, series: m.Series, vals: m.Values}
	if o.Transpose {
		r.cats, r.series = m.Series, m.Categories
		r.vals = make([][]float64, len(m.Categories))
		for e := range m.Categories {
			r.vals[e] = make([]float64, len(m.Series))
			for s := range m.Series {
				r.vals[e][s] = m.Values[s][e]
			}
		}
	}
	if o.Sort == ChartSortNone || len(r.vals) == 0 {
		return r
	}
	order := make([]int, len(r.cats))
	for i := range order {
		order[i] = i
	}
	key := r.vals[0]
	slices.SortStableFunc(order, func(a, b int) int {
		x, y := key[a], key[b]
		switch {
		case math.IsNaN(x) && math.IsNaN(y):
			return 0
		case math.IsNaN(x):
			return 1
		case math.IsNaN(y):
			return -1
		case x == y:
			return 0
		case (x < y) == (o.Sort == ChartSortAscending):
			return -1
		}
		return 1
	})
	cats := make([]string, len(order))
	vals := make([][]float64, len(r.vals))
	for s := range r.vals {
		vals[s] = make([]float64, len(order))
	}
	for i, k := range order {
		cats[i] = r.cats[k]
		for s := range r.vals {
			vals[s][i] = r.vals[s][k]
		}
	}
	r.cats, r.vals = cats, vals
	return r
}

// Render draws the model in a w×h box.
func (inst *ChartView) Render(m *ChartModel, o ChartOptions, valueName string, w, h float32) {
	if m == nil || m.Empty() {
		c.Label("The batch has no tagged section with a numeric value to chart.").Send()
		return
	}
	r := orient(m, o)
	if o.Mark == ChartMarkHeatmap {
		inst.renderHeatmap(r, o, valueName, w, h)
		return
	}
	xs := make([]float64, len(r.cats))
	for i := range xs {
		xs[i] = float64(i)
	}
	for p := range implot.Scoped(inst.ids, "##leeway-chart", w, h) {
		if !o.Legend {
			p.NoLegend()
		}
		if len(r.cats) <= chartMaxTickLabels {
			p.SetupAxisTicks(implot.AxisX1, xs, r.cats)
		}
		p.SetupAxes("", valueName, implot.AxisFlagsNone, implot.AxisFlagsNone)
		width := 0.8 / float64(max(len(r.series), 1))
		for s, name := range r.series {
			switch o.Mark {
			case ChartMarkBar:
				// Dodge: series side by side within each category's slot.
				bx := make([]float64, len(xs))
				off := (float64(s)+0.5)*width - 0.4
				for i := range bx {
					bx[i] = xs[i] + off
				}
				p.Bars(name, bx, r.vals[s], width)
			case ChartMarkScatter:
				p.Scatter(name, xs, r.vals[s], implot.MarkerCircle, 3)
			default:
				p.Line(name, xs, r.vals[s])
			}
		}
		// Half a slot either side, so the outer categories' bars and tick
		// labels sit inside the frame rather than against it, and 5% above
		// the tallest value. Bars keep their zero baseline.
		p.IncludeX(-0.5)
		p.IncludeX(float64(len(r.cats)) - 0.5)
		if lo, hi, ok := valueRange(r.vals); ok {
			if o.Mark == ChartMarkBar {
				lo = min(lo, 0)
				p.IncludeY(0)
			}
			pad := 0.05 * max(hi-lo, math.Abs(hi), 1e-9)
			p.IncludeY(hi + pad)
			if lo < 0 {
				p.IncludeY(lo - pad)
			}
		}
	}
}

func valueRange(vals [][]float64) (lo, hi float64, ok bool) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, row := range vals {
		for _, v := range row {
			if !math.IsNaN(v) {
				lo, hi, ok = min(lo, v), max(hi, v), true
			}
		}
	}
	return lo, hi, ok
}

func (inst *ChartView) renderHeatmap(r oriented, o ChartOptions, valueName string, w, h float32) {
	rows, cols := len(r.series), len(r.cats)
	vals := make([]float64, rows*cols)
	lo, hi := math.Inf(1), math.Inf(-1)
	for s := range r.series {
		// Row 0 is drawn at the top; the first series goes there.
		for k := range r.cats {
			v := r.vals[s][k]
			vals[s*cols+k] = v
			if !math.IsNaN(v) {
				lo, hi = min(lo, v), max(hi, v)
			}
		}
	}
	if !(hi > lo) {
		lo, hi = lo-0.5, lo+0.5
		if math.IsInf(lo, 0) {
			lo, hi = 0, 1
		}
	}
	if inst.cm == nil || !slices.Equal(inst.pal, o.Colormap) {
		inst.pal = o.Colormap
		inst.cm = colormap.NewConfig(o.Colormap, lo, hi)
		inst.cbar = nil
	}
	inst.cm.DataMin, inst.cm.DataMax = lo, hi
	const barH float32 = 44
	for p := range implot.Scoped(inst.ids, "##leeway-heatmap", w, max(h-barH, 120)) {
		p.NoLegend()
		centers := func(n int) []float64 {
			out := make([]float64, n)
			for i := range out {
				out[i] = float64(i) + 0.5
			}
			return out
		}
		if cols <= chartMaxTickLabels {
			p.SetupAxisTicks(implot.AxisX1, centers(cols), r.cats)
		}
		if rows <= chartMaxTickLabels {
			// Rows run top to bottom; the axis runs bottom to top.
			labels := slices.Clone(r.series)
			slices.Reverse(labels)
			p.SetupAxisTicks(implot.AxisY1, centers(rows), labels)
		}
		p.SetupAxes("", "", implot.AxisFlagsNone, implot.AxisFlagsNone)
		p.Heatmap(valueName, vals, rows, cols, inst.cm, 0, 0, float64(cols), float64(rows))
	}
	if inst.cbar == nil {
		inst.cbar = colorscale.New(inst.ids, "leeway-chart-cbar", inst.cm, colorscale.WithSize(min(w, 640), barH))
	}
	inst.cbar.Render()
	// The colour scale draws its tick labels below the box it is given;
	// the space keeps them inside what encloses the chart.
	c.AddSpace(cbarLabelOverflow)
}

// cbarLabelOverflow is how far the colour scale's tick labels reach below
// its box, rounded up (measured off a capture: 2026-09-25).
const cbarLabelOverflow float32 = 16
