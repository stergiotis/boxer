// Package trendsmooth is the shared modified-sinc smoothing overlay for the
// monitoring apps' trend plots — the app-consumer surface of
// [github.com/stergiotis/boxer/public/analytics/timeseries/mssmooth]
// (ADR-0152). It was written inside imzrt and lifted here when imztop
// adopted the same feature, following the SlidingWindow precedent
// (ADR-0061 SD13): the second consumer is the lift signal.
//
// The overlay carries three commitments a monitoring app should not have to
// re-derive:
//
//   - The raw series stays visible. Smoothing renders the raw values as a
//     faint underlay (same hue at reduced alpha) beneath the smoothed curve
//     — a monitor must not hide spikes. Both carry the same label, so
//     implot's same-label contract merges them into one legend entry with
//     one visibility toggle.
//   - The filter degree is fixed at 4. The paper behind ADR-0152 finds
//     nothing beyond it worth having for this use, and a monitor has no
//     business exposing a filter-design knob; only the half-width (the
//     smoothing strength) is adjustable.
//   - Errors fall back to raw. A NaN from a metric gap makes the smoother
//     decline, and the series renders exactly as if smoothing were off,
//     rather than disappearing.
//
// What to wire is the consuming app's decision, and the exclusions are the
// substance of it: series whose sharp shape is structure (a GC sawtooth) and
// spike trains of discrete events (forced GCs) must stay raw — a low-pass
// would misreport them. The dated updates in ADR-0061 and ADR-0020 carry the
// worked examples.
//
// One [State] per window, [State.BeginFrame] once per frame entry point:
// smoothed series live in a per-frame arena because implot does not copy,
// and [State.RenderControls] derives fixed widget ids, so a second State in
// the same id scope would collide.
package trendsmooth

import (
	"fmt"

	"github.com/stergiotis/boxer/public/analytics/timeseries/mssmooth"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// degree is the fixed filter degree (see the package comment).
const degree = 4

// MaxHalfWidth and DefaultHalfWidth bound and seed the smoothing strength;
// the lower bound is the kernel family's own minimum for the degree.
const MaxHalfWidth int32 = 60
const DefaultHalfWidth int32 = 12

// stepHalfWidth is the ± stepper increment.
const stepHalfWidth int32 = 4

// faintAlpha replaces the raw underlay's alpha channel: ~1/3, so raw and
// smoothed read as one series at two confidence levels, not two series.
const faintAlpha uint32 = 0x55

// edgeAlpha is the extrapolation-backed tail's alpha (see
// [State.LineWithEdge]) — between the raw underlay and the settled curve, so
// the tail reads as the same curve held less firmly rather than as a third
// series.
const edgeAlpha uint32 = 0xaa

// State is one window's smoothing selection plus the caches derived from it.
// Render-thread-only, like the per-window UI state it sits beside.
type State struct {
	// On toggles the overlay. Exported so tour Inits can pin it — captures
	// should showcase the feature while the interactive default stays off
	// (a monitor opens honest).
	On bool

	halfWidth int32
	kernel    *mssmooth.Kernel
	arena     [][]float64
	arenaIdx  int
}

func New() (inst *State) {
	inst = &State{halfWidth: DefaultHalfWidth}
	return
}

// BeginFrame resets the per-frame buffer arena. Call once at the top of each
// frame entry point (the app's renderApp and any tour scene).
func (inst *State) BeginFrame() {
	inst.arenaIdx = 0
}

// HalfWidth returns the current kernel half-width.
func (inst *State) HalfWidth() (halfWidth int32) {
	halfWidth = inst.halfWidth
	return
}

// SetHalfWidth clamps and applies a new half-width; the kernel rebuilds
// lazily on the next smoothed line.
func (inst *State) SetHalfWidth(halfWidth int32) {
	inst.halfWidth = min(max(halfWidth, mssmooth.MinHalfWidth(degree)), MaxHalfWidth)
}

// RenderControls renders the top-bar segment: a "smooth" checkbox bound to
// [State.On] plus the ±half-width stepper. The stepper stays visible while
// smoothing is off so the row does not reflow on toggle. The checkbox binds
// &inst.On directly — a State lives on its window's heap-held app, which
// satisfies the stable-pointer rule.
func (inst *State) RenderControls(ids *c.WidgetIdStack) {
	c.Checkbox(ids.PrepareStr("trendsmooth-on"), inst.On, "smooth").
		SendRespVal(&inst.On)
	c.Label(fmt.Sprintf("±%d", inst.halfWidth)).Send()
	if c.Button(ids.PrepareStr("trendsmooth-down"), c.Atoms().Text("−").Keep()).
		SendResp().HasPrimaryClicked() {
		inst.SetHalfWidth(inst.halfWidth - stepHalfWidth)
	}
	if c.Button(ids.PrepareStr("trendsmooth-up"), c.Atoms().Text("+").Keep()).
		SendResp().HasPrimaryClicked() {
		inst.SetHalfWidth(inst.halfWidth + stepHalfWidth)
	}
}

// Line declares one trend series on p. With smoothing off — or on any
// smoothing error — it renders exactly the raw line in cl at weight. With
// smoothing on it renders the raw values as a faint thin underlay and the
// MS-smoothed curve on top at weight+0.5, both under the same label.
//
// cl must be a literal color: the faint underlay is derived from its
// [color.Color.Literal] value.
func (inst *State) Line(p *implot.Plot, label string, t []float64, vals []float64, cl color.Color, weight float32) {
	if inst.On {
		if k := inst.ensureKernel(); k != nil {
			smoothed, err := k.SmoothE(vals, inst.buf(len(vals)))
			if err == nil {
				p.SetNextColor(cl.Literal()&^uint32(0xff) | faintAlpha).SetNextWeight(1.0)
				p.Line(label, t, vals)
				p.SetNextColor(cl.Literal()).SetNextWeight(weight + 0.5)
				p.Line(label, t, smoothed)
				return
			}
		}
	}
	p.SetNextColor(cl.Literal()).SetNextWeight(weight)
	p.Line(label, t, vals)
}

// LineWithEdge is [State.Line] with the extrapolation-backed tail drawn
// distinctly. Convolution is undefined within a half-width of the data ends,
// so [mssmooth.Kernel.SmoothE] extends the series by a weighted linear fit to
// define them (ADR-0152, the paper's eq 17–18). On a series whose right edge
// is the present, those trailing halfWidth values are therefore partly a
// projection of the trend rather than a reading of it — and they are exactly
// the values an eye goes to. They render at reduced alpha so the settled
// curve and the projected tail are not read as one claim.
//
// The tail keeps the series label, like the raw underlay: one legend entry,
// one visibility toggle (the package's existing commitment). What the fade
// MEANS is the caller's to caption — a plot drawing this owes its reader that
// sentence somewhere.
//
// With smoothing off, or on any smoothing error, this is [State.Line]: there
// is no extrapolation to mark.
func (inst *State) LineWithEdge(p *implot.Plot, label string, t []float64, vals []float64, cl color.Color, weight float32) {
	m := int(inst.halfWidth)
	if !inst.On || len(vals) <= 2*m+1 {
		inst.Line(p, label, t, vals, cl, weight)
		return
	}
	k := inst.ensureKernel()
	if k == nil {
		inst.Line(p, label, t, vals, cl, weight)
		return
	}
	smoothed, err := k.SmoothE(vals, inst.buf(len(vals)))
	if err != nil {
		inst.Line(p, label, t, vals, cl, weight)
		return
	}
	p.SetNextColor(cl.Literal()&^uint32(0xff) | faintAlpha).SetNextWeight(1.0)
	p.Line(label, t, vals)
	// The two smoothed segments SHARE the sample at the split, so the join is
	// continuous rather than a one-pixel gap that reads as missing data.
	cut := len(smoothed) - m
	p.SetNextColor(cl.Literal()).SetNextWeight(weight + 0.5)
	p.Line(label, t[:cut], smoothed[:cut])
	p.SetNextColor(cl.Literal()&^uint32(0xff) | edgeAlpha).SetNextWeight(weight + 0.5)
	p.Line(label, t[cut-1:], smoothed[cut-1:])
}

// ensureKernel returns the cached kernel, rebuilding when the half-width
// changed. Parameters are clamped, so construction cannot fail.
func (inst *State) ensureKernel() (k *mssmooth.Kernel) {
	if inst.kernel == nil || inst.kernel.HalfWidth() != inst.halfWidth {
		if built, err := mssmooth.NewKernelE(degree, inst.halfWidth); err == nil {
			inst.kernel = built
		}
	}
	k = inst.kernel
	return
}

// buf hands out the next arena slot, grown in place so the smoother's
// capacity check always reuses it rather than allocating a stray slice the
// arena would never see again.
func (inst *State) buf(n int) (buf []float64) {
	if inst.arenaIdx == len(inst.arena) {
		inst.arena = append(inst.arena, make([]float64, n))
	} else if cap(inst.arena[inst.arenaIdx]) < n {
		inst.arena[inst.arenaIdx] = make([]float64, n)
	}
	buf = inst.arena[inst.arenaIdx]
	inst.arenaIdx++
	return
}
