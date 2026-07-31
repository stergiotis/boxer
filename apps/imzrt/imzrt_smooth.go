package imzrt

import (
	"fmt"

	"github.com/stergiotis/boxer/public/analytics/timeseries/mssmooth"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// Modified-sinc smoothing for the noisy trend plots (ADR-0152, first app
// consumer). The wiring is deliberately partial: the GC-pause percentiles,
// GC cycle rate, allocation rate, goroutine count and scheduling-latency p99
// are rate/latency series whose sample-to-sample jitter hides the trend. The
// heap sawtooth and the stacked memory classes stay raw — their sharp GC
// drops are the signal, and smoothing peak-shaped structure away would
// misreport the runtime. The forced-GC series stays raw too: a spike train
// of discrete events smears into a misleading blur under any low-pass.
//
// When smoothing is on, the raw series remains visible as a faint underlay
// beneath the smoothed curve: a monitor must not hide spikes. Both carry the
// same label, so they share one legend entry and one visibility toggle
// (the implot same-label contract).
const (
	// smoothDegree is fixed at 4: the paper finds no benefit beyond it for
	// peak-shaped data at monitoring fidelity, and a monitor has no business
	// exposing a filter-design knob (ADR-0152).
	smoothDegree   = 4
	smoothMaxM     = 60
	smoothDefaultM = 12
	smoothStepM    = 4
)

// beginSmoothFrame resets the per-frame buffer arena. Called once at the top
// of each frame entry point (renderApp, renderTourScene): implot does not
// copy series, so every smoothed series needs its own backing slice that
// stays valid until the plot renders — the arena reuses them across frames.
func (inst *App) beginSmoothFrame() {
	inst.smoothArenaIdx = 0
}

// smoothBuf hands out the next arena slot, grown in place so the smoother's
// capacity check always reuses it rather than allocating a stray slice the
// arena would never see again.
func (inst *App) smoothBuf(n int) (buf []float64) {
	if inst.smoothArenaIdx == len(inst.smoothArena) {
		inst.smoothArena = append(inst.smoothArena, make([]float64, n))
	} else if cap(inst.smoothArena[inst.smoothArenaIdx]) < n {
		inst.smoothArena[inst.smoothArenaIdx] = make([]float64, n)
	}
	buf = inst.smoothArena[inst.smoothArenaIdx]
	inst.smoothArenaIdx++
	return
}

// setSmoothM clamps and applies a new half-width; the kernel is rebuilt
// lazily on the next smoothed line.
func (inst *App) setSmoothM(m int32) {
	inst.smoothM = min(max(m, mssmooth.MinHalfWidth(smoothDegree)), smoothMaxM)
}

// ensureSmoothKernel returns the cached kernel, rebuilding it when the
// half-width changed. The parameters are clamped, so construction cannot
// fail; a nil return only signals a programming error upstream.
func (inst *App) ensureSmoothKernel() (k *mssmooth.Kernel) {
	if inst.smoothKernel == nil || inst.smoothKernel.HalfWidth() != inst.smoothM {
		if built, err := mssmooth.NewKernelE(smoothDegree, inst.smoothM); err == nil {
			inst.smoothKernel = built
		}
	}
	k = inst.smoothKernel
	return
}

// renderSmoothControls is the top-bar segment: a checkbox plus the ±m
// stepper, the stepper mirroring the sampler's interval idiom two segments
// to its left. The stepper stays visible while smoothing is off so the row
// does not reflow on toggle. The checkbox binds &inst.smoothOn directly —
// the App is heap-held per window, satisfying the stable-pointer rule.
func (inst *App) renderSmoothControls() {
	c.Checkbox(inst.ids.PrepareStr("topbar-smooth"), inst.smoothOn, "smooth").
		SendRespVal(&inst.smoothOn)
	c.Label(fmt.Sprintf("±%d", inst.smoothM)).Send()
	if c.Button(inst.ids.PrepareStr("topbar-smooth-down"), c.Atoms().Text("−").Keep()).
		SendResp().HasPrimaryClicked() {
		inst.setSmoothM(inst.smoothM - smoothStepM)
	}
	if c.Button(inst.ids.PrepareStr("topbar-smooth-up"), c.Atoms().Text("+").Keep()).
		SendResp().HasPrimaryClicked() {
		inst.setSmoothM(inst.smoothM + smoothStepM)
	}
}

// lineSmoothed declares one trend series on p. With smoothing off (or on any
// smoothing error — a NaN from a metric gap falls back rather than dropping
// the series) it renders exactly the raw line. With smoothing on it renders
// the raw values as a faint thin underlay and the MS-smoothed curve on top,
// both under the same label.
func (inst *App) lineSmoothed(p *implot.Plot, label string, t []float64, vals []float64, cl color.Color, clFaint color.Color, weight float32) {
	if inst.smoothOn {
		if k := inst.ensureSmoothKernel(); k != nil {
			smoothed, err := k.SmoothE(vals, inst.smoothBuf(len(vals)))
			if err == nil {
				p.SetNextColor(clFaint.Literal()).SetNextWeight(1.0)
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
