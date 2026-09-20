package widgets

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/stergiotis/boxer/public/science/geo/vectorfield"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/basemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan/flowoverlay"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan/landoverlay"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timescrubber"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/worldmap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
)

// The flow-on-a-map demo of ADR-0249: a gridded vector field drawn as
// particles on a portolan map, by portolan/flowoverlay, as one more guest in
// the map's overlay callback.
//
// The field is analytic — a meandering jet and a row of vortices that drift
// east from step to step — so the demo needs no data file, and it is served
// through the same in-memory pyramid a real field would be: a half-degree
// global grid, nine three-hourly steps. The basemap is the offline country
// outlines, so the capture needs no tile server either.
//
// What to look at: the trails keep to the ground under a pan; zooming in
// leaves their pace on screen unchanged and asks the pyramid for a finer
// window; scrubbing the time blends two steps, and a vortex between two steps
// fades across rather than travelling — the defect of linear blending the
// package doc owns up to. The readout under the map tells the two means
// apart: particles follow the vector mean, colour follows the scalar mean.
const (
	flowOnMapW = 960
	flowOnMapH = 520
	// flowOnMapCaptureTicks is how long the capture lets the particles run:
	// long enough for every trail to be full length and the density even.
	flowOnMapCaptureTicks = 120
	flowOnMapSteps        = 9
	flowOnMapStepHours    = 3
	flowOnMapSpeedMax     = 32
)

type flowOnMapState struct {
	m     *portolan.Map
	layer *flowoverlay.Layer
	err   error

	land  *landoverlay.Layer
	atlas *worldmap.Atlas

	gv     *graphview.View
	nodes  []graphview.NodeSpec
	edges  []graphview.EdgeSpec
	origin portolan.Point

	// scrub is the time strip (ADR-0251); it owns the display time.
	scrub *timescrubber.Scrubber
	steps []timescrubber.Step

	capture   bool
	tiles     bool
	showGraph bool
	paused    bool
	density   float64
}

func newFlowOnMapState(ids *c.WidgetIdStack) *flowOnMapState {
	st := &flowOnMapState{
		m: portolan.New(ids, portolan.Options{
			Source:  basemap.PortolanSource(),
			Loader:  basemap.PortolanLoader(),
			Center:  portolan.LL(35, 5),
			Zoom:    2.6,
			NoTiles: true,
			// A dark sea and a slightly lighter land: trails are light.
			Background: 0x0e141bff,
		}),
		land:    &landoverlay.Layer{},
		capture: imzero2env.ScreenshotDir.Get() != "",
		density: 5,
	}
	if a, err := worldmap.LoadAtlas(); err == nil {
		st.atlas = a
	}

	t0 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	steps := make([]vectorfield.Step, 0, flowOnMapSteps)
	for i := range flowOnMapSteps {
		steps = append(steps, vectorfield.Step{Valid: t0.Add(time.Duration(i*flowOnMapStepHours) * time.Hour), Reference: t0})
	}
	src, err := vectorfield.NewPyramidE(context.Background(), vectorfield.Meta{
		Name: "synthetic jet and vortices", Quantity: "wind", Unit: "m/s",
		Surface:    vectorfield.Surface{Kind: vectorfield.SurfaceKindHeightAboveGround, Value: 10, Unit: "m"},
		Provenance: "closed form, sampled onto a half-degree global grid",
		West:       -180, East: 179.5, South: -90, North: 90, DLon: 0.5, DLat: 0.5, PeriodicLon: true,
		Steps: steps, SpeedMax: flowOnMapSpeedMax,
	}, vectorfield.NewGlobalAnalyticLoader(0.5, vectorfield.Swirl(4)), vectorfield.PyramidOptions{})
	if err != nil {
		st.err = err
		return st
	}
	st.layer = flowoverlay.New(src, flowoverlay.Options{Seed: 1})
	if st.capture {
		// A capture must not depend on when a goroutine ran or on the clock.
		st.layer.Opts.Synchronous = true
		st.layer.Opts.FixedTicks = flowOnMapCaptureTicks
	}
	st.scrub = timescrubber.New(ids, timescrubber.Options{ScopeKey: "fom-time"})
	if st.capture {
		// Between two steps, so the capture shows the blend.
		st.scrub.Opts.NoSnap = true
		st.scrub.Transport.Pos = 1.5
	}

	// The graph guest: the located sites of the graph-on-a-map demo, pinned.
	st.gv = graphview.New(ids, "flow-on-map-graph", graphview.Options{
		Layout: graphview.LayoutForceDirected, NoZoomAndPan: true, NodeClicking: true, LabelsAlways: true,
		Force: graphview.ForceParams{PauseOnSettle: true},
	})
	st.origin = st.m.View().ProjectAt(graphOnMapCentre, graphOnMapRefZoom)
	for _, l := range graphOnMapLinks {
		if l[0] <= uint64(len(graphOnMapSites)) && l[1] <= uint64(len(graphOnMapSites)) {
			st.edges = append(st.edges, graphview.EdgeSpec{From: l[0], To: l[1], Width: 2})
		}
	}
	return st
}

// flowStepState words the layer's step state for the time strip. The two
// enums are spelled alike and are not the same type: neither package imports
// the other.
func flowStepState(s flowoverlay.StepStateE) timescrubber.StepStateE {
	switch s {
	case flowoverlay.StepStateHeld:
		return timescrubber.StepStateHeld
	case flowoverlay.StepStateLoading:
		return timescrubber.StepStateLoading
	case flowoverlay.StepStateMissing:
		return timescrubber.StepStateMissing
	}
	return timescrubber.StepStateIdle
}

func demoFlowOnMap(ids *c.WidgetIdStack, st *flowOnMapState) {
	if st.err != nil {
		c.Label("the field could not be built: " + st.err.Error()).Wrap().Send()
		return
	}
	m, layer := st.m, st.layer
	m.SetNoTiles(!st.tiles)

	layer.SetStepPosition(st.scrub.Transport.Pos)
	layer.Opts.Paused = st.paused
	layer.Opts.Density = float32(st.density)

	var claim graphview.HostClaim
	if st.showGraph {
		canvas, area := m.Handles()
		claim = st.gv.HostedInput(graphview.HostCanvas{
			Canvas: canvas, Area: area, W: flowOnMapW, H: flowOnMapH,
			Camera: m.View().CameraAt(graphOnMapRefZoom, st.origin),
		})
	}
	m.SetPointerVeto(claim.Pointer)

	m.Render(flowOnMapW, flowOnMapH, func(p portolan.Projector) {
		// Call order is paint order: the land, the flow over it, the graph on top.
		ls := landoverlay.DefaultStyle()
		if !st.tiles {
			ls.Land, ls.Border = color.Hex(0x262d36ff), color.Hex(0x4a5563ff)
		}
		st.land.Draw(p, st.atlas, ls)
		layer.Draw(p)
		if st.showGraph {
			cam := p.CameraAt(graphOnMapRefZoom, st.origin)
			st.gv.SetHostCamera(cam)
			st.nodes = st.nodes[:0]
			for _, s := range graphOnMapSites {
				at := p.View().ProjectAt(portolan.LL(s.lat, s.lng), graphOnMapRefZoom)
				st.nodes = append(st.nodes, graphview.NodeSpec{
					Id: s.id, Label: s.label, Radius: graphOnMapNodePx / max(cam.Zoom, 1e-6),
					Pinned: true, PinX: float32(at.X - st.origin.X), PinY: float32(at.Y - st.origin.Y),
					Color: color.Hex(0xf2f2f2ff),
				})
			}
			st.gv.HostedPaint(st.nodes, st.edges)
		}
	})
	if st.showGraph {
		for range st.gv.Events() {
		}
	}

	// The time strip: each step at its valid time, with what the layer holds
	// of it. This source has no per-step summary, so the strip has no bars.
	meta := layer.Meta()
	st.steps = st.steps[:0]
	for i := range meta.Steps {
		st.steps = append(st.steps, timescrubber.Step{
			At: meta.Steps[i].Valid, State: flowStepState(layer.StepState(i)),
			Value: float32(math.NaN()), Peak: float32(math.NaN()),
		})
	}
	st.scrub.Render(flowOnMapW, st.steps)

	stats := layer.Stats()
	v := m.View()
	c.Label(fmt.Sprintf("%s   ·   zoom %.2f   ·   %d particles, %d segments in one paintSegments   ·   window %d × %d at level %d   ·   %d requests, %d late replies dropped",
		layer.Time().Format("2006-01-02 15:04 MST"), v.Zoom(), stats.Particles, stats.Segments,
		stats.WindowCols, stats.WindowRows, stats.WindowLevel, stats.Fetches, stats.DroppedReplies)).Send()
	if stats.LastError != nil {
		c.Label("last request failed: " + stats.LastError.Error()).Wrap().Send()
	}
	if ll, ok := m.Hover(); ok {
		if u, vv, speed, has := layer.At(ll); has {
			mean := math.Hypot(float64(u), float64(vv))
			from := math.Mod(math.Atan2(-float64(u), -float64(vv))*180/math.Pi+360, 360)
			c.Label(fmt.Sprintf("at %.2f, %.2f:   vector mean %.1f m/s from %03.0f° (what a particle follows)   ·   scalar mean %.1f m/s (what the colour shows)",
				ll.Lat, ll.Lng, mean, from, speed)).Send()
		} else {
			c.Label(fmt.Sprintf("at %.2f, %.2f:   no data", ll.Lat, ll.Lng)).Send()
		}
	} else {
		c.Label("hover the map to read the field").Send()
	}
	c.Label("ADR-0249: the animation shows direction and relative speed, not transport — a particle's pace is a screen quantity, " +
		"the same at every zoom, and a trail is a streamlet of the field at the display time, not a trajectory. " +
		"Between two steps the field is blended linearly, so a vortex fades across instead of travelling.").Wrap().Send()

	for range c.CollapsingHeader(ids.PrepareStr("fom-controls"), c.WidgetText().Text("controls").Keep()).DefaultOpen(true).KeepIter() {
		for range c.HorizontalTop().KeepIter() {
			c.Checkbox(ids.PrepareStr("fom-pause"), st.paused, "pause the particles").SendRespVal(&st.paused)
			c.Checkbox(ids.PrepareStr("fom-graph"), st.showGraph, "a graph on top (hosted canvas)").SendRespVal(&st.showGraph)
			c.Checkbox(ids.PrepareStr("fom-tiles"), st.tiles, "basemap tiles (needs a tile server)").SendRespVal(&st.tiles)
		}
		c.SliderF64(ids.PrepareStr("fom-density"), st.density, 1, 12).Text("particles per 1000 px²").SendRespVal(&st.density)
	}
}
