package implot

import (
	"math"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// paletteDeep is ImPlot's default colormap ("Deep", seaborn's deep 10),
// cycled by series declaration order.
var paletteDeep = [10]uint32{
	0x4c72b0ff, 0xdd8452ff, 0x55a868ff, 0xc44e52ff, 0x8172b3ff,
	0x937860ff, 0xda8bc3ff, 0x8c8c8cff, 0xccb974ff, 0x64b5cdff,
}

// Dark-theme chrome, matched to the house canvas demos.
const (
	colPlotBg     = 0x111318ff
	colAreaBg     = 0x1a1d24ff
	colBorder     = 0x3a3f4bff
	colGridMajor  = 0x2c313cff
	colGridMinor  = 0x21252eff
	colTickLabel  = 0xaab2c0ff
	colAxisLabel  = 0xcdd3ddff
	colTitle      = 0xe6e9eeff
	colReadout    = 0x8891a0ff
	colBoxFill    = 0x4c72b028
	colBoxStroke  = 0x4c72b0cc
	tickFontSize  = 10.5
	labelFontSize = 12.0
	titleFontSize = 13.5
	tickLen       = 4.0
	// charW is the house estimation idiom for text width (ASCII tick labels
	// are digits, for which a monospace estimate is nearly exact).
	charW = tickFontSize * 0.62
)

// axisState is the retained per-axis half of a plot's state.
type axisState struct {
	rng      Range
	fitNext  bool
	hasRange bool
	flags    AxisFlags
	label    string
}

// plotState is the retained state of one plot id across frames — the
// ImPool slot of the port. Interaction gestures read it one frame behind.
type plotState struct {
	x, y        axisState
	initialized bool
	onceApplied bool

	// previous frame's transform + plot-area rect (canvas px), the basis
	// for interpreting this frame's gestures.
	prev     transform
	prevOk   bool
	dragging bool
	dragBox  bool
	boxStart [2]float32
	boxCur   [2]float32
	lastDrag [2]float32
	hoverPos [2]float32
	hoverOk  bool

	// Legend state: per-label visibility toggles (clicks) and the label
	// hovered last frame (series highlight). Labels must be unique within
	// a plot, as in ImPlot.
	hidden      map[string]bool
	legendHover string

	// scratch buffers reused across frames to keep steady-state allocation flat.
	scratchX []float32
	scratchY []float32
	ticksX   []tick
	ticksY   []tick
}

var pool = make(map[uint64]*plotState, 8)

// seriesFrame is one item call, held until End so auto-fit sees the whole
// frame's data before the ranges freeze. Fields beyond xs/ys apply per
// kind: marker+radius for scatter, width for bars, yref for shaded/stems.
type seriesFrame struct {
	kind   seriesKind
	label  string
	xs, ys []float64
	marker MarkerE
	radius float32
	width  float64
	yref   float64
}

// Plot is the frame-transient handle between Begin and End. Methods follow
// ImPlot's protocol: Setup* first, then items; the first item locks setup.
type Plot struct {
	ids         *c.WidgetIdStack
	st          *plotState
	scopeId     uint64
	w, h        float32
	title       string
	setupLocked bool
	series      []seriesFrame
	dataXMin    float64
	dataXMax    float64
	dataYMin    float64
	dataYMax    float64
	dataOk      bool
}

// Begin opens a plot with the given title (which is also its identity, as
// in ImPlot) and canvas size in pixels. Interactions from the previous
// frame are applied to the retained ranges here, before any Setup call.
// Every Begin must be paired with End.
func Begin(ids *c.WidgetIdStack, title string, w float32, h float32) *Plot {
	scope := ids.PrepareStr(title)
	scopeId := scope.DeriveStacked()
	st, ok := pool[scopeId]
	if !ok {
		st = &plotState{hidden: make(map[string]bool, 4)}
		pool[scopeId] = st
	}
	p := &Plot{ids: ids, st: st, scopeId: scopeId, w: w, h: h, title: title,
		dataXMin: math.Inf(1), dataXMax: math.Inf(-1), dataYMin: math.Inf(1), dataYMax: math.Inf(-1)}
	p.applyInteractions()
	return p
}

// canvasHandle / areaHandle derive the same ids the render pass will use,
// without consuming the id-stack state (prepare + Derive is deterministic).
func (p *Plot) canvasHandle() widgethandle.WidgetHandle {
	return widgethandle.Make(p.ids.PrepareStr("implot-canvas").Derive())
}

func (p *Plot) areaHandle() widgethandle.WidgetHandle {
	return widgethandle.Make(p.ids.PrepareStr("implot-area").Derive())
}

// applyInteractions interprets last frame's gesture registers against last
// frame's transform: drag pan, Shift+drag box-zoom, anchored wheel zoom,
// double-click fit. One-frame lag by design (see doc.go).
func (p *Plot) applyInteractions() {
	st := p.st
	sm := c.CurrentApplicationState.StateManager
	cur, curOk := sm.GetCanvasCursor(p.canvasHandle())
	canvasFlags := sm.GetResponse(p.canvasHandle())
	flags := sm.GetResponse(p.areaHandle())
	wheel := sm.GetCanvasWheel(p.canvasHandle())
	mods := sm.GetModifiers()
	ptr := sm.GetPointer()

	// Gesture positions come from the global R20 pointer minus the R24
	// canvas origin: R24's own pointer field goes NaN while the sense
	// region (registered above the canvas) owns a drag, since the canvas
	// then neither hovers nor drags. The origin half of R24 is always
	// stamped. Hover display additionally gates on the canvas's
	// contains_pointer bit so a pointer over an overlapping window does
	// not read as a plot hover.
	posOk := curOk && ptr.Valid
	var posX, posY float32
	if posOk {
		posX = ptr.X - cur.OriginX
		posY = ptr.Y - cur.OriginY
	}
	// The sense region's own R24 row is more precise when present: on the
	// drag-started frame it reports the press origin and the press event's
	// own modifiers (fast input batches press, moves and release into one
	// frame; R20 would start mid-gesture and the frame-end modifier state
	// would miss a modifier already released again).
	boxMod := mods.Shift
	if areaCur, areaOk := sm.GetCanvasCursor(p.areaHandle()); areaOk {
		boxMod = areaCur.Shift()
		if !isNaN32(areaCur.PosX) {
			posX, posY = areaCur.PosX, areaCur.PosY
			posOk = true
		}
	}

	st.hoverOk = posOk && canvasFlags.HasContainsPointer() && st.prevOk &&
		float64(posX) >= st.prev.px0 && float64(posX) <= st.prev.px0+st.prev.plotW &&
		float64(posY) >= st.prev.py0 && float64(posY) <= st.prev.py0+st.prev.plotH
	if posOk {
		st.hoverPos = [2]float32{posX, posY}
	}

	if !st.prevOk {
		return
	}

	if flags.HasDragStarted() && posOk {
		st.dragging = true
		st.dragBox = boxMod
		st.boxStart = [2]float32{posX, posY}
		st.boxCur = st.boxStart
		st.lastDrag = st.boxStart
	}
	if st.dragging && flags.HasDragged() && posOk {
		if st.dragBox {
			st.boxCur = [2]float32{posX, posY}
		} else {
			dxPlot := float64(posX-st.lastDrag[0]) / st.prev.sx
			dyPlot := float64(posY-st.lastDrag[1]) / st.prev.sy
			st.x.rng.Min -= dxPlot
			st.x.rng.Max -= dxPlot
			st.y.rng.Min += dyPlot // pixel y grows downward
			st.y.rng.Max += dyPlot
			st.lastDrag = [2]float32{posX, posY}
		}
	}
	if flags.HasDragStopped() && st.dragging {
		if st.dragBox {
			x0 := st.prev.plotX(st.boxStart[0])
			x1 := st.prev.plotX(st.boxCur[0])
			y0 := st.prev.plotY(st.boxStart[1])
			y1 := st.prev.plotY(st.boxCur[1])
			if abs32(st.boxCur[0]-st.boxStart[0]) > 5 && abs32(st.boxCur[1]-st.boxStart[1]) > 5 {
				st.x.rng = Range{math.Min(x0, x1), math.Max(x0, x1)}
				st.y.rng = Range{math.Min(y0, y1), math.Max(y0, y1)}
			}
		}
		st.dragging = false
		st.dragBox = false
	}
	if flags.HasDoubleClicked() {
		st.x.fitNext = true
		st.y.fitNext = true
	}

	// Wheel: egui delivers pinch/ctrl-wheel as Zoom and plain wheel as
	// ScrollY; ImPlot's convention is that the plain wheel zooms a plot, so
	// both fold into one multiplicative factor, anchored at the pointer.
	zoom := float64(wheel.Zoom)
	if wheel.ScrollY != 0 {
		zoom *= math.Pow(1.1, float64(wheel.ScrollY)/40.0)
	}
	if zoom != 1.0 && zoom > 0 {
		ax := st.prev.plotX(wheel.HoverX)
		ay := st.prev.plotY(wheel.HoverY)
		if isNaN32(wheel.HoverX) {
			ax = (st.x.rng.Min + st.x.rng.Max) / 2
			ay = (st.y.rng.Min + st.y.rng.Max) / 2
		}
		st.x.rng.Min = ax - (ax-st.x.rng.Min)/zoom
		st.x.rng.Max = ax + (st.x.rng.Max-ax)/zoom
		st.y.rng.Min = ay - (ay-st.y.rng.Min)/zoom
		st.y.rng.Max = ay + (st.y.rng.Max-ay)/zoom
	}
}

// SetupAxes names the axes and sets their flags. Must precede the first
// item call, per the ImPlot protocol.
func (p *Plot) SetupAxes(xlabel string, ylabel string, xflags AxisFlags, yflags AxisFlags) *Plot {
	if p.warnIfLocked("SetupAxes") {
		return p
	}
	p.st.x.label, p.st.y.label = xlabel, ylabel
	p.st.x.flags, p.st.y.flags = xflags, yflags
	return p
}

// AxisE selects an axis for SetupAxisLimits. M1 has one x and one y.
type AxisE uint8

const (
	AxisX1 AxisE = iota
	AxisY1
)

// SetupAxisLimits sets an axis range. CondOnce applies only the first
// time this plot id is seen; CondAlways pins the axis every frame.
func (p *Plot) SetupAxisLimits(axis AxisE, vmin float64, vmax float64, cond Cond) *Plot {
	if p.warnIfLocked("SetupAxisLimits") {
		return p
	}
	ax := &p.st.x
	if axis == AxisY1 {
		ax = &p.st.y
	}
	if cond == CondAlways || !p.st.onceApplied {
		ax.rng = Range{vmin, vmax}.sanitize()
		ax.hasRange = true
	}
	return p
}

func (p *Plot) warnIfLocked(fn string) bool {
	if p.setupLocked {
		log.Debug().Str("plot", p.title).Str("call", fn).
			Msg("implot: Setup call after the first item is ignored (protocol: Setup* before items)")
		return true
	}
	return false
}

// Line declares a line series. The slices must be equal-length; NaN points
// split the line, as in ImPlot. Data is not copied — the slices must stay
// valid until End.
func (p *Plot) Line(label string, xs []float64, ys []float64) *Plot {
	p.addSeries(seriesFrame{kind: kindLine, label: label, xs: xs, ys: ys}, true, true)
	return p
}

func isNaN32(v float32) bool { return v != v }

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
