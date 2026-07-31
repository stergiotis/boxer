package widgets

import (
	"fmt"
	"math"

	"github.com/stergiotis/boxer/public/analytics/timeseries/mssmooth"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// mssmoothDemoState carries the live strip-chart's buffers and controls.
// Slices are stable across frames (implot does not copy) and reused as
// destination buffers by the smoother. Control bindings live here because
// sliders need stable heap pointers across the FFI's one-frame delay.
type mssmoothDemoState struct {
	head int // absolute index of the newest visible sample

	xs     []float64
	clean  []float64
	cleanD []float64 // analytic derivative of the clean signal
	noisy  []float64
	rawD   []float64 // centered difference of the raw noisy input
	smooth []float64 // MS-smoothed noisy signal
	deriv  []float64 // MS smooth-then-difference derivative

	kernel *mssmooth.Kernel
	kDeg   int32
	kM     int32

	degree int32
	m      float64 // slider binding; rounded and clamped before kernel build
	noise  float64 // noise σ slider binding
	paused bool
}

const (
	mssmoothDemoWindow  = 700 // visible samples
	mssmoothDemoAdvance = 2   // samples per frame while running
	mssmoothDemoFwhm    = 18.0
	mssmoothDemoPeriod  = 260 // spacing of the peak train
)

// mssmoothDemoCleanAt evaluates the clean signal and its analytic derivative
// at absolute sample t: a slow carrier plus a train of Gaussian peaks with
// alternating heights — peak-shaped content is where the flat passband
// matters, and the analytic derivative gives the truth curve for the lower
// plot.
func mssmoothDemoCleanAt(t int) (v float64, dv float64) {
	const omega = 2 * math.Pi / 240.0
	v = 0.55 * math.Sin(omega*float64(t))
	dv = 0.55 * omega * math.Cos(omega*float64(t))

	k := math.Round(float64(t) / mssmoothDemoPeriod)
	center := k * mssmoothDemoPeriod
	amp := 1.0
	if int(k)%2 != 0 {
		amp = 0.65
	}
	x := float64(t) - center
	cg := 4.0 * math.Ln2 / (mssmoothDemoFwhm * mssmoothDemoFwhm)
	g := amp * math.Exp(-cg*x*x)
	v += g
	dv += -2.0 * cg * x * g
	return
}

// mssmoothDemoNoiseAt returns frozen per-sample noise with unit σ: the value
// depends only on the absolute index, so the noise scrolls with the signal
// instead of re-rolling every frame (a smoothing demo over shimmering noise
// would demonstrate nothing).
func mssmoothDemoNoiseAt(t int) (n float64) {
	z := uint64(t)*0x9e3779b97f4a7c15 + 0xbf58476d1ce4e5b9
	var sum float64
	for range 4 {
		z ^= z >> 30
		z *= 0xbf58476d1ce4e5b9
		z ^= z >> 27
		z *= 0x94d049bb133111eb
		sum += float64(z>>11) / float64(1<<53)
	}
	// Sum of four uniforms: mean 2, σ = 1/√3 — scale to unit σ.
	n = (sum - 2.0) * math.Sqrt(3.0)
	return
}

func init() {
	registry.Register(registry.Demo{
		Name:     "mssmooth",
		Category: "Charts & plots",
		Title:    icons.IconWaveform + " MS smoothing (live signal + derivative)",
		Stage:    [2]float32{660, 690},
		Flags:    registry.DemoFlagNeedsLargeArea | registry.DemoFlagNonDeterministic,
		Kind:     registry.DemoKindMixed,
		Description: "ADR-0152 modified-sinc smoothing on a live strip chart: a scrolling noisy " +
			"peak train against its MS-smoothed curve, and below it the derivative — raw " +
			"differencing drowning in amplified noise while smooth-then-difference tracks the " +
			"truth. The right edge shows the boundary extrapolation working on the newest samples.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			st := &mssmoothDemoState{
				head:   mssmoothDemoWindow,
				degree: 4,
				m:      25,
				noise:  0.18,
			}
			st.xs = make([]float64, mssmoothDemoWindow)
			st.clean = make([]float64, mssmoothDemoWindow)
			st.cleanD = make([]float64, mssmoothDemoWindow)
			st.noisy = make([]float64, mssmoothDemoWindow)
			st.rawD = make([]float64, mssmoothDemoWindow)
			return st
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*mssmoothDemoState)
			if !st.paused {
				st.head += mssmoothDemoAdvance
			}

			// Controls, two equal-height rows → top-aligned (see the ragged
			// control row pitfall). One row would overflow the 660 px stage.
			for range c.HorizontalTop().KeepIter() {
				for i, deg := range []int32{2, 4, 6, 8, 10} {
					label := c.Atoms().Text(fmt.Sprintf("n=%d", deg)).Keep()
					var clicked bool
					if c.RadioButton(ids.PrepareSeq(uint64(0x5310+i)), label, st.degree == deg).
						SendRespVal(&clicked).HasPrimaryClicked() {
						st.degree = deg
					}
				}
				c.Checkbox(ids.PrepareStr("pause"), st.paused, "pause").SendRespVal(&st.paused)
			}
			for range c.HorizontalTop().KeepIter() {
				c.SliderF64(ids.PrepareStr("m"), st.m, 5, 80).Text("half-width m").SendRespVal(&st.m)
				c.SliderF64(ids.PrepareStr("noise"), st.noise, 0, 0.5).Text("noise σ").SendRespVal(&st.noise)
			}

			mi := max(int32(math.Round(st.m)), mssmooth.MinHalfWidth(st.degree))
			if st.kernel == nil || st.kDeg != st.degree || st.kM != mi {
				if k, err := mssmooth.NewKernelE(st.degree, mi); err == nil {
					st.kernel = k
					st.kDeg = st.degree
					st.kM = mi
				}
			}
			c.Label(fmt.Sprintf("kernel: %d points   f-3dB = %.4f fs   (SG at this cutoff: m = %d)",
				2*st.kernel.HalfWidth()+1, st.kernel.Bandwidth(), mssmoothDemoSGEquivalent(st.kernel))).Send()

			for i := range mssmoothDemoWindow {
				t := st.head - mssmoothDemoWindow + 1 + i
				v, dv := mssmoothDemoCleanAt(t)
				st.xs[i] = float64(t)
				st.clean[i] = v
				st.cleanD[i] = dv
				st.noisy[i] = v + st.noise*mssmoothDemoNoiseAt(t)
			}
			for i := range mssmoothDemoWindow {
				lo := max(i-1, 0)
				hi := min(i+1, mssmoothDemoWindow-1)
				st.rawD[i] = (st.noisy[hi] - st.noisy[lo]) / float64(hi-lo)
			}
			var err error
			st.smooth, err = st.kernel.SmoothE(st.noisy, st.smooth)
			if err == nil {
				st.deriv, err = st.kernel.DerivativeE(st.noisy, st.deriv)
			}
			if err != nil {
				c.Label("smoothing failed: " + err.Error()).Send()
				return
			}

			xLo := float64(st.head - mssmoothDemoWindow + 1)
			xHi := float64(st.head)
			for p := range implot.Scoped(ids, "signal", 620, 230) {
				p.SetupAxes("sample", "value", implot.AxisFlagsNone, implot.AxisFlagsNone)
				p.SetupAxisLimits(implot.AxisX1, xLo, xHi, implot.CondAlways)
				p.SetupAxisLimits(implot.AxisY1, -1.4, 2.0, implot.CondOnce)
				p.Line("noisy input", st.xs, st.noisy)
				p.Line("clean (truth)", st.xs, st.clean)
				p.Line("MS smoothed", st.xs, st.smooth)
			}
			for p2 := range implot.Scoped(ids, "derivative", 620, 180) {
				p2.SetupAxes("sample", "d/dt", implot.AxisFlagsNone, implot.AxisFlagsNone)
				p2.SetupAxisLimits(implot.AxisX1, xLo, xHi, implot.CondAlways)
				p2.SetupAxisLimits(implot.AxisY1, -0.30, 0.30, implot.CondOnce)
				p2.Line("raw difference", st.xs, st.rawD)
				p2.Line("truth", st.xs, st.cleanD)
				p2.Line("MS smooth→diff", st.xs, st.deriv)
			}

			if !st.paused {
				c.RequestRepaintAfter(0.05)
			}
		},
	})
}

// mssmoothDemoSGEquivalent inverts the paper's bandwidth fit to report which
// traditional Savitzky–Golay half-width the current kernel replaces — the
// "you would have typed this" number for readers arriving from SG habits.
func mssmoothDemoSGEquivalent(k *mssmooth.Kernel) (m int) {
	b := k.Bandwidth()
	n := float64(k.Degree())
	// Invert eq 14 for m+1/2: b = 1/(a·mm − c/mm) with a = 6.352/(n+1.379),
	// c = 0.513+0.316n ⇒ a·mm² − mm/b − c = 0.
	a := 6.352 / (n + 1.379)
	cc := 0.513 + 0.316*n
	mm := (1.0/b + math.Sqrt(1.0/(b*b)+4.0*a*cc)) / (2.0 * a)
	m = int(math.Round(mm - 0.5))
	return
}
