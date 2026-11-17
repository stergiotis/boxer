package implot

import (
	"math"
	"time"
)

// GestureE names what moved an axis's range when the person did it — the
// kind half of [Plot.RangeGesture]. A house extension: upstream ImPlot has no
// gesture readback, only the limits.
type GestureE uint8

const (
	GestureNone GestureE = iota
	// GestureBoxZoom is a Shift+drag box released over the plot area.
	GestureBoxZoom
	// GestureWheelZoom is a run of wheel or pinch steps, reported once the
	// wheel has been quiet for wheelQuiet.
	GestureWheelZoom
	// GesturePan is a drag released after it moved the axis.
	GesturePan
	// GestureFit is a double-click on the plot area, which fits the axis to
	// the data.
	GestureFit
	// GestureSelect is a plain drag released under DragSelects: the range
	// under it, with the view left where it was.
	GestureSelect
)

// String names the gesture for logs and readouts.
func (inst GestureE) String() string {
	switch inst {
	case GestureBoxZoom:
		return "box zoom"
	case GestureWheelZoom:
		return "wheel zoom"
	case GesturePan:
		return "pan"
	case GestureFit:
		return "fit"
	case GestureSelect:
		return "select"
	}
	return "none"
}

// wheelQuiet is how long the wheel must rest before a run of wheel steps is
// one finished gesture. The wheel has no release, and each notch moves the
// range, so without a pause every notch would be a gesture of its own.
const wheelQuiet = 300 * time.Millisecond

// clockNow is the clock the wheel run and the crosshair link are timed
// against; tests replace it.
var clockNow = time.Now

// gestureRec is one finished gesture on one axis: its kind and the range it
// asked for.
type gestureRec struct {
	kind GestureE
	rng  Range
}

// gestureTrack is the per-plot record of range gestures, indexed by AxisE.
// A gesture finishes in applyInteractions (pending), takes its final range
// at End (settle → done), and is handed to the next frame's Plot at Begin,
// which reports it until its own End. Every gesture therefore reaches
// exactly one frame's handle, and a range that moved for any other reason —
// a fit the caller asked for, AxisFlagsAutoFit or Follow, a link adopting
// another plot's range, SetupAxisLimits — never makes one.
type gestureTrack struct {
	pending  [2]gestureRec
	done     [2]gestureRec
	panMoved [2]bool
	// The wheel run in progress: the range after its latest step per axis
	// it moved, and when that step was.
	wheel     [2]gestureRec
	wheelLast time.Time
}

// finish records a gesture that ended this frame on one axis. A later
// gesture on the same axis in the same frame replaces it.
func (inst *gestureTrack) finish(axis AxisE, kind GestureE, rng Range) {
	inst.pending[axis] = gestureRec{kind: kind, rng: rng}
}

// wheelStep extends the wheel run by one step that moved the given axes to
// the given ranges.
func (inst *gestureTrack) wheelStep(moved [2]bool, rngs [2]Range, now time.Time) {
	for a := range moved {
		if moved[a] {
			inst.wheel[a] = gestureRec{kind: GestureWheelZoom, rng: rngs[a]}
		}
	}
	inst.wheelLast = now
}

// wheelActive reports whether a wheel run is waiting to finish.
func (inst *gestureTrack) wheelActive() bool {
	return inst.wheel[0].kind != GestureNone || inst.wheel[1].kind != GestureNone
}

// wheelFlush finishes the wheel run when the wheel has rested long enough,
// or at once when force is set (another gesture starts, so the run is over).
// It reports whether a run is still waiting afterwards.
func (inst *gestureTrack) wheelFlush(now time.Time, force bool) (waiting bool) {
	if !inst.wheelActive() {
		return false
	}
	if !force && now.Sub(inst.wheelLast) < wheelQuiet {
		return true
	}
	for a := range inst.wheel {
		if inst.wheel[a].kind != GestureNone {
			inst.pending[a] = inst.wheel[a]
		}
	}
	inst.wheel = [2]gestureRec{}
	return false
}

// settle gives this frame's finished gestures their final ranges and moves
// them to done, from where the next Begin hands them out. A fit takes the
// range End settled on; the others keep the range the gesture asked for,
// sanitized and held to this frame's constraints — a caller that pins the
// axis (SetupAxisLimits with CondAlways) still learns what the person chose.
func (inst *gestureTrack) settle(x *axisState, y *axisState) {
	for a, ax := range [2]*axisState{x, y} {
		g := inst.pending[a]
		switch g.kind {
		case GestureNone:
			continue
		case GestureFit:
			g.rng = ax.rng
		case GestureSelect:
			// A selection lies inside the view it was made on; the
			// constraints govern where the view may go, not what part of
			// it the person marked.
			g.rng = sanitizeScaled(g.rng, ax.scale)
		default:
			g.rng = ax.constrainRange(sanitizeScaled(g.rng, ax.scale))
		}
		inst.done[a] = g
		inst.pending[a] = gestureRec{}
	}
}

// boxZoomRanges is the range a released box asks for on each axis, through
// last frame's transform: none for a box under 5 px on either side (a click
// with Shift held, not a box), none on a NoZoom axis.
func boxZoomRanges(pr transform, start [2]float32, cur [2]float32, locks gestureLocks) (rx Range, ry Range, okX bool, okY bool) {
	if abs32(cur[0]-start[0]) <= 5 || abs32(cur[1]-start[1]) <= 5 {
		return
	}
	if !locks.noZoomX {
		x0, x1 := pr.plotX(start[0]), pr.plotX(cur[0])
		rx, okX = Range{math.Min(x0, x1), math.Max(x0, x1)}, true
	}
	if !locks.noZoomY {
		y0, y1 := pr.plotY(start[1]), pr.plotY(cur[1])
		ry, okY = Range{math.Min(y0, y1), math.Max(y0, y1)}, true
	}
	return
}

// SelectE names the axes a plain drag selects on under [Plot.DragSelects].
type SelectE uint8

const (
	// SelectNone leaves the plain drag to pan, the default.
	SelectNone SelectE = iota
	// SelectX selects a range of x: a band across the plot's full height.
	SelectX
	// SelectY selects a range of y: a band across the plot's full width.
	SelectY
	// SelectXY selects a rectangle, a range on each axis.
	SelectXY
)

func (inst SelectE) onX() bool { return inst == SelectX || inst == SelectXY }
func (inst SelectE) onY() bool { return inst == SelectY || inst == SelectXY }

// DragSelects makes a plain drag select a range instead of panning, for the
// frame: the selection is drawn while the drag lasts and reported on release
// by [Plot.RangeGesture] as GestureSelect, and the view does not move. What
// selecting means is the caller's — a dashboard sets its time range from it,
// as Grafana does on a plain drag. Shift+drag still box-zooms and a
// double-click still fits; there is no pan. A house extension standing in
// for upstream's query rectangle, which upstream later replaced with
// DragRect: the selection here is not kept after the release, and cannot be
// moved or resized. The axis locks (NoPan, NoZoom) do not apply, since the
// range does not change. Callable any time before End; a drag takes the mode
// in force on the frame it starts.
func (p *Plot) DragSelects(axes SelectE) *Plot {
	if axes > SelectXY {
		axes = SelectNone
	}
	p.dragSelects = axes
	return p
}

// selectRanges is the range a released selection covers on each selected
// axis, through last frame's transform, with the drag held to the plot area:
// none at all when a selected side is 5 px or under (a click, not a drag).
func selectRanges(pr transform, mode SelectE, start [2]float32, cur [2]float32) (rx Range, ry Range, okX bool, okY bool) {
	x0, x1 := clamp32(start[0], float32(pr.px0), float32(pr.px0+pr.plotW)), clamp32(cur[0], float32(pr.px0), float32(pr.px0+pr.plotW))
	y0, y1 := clamp32(start[1], float32(pr.py0), float32(pr.py0+pr.plotH)), clamp32(cur[1], float32(pr.py0), float32(pr.py0+pr.plotH))
	if (mode.onX() && abs32(x1-x0) <= 5) || (mode.onY() && abs32(y1-y0) <= 5) {
		return
	}
	if mode.onX() {
		a, b := pr.plotX(x0), pr.plotX(x1)
		rx, okX = Range{math.Min(a, b), math.Max(a, b)}, true
	}
	if mode.onY() {
		a, b := pr.plotY(y0), pr.plotY(y1)
		ry, okY = Range{math.Min(a, b), math.Max(a, b)}, true
	}
	return
}

// RangeGesture reports a range gesture the person finished on axis — a box
// zoom, a run of wheel steps, a pan, a double-click fit, or a selection
// under DragSelects — with the range it asked for. A house extension, for an app whose data depends on the
// range: it learns that the person chose one, as distinct from a range that
// moved because of an auto-fit, a link to another plot or the app's own
// SetupAxisLimits, none of which report here.
//
// Each gesture is reported once, by the handle of the frame after the one
// it settled in, from Begin to End — one frame later than the range itself,
// like every register read. A box zoom moves both axes and so reports on
// each, a selection on the axes it selects; a NoZoom axis reports no box or wheel zoom, a NoPan axis no pan. The
// wheel reports after it has rested for 300 ms, with the range of its last
// step.
//
// The range is the one the gesture asked for, held to the constraints, not
// the one the axis settled on: a caller that pins the axis every frame
// (CondAlways) and sets its own range from the gesture reads the person's
// choice here. Pinned, the axis cannot move far enough for a pan or a run of
// wheel steps to mean much — each step starts again from the pin — so such a
// caller reads box zooms and fits.
func (p *Plot) RangeGesture(axis AxisE) (vmin float64, vmax float64, kind GestureE, ok bool) {
	if p == nil || axis > AxisY1 {
		return 0, 0, GestureNone, false
	}
	g := p.gesture[axis]
	if g.kind == GestureNone {
		return 0, 0, GestureNone, false
	}
	return g.rng.Min, g.rng.Max, g.kind, true
}
