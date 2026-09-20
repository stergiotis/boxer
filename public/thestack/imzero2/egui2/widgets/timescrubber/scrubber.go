package timescrubber

import (
	"fmt"
	"math"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
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

// Options tunes a [Scrubber]; the zero value is usable.
type Options struct {
	// ScopeKey scopes the widget's ids; two scrubbers on one id stack need
	// different keys. Empty takes "timescrubber".
	ScopeKey string
	// Height is the strip's height in logical pixels. Zero takes 96.
	Height float32
	// Location is the zone times are shown in. nil takes UTC.
	Location *time.Location
	// ValueName and ValueUnit word the hover readout: "speed", "m/s".
	ValueName, ValueUnit string
	// NoSnap leaves the playhead where a drag let go of it, between two
	// steps; by default it settles on the nearest step.
	NoSnap bool
	// NoKeyboard leaves the keys to the enclosing pane.
	NoKeyboard bool
}

// Events is what a frame's input did.
type Events struct {
	// Moved says a person moved the playhead this frame.
	Moved bool
	// Dragging says the playhead or a range handle is held.
	Dragging bool
}

const (
	defaultHeight = 96
	padX          = 16
	bandH         = 11
	// labelRowH is kept free under the band for the playhead's time and the
	// calendar context, so neither is drawn across a bar.
	labelRowH = 15
	rulerH    = 19
	notchH    = 4
	stemW     = 2
	// flagReach and contextReach are how far the playhead's flag and the
	// context label extend, generously: both are short texts at one size.
	flagReach     = 84
	contextReach  = 96
	minBarW       = 2
	maxBarW       = 22
	canvasKey     = "timescrubber-canvas"
	areaKey       = "timescrubber-area"
	keysKey       = "timescrubber-keys"
	shiftStep     = 6
	tickSpacingPx = 96
	probeSaltSeed = uint64(0x51c7_0be2_94ad_3f16)
)

var keyMask = keycodes.MaskOf(keycodes.Space, keycodes.ArrowLeft, keycodes.ArrowRight, keycodes.Home, keycodes.End)

// rates are the playback speeds offered, in steps per second.
var rates = []float64{0.25, 0.5, 1, 2, 4}

type dragE uint8

const (
	dragNone dragE = iota
	dragPlayhead
	dragRange
)

// Scrubber is a time strip for a stepped series (ADR-0251): steps at their
// own instants on a calendar axis, a bar per step, each step's load state, a
// playhead to grab, a loop range, and the transport that plays it.
//
// It owns the [Transport]; the caller reads Transport.Pos after Render and
// writes it to move the playhead from outside. It knows nothing of what the
// steps are steps of.
type Scrubber struct {
	Opts      Options
	Transport Transport
	// ValueColor colours a bar by its value and a stem by its peak,
	// 0xRRGGBBAA. nil takes the accent colour. Heights are scaled to the
	// largest value among the steps; what a value is in absolute terms is
	// the colour's to say and the hover readout's.
	ValueColor func(value float32) uint32

	ids        *c.WidgetIdStack
	keyFrameID uint64
	probeSalt  uint64
	lastFrame  time.Time
	now        func() time.Time

	drag       dragE
	rangeFrom  int
	prevTick   timeticks.TimeStep
	hoverStep  int
	hoverValid bool

	xs0, ys0, xs1, ys1 []float32
	cols               color.Colors
	ticks              []axisruler.Tick
}

// New makes a scrubber. ids scopes every id it derives.
func New(ids *c.WidgetIdStack, opts Options) (inst *Scrubber) {
	return &Scrubber{Opts: opts, ids: ids, now: time.Now}
}

func (inst *Scrubber) scopeKey() string {
	if inst.Opts.ScopeKey != "" {
		return inst.Opts.ScopeKey
	}
	return "timescrubber"
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
	inst.Transport.Pos = clampPos(inst.Transport.Pos, 0, max(n-1, 0))

	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey())) {
		moved := inst.renderTransportRow(steps)
		if inst.Opts.NoKeyboard {
			inst.keyFrameID = 0
			ev = inst.strip(w, steps)
		} else {
			kf := c.Frame(inst.ids.PrepareStr(keysKey)).CaptureKeys(uint64(keyMask))
			inst.keyFrameID = kf.Id()
			for range kf.KeepIter() {
				ev = inst.strip(w, steps)
			}
		}
		ev.Moved = ev.Moved || moved
	}

	inst.Transport.Advance(dt, n, func(step int) bool {
		s := steps[step].State
		return s == StepStateHeld || s == StepStateMissing
	})
	if inst.Transport.Playing || inst.drag != dragNone {
		c.RequestRepaintAfter(1.0 / 30)
	}
	return
}

func (inst *Scrubber) focusKeys() {
	if inst.keyFrameID != 0 {
		c.RequestFocus(inst.keyFrameID)
	}
}

// renderTransportRow draws the buttons. Every label carries a word beside
// its glyph: a glyph alone has no name for a screen reader or a scripted
// driver to find it by.
func (inst *Scrubber) renderTransportRow(steps []Step) (moved bool) {
	t := &inst.Transport
	n := len(steps)
	button := func(key, label string) bool {
		return c.Button(inst.ids.PrepareStr(key), c.Atoms().Text(label).Keep()).SendResp().HasPrimaryClicked()
	}
	for range c.Horizontal().KeepIter() {
		if button("first", icons.PhSkipBack+" First") {
			t.First(n)
			moved = true
		}
		if button("prev", icons.PhCaretLeft+" Prev") {
			t.StepBy(-1, n)
			moved = true
		}
		play := icons.PhPlay + " Play"
		if t.Playing {
			play = icons.PhPause + " Pause"
		}
		if button("play", play) {
			t.Toggle(n)
		}
		if button("next", "Next "+icons.PhCaretRight) {
			t.StepBy(1, n)
			moved = true
		}
		if button("last", "Last "+icons.PhSkipForward) {
			t.Last(n)
			moved = true
		}
		c.Separator().Vertical().Send()

		rate := t.Rate
		if !(rate > 0) {
			rate = defaultRate
		}
		for range c.ComboBox(inst.ids.PrepareStr("rate"),
			c.WidgetText().Text("").Keep(),
			c.WidgetText().Text(formatRate(rate)).Keep()).KeepIter() {
			for i, r := range rates {
				if c.Button(inst.ids.PrepareSeq(uint64(0x7200+i)), c.Atoms().Text(formatRate(r)).Keep()).
					Frame(false).Selected(r == rate).SendResp().HasPrimaryClicked() {
					t.Rate = r
				}
			}
		}
		if button("mode", modeIcon(t.Mode)+" "+t.Mode.label()) {
			t.Mode = AllModes[(int(t.Mode)+1)%len(AllModes)]
		}
		if n > 1 {
			if at := inst.now(); !at.Before(steps[0].At) && !at.After(steps[n-1].At) {
				if button("now", icons.PhClock+" Now") {
					axis := newTimeAxis(steps, 0, 1)
					t.Seek(math.Round(axis.msToPos(float64(at.UnixMilli()))), n)
					moved = true
				}
			}
		}
		if t.RangeOn {
			if button("clear-range", "Clear range") {
				t.ClearRange()
			}
		}
		c.Separator().Vertical().Send()
		c.Label(inst.readout(steps)).Send()
	}
	return
}

func modeIcon(m ModeE) string {
	switch m {
	case ModeBounce:
		return icons.PhArrowsLeftRight
	case ModeOnce:
		return icons.PhArrowLineRight
	}
	return icons.PhRepeat
}

func formatRate(r float64) string {
	if r >= 1 {
		return fmt.Sprintf("%g steps/s", r)
	}
	return fmt.Sprintf("1 step / %g s", 1/r)
}

// readout is the display time in words, and what playback is waiting for.
func (inst *Scrubber) readout(steps []Step) string {
	n := len(steps)
	if n == 0 {
		return "no steps"
	}
	t := &inst.Transport
	axis := newTimeAxis(steps, 0, 1)
	s := axis.timeAt(t.Pos, inst.location()).Format("Mon 2006-01-02 15:04 MST")
	k := int(math.Round(t.Pos))
	if math.Abs(t.Pos-float64(k)) < entrance {
		s += fmt.Sprintf(" · step %d of %d", k+1, n)
	} else {
		s += fmt.Sprintf(" · between steps %d and %d", int(t.Pos)+1, int(t.Pos)+2)
	}
	if t.RangeOn {
		lo, hi := t.Bounds(n)
		s += fmt.Sprintf(" · range %d–%d", lo+1, hi+1)
	}
	if t.Buffering {
		s += " · waiting for data"
	}
	return s
}

// strip is the canvas: input first, against last frame's geometry, then the
// drawing.
func (inst *Scrubber) strip(w float32, steps []Step) (ev Events) {
	h := inst.Opts.Height
	if !(h > 0) {
		h = defaultHeight
	}
	n := len(steps)
	if w < 2*padX+8 || n == 0 {
		return
	}
	sm := c.CurrentApplicationState.StateManager
	t := &inst.Transport
	axis := newTimeAxis(steps, padX, w-padX)
	baseY := h - rulerH
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
		if flags.HasDragStarted() && posOk {
			// The press origin decides the gesture, not where the pointer
			// had got to by the time the drag was recognised.
			ox, oy := posX, cur.PosY
			if areaOk && !isNaN32(areaCur.PosX) {
				ox, oy = areaCur.PosX, areaCur.PosY
			}
			if n > 1 && oy < bandH+2 {
				inst.drag = dragRange
				inst.rangeFrom = int(math.Round(axis.xToPos(ox)))
			} else {
				inst.drag = dragPlayhead
			}
			inst.focusKeys()
		}
		if inst.drag != dragNone && (flags.HasDragged() || flags.HasDragStopped()) && posOk {
			switch inst.drag {
			case dragPlayhead:
				t.Seek(axis.xToPos(posX), n)
				ev.Moved = true
			case dragRange:
				t.SetRange(inst.rangeFrom, int(math.Round(axis.xToPos(posX))))
			}
		}
		if flags.HasDragStopped() {
			if inst.drag == dragPlayhead && !inst.Opts.NoSnap {
				t.Seek(math.Round(t.Pos), n)
				ev.Moved = true
			}
			inst.drag = dragNone
		}
		switch {
		case flags.HasDoubleClicked():
			t.ClearRange()
			inst.focusKeys()
		case flags.HasPrimaryClicked() && posOk:
			t.Seek(math.Round(axis.xToPos(posX)), n)
			ev.Moved = true
			inst.focusKeys()
		}
		if inst.drag == dragNone && flags.HasHovered() && posOk && !isNaN32(cur.PosY) {
			inst.hoverStep = int(math.Round(axis.xToPos(posX)))
			inst.hoverValid = true
		}
	}
	if inst.handleKeys(sm, n) {
		ev.Moved = true
	}
	ev.Dragging = inst.drag != dragNone

	// ---- draw ----
	c.PaintClipPush(0, 0, w, h).Send()
	inst.paintRange(axis, steps, w, baseY, vis)
	inst.paintBars(axis, steps, baseY, vis)
	inst.paintShade(axis, steps, w, baseY, vis)
	inst.paintAxis(axis, steps, w, baseY, vis)
	inst.paintNotches(axis, steps, baseY, vis)
	if inst.hoverValid {
		inst.paintHover(axis, steps, w, baseY, vis)
	}
	inst.paintPlayhead(axis, w, baseY, vis)
	c.PaintClipPop().Send()

	c.PaintSenseRegion(inst.ids.PrepareStr(areaKey), 0, 0, w, h).Send()
	c.PaintCanvas(inst.ids.PrepareStr(canvasKey), w, h).
		Background(vis.background).
		Sense(true, false, true).
		Send()
	return
}

func (inst *Scrubber) handleKeys(sm *c.StateManager, steps int) (moved bool) {
	if inst.keyFrameID == 0 {
		return
	}
	t := &inst.Transport
	for _, k := range sm.GetCapturedKeys(widgethandle.Make(inst.keyFrameID)) {
		switch k.Code {
		case keycodes.Space:
			t.Toggle(steps)
		case keycodes.ArrowLeft, keycodes.ArrowRight:
			by := 1
			if k.Shift() {
				by = shiftStep
			}
			if k.Code == keycodes.ArrowLeft {
				by = -by
			}
			t.StepBy(by, steps)
			moved = true
		case keycodes.Home:
			t.First(steps)
			moved = true
		case keycodes.End:
			t.Last(steps)
			moved = true
		}
	}
	return
}

type visuals struct {
	background, band, rangeFill, rangeEdge, shade color.Color
	bar, peak, playhead, playheadText, hover      color.Color
	hoverText, context, now                       color.Color
	held, loading, missing, idle                  color.Color
}

func newVisuals() visuals {
	hex := func(t styletokens.RGBA8) color.Color { return color.Hex(t.AsHex()) }
	return visuals{
		background:   hex(styletokens.NeutralBgPanel),
		band:         hex(styletokens.NeutralBorderFaint),
		rangeFill:    withAlpha(hex(styletokens.SuccessDefault), 0x70),
		rangeEdge:    hex(styletokens.SuccessDefault),
		shade:        withAlpha(hex(styletokens.NeutralBgPanel), 0xb8),
		bar:          hex(styletokens.AccentDefault),
		peak:         hex(styletokens.NeutralTextSecondary),
		playhead:     hex(styletokens.WarningDefault),
		playheadText: hex(styletokens.NeutralTextPrimary),
		hover:        withAlpha(hex(styletokens.NeutralTextSecondary), 0x90),
		hoverText:    hex(styletokens.NeutralTextSecondary),
		context:      hex(styletokens.NeutralTextSecondary),
		now:          hex(styletokens.InfoDefault),
		held:         hex(styletokens.AccentStrong),
		loading:      hex(styletokens.WarningStrong),
		missing:      hex(styletokens.ErrorDefault),
		idle:         hex(styletokens.NeutralBorderFaint),
	}
}

func withAlpha(col color.Color, alpha uint8) color.Color {
	return color.Hex(col.Literal()&^0xff | uint32(alpha))
}

func isNaN32(f float32) bool { return f != f }

// paintRange draws the top band, the chosen range on it, and a shade over
// what playback leaves out.
func (inst *Scrubber) paintRange(axis timeAxis, steps []Step, w, baseY float32, vis visuals) {
	c.PaintLine(axis.x0, bandH/2, axis.x1, bandH/2, vis.band, styletokens.StrokeHair).Send()
	t := &inst.Transport
	if !t.RangeOn {
		return
	}
	lo, hi := t.Bounds(len(steps))
	xa, xb := axis.posToX(float64(lo)), axis.posToX(float64(hi))
	c.PaintRectFilled(xa, 1, xb, bandH-1, 2, vis.rangeFill).Send()
	for _, x := range []float32{xa, xb} {
		c.PaintRectFilled(x-1.5, 0, x+1.5, bandH, 1, vis.rangeEdge).Send()
		c.PaintLine(x, bandH, x, baseY, vis.rangeEdge, styletokens.StrokeHair).Send()
	}
}

// paintShade dims what playback leaves out. It goes over the bars, which is
// what makes it show: under them it is the background's colour on the
// background.
func (inst *Scrubber) paintShade(axis timeAxis, steps []Step, w, baseY float32, vis visuals) {
	t := &inst.Transport
	if !t.RangeOn {
		return
	}
	lo, hi := t.Bounds(len(steps))
	half := barWidth(axis)/2 + 1
	xa, xb := axis.posToX(float64(lo))-half, axis.posToX(float64(hi))+half
	c.PaintRectFilled(0, bandH, xa, baseY-notchH-4, 0, vis.shade).Send()
	c.PaintRectFilled(xb, bandH, w, baseY-notchH-4, 0, vis.shade).Send()
}

// barWidth is as wide as the narrowest gap between two steps allows.
func barWidth(axis timeAxis) (w float32) {
	w = maxBarW
	for i := 1; i < len(axis.ms); i++ {
		gap := axis.posToX(float64(i)) - axis.posToX(float64(i-1))
		w = min(w, gap*0.72)
	}
	return max(w, minBarW)
}

// paintBars draws, per step, a bar up to its value and a stem from there up
// to its peak with a cap on it — a mean and a maximum read as one mark — all
// as one batch. Where steps are closer than a bar is wide the bars overlap and
// the strip is an envelope, which is what that many steps can show.
func (inst *Scrubber) paintBars(axis timeAxis, steps []Step, baseY float32, vis visuals) {
	top := float32(bandH + labelRowH)
	full := baseY - top
	if full < 4 {
		return
	}
	vmax := float32(0)
	for i := range steps {
		vmax = max(vmax, steps[i].Value, steps[i].Peak)
	}
	if !(vmax > 0) {
		return
	}
	vmax *= 1.04
	half := barWidth(axis) / 2
	inst.xs0, inst.ys0, inst.xs1, inst.ys1 = inst.xs0[:0], inst.ys0[:0], inst.xs1[:0], inst.ys1[:0]
	inst.cols = inst.cols[:0]
	add := func(x0, y0, x1, y1 float32, col uint32) {
		inst.xs0 = append(inst.xs0, x0)
		inst.ys0 = append(inst.ys0, y0)
		inst.xs1 = append(inst.xs1, x1)
		inst.ys1 = append(inst.ys1, y1)
		inst.cols = append(inst.cols, col)
	}
	colorOf := func(v float32) uint32 {
		if inst.ValueColor != nil {
			return inst.ValueColor(v)
		}
		return vis.bar.Literal()
	}
	for i := range steps {
		v, p := steps[i].Value, steps[i].Peak
		x := axis.posToX(float64(i))
		yv := baseY
		if v > 0 {
			yv = baseY - full*min(v/vmax, 1)
			add(x-half, yv, x+half, baseY, colorOf(v))
		}
		if p > 0 && p >= v {
			yp := baseY - full*min(p/vmax, 1)
			add(x-stemW/2, yp, x+stemW/2, yv, colorOf(p))
			add(x-half*0.6, yp-0.75, x+half*0.6, yp+0.75, colorOf(p))
		}
	}
	if len(inst.cols) > 0 {
		c.PaintRectsFilled(inst.xs0, inst.ys0, inst.xs1, inst.ys1, inst.cols).Send()
	}
}

// paintAxis draws calendar ticks under the baseline, and the coarser unit —
// the day, usually — once along the top of the bars where it changes.
func (inst *Scrubber) paintAxis(axis timeAxis, steps []Step, w, baseY float32, vis visuals) {
	st := axisruler.DefaultStyle()
	inst.ticks = inst.ticks[:0]
	n := len(steps)
	if axis.byIndex {
		every := max(int(math.Ceil(float64(n)*tickSpacingPx/float64(max(w-2*padX, 1)))), 1)
		for i := 0; i < n; i += every {
			inst.ticks = append(inst.ticks, axisruler.Tick{Pos: axis.posToX(float64(i)), Label: fmt.Sprintf("%d", i+1)})
		}
		axisruler.Paint(axisruler.SideBottom, baseY, axis.x0, axis.x1, inst.ticks, st)
		return
	}
	layout := timeticks.TimeTicks(steps[0].At, steps[n-1].At, timeticks.TimeTickOptions{
		PanelWidthPx:    int32(w - 2*padX),
		TargetSpacingPx: tickSpacingPx,
		Location:        inst.location(),
		PrevStep:        inst.prevTick,
		HysteresisFrac:  0.25,
	})
	inst.prevTick = layout.Step
	xOf := func(at time.Time) float32 { return axis.posToX(axis.msToPos(float64(at.UnixMilli()))) }
	for i, at := range layout.TickValues {
		if at.Before(steps[0].At) || at.After(steps[n-1].At) || i >= len(layout.TickLabels) {
			continue
		}
		inst.ticks = append(inst.ticks, axisruler.Tick{Pos: xOf(at), Label: layout.TickLabels[i]})
	}
	axisruler.Paint(axisruler.SideBottom, baseY, axis.x0, axis.x1, inst.ticks, st)
	if len(layout.RolloverRows) == 0 {
		return
	}
	for _, label := range layout.RolloverRows[0].Labels {
		if int(label.StartIdx) >= len(layout.TickValues) {
			continue
		}
		at := layout.TickValues[label.StartIdx]
		x := axis.x0
		if !at.Before(steps[0].At) {
			x = xOf(at)
			c.PaintLine(x, bandH, x, baseY, vis.band, styletokens.StrokeHair).Send()
		}
		// The playhead's flag shares this row; where the two would print over
		// each other the flag wins, since it is the one that moves.
		if px := axis.posToX(inst.Transport.Pos); px < x-flagReach || px > x+contextReach {
			c.PaintText(x+3, bandH+2, 0, 0, label.Label, st.FontSize, vis.context).Send()
		}
		break // the first is the context the strip opens in; the rest are marked by their lines
	}
}

// paintNotches marks every step on the baseline, coloured by its state.
func (inst *Scrubber) paintNotches(axis timeAxis, steps []Step, baseY float32, vis visuals) {
	inst.xs0, inst.ys0, inst.xs1, inst.ys1 = inst.xs0[:0], inst.ys0[:0], inst.xs1[:0], inst.ys1[:0]
	inst.cols = inst.cols[:0]
	for i := range steps {
		x := axis.posToX(float64(i))
		col, grow := vis.idle, float32(0)
		switch steps[i].State {
		case StepStateHeld:
			col, grow = vis.held, 3
		case StepStateLoading:
			col, grow = vis.loading, 2
		case StepStateMissing:
			col, grow = vis.missing, 2
		}
		inst.xs0 = append(inst.xs0, x-1-grow*0.5)
		inst.ys0 = append(inst.ys0, baseY-notchH-grow)
		inst.xs1 = append(inst.xs1, x+1+grow*0.5)
		inst.ys1 = append(inst.ys1, baseY+1)
		inst.cols = append(inst.cols, col.Literal())
	}
	c.PaintRectsFilled(inst.xs0, inst.ys0, inst.xs1, inst.ys1, inst.cols).Send()
}

func (inst *Scrubber) paintHover(axis timeAxis, steps []Step, w, baseY float32, vis visuals) {
	i := min(max(inst.hoverStep, 0), len(steps)-1)
	x := axis.posToX(float64(i))
	c.PaintLine(x, bandH, x, baseY, vis.hover, styletokens.StrokeHair).Send()
	text := steps[i].At.In(inst.location()).Format("Mon 15:04")
	if v := steps[i].Value; v == v {
		name := inst.Opts.ValueName
		if name != "" {
			name += " "
		}
		text += fmt.Sprintf(" · %s%.3g", name, v)
		if p := steps[i].Peak; p == p {
			text += fmt.Sprintf(" (max %.3g)", p)
		}
		if inst.Opts.ValueUnit != "" {
			text += " " + inst.Opts.ValueUnit
		}
	}
	switch steps[i].State {
	case StepStateLoading:
		text += " · loading"
	case StepStateMissing:
		text += " · missing"
	}
	anchorH, tx := uint8(0), x+5
	if x > w/2 {
		anchorH, tx = 2, x-5
	}
	c.PaintText(tx, baseY-3, anchorH, 2, text, 11, vis.hoverText).Send()
}

func (inst *Scrubber) paintPlayhead(axis timeAxis, w, baseY float32, vis visuals) {
	pos := inst.Transport.Pos
	x := axis.posToX(pos)
	if n := len(axis.ms); n > 1 && !axis.byIndex {
		// Where the wall clock stands in a series that spans it — the line
		// between analysis and forecast.
		if nowMS := inst.now().UnixMilli(); nowMS > axis.ms[0] && nowMS < axis.ms[n-1] {
			xn := axis.posToX(axis.msToPos(float64(nowMS)))
			c.PaintLine(xn, bandH, xn, baseY, vis.now, styletokens.StrokeHair).Send()
			c.PaintText(xn+3, baseY-2, 0, 2, "now", 10, vis.now).Send()
		}
	}
	lo, _ := inst.Transport.Bounds(len(axis.ms))
	if xlo := axis.posToX(float64(lo)); x > xlo {
		c.PaintLine(xlo, baseY, x, baseY, vis.playhead, styletokens.StrokeRegular).Send()
	}
	c.PaintLine(x, bandH-1, x, baseY+2, vis.playhead, styletokens.StrokeRegular).Send()
	c.PaintPolygonFilled([]float32{x - 5, x + 5, x}, []float32{bandH - 6, bandH - 6, bandH + 1}, vis.playhead).Send()
	if axis.byIndex {
		return
	}
	text := axis.timeAt(pos, inst.location()).Format("Mon 15:04")
	anchorH, tx := uint8(0), x+6
	if x > w-90 {
		anchorH, tx = 2, x-6
	}
	c.PaintText(tx, bandH+2, anchorH, 0, text, 11, vis.playheadText).Send()
}
