package widgets

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan/flowoverlay"
	"github.com/stergiotis/boxer/public/thestack/imzero2/metrics"
)

// The measurement harness of the flow-particles trial (ADR-0249 M5;
// doc/trials/flow-particles-frame-cost). It is not a showcase: one NoTiles map
// with a flow layer on it and nothing else, a particle count and a paint arm
// chosen from the controls, and a window of frames summarised into one label
// and one log line.
//
// What it can and cannot see. The Go side's cost of the layer is timed
// directly around Layer.Draw. The host's dispatch of the frame's opcodes
// (InterpretNs) and the bytes written across the FFFI boundary come from the
// frame metrics and cover the whole frame, gallery chrome included, which is
// why the trial has a no-particles arm to subtract. Tessellation and
// rasterisation happen after dispatch and are not visible from here at all;
// the headless hosts report them under IMZERO2_HEADLESS_RASTER_STATS.
const (
	flowBenchW = 960
	flowBenchH = 600
	// flowBenchWarmup frames are dropped after a control changes: trails have
	// to fill, and the first frames of a new count allocate.
	flowBenchWarmup = 90
	flowBenchWindow = 300
)

var flowBenchArms = []string{"segments-mesh", "segments-tessellated", "line-per-segment"}

// flowBenchCounts are the particle counts a run can ask for. Buttons and not
// a slider: a driver clicks a button by name.
var flowBenchCounts = []int{0, 1000, 2500, 5000, 10000, 20000, 40000}

type flowBenchState struct {
	m     *portolan.Map
	layer *flowoverlay.Layer
	err   error

	count int
	arm   int

	appliedCount int
	appliedArm   int
	frames       int
	drawNs       []int64
	interpretNs  []int64
	written      []int64
	segments     int64
	reported     bool
	summary      string
	summaryGo    string
	summaryHost  string
}

func newFlowBenchState(ids *c.WidgetIdStack) *flowBenchState {
	st := &flowBenchState{
		m: portolan.New(ids, portolan.Options{
			Center: portolan.LL(35, 5), Zoom: 2.6, NoTiles: true, Background: 0x0e141bff,
			NoDragging: true, NoScrollWheelZoom: true, NoDoubleClickZoom: true, NoKeyboard: true,
		}),
		// Nothing until the trial's script asks: a run's raster statistics are
		// cumulative, and frames of a configuration nobody asked for would be
		// in them.
		count:        0,
		appliedCount: -1,
		drawNs:       make([]int64, 0, flowBenchWindow),
		interpretNs:  make([]int64, 0, flowBenchWindow),
		written:      make([]int64, 0, flowBenchWindow),
	}
	t0 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	src, err := vectorfield.NewPyramidE(context.Background(), vectorfield.Meta{
		Name: "synthetic jet and vortices", Unit: "m/s", SpeedMax: flowOnMapSpeedMax,
		// The geometry is given, so nothing is loaded while the gallery mounts.
		West: -180, East: 179.5, South: -90, North: 90, DLon: 0.5, DLat: 0.5, PeriodicLon: true,
		Steps: []vectorfield.Step{{Valid: t0}},
	}, vectorfield.NewGlobalAnalyticLoader(0.5, vectorfield.Swirl(0)), vectorfield.PyramidOptions{})
	if err != nil {
		st.err = err
		return st
	}
	// A density no canvas reaches, so the cap is the count.
	st.layer = flowoverlay.New(src, flowoverlay.Options{Seed: 1, Density: 1e6})
	return st
}

func demoFlowBench(ids *c.WidgetIdStack, st *flowBenchState) {
	if st.err != nil {
		c.Label("the field could not be built: " + st.err.Error()).Wrap().Send()
		return
	}
	count := st.count
	if count != st.appliedCount || st.arm != st.appliedArm {
		st.appliedCount, st.appliedArm = count, st.arm
		st.frames, st.segments, st.reported = 0, 0, false
		st.drawNs, st.interpretNs, st.written = st.drawNs[:0], st.interpretNs[:0], st.written[:0]
		st.summary = ""
	}
	o := &st.layer.Opts
	o.MaxParticles = max(count, 1)
	o.Paused = count == 0
	o.Tessellated = st.arm == 1
	o.LinePerSegment = st.arm == 2

	if st.summary != "" {
		c.Label(st.summary).Send()
		c.Label(st.summaryGo).Send()
		c.Label(st.summaryHost).Send()
	} else {
		c.Label(fmt.Sprintf("bench running arm=%s particles=%d measured=%d of %d (after %d warm-up frames)",
			flowBenchArms[st.arm], count, len(st.drawNs), flowBenchWindow, flowBenchWarmup)).Send()
	}
	// The controls come first: the gallery's window shows the top of a demo,
	// and a driver cannot set a slider it cannot see.
	for range c.HorizontalTop().KeepIter() {
		for i, n := range flowBenchCounts {
			label := fmt.Sprintf("n=%d", n)
			if c.Button(ids.PrepareSeq(uint64(0xfc00+i)), c.Atoms().Text(label).Keep()).Selected(st.count == n).SendResp().HasPrimaryClicked() {
				st.count = n
			}
		}
	}
	for range c.HorizontalTop().KeepIter() {
		for i, name := range flowBenchArms {
			if c.Button(ids.PrepareSeq(uint64(0xfb00+i)), c.Atoms().Text(name).Keep()).Selected(st.arm == i).SendResp().HasPrimaryClicked() {
				st.arm = i
			}
		}
	}

	var drawNs int64
	st.m.Render(flowBenchW, flowBenchH, func(p portolan.Projector) {
		if count == 0 {
			return
		}
		t := time.Now()
		st.layer.Draw(p)
		drawNs = time.Since(t).Nanoseconds()
	})
	if count == 0 {
		// Nothing asks for the next frame when no layer animates.
		c.RequestRepaintAfter(1.0 / 30)
	}

	stats := st.layer.Stats()
	ready := count == 0 || stats.WindowCols > 0
	if ready {
		st.frames++
	}
	if ready && st.frames > flowBenchWarmup && len(st.drawNs) < flowBenchWindow {
		// The frame metrics describe the frame before this one, which the
		// warm-up makes a frame of the same configuration.
		st.drawNs = append(st.drawNs, drawNs)
		st.interpretNs = append(st.interpretNs, metrics.Current.LastInterpretNs)
		st.written = append(st.written, metrics.Current.LastWritten)
		st.segments += int64(stats.Segments)
	}
	if len(st.drawNs) == flowBenchWindow && !st.reported {
		st.reported = true
		segs := st.segments / flowBenchWindow
		if count == 0 {
			segs = 0
		}
		draw50, draw95 := flowBenchQuantiles(st.drawNs)
		int50, int95 := flowBenchQuantiles(st.interpretNs)
		wr50, _ := flowBenchQuantiles(st.written)
		// Three short labels and not one long one: a driver's tree listing
		// clips a long value.
		st.summary = fmt.Sprintf("bench done arm=%s particles=%d segments=%d frames=%d", flowBenchArms[st.arm], count, segs, flowBenchWindow)
		st.summaryGo = fmt.Sprintf("bench go draw_p50_us=%d draw_p95_us=%d", draw50/1000, draw95/1000)
		st.summaryHost = fmt.Sprintf("bench host interpret_p50_us=%d interpret_p95_us=%d written_p50_bytes=%d", int50/1000, int95/1000, wr50)
		log.Info().Str("arm", flowBenchArms[st.arm]).Int("particles", count).Int64("segments", segs).
			Int("frames", flowBenchWindow).
			Int64("drawP50Us", draw50/1000).Int64("drawP95Us", draw95/1000).
			Int64("interpretP50Us", int50/1000).Int64("interpretP95Us", int95/1000).
			Int64("writtenP50Bytes", wr50).
			Msg("flowbench window complete")
	}

	c.Label("ADR-0249 M5. Draw time is the Go side of the layer alone; interpret time and bytes are the whole frame's, " +
		"so compare against particles=0. Tessellation and rasterisation are the host's and are reported by it " +
		"(IMZERO2_HEADLESS_RASTER_STATS), not here.").Wrap().Send()
}

// flowBenchQuantiles returns the median and the 95th percentile.
func flowBenchQuantiles(samples []int64) (p50, p95 int64) {
	sorted := slices.Clone(samples)
	slices.Sort(sorted)
	n := len(sorted)
	return sorted[n/2], sorted[min(n*95/100, n-1)]
}
