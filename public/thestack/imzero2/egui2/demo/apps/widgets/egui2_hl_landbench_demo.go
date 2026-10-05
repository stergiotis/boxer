package widgets

import (
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan/landoverlay"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/worldmap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/metrics"
)

// The measurement harness of the land frame-cost trial
// (doc/trials/portolan-land-frame-cost): one NoTiles map with the landoverlay
// on it, a view and a paint arm chosen from the controls, and a window of
// frames summarised into labels and one log line — flowbench's shape, for
// static vector geometry instead of particles.
//
// What it can and cannot see is flowbench's: the Go side of the layer is timed
// around Layer.Paint; the host's dispatch (InterpretNs) and the bytes written
// are the whole frame's, gallery chrome included, hence the "off" arm to
// subtract; tessellation and rasterisation are the headless hosts' own
// statistics. The concave fills are triangulated while the host dispatches the
// canvas, so their cost is in the interpret figure, and fill against nofill is
// how the trial separates it.
const (
	landBenchW      = 960
	landBenchH      = 600
	landBenchWarmup = 90
	landBenchWindow = 300
)

var landBenchArms = []string{"fill", "nofill", "off"}

var landBenchViews = []struct {
	name     string
	lat, lng float64
	zoom     float64
}{
	{"world-z0", 20, 0, 0},
	{"europe-z2.6", 35, 5, 2.6},
	{"alps-z5", 46.5, 9, 5},
	{"swiss-z8", 46.8, 8.2, 8},
}

type landBenchState struct {
	m     *portolan.Map
	layer *landoverlay.Layer
	atlas *worldmap.Atlas
	err   error

	// view is -1 until the trial's script asks: a launch's raster statistics
	// are cumulative, and frames nobody asked for would be in them.
	view, arm               int
	appliedView, appliedArm int
	frames                  int
	drawNs                  []int64
	interpretNs             []int64
	written                 []int64
	countries               int
	reported                bool
	summary                 [3]string
}

func newLandBenchState(ids *c.WidgetIdStack) *landBenchState {
	st := &landBenchState{
		m: portolan.New(ids, "lb-map", portolan.Options{
			Center: portolan.LL(20, 0), Zoom: 0, NoTiles: true, Background: 0x0e141bff,
			NoDragging: true, NoScrollWheelZoom: true, NoDoubleClickZoom: true, NoKeyboard: true,
		}),
		layer:       &landoverlay.Layer{},
		view:        -1,
		appliedView: -2,
		drawNs:      make([]int64, 0, landBenchWindow),
		interpretNs: make([]int64, 0, landBenchWindow),
		written:     make([]int64, 0, landBenchWindow),
	}
	st.atlas, st.err = worldmap.LoadAtlas()
	return st
}

func demoLandBench(ids *c.WidgetIdStack, st *landBenchState) {
	if st.err != nil {
		c.Label("the atlas could not be loaded: " + st.err.Error()).Wrap().Send()
		return
	}
	if st.view != st.appliedView || st.arm != st.appliedArm {
		st.appliedView, st.appliedArm = st.view, st.arm
		st.frames, st.countries, st.reported = 0, 0, false
		st.drawNs, st.interpretNs, st.written = st.drawNs[:0], st.interpretNs[:0], st.written[:0]
		st.summary = [3]string{}
		if st.view >= 0 {
			v := landBenchViews[st.view]
			st.m.View().SetView(portolan.LL(v.lat, v.lng), v.zoom)
		}
	}
	viewName := "none"
	if st.view >= 0 {
		viewName = landBenchViews[st.view].name
	}
	if st.summary[0] != "" {
		for _, s := range st.summary {
			c.Label(s).Send()
		}
	} else {
		c.Label(fmt.Sprintf("bench running arm=%s view=%s measured=%d of %d (after %d warm-up frames)",
			landBenchArms[st.arm], viewName, len(st.drawNs), landBenchWindow, landBenchWarmup)).Send()
	}
	// The controls come first: the gallery's window shows the top of a demo,
	// and a driver cannot click a button it cannot see.
	for range c.HorizontalTop().KeepIter() {
		for i, v := range landBenchViews {
			if c.Button(ids.PrepareSeq(uint64(0xfd00+i)), c.Atoms().Text(v.name).Keep()).Selected(st.view == i).SendResp().HasPrimaryClicked() {
				st.view = i
			}
		}
	}
	for range c.HorizontalTop().KeepIter() {
		for i, name := range landBenchArms {
			if c.Button(ids.PrepareSeq(uint64(0xfe00+i)), c.Atoms().Text(name).Keep()).Selected(st.arm == i).SendResp().HasPrimaryClicked() {
				st.arm = i
			}
		}
	}

	var drawNs int64
	style := landoverlay.DefaultStyle()
	style.NoFill = st.arm == 1
	st.m.Render(landBenchW, landBenchH, func(p portolan.Projector) {
		if st.view < 0 || st.arm == 2 {
			return
		}
		t := time.Now()
		st.layer.Paint(p, st.atlas, style)
		drawNs = time.Since(t).Nanoseconds()
	})
	// A static map asks for no frames of its own; the window needs a steady
	// stream of them.
	c.RequestRepaintAfter(1.0 / 30)

	if st.view < 0 {
		return
	}
	st.frames++
	if st.frames > landBenchWarmup && len(st.drawNs) < landBenchWindow {
		// The frame metrics describe the frame before this one, which the
		// warm-up makes a frame of the same configuration.
		st.drawNs = append(st.drawNs, drawNs)
		st.interpretNs = append(st.interpretNs, metrics.Current.LastInterpretNs)
		st.written = append(st.written, metrics.Current.LastWritten)
		st.countries = st.layer.Drawn()
		if st.arm == 2 {
			st.countries = 0
		}
	}
	if len(st.drawNs) == landBenchWindow && !st.reported {
		st.reported = true
		draw50, draw95 := flowBenchQuantiles(st.drawNs)
		int50, int95 := flowBenchQuantiles(st.interpretNs)
		wr50, _ := flowBenchQuantiles(st.written)
		arm := landBenchArms[st.arm]
		st.summary[0] = fmt.Sprintf("bench done arm=%s view=%s countries=%d frames=%d", arm, viewName, st.countries, landBenchWindow)
		st.summary[1] = fmt.Sprintf("bench go draw_p50_us=%d draw_p95_us=%d", draw50/1000, draw95/1000)
		st.summary[2] = fmt.Sprintf("bench host interpret_p50_us=%d interpret_p95_us=%d written_p50_bytes=%d", int50/1000, int95/1000, wr50)
		log.Info().Str("arm", arm).Str("view", viewName).Int("countries", st.countries).Int("frames", landBenchWindow).
			Int64("drawP50Us", draw50/1000).Int64("drawP95Us", draw95/1000).
			Int64("interpretP50Us", int50/1000).Int64("interpretP95Us", int95/1000).
			Int64("writtenP50Bytes", wr50).
			Msg("landbench window complete")
	}
	c.Label("Draw time is the Go side of the layer alone; interpret time and bytes are the whole frame's, " +
		"so compare against arm=off. Tessellation and rasterisation are the host's (IMZERO2_HEADLESS_RASTER_STATS).").Wrap().Send()
}
