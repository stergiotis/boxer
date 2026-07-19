package widgets

import (
	"fmt"
	"math"
	"time"

	"github.com/stergiotis/boxer/public/hmi/progressbar"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// =============================================================================
// progressbar package demo
//
// Drives a simulated worker through progressbar.Estimator (Holt's Double
// Exponential Smoothing) and visualises the state with egui2's native
// ProgressBar widget + live metric labels. The point is to show the ETA
// algorithm that the CLI renderer uses, without the terminal.
//
// Rate presets exercise the trend tracker:
//   - steady:       constant rate       → B ≈ 0
//   - accelerating: rate ramps up       → B > 0, damped ETA stays ahead of raw
//   - decelerating: rate ramps down     → B < 0, damped ETA holds (monotonic clamp)
//   - spiky:        rate jitter         → damping suppresses small ETA jumps
// =============================================================================

type pbRateMode int

const (
	pbRateSteady pbRateMode = iota
	pbRateAccel
	pbRateDecel
	pbRateSpiky
)

// progressBarDemoState carries the per-window simulation state for
// the progress-bar demo: how many items have been processed, whether
// the simulation is running, the rate presets / multipliers driving
// the synthetic workload, and the Holt's-DES estimator each window
// independently warms up.
type progressBarDemoState struct {
	total      float64
	processed  float64
	running    bool
	baseRate   float64 // items/sec
	rateMult   float64
	rateMode   pbRateMode
	lastTick   time.Time
	startTime  time.Time
	estimator  *progressbar.Estimator
	showRawETA bool
}

func init() {
	registry.Register(registry.Demo{
		Name:        "progress-bar",
		Category:    "Inspectors & feedback",
		Title:       "progress bar (Holt ETA)",
		Stage:       [2]float32{1024, 700},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindUX,
		Description: "Progress bar with a Holt double-exponential smoothing ETA estimator that updates as work progresses.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			est := progressbar.NewEstimator()
			est.Start(time.Now(), 0)
			state = &progressBarDemoState{
				total:      10000,
				baseRate:   80.0,
				rateMult:   1.0,
				rateMode:   pbRateSteady,
				estimator:  est,
				showRawETA: true,
			}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoProgressBar(ids, state.(*progressBarDemoState))
		},
		SourceFunc: demoProgressBar,
	})
}

// currentRate blends the slider-controlled base rate with a mode-specific
// modulation so the estimator has something non-trivial to track.
func (st *progressBarDemoState) currentRate(now time.Time) float64 {
	base := st.baseRate * st.rateMult
	if st.startTime.IsZero() {
		return base
	}
	t := now.Sub(st.startTime).Seconds()
	switch st.rateMode {
	case pbRateAccel:
		// 0.3x → 2.0x over 20 s, clamped
		k := t / 20.0
		if k > 1 {
			k = 1
		}
		return base * (0.3 + 1.7*k)
	case pbRateDecel:
		// 2.0x → 0.3x over 20 s
		k := t / 20.0
		if k > 1 {
			k = 1
		}
		return base * (2.0 - 1.7*k)
	case pbRateSpiky:
		// sinusoidal swing 0.4x..1.6x on a ~5 s period
		return base * (1.0 + 0.6*math.Sin(t*(2*math.Pi/5.0)))
	default:
		return base
	}
}

func (st *progressBarDemoState) resetSimulation() {
	st.processed = 0
	st.running = false
	st.startTime = time.Time{}
	st.lastTick = time.Time{}
	st.estimator = progressbar.NewEstimator()
	st.estimator.Start(time.Now(), 0)
}

func demoProgressBar(ids *c.WidgetIdStack, st *progressBarDemoState) {
	now := time.Now()

	// Advance the simulated counter.
	if st.running && st.processed < st.total {
		if st.startTime.IsZero() {
			st.startTime = now
			st.lastTick = now
			st.estimator.Reset(now, 0)
		}
		dt := now.Sub(st.lastTick).Seconds()
		st.lastTick = now
		st.processed += st.currentRate(now) * dt
		if st.processed >= st.total {
			st.processed = st.total
			st.running = false
		}
		st.estimator.Update(now, int64(st.processed))
	}

	// --- Explanatory preamble.
	c.Label("Advanced ETA — Holt's Double Exponential Smoothing").Send()
	c.Label("Two smoothed state variables track the workload:").Send()
	c.Label("    S  =  α · rate + (1−α) · (S + B)       ← level (items/sec)").Send()
	c.Label("    B  =  β · (S − S_prev) + (1−β) · B     ← trend (d rate / d t)").Send()
	c.Label("Merit vs. a plain moving-average rate:").Send()
	c.Label("  • B projects forward, so ETA anticipates acceleration / deceleration").Send()
	c.Label("    (TCP slow-start, cache warm-up, I/O back-pressure …) rather than lagging.").Send()
	c.Label("  • Display damping clamps the shown ETA monotonically: decreases pass through,").Send()
	c.Label("    small increases are suppressed, large increases break through — no \"3m → 7m → 2m\" oscillation.").Send() // designlint:ignore=L1 (continuation of preceding bullet, leading spaces)
	c.Label("  • Label precision coarsens with magnitude (Harrison et al. 2007): \"~1h15m\" feels faster than \"1:14:37\".").Send()
	c.Label("Watch the metrics below flip sign under decel, and the damped ETA hold while raw ETA jitters under spiky load.").Send()
	c.AddSpace(gapInline())
	c.Separator().Send()
	c.AddSpace(padInner())

	// --- Visual bar.
	progress := float32(st.processed / st.total)
	if progress > 1 {
		progress = 1
	}
	barText := fmt.Sprintf("%d / %d items", int(st.processed), int(st.total))
	c.ProgressBar(progress).Text(barText).ShowPercentage().DesiredWidth(420).Send()

	// --- Live metrics from the Estimator.
	c.AddSpace(padInner())
	remaining := st.total - st.processed
	dampedETA, dampedValid := st.estimator.EstimateETA(remaining)
	rawETA, rawValid := st.estimator.RawETA(remaining)

	rate := st.estimator.SmoothedRate()
	trend := st.estimator.SmoothedTrend()

	elapsed := time.Duration(0)
	if !st.startTime.IsZero() {
		elapsed = now.Sub(st.startTime)
	}

	c.Label(fmt.Sprintf("elapsed       : %s", progressbar.FormatDuration(elapsed))).Send()
	c.Label(fmt.Sprintf("smoothed rate : %.2f items/s  (S level)", rate)).Send()
	c.Label(fmt.Sprintf("smoothed trend: %+.3f items/s per step  (B)", trend)).Send()

	if dampedValid {
		c.Label(fmt.Sprintf("ETA (damped)  : %s   [%s]",
			progressbar.FormatETA(dampedETA), dampedETA.Round(time.Second))).Send()
	} else {
		c.Label("ETA (damped)  : — (warming up)").Send()
	}
	if st.showRawETA {
		if rawValid {
			c.Label(fmt.Sprintf("ETA (raw)     : %s   [%s]",
				progressbar.FormatETA(rawETA), rawETA.Round(time.Second))).Send()
		} else {
			c.Label("ETA (raw)     : —").Send()
		}
	}

	// --- Controls.
	c.AddSpace(gapInline())
	c.Separator().Send()
	c.Label("Controls").Send()

	for range c.Horizontal().KeepIter() {
		startLabel := "Start"
		if st.running {
			startLabel = "Pause"
		}
		if c.Button(ids.PrepareSeq(0xb0a05100), c.Atoms().Text(startLabel).Keep()).
			SendResp().HasPrimaryClicked() {
			st.running = !st.running
			if st.running {
				st.lastTick = time.Now()
			}
		}
		if c.Button(ids.PrepareSeq(0xb0a05101), c.Atoms().Text("Reset").Keep()).
			SendResp().HasPrimaryClicked() {
			st.resetSimulation()
		}
	}

	c.SliderF64(ids.PrepareSeq(0xb0a05110), st.baseRate, 1.0, 500.0).
		Text("base rate (items/s)").
		SendRespVal(&st.baseRate)
	c.SliderF64(ids.PrepareSeq(0xb0a05111), st.total, 100, 100000).
		Text("total items").
		SendRespVal(&st.total)

	// --- Rate mode selector.
	c.AddSpace(padInner())
	c.Label("Rate mode (drives trend tracker)").Send()
	for range c.Horizontal().KeepIter() {
		modes := []struct {
			id    uint64
			label string
			mode  pbRateMode
		}{
			{0xb0a05200, "steady", pbRateSteady},
			{0xb0a05201, "accel", pbRateAccel},
			{0xb0a05202, "decel", pbRateDecel},
			{0xb0a05203, "spiky", pbRateSpiky},
		}
		for _, m := range modes {
			sel := st.rateMode == m.mode
			if c.Button(ids.PrepareSeq(m.id), c.Atoms().Text(m.label).Keep()).
				Selected(sel).FrameWhenInactive(!sel).Frame(true).
				SendResp().HasPrimaryClicked() {
				st.rateMode = m.mode
			}
		}
	}

	// --- Manual perturbations — one-shot clicks that step the base rate.
	c.AddSpace(padInner())
	c.Label("One-shot rate perturbations (show damping behaviour)").Send()
	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareSeq(0xb0a05300), c.Atoms().Text("spike x3").Keep()).
			SendResp().HasPrimaryClicked() {
			st.rateMult = 3.0
		}
		if c.Button(ids.PrepareSeq(0xb0a05301), c.Atoms().Text("slowdown x0.3").Keep()).
			SendResp().HasPrimaryClicked() {
			st.rateMult = 0.3
		}
		if c.Button(ids.PrepareSeq(0xb0a05302), c.Atoms().Text("restore x1").Keep()).
			SendResp().HasPrimaryClicked() {
			st.rateMult = 1.0
		}
	}

	// Repaint every frame while running so the simulation advances smoothly.
	if st.running {
		c.RequestRepaintAfter(0.05)
	}
}
