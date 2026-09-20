package timescrubber

import (
	"math"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/math/numerical/timeticks"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/axisruler"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// StepStateE says what a step's data is doing, which is what decides whether
// moving there costs a wait.
type StepStateE uint8

const (
	// StepStateIdle is a step nobody has asked for.
	StepStateIdle StepStateE = iota
	// StepStateHeld is a step whose data is there.
	StepStateHeld
	// StepStateLoading is a step being fetched.
	StepStateLoading
	// StepStateMissing is a step the source lists and cannot serve.
	StepStateMissing
)

// Step is one step of the series, as the caller knows it this frame.
type Step struct {
	At    time.Time
	State StepStateE
	// Value and Peak are the step's bar and the cap above it — a mean and a
	// maximum, say. NaN draws neither.
	Value, Peak float32
}

// Mark is an instant worth naming on the strip: a model run's start, the end
// of the analysis.
type Mark struct {
	At    time.Time
	Label string
}

// Options tunes a [Scrubber]; the zero value is usable.
type Options struct {
	// ScopeKey scopes the widget's ids; two scrubbers on one id stack need
	// different keys. Empty takes "timescrubber".
	ScopeKey string
	// Height is the strip's height in logical pixels. Zero takes 104.
	Height float32
	// Location is the zone times are shown in. nil takes UTC.
	Location *time.Location
	// ValueName and ValueUnit word the hover readout and the key: "speed",
	// "m/s". PeakName words the cap; empty takes "max".
	ValueName, ValueUnit, PeakName string
	// NoSnap leaves the playhead where a drag let go of it, between two
	// steps; by default it settles on the nearest step.
	NoSnap bool
	// NoKeyboard leaves the keys to the enclosing pane.
	NoKeyboard bool
	// ByIndex spreads the steps evenly whatever their times, for a series
	// whose dense block is squeezed by its sparse one. Steps whose times do
	// not strictly increase are laid out this way regardless.
	ByIndex bool
	// Compact draws one line: play and pause, the state lane with the
	// playhead and the range on it, and the time.
	Compact bool
	// Now is the wall clock the now line, the tint before it and the offset
	// from now are read from; nil takes time.Now. It does NOT pace playback,
	// which always runs on elapsed real time — a capture that fixes this to
	// get the same picture every run still plays.
	Now func() time.Time
}

// Events is what a frame did to the position.
type Events struct {
	// Moved says a person moved the playhead this frame.
	Moved bool
	// Dragging says the playhead or the range is held.
	Dragging bool
	// Settled says the position came to rest this frame — a release, a
	// click, a key, a button, and each whole step playback reached — and Step
	// is the step it rests nearest. A consumer whose work per position is a
	// query acts on this and needs no debounce of its own (ADR-0251 §SD3).
	Settled bool
	Step    int
}

const (
	defaultHeight = 104
	compactHeight = 24
	padX          = 16
	bandH         = 14
	// labelRowH is kept free under the band for the playhead's time, the
	// marks and the calendar context, so none is drawn across a bar.
	labelRowH = 15
	rulerH    = 19
	notchH    = 4
	stemW     = 2
	// flagReach and contextReach are how far the playhead's flag and the
	// context label extend, generously: both are short texts at one size.
	flagReach     = 116
	contextReach  = 96
	minBarW       = 2
	maxBarW       = 22
	edgeGrab      = 6
	canvasKey     = "timescrubber-canvas"
	areaKey       = "timescrubber-area"
	keysKey       = "timescrubber-keys"
	tickSpacingPx = 96
	probeSaltSeed = uint64(0x51c7_0be2_94ad_3f16)
	// loadingAfter is how old a load is before it is drawn as one, so a
	// quick load does not flicker.
	loadingAfter = 250 * time.Millisecond
	// waitingAfter is the same for the readout's words, in seconds.
	waitingAfter = 0.3
	// compactReserve is the room a compact strip leaves its button and time.
	compactReserve = 300
)

var keyMask = keycodes.MaskOf(keycodes.Space, keycodes.ArrowLeft, keycodes.ArrowRight,
	keycodes.Home, keycodes.End, keycodes.PageUp, keycodes.PageDown, keycodes.Delete)

// rates are the playback speeds offered, in steps per second.
var rates = []float64{0.25, 0.5, 1, 2, 4}

type dragE uint8

const (
	dragNone dragE = iota
	dragPlayhead
	dragRangeNew
	dragRangeLo
	dragRangeHi
	dragRangeBody
	dragCancelled // Escape was pressed; the gesture is ignored until it ends
)

// Scrubber is a time strip for a stepped series (ADR-0251): steps at their
// own instants on a calendar axis, a bar per step, each step's load state, a
// playhead to grab, a loop range, and the transport that plays it.
//
// It owns the [Transport]; the caller reads Transport.Pos after Render and
// writes it to move the playhead from outside. It knows nothing of what the
// steps are steps of, and it asks for nothing: hover, the snap preview and
// the drawing read what the caller handed in (ADR-0251 §SD7).
type Scrubber struct {
	Opts      Options
	Transport Transport
	// ValueColor colours a bar by its value and a cap by its peak,
	// 0xRRGGBBAA. nil takes the accent colour. Heights are scaled to the
	// largest value among the steps; what a value is in absolute terms is
	// the colour's to say and the hover readout's.
	ValueColor func(value float32) uint32
	// Marks are drawn in the label row and reached with Alt+Left/Right. The
	// caller may change them any frame.
	Marks []Mark

	ids        *c.WidgetIdStack
	keyFrameID uint64
	probeSalt  uint64
	lastFrame  time.Time
	// now is the frame clock: what a frame's elapsed time and the age of a
	// load are measured against. Opts.Now is the wall clock and is a
	// different question; a test overrides this one.
	now func() time.Time

	drag                 dragE
	rangeFrom, grabStep  int
	savedOn              bool
	savedLo, savedHi     int
	prevTick             timeticks.TimeStep
	hoverStep            int
	hoverValid, hoverTop bool

	prevMS       []int64
	shownMS      float64
	loadingSince map[int]time.Time

	timeText, timeBefore string
	timeFocused          bool

	xs0, ys0, xs1, ys1 []float32
	cols               color.Colors
	ticks              []axisruler.Tick
	columns            []column
}

// New makes a scrubber. ids scopes every id it derives.
func New(ids *c.WidgetIdStack, opts Options) (inst *Scrubber) {
	return &Scrubber{Opts: opts, ids: ids, now: time.Now, loadingSince: make(map[int]time.Time)}
}

func (inst *Scrubber) scopeKey() string {
	if inst.Opts.ScopeKey != "" {
		return inst.Opts.ScopeKey
	}
	return "timescrubber"
}

// wall is the clock the now line and the offsets from now are read against.
func (inst *Scrubber) wall() time.Time {
	if inst.Opts.Now != nil {
		return inst.Opts.Now()
	}
	return inst.now()
}

func (inst *Scrubber) location() *time.Location {
	if inst.Opts.Location != nil {
		return inst.Opts.Location
	}
	return time.UTC
}

// RenderFillWidth draws the transport row and the strip across the width of
// the enclosing pane, read back through a size probe one frame behind;
// fallbackW serves until the probe reports.
func (inst *Scrubber) RenderFillWidth(steps []Step, fallbackW float32) (ev Events) {
	if inst.probeSalt == 0 {
		inst.probeSalt = inst.ids.PrepareHighEntropy(probeSaltSeed).Derive()
	}
	w, _, ok := c.CapturePaneSize(c.ProbeSeq(inst.scopeKey(), "pane") ^ inst.probeSalt)
	if !ok || w < 1 {
		w = fallbackW
	}
	return inst.Render(w, steps)
}

// Render draws the transport row and, below it, the strip w pixels wide, and
// advances playback by the time since the previous call.
func (inst *Scrubber) Render(w float32, steps []Step) (ev Events) {
	now := inst.now()
	dt := 0.0
	if !inst.lastFrame.IsZero() {
		dt = now.Sub(inst.lastFrame).Seconds()
	}
	inst.lastFrame = now
	n := len(steps)
	t := &inst.Transport
	followed := inst.followSteps(steps)
	t.Pos = clampPos(t.Pos, 0, max(n-1, 0))
	inst.trackLoading(steps, now)

	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey())) {
		if inst.Opts.Compact {
			ev = inst.renderCompact(w, steps)
		} else {
			moved := inst.renderTransportRow(steps)
			ev = inst.keyed(func() Events { return inst.strip(w, inst.height(), steps, false) })
			typed := inst.renderReadoutRow(steps)
			ev.Moved = ev.Moved || moved || typed
		}
	}

	t.Advance(dt, n, func(step int) bool {
		s := steps[step].State
		return s == StepStateHeld || s == StepStateMissing
	})
	ev.Moved = ev.Moved || followed
	ev.Settled = t.TakeSettled() || followed
	ev.Step = int(math.Round(t.Pos))
	if n > 0 {
		inst.shownMS = newAxis(steps, 0, 1, false).posToMS(t.Pos)
	}
	if t.Playing || inst.drag != dragNone || len(inst.loadingSince) > 0 {
		c.RequestRepaintAfter(1.0 / 30)
	}
	return
}

func (inst *Scrubber) height() float32 {
	if h := inst.Opts.Height; h > 0 {
		return h
	}
	return defaultHeight
}

// keyed runs body under the frame that captures the strip's keys.
func (inst *Scrubber) keyed(body func() Events) (ev Events) {
	if inst.Opts.NoKeyboard {
		inst.keyFrameID = 0
		return body()
	}
	mask := keyMask
	if inst.drag != dragNone {
		// Escape is the enclosing window's except while there is a gesture
		// of ours to cancel.
		mask |= keycodes.MaskOf(keycodes.Escape)
	}
	kf := c.Frame(inst.ids.PrepareStr(keysKey)).CaptureKeys(uint64(mask))
	inst.keyFrameID = kf.Id()
	for range kf.KeepIter() {
		ev = body()
	}
	return
}

// followSteps keeps the instant when the steps change: the playhead goes to
// the step nearest the instant it was showing, and the range's ends likewise.
// A rebuilt series keeps the time and not the index (ADR-0251 §SD2).
func (inst *Scrubber) followSteps(steps []Step) (moved bool) {
	axis := newAxis(steps, 0, 1, false)
	if sameSteps(inst.prevMS, axis.ms) {
		return
	}
	prev := timeAxis{ms: append([]int64(nil), inst.prevMS...)}
	inst.prevMS = append(inst.prevMS[:0], axis.ms...)
	clear(inst.loadingSince)
	if !prev.ordered() || !axis.ordered() {
		return
	}
	t := &inst.Transport
	pos := axis.msToPos(inst.shownMS)
	if !t.Playing {
		pos = math.Round(pos)
	}
	moved = pos != t.Pos
	t.Pos = pos
	if t.RangeOn {
		lo, hi := t.Bounds(len(prev.ms))
		a := int(math.Round(axis.msToPos(float64(prev.ms[lo]))))
		b := int(math.Round(axis.msToPos(float64(prev.ms[hi]))))
		t.SetRange(a, b)
	}
	return
}

// trackLoading notes since when each loading step has been loading.
func (inst *Scrubber) trackLoading(steps []Step, now time.Time) {
	for i := range inst.loadingSince {
		if i >= len(steps) || steps[i].State != StepStateLoading {
			delete(inst.loadingSince, i)
		}
	}
	for i := range steps {
		if steps[i].State == StepStateLoading {
			if _, ok := inst.loadingSince[i]; !ok {
				inst.loadingSince[i] = now
			}
		}
	}
}

// shownState is a step's state as the strip draws it: a load too young to
// flicker reads as idle.
func (inst *Scrubber) shownState(steps []Step, i int) StepStateE {
	s := steps[i].State
	if s == StepStateLoading && inst.now().Sub(inst.loadingSince[i]) < loadingAfter {
		return StepStateIdle
	}
	return s
}

func (inst *Scrubber) focusKeys() {
	if inst.keyFrameID != 0 {
		c.RequestFocus(inst.keyFrameID)
	}
}

// geometry is where the strip's lanes are.
type geometry struct {
	w, h         float32
	band, labels float32 // heights; zero in the compact form
	baseY        float32
	compact      bool
}

func newGeometry(w, h float32, compact bool) (g geometry) {
	g.w, g.h, g.compact = w, h, compact
	if compact {
		g.baseY = h - 5
		return
	}
	g.band, g.labels, g.baseY = bandH, labelRowH, h-rulerH
	return
}

// strip is the canvas: input first, against last frame's geometry, then the
// drawing.
func (inst *Scrubber) strip(w, h float32, steps []Step, compact bool) (ev Events) {
	n := len(steps)
	if w < 2*padX+8 || n == 0 {
		return
	}
	sm := c.CurrentApplicationState.StateManager
	t := &inst.Transport
	g := newGeometry(w, h, compact)
	axis := newAxis(steps, padX, w-padX, inst.Opts.ByIndex)
	vis := newVisuals()

	canvasH := widgethandle.Make(inst.ids.PrepareStr(canvasKey).Derive())
	areaH := widgethandle.Make(inst.ids.PrepareStr(areaKey).Derive())
	cur, live := sm.GetCanvasCursor(canvasH)
	flags := sm.GetResponse(areaH)
	areaCur, areaOk := sm.GetCanvasCursor(areaH)
	ptr := sm.GetPointer()

	inst.hoverValid = false
	if live {
		posX, posOk := cur.PosX, !isNaN32(cur.PosX)
		if ptr.Valid && !isNaN32(ptr.X) && !isNaN32(cur.OriginX) {
			// A drag goes on outside the canvas; the pointer still knows.
			posX, posOk = ptr.X-cur.OriginX, true
		}
		// The press origin decides the gesture, not where the pointer had got
		// to by the time the drag or the click was recognised.
		ox, oy := posX, cur.PosY
		if areaOk && !isNaN32(areaCur.PosX) {
			ox, oy = areaCur.PosX, areaCur.PosY
		}
		onBand := g.band > 0 && n > 1 && oy < g.band+2
		if flags.HasDragStarted() && posOk {
			inst.beginDrag(axis, ox, onBand, n)
			inst.focusKeys()
		}
		if flags.HasDragged() || flags.HasDragStopped() {
			if posOk && inst.continueDrag(axis, posX, n) {
				ev.Moved = true
			}
		}
		if flags.HasDragStopped() {
			if inst.drag == dragPlayhead {
				pos := t.Pos
				if !inst.Opts.NoSnap {
					pos = math.Round(pos)
				}
				t.Seek(pos, n) // the release is where the position settles
				ev.Moved = true
			}
			inst.drag = dragNone
		}
		switch {
		case flags.HasDoubleClicked() && onBand:
			t.ClearRange()
			inst.focusKeys()
		case flags.HasPrimaryClicked() && posOk && !onBand:
			// The band is the range's alone: a click there does not seek, so
			// the double click that clears the range moves nothing.
			t.Seek(math.Round(axis.xToPos(posX)), n)
			ev.Moved = true
			inst.focusKeys()
		case flags.HasPrimaryClicked():
			inst.focusKeys()
		}
		if inst.drag == dragNone && flags.HasHovered() && posOk && !isNaN32(cur.PosY) {
			inst.hoverStep = int(math.Round(axis.xToPos(posX)))
			inst.hoverValid = true
			inst.hoverTop = g.band > 0 && cur.PosY < g.band+2
		}
	}
	if inst.handleKeys(sm, axis, steps) {
		ev.Moved = true
	}
	ev.Dragging = inst.drag != dragNone && inst.drag != dragCancelled

	inst.paint(axis, steps, g, vis)

	c.PaintSenseRegion(inst.ids.PrepareStr(areaKey), 0, 0, w, h).Send()
	c.PaintCanvas(inst.ids.PrepareStr(canvasKey), w, h).
		Background(vis.background).
		Sense(true, false, true).
		Send()
	return
}

// beginDrag decides what a drag is from where it was pressed: on the band an
// edge of the range, its body, or a new range; anywhere else the playhead.
func (inst *Scrubber) beginDrag(axis timeAxis, ox float32, onBand bool, n int) {
	t := &inst.Transport
	inst.savedOn, inst.savedLo, inst.savedHi = t.RangeOn, t.RangeLo, t.RangeHi
	if !onBand {
		inst.drag = dragPlayhead
		return
	}
	inst.drag = dragRangeNew
	inst.rangeFrom = int(math.Round(axis.xToPos(ox)))
	if !t.RangeOn {
		return
	}
	lo, hi := t.Bounds(n)
	xa, xb := axis.posToX(float64(lo)), axis.posToX(float64(hi))
	nearLo, nearHi := absf(ox-xa) <= edgeGrab, absf(ox-xb) <= edgeGrab
	switch {
	case nearLo && nearHi:
		// The edges are closer than a grip is wide: the side the pointer is
		// on decides which one moves.
		inst.drag = dragRangeHi
		if ox < (xa+xb)/2 {
			inst.drag = dragRangeLo
		}
	case nearLo:
		inst.drag = dragRangeLo
	case nearHi:
		inst.drag = dragRangeHi
	case ox > xa && ox < xb:
		inst.drag, inst.grabStep = dragRangeBody, inst.rangeFrom
	}
}

// continueDrag applies the pointer to the gesture in hand. The edge that was
// grabbed stays the one being dragged, and the body slides from where it was
// when it was grabbed, not by a sum of steps.
func (inst *Scrubber) continueDrag(axis timeAxis, posX float32, n int) (moved bool) {
	t := &inst.Transport
	at := int(math.Round(axis.xToPos(posX)))
	switch inst.drag {
	case dragPlayhead:
		t.scrub(axis.xToPos(posX), n)
		moved = true
	case dragRangeNew:
		if at == inst.rangeFrom {
			// Shorter than a step: neither a range nor the end of one.
			inst.restoreRange()
		} else {
			t.SetRange(inst.rangeFrom, at)
		}
	case dragRangeLo:
		t.MoveRangeEdge(false, at, n)
	case dragRangeHi:
		t.MoveRangeEdge(true, at, n)
	case dragRangeBody:
		inst.restoreRange()
		t.SlideRange(at-inst.grabStep, n)
	}
	return
}

func (inst *Scrubber) restoreRange() {
	t := &inst.Transport
	t.RangeOn, t.RangeLo, t.RangeHi = inst.savedOn, inst.savedLo, inst.savedHi
}

// stride is the longer step of Shift and of PageUp and PageDown: a tenth of
// the series, within reason.
func stride(steps int) int { return min(max(int(math.Round(float64(steps)/10)), 2), 24) }

func (inst *Scrubber) handleKeys(sm *c.StateManager, axis timeAxis, steps []Step) (moved bool) {
	if inst.keyFrameID == 0 {
		return
	}
	n := len(steps)
	t := &inst.Transport
	for _, k := range sm.GetCapturedKeys(widgethandle.Make(inst.keyFrameID)) {
		at := int(math.Round(t.Pos))
		switch k.Code {
		case keycodes.Space:
			t.Toggle(n)
		case keycodes.ArrowLeft, keycodes.ArrowRight:
			dir := 1
			if k.Code == keycodes.ArrowLeft {
				dir = -1
			}
			switch {
			case k.Ctrl() || k.Command():
				t.Seek(float64(inst.dayJump(axis, dir)), n)
			case k.Alt():
				t.Seek(float64(inst.markJump(axis, dir)), n)
			case k.Shift():
				t.StepBy(dir*stride(n), n)
			default:
				t.StepBy(dir, n)
			}
			moved = true
		case keycodes.PageUp:
			t.StepBy(stride(n), n)
			moved = true
		case keycodes.PageDown:
			t.StepBy(-stride(n), n)
			moved = true
		case keycodes.Home:
			if k.Shift() {
				t.SetIn(at, n)
			} else {
				t.First(n)
				moved = true
			}
		case keycodes.End:
			if k.Shift() {
				t.SetOut(at, n)
			} else {
				t.Last(n)
				moved = true
			}
		case keycodes.Delete:
			t.ClearRange()
		case keycodes.Escape:
			if inst.drag != dragNone && inst.drag != dragPlayhead {
				inst.restoreRange()
				inst.drag = dragCancelled
			}
		}
	}
	return
}

// dayJump is the first step of the next day, or of this day and then of the
// one before.
func (inst *Scrubber) dayJump(axis timeAxis, dir int) int {
	at := int(math.Round(inst.Transport.Pos))
	if !axis.ordered() {
		return at + dir*stride(len(axis.ms))
	}
	loc := inst.location()
	y, m, d := time.UnixMilli(axis.ms[at]).In(loc).Date()
	if dir > 0 {
		return axis.firstAtOrAfter(time.Date(y, m, d+1, 0, 0, 0, 0, loc).UnixMilli())
	}
	first := axis.firstAtOrAfter(time.Date(y, m, d, 0, 0, 0, 0, loc).UnixMilli())
	if first < at {
		return first
	}
	return axis.firstAtOrAfter(time.Date(y, m, d-1, 0, 0, 0, 0, loc).UnixMilli())
}

// markJump is the step nearest the next mark that is not the step in hand.
func (inst *Scrubber) markJump(axis timeAxis, dir int) int {
	at := int(math.Round(inst.Transport.Pos))
	if !axis.ordered() {
		return at
	}
	best, found := at, false
	for i := range inst.Marks {
		step := int(math.Round(axis.msToPos(float64(inst.Marks[i].At.UnixMilli()))))
		if (dir > 0 && step > at && (!found || step < best)) || (dir < 0 && step < at && (!found || step > best)) {
			best, found = step, true
		}
	}
	return best
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func isNaN32(f float32) bool { return f != f }
