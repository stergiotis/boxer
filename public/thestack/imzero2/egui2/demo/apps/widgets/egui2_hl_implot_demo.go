package widgets

import (
	"math"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// implotDemoState holds the demo's series so the slices stay valid across
// the frame (implot.Line does not copy) and stable across frames.
type implotDemoState struct {
	xs, sin, cos, damped []float64
}

// implotM3State carries the M3 scales demo's series.
type implotM3State struct {
	tXs, tYs         []float64
	eXs              []float64
	e1Ys, e2Ys, e3Ys []float64
}

// implotM4State carries the M4 heatmap/histogram demo's data.
type implotM4State struct {
	smallVals, bigVals []float64
	cmSmall, cmBig     *colormap.Config
	samples            []float64
}

// implotM5State carries the M5 tools demo: the series plus the caller-held
// tool values (stable across frames, per the heap-pointer rule).
type implotM5State struct {
	xs, ys         []float64
	threshold      float64
	probeX, probeY float64
	limitY         float64
}

// implotM6State carries the M6 linked-subplots demo's series.
type implotM6State struct {
	xs, a, b, c, d []float64
}

// implotTicksState carries the tick-label demo's three category axes: the
// names, and the bar heights and positions that go under them.
type implotTicksState struct {
	few, some, many    []string
	fewV, someV, manyV []float64
	fewX, someX, manyX []float64
}

// implotM2State carries the M2 item-breadth demo's series.
type implotM2State struct {
	barXs, barYs     []float64
	scXs, scYs       []float64
	stairXs, stairYs []float64
	stemXs, stemYs   []float64
	shXs, shYs       []float64
}

// implotM7State carries the M7 remainder demo's data: error-barred series,
// pie shares, digital channels with their analog overlay, and the
// caller-owned RGBA image.
type implotM7State struct {
	ebXs, ebBars, ebErr          []float64
	ebLXs, ebLYs, ebLNeg, ebLPos []float64
	pieLabels                    []string
	pieVals                      []float64
	digXs, digA, digB, digSin    []float64
	imgPix                       []uint32
	imgMarkX, imgMarkY           []float64
}

func init() {
	registry.Register(registry.Demo{
		Name:        "implot_m1",
		Category:    "Graphics & canvas",
		Title:       icons.IconPaintBucket + " implot M1 (axes / ticks / lines / pan-zoom)",
		Stage:       [2]float32{660, 580},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "ADR-0149 M1 plot frame core: linear axes with located ticks, grid, clipped line series (NaN-split), drag pan, anchored wheel zoom, Shift+drag box-zoom, double-click fit.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			st := &implotDemoState{}
			const n = 400
			st.xs = make([]float64, n)
			st.sin = make([]float64, n)
			st.cos = make([]float64, n)
			st.damped = make([]float64, n)
			for i := range n {
				x := float64(i) / float64(n-1) * 4 * math.Pi
				st.xs[i] = x
				st.sin[i] = math.Sin(x)
				st.cos[i] = 0.75 * math.Cos(x)
				st.damped[i] = math.Exp(-x/6) * math.Sin(3*x)
				// A NaN gap proves the split-at-NaN contract visibly.
				if i > 180 && i < 195 {
					st.damped[i] = math.NaN()
				}
			}
			return st
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*implotDemoState)
			for p := range implot.Scoped(ids, "waves", 620, 380) {
				p.SetupAxes("x [rad]", "amplitude", implot.AxisFlagsNone, implot.AxisFlagsNone)
				p.SetupAxisLimits(implot.AxisX1, 0, 4*math.Pi, implot.CondOnce)
				p.SetupAxisLimits(implot.AxisY1, -1.25, 1.25, implot.CondOnce)
				p.Line("sin(x)", st.xs, st.sin)
				p.Line("0.75·cos(x)", st.xs, st.cos)
				p.Line("damped (NaN gap)", st.xs, st.damped)
			}
		},
	})
	registry.Register(registry.Demo{
		Name:        "implot_tick_labels",
		Category:    "Graphics & canvas",
		Title:       icons.IconPaintBucket + " implot tick labels (callouts / stacking / thinning)",
		Stage:       [2]float32{660, 690},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "Category axes whose names do not fit, one rung of the label ladder each: eight sit centred on their ticks, fifteen slide and stack with a leader line back to the tick each names, forty-four thin to every k-th.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			// Module short names, the shape the data-catalog books put on a
			// category axis (apps/sqlapplet/bookcodevol/vol-top.md).
			names := []string{
				"clickhouse-go", "arrow-go", "zerolog", "protobuf", "grpc",
				"sqlite", "prometheus", "opentelemetry", "yaml.v3", "testify",
				"crypto", "net-http2", "goldmark", "uuid", "compress",
				"errors", "sync", "atomic", "unicode", "encoding",
				"reflect", "runtime", "strconv", "bufio", "regexp",
			}
			st := &implotTicksState{}
			fill := func(n int, wrap bool) ([]string, []float64, []float64) {
				lbl := make([]string, n)
				val := make([]float64, n)
				pos := make([]float64, n)
				for i := range n {
					lbl[i] = names[i%len(names)]
					if wrap && i >= len(names) {
						lbl[i] += "-v2"
					}
					// A plausible ranking: geometric decay off the leader.
					val[i] = 900 * math.Exp(-float64(i)/9)
					pos[i] = float64(i)
				}
				return lbl, val, pos
			}
			st.few, st.fewV, st.fewX = fill(8, false)
			st.some, st.someV, st.someX = fill(15, false)
			st.many, st.manyV, st.manyX = fill(44, true)
			return st
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*implotTicksState)
			bars := func(title string, labels []string, xs, ys []float64, h float32) {
				for p := range implot.Scoped(ids, title, 620, h) {
					p.SetupAxisTicks(implot.AxisX1, xs, labels)
					p.SetupAxisLimits(implot.AxisX1, -0.7, float64(len(xs))-0.3, implot.CondOnce)
					p.SetupAxes("", "bytes", implot.AxisFlagsNone, implot.AxisFlagsAutoFit)
					p.Bars("text_bytes", xs, ys, 0.7)
				}
			}
			bars("8 names — they fit", st.few, st.fewX, st.fewV, 165)
			bars("15 names — slid and stacked, with leader lines", st.some, st.someX, st.someV, 185)
			bars("44 names — thinned to every k-th", st.many, st.manyX, st.manyV, 165)
		},
	})
	registry.Register(registry.Demo{
		Name:        "implot_m2",
		Category:    "Graphics & canvas",
		Title:       icons.IconPaintBucket + " implot M2 (items / legend)",
		Stage:       [2]float32{660, 580},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "ADR-0149 M2 item breadth on one plot: bars, scatter, stairs, stems, shaded, infinite reference lines — with a clickable legend (toggle visibility, hover to highlight).",
		Init: func(_ *c.WidgetIdStack) (state any) {
			st := &implotM2State{}
			const n = 24
			for i := range n {
				x := float64(i)
				st.barXs = append(st.barXs, x)
				st.barYs = append(st.barYs, 2.2+1.6*math.Sin(x/3.2)+0.6*math.Cos(x/1.1))
				st.stairXs = append(st.stairXs, x)
				st.stairYs = append(st.stairYs, 5.4+1.1*math.Sin(x/4.5))
				st.stemXs = append(st.stemXs, x)
				st.stemYs = append(st.stemYs, -1.2+0.8*math.Sin(x/2.1))
			}
			const m = 60
			for i := range m {
				x := float64(i) / float64(m-1) * 23.0
				st.scXs = append(st.scXs, x)
				st.scYs = append(st.scYs, 7.4+0.7*math.Sin(x/1.7)+0.25*math.Cos(x*1.3))
				st.shXs = append(st.shXs, x)
				st.shYs = append(st.shYs, 9.4+0.6*math.Sin(x/2.6))
			}
			return st
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*implotM2State)
			for p := range implot.Scoped(ids, "item breadth", 620, 400) {
				p.SetupAxes("x", "value", implot.AxisFlagsNone, implot.AxisFlagsNone)
				p.SetupAxisLimits(implot.AxisX1, -0.8, 23.8, implot.CondOnce)
				p.SetupAxisLimits(implot.AxisY1, -2.6, 10.6, implot.CondOnce)
				p.Bars("bars", st.barXs, st.barYs, 0.66)
				p.Stairs("stairs", st.stairXs, st.stairYs)
				p.Stems("stems", st.stemXs, st.stemYs, -2.2)
				p.Scatter("scatter", st.scXs, st.scYs, implot.MarkerDiamond, 3.5)
				p.Shaded("shaded", st.shXs, st.shYs, 8.8)
				p.InfLinesH("mean ref", []float64{4.6})
			}
		},
	})
	registry.Register(registry.Demo{
		Name:        "implot_m3",
		Category:    "Graphics & canvas",
		Title:       icons.IconPaintBucket + " implot M3 (time / log scales)",
		Stage:       [2]float32{660, 580},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "ADR-0149 M3 axis scales, two plots in one frame (the per-id R24 register at work): a Unix-seconds time axis with boundary-snapped ticks, and a log10 y axis with decade ticks turning exponentials into straight lines.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			st := &implotM3State{}
			// Fixed epoch keeps the tour capture deterministic.
			base := float64(1_780_000_000)
			const n = 200
			for i := range n {
				tt := base + float64(i)/float64(n-1)*48*3600
				st.tXs = append(st.tXs, tt)
				st.tYs = append(st.tYs, 42+9*math.Sin(float64(i)/11)+3*math.Cos(float64(i)/3.1))
				x := float64(i) / float64(n-1) * 10
				st.eXs = append(st.eXs, x)
				st.e1Ys = append(st.e1Ys, math.Exp(x*0.9))
				st.e2Ys = append(st.e2Ys, 40*math.Exp(x*0.35))
				st.e3Ys = append(st.e3Ys, 3000*math.Exp(-x*0.4))
			}
			return st
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*implotM3State)
			for p := range implot.Scoped(ids, "requests over 48 h", 620, 240) {
				p.SetupAxes("", "rate", implot.AxisFlagsNone, implot.AxisFlagsNone)
				p.SetupAxisScale(implot.AxisX1, implot.ScaleTime)
				p.Line("rate", st.tXs, st.tYs)
			}
			for p2 := range implot.Scoped(ids, "log-scale growth", 620, 240) {
				p2.SetupAxes("x", "value (log10)", implot.AxisFlagsNone, implot.AxisFlagsNone)
				p2.SetupAxisScale(implot.AxisY1, implot.ScaleLog10)
				p2.SetupAxisLimits(implot.AxisY1, 0.5, 20000, implot.CondOnce)
				p2.Line("e^0.9x", st.eXs, st.e1Ys)
				p2.Line("40·e^0.35x", st.eXs, st.e2Ys)
				p2.Line("3000·e^-0.4x", st.eXs, st.e3Ys)
			}
		},
	})
	registry.Register(registry.Demo{
		Name:        "implot_m4",
		Category:    "Graphics & canvas",
		Title:       icons.IconPaintBucket + " implot M4 (heatmaps / histograms)",
		Stage:       [2]float32{660, 580},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "ADR-0149 M4: a small heatmap on the rect-batch route and a 256×160 field on the paintImage texture route (Viridis and Inferno via the colormap widget), plus a Sturges-binned histogram.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			st := &implotM4State{}
			st.smallVals = make([]float64, 24*14)
			for r := range 14 {
				for cix := range 24 {
					st.smallVals[r*24+cix] = math.Sin(float64(cix)/3.5) * math.Cos(float64(r)/2.2)
				}
			}
			st.cmSmall = colormap.NewConfig(colormap.Viridis8, -1, 1)
			st.bigVals = make([]float64, 256*160)
			for r := range 160 {
				for cix := range 256 {
					x := float64(cix) / 32.0
					y := float64(r) / 24.0
					st.bigVals[r*256+cix] = math.Sin(x)*math.Cos(y) + 0.4*math.Sin(2.4*x+1.7*y)
				}
			}
			st.cmBig = colormap.NewConfig(colormap.Inferno8, -1.4, 1.4)
			// Deterministic LCG samples: a Gaussian-ish sum of uniforms.
			lcg := uint64(88172645463325252)
			next := func() float64 {
				lcg = lcg*6364136223846793005 + 1442695040888963407
				return float64(lcg>>11) / float64(1<<53)
			}
			for range 4000 {
				s := 0.0
				for range 6 {
					s += next()
				}
				st.samples = append(st.samples, s)
			}
			return st
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*implotM4State)
			for p := range implot.Scoped(ids, "heatmaps: rect route / texture route", 620, 250) {
				p.SetupAxes("", "", implot.AxisFlagsNone, implot.AxisFlagsNone)
				p.SetupAxisLimits(implot.AxisX1, 0, 21, implot.CondOnce)
				p.SetupAxisLimits(implot.AxisY1, 0, 10, implot.CondOnce)
				p.Heatmap("small (336 rects)", st.smallVals, 14, 24, st.cmSmall, 0, 0, 10, 10)
				p.Heatmap("big (256x160 texture)", st.bigVals, 160, 256, st.cmBig, 11, 0, 21, 10)
			}
			for p2 := range implot.Scoped(ids, "histograms", 620, 250) {
				p2.SetupAxes("value", "count", implot.AxisFlagsAutoFit, implot.AxisFlagsAutoFit)
				p2.Histogram("histogram", st.samples, 0, false)
			}
		},
	})
	registry.Register(registry.Demo{
		Name:        "implot_m5",
		Category:    "Graphics & canvas",
		Title:       icons.IconPaintBucket + " implot M5 (drag tools / annotations / tags)",
		Stage:       [2]float32{660, 580},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "ADR-0149 M5 tools: a draggable x threshold line and horizontal limit line with axis tags, a draggable probe point with a clamped annotation callout — drag tools win the hit-test over plot pan — and a right-click context menu with the fit actions.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			st := &implotM5State{threshold: 7.5, probeX: 3.2, probeY: 0.6, limitY: -0.8}
			const n = 300
			for i := range n {
				x := float64(i) / float64(n-1) * 4 * math.Pi
				st.xs = append(st.xs, x)
				st.ys = append(st.ys, math.Sin(x)*math.Exp(-x/12))
			}
			return st
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*implotM5State)
			for p := range implot.Scoped(ids, "tools", 620, 420) {
				p.SetupAxes("x", "y", implot.AxisFlagsNone, implot.AxisFlagsNone)
				p.SetupAxisLimits(implot.AxisX1, 0, 4*math.Pi, implot.CondOnce)
				p.SetupAxisLimits(implot.AxisY1, -1.2, 1.2, implot.CondOnce)
				p.Line("signal", st.xs, st.ys)
				p.DragLineX("threshold", &st.threshold, 0xdd8452ff)
				p.DragLineY("limit", &st.limitY, 0x55a868ff)
				p.DragPoint("probe", &st.probeX, &st.probeY, 0xc44e52ff)
				p.TagX(st.threshold, 0xdd8452ff)
				p.TagY(st.limitY, 0x55a868ff)
				p.Annotation(st.probeX, st.probeY, 18, -24, 0xc44e52ff, true,
					"probe")
			}
		},
	})
	registry.Register(registry.Demo{
		Name:        "implot_m7",
		Category:    "Graphics & canvas",
		Title:       icons.IconPaintBucket + " implot M7 (error bars / pie / digital / image)",
		Stage:       [2]float32{660, 690},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "ADR-0149 M7 remainder: (a)symmetric error whiskers merged into their series' legend entry, a pie with per-slice legend toggles, bottom-pinned digital channels (immune to y pan/zoom), and an RGBA image in plot space.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			st := &implotM7State{}
			// Error bars: five bars with symmetric errors, a trend line
			// with asymmetric errors.
			st.ebXs = []float64{1, 2, 3, 4, 5}
			st.ebBars = []float64{2.3, 3.8, 3.1, 4.4, 2.9}
			st.ebErr = []float64{0.35, 0.5, 0.28, 0.6, 0.4}
			for i := range 21 {
				x := 0.6 + float64(i)/20*4.4
				st.ebLXs = append(st.ebLXs, x)
				st.ebLYs = append(st.ebLYs, 5.4+0.5*math.Sin(x*1.4))
				st.ebLNeg = append(st.ebLNeg, 0.12)
				st.ebLPos = append(st.ebLPos, 0.3)
			}
			// Pie: fibonacci shares, auto-normalized (sum > 1).
			st.pieLabels = []string{"A", "B", "C", "D", "E"}
			st.pieVals = []float64{1, 1, 2, 3, 5}
			// Digital: two logic channels (one with a NaN gap) + analog sin.
			const n = 300
			for i := range n {
				x := float64(i) / float64(n-1) * 10
				st.digXs = append(st.digXs, x)
				hi := 0.0
				if math.Sin(x) > 0 {
					hi = 1
				}
				st.digA = append(st.digA, hi)
				sq := 0.0
				if math.Mod(x, 2) < 1 {
					sq = 1
				}
				if x > 6.2 && x < 6.8 {
					sq = math.NaN() // proves the run-split contract
				}
				st.digB = append(st.digB, sq)
				st.digSin = append(st.digSin, math.Sin(x))
			}
			// Image: a 48×48 plasma field, RGBA 0xRRGGBBAA, row 0 = top.
			const side = 48
			st.imgPix = make([]uint32, side*side)
			for r := range side {
				for cix := range side {
					fx := float64(cix) / side * 6
					fy := float64(r) / side * 6
					v := math.Sin(fx) + math.Cos(fy) + math.Sin((fx+fy)/2)
					rr := uint32(128 + 90*math.Sin(v*math.Pi/3))
					gg := uint32(128 + 90*math.Sin(v*math.Pi/3+2.1))
					bb := uint32(128 + 90*math.Sin(v*math.Pi/3+4.2))
					st.imgPix[r*side+cix] = rr<<24 | gg<<16 | bb<<8 | 0xff
				}
			}
			st.imgMarkX = []float64{0.7, 1.6, 2.5, 3.4}
			st.imgMarkY = []float64{0.8, 2.9, 1.5, 3.3}
			return st
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*implotM7State)
			for range c.Horizontal().KeepIter() {
				for p := range implot.Scoped(ids, "error bars", 306, 236) {
					p.SetupAxes("", "", implot.AxisFlagsNone, implot.AxisFlagsNone)
					p.SetupAxisLimits(implot.AxisX1, 0.2, 5.8, implot.CondOnce)
					p.SetupAxisLimits(implot.AxisY1, 0, 6.4, implot.CondOnce)
					p.Bars("bars", st.ebXs, st.ebBars, 0.55)
					p.ErrorBars("bars", st.ebXs, st.ebBars, st.ebErr, st.ebErr)
					p.Line("trend", st.ebLXs, st.ebLYs)
					p.ErrorBars("trend", st.ebLXs, st.ebLYs, st.ebLNeg, st.ebLPos)
				}
				for p2 := range implot.Scoped(ids, "pie (fib shares)", 306, 236) {
					p2.SetupAxes("", "",
						implot.AxisFlagsNoGrid|implot.AxisFlagsNoTickLabels,
						implot.AxisFlagsNoGrid|implot.AxisFlagsNoTickLabels)
					p2.SetupAxisLimits(implot.AxisX1, 0, 1, implot.CondOnce)
					p2.SetupAxisLimits(implot.AxisY1, 0, 1, implot.CondOnce)
					p2.Pie(st.pieLabels, st.pieVals, 0.5, 0.5, 0.42, 90, "%.0f")
				}
			}
			for range c.Horizontal().KeepIter() {
				for p3 := range implot.Scoped(ids, "digital + analog", 306, 236) {
					p3.SetupAxes("t [s]", "", implot.AxisFlagsNone, implot.AxisFlagsNone)
					p3.SetupAxisLimits(implot.AxisX1, 0, 10, implot.CondOnce)
					p3.SetupAxisLimits(implot.AxisY1, -1.4, 1.4, implot.CondOnce)
					p3.Line("analog sin", st.digXs, st.digSin)
					p3.Digital("d0: sin>0", st.digXs, st.digA)
					p3.Digital("d1: square", st.digXs, st.digB)
				}
				for p4 := range implot.Scoped(ids, "image", 306, 236) {
					p4.SetupAxes("", "", implot.AxisFlagsNone, implot.AxisFlagsNone)
					p4.SetupAxisLimits(implot.AxisX1, -0.3, 4.3, implot.CondOnce)
					p4.SetupAxisLimits(implot.AxisY1, -0.3, 4.3, implot.CondOnce)
					p4.Image("plasma", st.imgPix, 48, 48, 0, 0, 4, 4, 1)
					p4.Scatter("probes", st.imgMarkX, st.imgMarkY, implot.MarkerCross, 4.5)
				}
			}
		},
	})
	registry.Register(registry.Demo{
		Name:        "implot_m6",
		Category:    "Graphics & canvas",
		Title:       icons.IconPaintBucket + " implot M6 (subplots / linked axes)",
		Stage:       [2]float32{660, 580},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "ADR-0149 M6: a 2×2 subplot grid with all x axes linked (SetupAxisLinks, the upstream pointer contract) — pan or zoom any cell and the other three follow.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			st := &implotM6State{}
			const n = 240
			for i := range n {
				x := float64(i) / float64(n-1) * 12
				st.xs = append(st.xs, x)
				st.a = append(st.a, math.Sin(x))
				st.b = append(st.b, math.Cos(x*1.3)*0.8)
				st.c = append(st.c, math.Sin(x*0.7)+0.3*math.Sin(x*2.9))
				st.d = append(st.d, math.Exp(-x/8)*math.Cos(2*x))
			}
			return st
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*implotM6State)
			series := [][]float64{st.a, st.b, st.c, st.d}
			names := []string{"sin x", "0.8 cos 1.3x", "mixed", "damped"}
			implot.Subplots(ids, "linked grid", 2, 2, 620, 440, implot.SubplotFlagsLinkAllX,
				func(sp *implot.SubplotCtx, row int, col int) {
					i := row*2 + col
					for p := range sp.Scoped(names[i]) {
						p.SetupAxisLimits(implot.AxisX1, 0, 12, implot.CondOnce)
						p.SetupAxisLimits(implot.AxisY1, -1.3, 1.3, implot.CondOnce)
						p.Line(names[i], st.xs, series[i])
					}
				})
		},
	})
}
