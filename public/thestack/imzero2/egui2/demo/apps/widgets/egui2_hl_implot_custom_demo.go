package widgets

import (
	"fmt"
	"math"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// The custom-item lane demo (ADR-0149 Update 2026-07-31): a mini
// interval-lane chart built entirely from Custom closures on an implot
// time axis. It prototypes the timeline-adoption pattern (survey P3):
// implot owns the x axis and its gestures, the y axis is pinned and the
// lanes are pixel-space geometry (the Digital-item pattern), hit tests run
// one frame behind against PlotAreaPrev.

// implotLaneIvl is one interval event: [t0, t1] Unix seconds on a lane.
type implotLaneIvl struct {
	t0, t1 float64
	lane   int
	name   string
}

type implotCustomState struct {
	intervals      []implotLaneIvl
	flagXs         []float64
	loadXs, loadYs []float64
	tMin, tMax     float64
	selected       int
}

// Lane geometry, shared by the draw closure and the hit test: lanes hang
// from the plot-area top in pixel space and never rescale under zoom.
const (
	implotLaneCount = 3
	implotLaneTop   = float32(10)
	implotLaneH     = float32(26)
	implotLaneGap   = float32(8)
)

// implotLaneAt maps a canvas-pixel y to a lane index, or -1.
func implotLaneAt(areaY float32, py float32) int {
	rel := py - areaY - implotLaneTop
	if rel < 0 {
		return -1
	}
	li := int(rel / (implotLaneH + implotLaneGap))
	if li >= implotLaneCount || rel-float32(li)*(implotLaneH+implotLaneGap) > implotLaneH {
		return -1
	}
	return li
}

// implotIvlAt finds the interval covering (lane, t), or -1.
func implotIvlAt(ivls []implotLaneIvl, lane int, t float64) int {
	if lane < 0 {
		return -1
	}
	for i := range ivls {
		if ivls[i].lane == lane && t >= ivls[i].t0 && t <= ivls[i].t1 {
			return i
		}
	}
	return -1
}

func init() {
	registry.Register(registry.Demo{
		Name:        "implot_custom",
		Category:    "Graphics & canvas",
		Title:       icons.IconPaintBucket + " implot custom items (lane chart)",
		Stage:       [2]float32{700, 480},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "The custom-item lane: an interval-lane chart from Custom closures on a time axis — a maintenance band under a Line series, pixel-pinned lanes and event flags immune to zoom, and an unclipped selection callout that may spill past the plot border. Declaration order is z-order; legend toggles hide custom items; lane hit tests read HoverPixelPos/ClickedPixelPos against PlotAreaPrev, one frame behind. Prototypes the timeline adoption.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			st := &implotCustomState{selected: -1}
			// Fixed base (2026-07-30 00:00 UTC) keeps the capture deterministic.
			const base = 1_785_369_600.0
			const hour = 3600.0
			st.tMin, st.tMax = base, base+72*hour
			st.intervals = []implotLaneIvl{
				{base + 19*hour, base + 22.5*hour, 0, "ingest v41"},
				{base + 30*hour, base + 31*hour, 0, "ingest v42"},
				{base + 46*hour, base + 53*hour, 0, "ingest v43"},
				{base + 21*hour, base + 27*hour, 1, "api rollout"},
				{base + 44*hour, base + 45.5*hour, 1, "api hotfix"},
				{base + 62*hour, base + 68*hour, 1, "api v2 canary"},
				{base + 24*hour, base + 26*hour, 2, "schema migrate"},
				{base + 50*hour, base + 58*hour, 2, "backfill"},
			}
			st.flagXs = []float64{base + 20*hour, base + 25.5*hour, base + 44.8*hour,
				base + 52*hour, base + 63*hour}
			for i := range 73 {
				t := base + float64(i)*hour
				st.loadXs = append(st.loadXs, t)
				st.loadYs = append(st.loadYs, 0.16+0.09*math.Sin(float64(i)/5.5)+
					0.05*math.Sin(float64(i)/1.9))
			}
			return st
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			st := state.(*implotCustomState)
			hoverIvl := -1
			for p := range implot.Scoped(ids, "deploys##lanechart", 660, 360) {
				p.SetupAxes("", "", implot.AxisFlagsNone,
					implot.AxisFlagsNoTickLabels|implot.AxisFlagsNoGrid)
				p.SetupAxisScale(implot.AxisX1, implot.ScaleTime)
				// y pinned: the axis is not navigable, so gestures act on x
				// only — the lane-chart contract. Lanes live in pixel space.
				p.SetupAxisLimits(implot.AxisY1, 0, 1, implot.CondAlways)
				p.IncludeX(st.tMin)
				p.IncludeX(st.tMax)

				// Hit tests, one frame behind: pixel y → lane (against last
				// frame's area rect), plot x → time.
				if _, py, ok := p.HoverPixelPos(); ok {
					if _, ay, _, _, aok := p.PlotAreaPrev(); aok {
						if hx, _, hok := p.HoverPlotPos(); hok {
							hoverIvl = implotIvlAt(st.intervals, implotLaneAt(ay, py), hx)
						}
					}
				}
				if _, py, ok := p.ClickedPixelPos(); ok {
					if _, ay, _, _, aok := p.PlotAreaPrev(); aok {
						if cx, _, cok := p.Clicked(); cok {
							st.selected = implotIvlAt(st.intervals, implotLaneAt(ay, py), cx)
						}
					}
				}

				// Declaration order is z-order: band under the line, lanes
				// and flags over it, the callout on top and unclipped.
				p.Custom("", func(dc implot.DrawCtx) {
					x0 := dc.T.PxX(st.tMin + 36*3600)
					x1 := dc.T.PxX(st.tMin + 42*3600)
					c.PaintRectFilled(x0, dc.AreaY, x1, dc.AreaY+dc.AreaH, 0,
						color.Hex(0x8891a01c)).Send()
					c.PaintText((x0+x1)/2, dc.AreaY+dc.AreaH-6, 1, 2, "maintenance",
						10.5, color.Hex(0x8891a0aa)).Send()
				})
				p.Line("load", st.loadXs, st.loadYs)
				p.Custom("deploys", func(dc implot.DrawCtx) {
					for i := range st.intervals {
						iv := &st.intervals[i]
						y0 := dc.AreaY + implotLaneTop + float32(iv.lane)*(implotLaneH+implotLaneGap)
						x0, x1 := dc.T.PxX(iv.t0), dc.T.PxX(iv.t1)
						fill := (dc.Color &^ 0xff) | 0xb4
						if i == st.selected || i == hoverIvl {
							fill = dc.Color
						}
						c.PaintRectFilled(x0, y0, x1, y0+implotLaneH, 3, color.Hex(fill)).Send()
						if i == st.selected {
							c.PaintRectStroke(x0, y0, x1, y0+implotLaneH, 3,
								color.Hex(0xe6e9eeff), 1.5).Send()
						}
						if x1-x0 > float32(len(iv.name))*6.5+8 {
							c.PaintText(x0+5, y0+implotLaneH/2, 0, 1, iv.name,
								10.5, color.Hex(0x111318ff)).Send()
						}
					}
				})
				p.Custom("flags", func(dc implot.DrawCtx) {
					xs := make([]float32, 0, len(st.flagXs))
					ys := make([]float32, 0, len(st.flagXs))
					for _, t := range st.flagXs {
						xs = append(xs, dc.T.PxX(t))
						ys = append(ys, dc.AreaY+dc.AreaH-12)
					}
					c.PaintMarkers(xs, ys, uint8(implot.MarkerUp), 5, color.Hex(dc.Color), dc.Weight).Send()
				})
				p.CustomUnclipped("", func(dc implot.DrawCtx) {
					if st.selected < 0 || st.selected >= len(st.intervals) {
						return
					}
					iv := &st.intervals[st.selected]
					y := dc.AreaY + implotLaneTop + float32(iv.lane)*(implotLaneH+implotLaneGap) + implotLaneH/2
					x := dc.T.PxX(iv.t1)
					txt := fmt.Sprintf("%s · %dm", iv.name, int((iv.t1-iv.t0)/60))
					w := float32(len(txt))*6.5 + 10
					// Deliberately allowed to spill past the plot border —
					// the unclipped lane's reason to exist.
					c.PaintRectFilled(x+6, y-9, x+6+w, y+9, 3, color.Hex(0x14171dee)).Send()
					c.PaintRectStroke(x+6, y-9, x+6+w, y+9, 3, color.Hex(0x3a3f4bff), 1).Send()
					c.PaintText(x+11, y, 0, 1, txt, 10.5, color.Hex(0xcdd3ddff)).Send()
				})
			}
			status := "hover a lane; click selects (click empty space to clear)"
			if hoverIvl >= 0 {
				iv := &st.intervals[hoverIvl]
				status = fmt.Sprintf("%s — %s, %d min", iv.name,
					time.Unix(int64(iv.t0), 0).UTC().Format("Jan 2 15:04"),
					int((iv.t1-iv.t0)/60))
			}
			c.LabelAtoms(c.Atoms().Text(status).Keep()).Send()
		},
	})
}
