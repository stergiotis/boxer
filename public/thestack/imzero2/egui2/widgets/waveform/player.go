package waveform

import (
	"math"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/science/audio/sink"
	"github.com/stergiotis/boxer/public/science/audio/track"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/axisruler"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Visuals are the colours and dimensions of the player. Colours are
// literal (not retained) so a per-column colour array can carry them.
type Visuals struct {
	Background     color.Color
	Wave           color.Color // unplayed samples
	Progress       color.Color // played samples, up to the playhead
	Playhead       color.Color
	Hover          color.Color // the hairline under the pointer
	HoverText      color.Color
	ChannelDivider color.Color
	// Unbuilt marks the span of the track whose peaks are not built yet
	// (ADR-0208 SD4): a hairline where the waveform will appear.
	Unbuilt       color.Color
	Ruler         axisruler.Style
	RulerHeight   float32 // the strip under the waveform holding the ruler
	ChannelGap    float32 // between channel bands when channels are split
	HoverFontSize float32
}

// DefaultVisuals reads the design-system palette (ADR-0031).
func DefaultVisuals() (vis Visuals) {
	vis = Visuals{
		Background:     color.Hex(styletokens.NeutralBgPanel.AsHex()),
		Wave:           color.Hex(styletokens.AccentDefault.AsHex()),
		Progress:       color.Hex(styletokens.AccentStrong.AsHex()),
		Playhead:       color.Hex(styletokens.WarningDefault.AsHex()),
		Hover:          color.Hex(styletokens.NeutralTextSecondary.AsHex()),
		HoverText:      color.Hex(styletokens.NeutralTextPrimary.AsHex()),
		ChannelDivider: color.Hex(styletokens.NeutralBorderFaint.AsHex()),
		Unbuilt:        color.Hex(styletokens.NeutralBorderFaint.AsHex()),
		Ruler:          axisruler.DefaultStyle(),
		RulerHeight:    20,
		ChannelGap:     2,
		HoverFontSize:  11,
	}
	return vis
}

// Options configure a [Player]. The zero value is a usable player: split
// channels, continuous columns, auto-scroll on, keyboard on.
type Options struct {
	// ScopeKey names the id scope the player's widgets derive from; two
	// players under one id stack need different keys. Empty is "waveform".
	ScopeKey string
	// OverlayChannels draws every channel over the same band instead of one
	// band per channel.
	OverlayChannels bool
	// BarWidth and BarGap switch the continuous column rendering to bars of
	// BarWidth pixels separated by BarGap pixels. Zero BarWidth is
	// continuous.
	BarWidth float32
	BarGap   float32
	// Normalize scales the waveform so the track's loudest sample reaches
	// full height. It applies only once the peaks pyramid is complete
	// (ADR-0208 SD4).
	Normalize bool
	// NoAutoScroll stops the view from following the playhead when it
	// leaves the visible span while playing.
	NoAutoScroll bool
	// AutoCenter keeps the playhead centred while playing (implies
	// following); a user drag suspends it for the drag's duration.
	AutoCenter bool
	// NoKeyboard leaves out the key-capturing Frame.
	NoKeyboard bool
	// SeekStep is the arrow-key seek distance; zero is five seconds. Shift
	// multiplies it by six.
	SeekStep time.Duration
	// Visuals overrides [DefaultVisuals] when non-nil.
	Visuals *Visuals
}

// Player draws one track and owns the view over it. Construct with [New],
// draw with [Player.Render] every frame from the frame goroutine.
type Player struct {
	ids  *c.WidgetIdStack
	tr   *track.Track
	opts Options
	vis  Visuals

	view     View
	viewInit bool
	width    float32
	height   float32

	keyFrameID uint64
	probeSalt  uint64

	dragging      bool
	dragOriginX   float32
	dragFromFrame float64

	hoverFrame   int64
	hoverOk      bool
	clickedFrame int64
	clickedOk    bool

	// Scratch, reused across frames (CODINGSTANDARDS § Memory).
	minXs, minYs, maxXs, maxYs []float32
	cols                       color.Colors
	qMin, qMax                 []int8
	fMin, fMax                 []float32
	xs, ys                     []float32
	tickBuf                    []time.Duration
	ticks                      []axisruler.Tick

	// windowWait is set during a frame that asked the track for a raw window
	// it did not have yet (ADR-0208 SD3): the pyramid is drawn instead and
	// the frame loop is kept hot until the window arrives.
	windowWait bool
}

const (
	canvasKey = "waveform-canvas"
	areaKey   = "waveform-area"
	keysKey   = "waveform-keys"

	probeSaltSeed uint64 = 0x7a8e_5b1c_0a44_d9f3

	rulerGap        float32 = 2
	playheadCaretW  float32 = 10
	playheadCaretH  float32 = 6
	rulerTargetPx   float32 = 90
	markerAtFpp             = 0.25 // a sample four pixels wide gets a marker
	defaultSeekStep         = 5 * time.Second
	shiftSeekMul            = 6
	keyZoomFactor           = 2.0
)

// keyMask is what the player eats while focused (ADR-0177). Plus/minus are
// not in the key vocabulary; PageUp/PageDown zoom instead.
var keyMask = keycodes.MaskOf(keycodes.Space, keycodes.ArrowLeft, keycodes.ArrowRight,
	keycodes.Home, keycodes.End, keycodes.PageUp, keycodes.PageDown)

// New makes a player over tr. ids scopes every id the player derives; the
// player does not take ownership of the track.
func New(ids *c.WidgetIdStack, tr *track.Track, opts Options) (inst *Player) {
	if opts.ScopeKey == "" {
		opts.ScopeKey = "waveform"
	}
	if opts.SeekStep <= 0 {
		opts.SeekStep = defaultSeekStep
	}
	vis := DefaultVisuals()
	if opts.Visuals != nil {
		vis = *opts.Visuals
	}
	return &Player{ids: ids, tr: tr, opts: opts, vis: vis}
}

// Track returns the track the player draws.
func (inst *Player) Track() (tr *track.Track) { return inst.tr }

// View returns the current view.
func (inst *Player) View() (v View) { return inst.view }

// Size returns the canvas size of the last Render, in logical pixels.
func (inst *Player) Size() (w, h float32) { return inst.width, inst.height }

// SetView replaces the view; it is clamped at the next Render.
func (inst *Player) SetView(v View) {
	inst.view = v
	inst.viewInit = true
}

// FitAll shows the whole track at the next Render.
func (inst *Player) FitAll() { inst.viewInit = false }

// ZoomBy scales the zoom by factor (> 1 zooms in) about the playhead when it
// is visible, else about the centre of the canvas.
func (inst *Player) ZoomBy(factor float64) {
	if !inst.viewInit || inst.width < 1 {
		return
	}
	anchor := inst.width / 2
	if x := inst.view.FrameToX(float64(inst.Position())); x >= 0 && x <= inst.width {
		anchor = x
	}
	inst.view = inst.view.zoomAt(anchor, factor, inst.tr.Frames(), inst.width)
}

// Hover returns the frame under the pointer as of the last frame.
func (inst *Player) Hover() (frame int64, ok bool) { return inst.hoverFrame, inst.hoverOk }

// Clicked returns the frame a primary click landed on during the last frame.
func (inst *Player) Clicked() (frame int64, ok bool) { return inst.clickedFrame, inst.clickedOk }

// Position is the sink's audible frame.
func (inst *Player) Position() (frame int64) { return inst.tr.Sink().Position() }

// IsPlaying reports whether the sink is playing.
func (inst *Player) IsPlaying() (yes bool) { return inst.tr.Sink().State() == sink.StatePlaying }

// TogglePlay plays a paused or stopped sink and pauses a playing one.
func (inst *Player) TogglePlay() {
	s := inst.tr.Sink()
	if s.State() == sink.StatePlaying {
		s.Pause()
	} else {
		s.Play()
	}
}

// SeekTo moves the sink to frame, clamped to the track.
func (inst *Player) SeekTo(frame int64) {
	frame = max(0, min(frame, inst.tr.Frames()))
	err := inst.tr.Sink().SeekE(frame)
	if err != nil {
		// Only a closed sink refuses a seek; the player keeps drawing.
		log.Debug().Err(err).Int64("frame", frame).Msg("waveform: seek refused")
	}
}

// FormatOffset renders a frame as the ruler would: an offset from the start
// of the track, or a wall-clock time when the track has an epoch.
func (inst *Player) FormatOffset(frame int64) (s string) {
	return inst.formatFrame(frame, inst.readoutStep())
}

// Render draws the player into a canvas of w×h logical pixels at the current
// position of the enclosing Ui.
func (inst *Player) Render(w, h float32) {
	for range c.IdScope(inst.ids.PrepareStr(inst.opts.ScopeKey)) {
		if inst.opts.NoKeyboard {
			inst.keyFrameID = 0
			inst.frame(w, h)
			continue
		}
		kf := c.Frame(inst.ids.PrepareStr(keysKey)).CaptureKeys(uint64(keyMask))
		inst.keyFrameID = kf.Id()
		for range kf.KeepIter() {
			inst.frame(w, h)
		}
	}
}

// RenderFillWidth draws the player h pixels tall across the width of the
// enclosing pane, read back through a size probe (one frame behind); the
// fallback width is used until the probe reports.
func (inst *Player) RenderFillWidth(h float32, fallbackW float32) {
	w, _, ok := c.CapturePaneSize(inst.probeSeq("waveform-pane"))
	if !ok || w < 1 {
		w = fallbackW
	}
	inst.Render(w, h)
}

// RenderFill draws the player across the whole enclosing pane, with the
// fallback size until the probe reports.
func (inst *Player) RenderFill(fallbackW, fallbackH float32) {
	w, h, ok := c.CapturePaneSize(inst.probeSeq("waveform-pane"))
	if !ok || w < 1 || h < 1 {
		w, h = fallbackW, fallbackH
	}
	inst.Render(w, h)
}

func (inst *Player) probeSeq(role string) (seq uint64) {
	if inst.probeSalt == 0 {
		inst.probeSalt = inst.ids.PrepareHighEntropy(probeSaltSeed).Derive()
	}
	return c.ProbeSeq(inst.opts.ScopeKey, role) ^ inst.probeSalt
}

func (inst *Player) frame(w, h float32) {
	sm := c.CurrentApplicationState.StateManager
	frames := inst.tr.Frames()
	if w < 1 {
		w = 1
	}
	if !inst.viewInit {
		inst.view = fitAll(frames, w)
		inst.viewInit = true
	} else {
		inst.view = inst.view.clamp(frames, w)
	}
	inst.width, inst.height = w, h
	waveH := max(h-inst.vis.RulerHeight-rulerGap, 1)

	canvasH := widgethandle.Make(inst.ids.PrepareStr(canvasKey).Derive())
	areaH := widgethandle.Make(inst.ids.PrepareStr(areaKey).Derive())
	cur, live := sm.GetCanvasCursor(canvasH)
	flags := sm.GetResponse(areaH)
	areaCur, areaOk := sm.GetCanvasCursor(areaH)
	wheel := sm.GetCanvasWheel(canvasH)
	ptr := sm.GetPointer()

	inst.hoverOk, inst.clickedOk = false, false
	if live {
		inst.handleInput(cur, areaCur, areaOk, flags, wheel, ptr, frames, w, waveH)
	}
	if !inst.opts.NoKeyboard {
		inst.handleKeys(sm, frames, w)
	}

	s := inst.tr.Sink()
	pos := s.Position()
	playing := s.State() == sink.StatePlaying
	inst.followPlayhead(pos, playing, frames, w)
	inst.windowWait = false

	c.PaintClipPush(0, 0, w, h).Send()
	inst.paintWave(w, waveH, pos)
	inst.paintRuler(w, waveH)
	inst.paintPlayhead(pos, w, waveH)
	inst.paintHover(w, waveH)
	c.PaintClipPop().Send()

	// The drag-owning region last, so it wins the hit test over a canvas
	// that senses click and hover only (ADR-0204 §SD6).
	c.PaintSenseRegion(inst.ids.PrepareStr(areaKey), 0, 0, w, h).Send()
	c.PaintCanvas(inst.ids.PrepareStr(canvasKey), w, h).
		Background(inst.vis.Background).
		Sense(true, false, true).
		CaptureZoom().
		CaptureScroll().
		Send()

	// Keep the loop hot while anything is still arriving: the sink's clock,
	// a drag, a raw window in flight, or a pyramid still building.
	if playing || inst.dragging || inst.windowWait || inst.tr.WindowPending() || !inst.tr.Peaks().IsComplete() {
		c.RequestRepaintAfter(1.0 / 60)
	}
}

func (inst *Player) handleInput(cur, areaCur c.CanvasCursorValue, areaOk bool, flags c.ResponseFlagsE,
	wheel c.CanvasWheelValue, ptr c.PointerValue, frames int64, w, waveH float32) {
	posX, posY, posOk := cur.PosX, cur.PosY, !isNaN32(cur.PosX) && !isNaN32(cur.PosY)
	if ptr.Valid && !isNaN32(ptr.X) && !isNaN32(ptr.Y) && !isNaN32(cur.OriginX) && !isNaN32(cur.OriginY) {
		posX, posY, posOk = ptr.X-cur.OriginX, ptr.Y-cur.OriginY, true
	}

	if flags.HasDragStarted() && posOk {
		origin := posX
		if areaOk && !isNaN32(areaCur.PosX) {
			// The sense region's drag-started row carries the press origin.
			origin = areaCur.PosX
		}
		inst.dragging = true
		inst.dragOriginX = origin
		inst.dragFromFrame = inst.view.FromFrame
		inst.focusKeys()
	}
	if inst.dragging && (flags.HasDragged() || flags.HasDragStopped()) && posOk {
		v := inst.view
		v.FromFrame = inst.dragFromFrame - float64(posX-inst.dragOriginX)*v.FramesPerPx
		inst.view = v.clamp(frames, w)
	}
	if flags.HasDragStopped() {
		inst.dragging = false
	}

	if posOk && flags.HasContainsPointer() && posX >= 0 && posX <= w && posY >= 0 && posY <= waveH {
		inst.hoverFrame = clampFrame(inst.view.FrameAtX(posX), frames)
		inst.hoverOk = true
	}
	if posOk && flags.HasPrimaryClicked() && posY >= 0 && posY <= waveH {
		f := clampFrame(inst.view.FrameAtX(posX), frames)
		inst.SeekTo(f)
		inst.clickedFrame, inst.clickedOk = f, true
		// A click's release surrenders the focus its press asked for.
		inst.focusKeys()
	}

	if !inst.dragging {
		if wheel.Zoom > 0 && wheel.Zoom != 1 {
			anchor := w / 2
			if !isNaN32(wheel.HoverX) {
				anchor = wheel.HoverX
			}
			inst.view = inst.view.zoomAt(anchor, float64(wheel.Zoom), frames, w)
		}
		if d := wheel.ScrollX + wheel.ScrollY; d != 0 && !isNaN32(d) {
			inst.view = inst.view.panPx(d, frames, w)
		}
	}
}

func (inst *Player) handleKeys(sm *c.StateManager, frames int64, w float32) {
	if inst.keyFrameID == 0 {
		return
	}
	for _, k := range sm.GetCapturedKeys(widgethandle.Make(inst.keyFrameID)) {
		switch k.Code {
		case keycodes.Space:
			inst.TogglePlay()
		case keycodes.ArrowLeft, keycodes.ArrowRight:
			step := inst.tr.TimeBase().DurationToFrame(inst.opts.SeekStep)
			if k.Shift() {
				step *= shiftSeekMul
			}
			if k.Code == keycodes.ArrowLeft {
				step = -step
			}
			inst.SeekTo(inst.Position() + step)
		case keycodes.Home:
			inst.SeekTo(0)
		case keycodes.End:
			inst.SeekTo(frames)
		case keycodes.PageUp:
			inst.ZoomBy(keyZoomFactor)
		case keycodes.PageDown:
			inst.ZoomBy(1 / keyZoomFactor)
		}
	}
	_ = w
}

// followPlayhead applies auto-scroll and auto-centre while playing.
func (inst *Player) followPlayhead(pos int64, playing bool, frames int64, w float32) {
	if !playing || inst.dragging || inst.opts.NoAutoScroll && !inst.opts.AutoCenter {
		return
	}
	v := inst.view
	span := float64(w) * v.FramesPerPx
	switch {
	case inst.opts.AutoCenter:
		v.FromFrame = float64(pos) - span/2
	case !v.Contains(float64(pos), w):
		// Page: the playhead re-enters near the left edge.
		v.FromFrame = float64(pos) - span*0.05
	default:
		return
	}
	inst.view = v.clamp(frames, w)
}

func (inst *Player) focusKeys() {
	if inst.keyFrameID != 0 {
		c.RequestFocus(inst.keyFrameID)
	}
}

// ---- painting ------------------------------------------------------------

func (inst *Player) paintWave(w, waveH float32, pos int64) {
	format := inst.tr.Format()
	channels := int(format.Channels)
	if channels == 0 {
		return
	}
	bands := channels
	if inst.opts.OverlayChannels {
		bands = 1
	}
	gap := inst.vis.ChannelGap
	bandH := (waveH - gap*float32(bands-1)) / float32(bands)
	if bandH < 1 {
		bandH, gap = waveH/float32(bands), 0
	}
	for b := 1; b < bands; b++ {
		y := float32(b)*(bandH+gap) - gap/2
		c.PaintLine(0, y, w, y, inst.vis.ChannelDivider, styletokens.StrokeHair).Send()
	}

	gain := float32(1)
	p := inst.tr.Peaks()
	if inst.opts.Normalize && p.IsComplete() {
		if peak := p.GlobalPeak(); peak > 0 {
			gain = 127 / float32(peak)
		}
	}

	colW := float32(1)
	barW := float32(1)
	if inst.opts.BarWidth > 0 {
		barW = inst.opts.BarWidth
		colW = inst.opts.BarWidth + max(inst.opts.BarGap, 0)
	}
	cols := int(w / colW)
	if cols < 1 {
		cols = 1
	}
	inst.growScratch(cols)

	view := inst.view
	fpp := view.FramesPerPx
	fppCol := fpp * float64(colW)
	frames := inst.tr.Frames()
	fromI := int64(math.Floor(view.FromFrame))
	toI := int64(math.Ceil(view.ToFrame(w)))
	fromI = clampFrame(fromI, frames)
	toI = clampFrame(toI, frames)
	if toI <= fromI {
		return
	}

	for ch := range channels {
		band := ch
		if inst.opts.OverlayChannels {
			band = 0
		}
		top := float32(band) * (bandH + gap)
		yc := top + bandH/2
		hh := bandH / 2

		switch {
		case fppCol >= float64(p.BaseBin()):
			inst.emitPyramid(fromI, toI, ch, cols, colW, barW, yc, hh, gain, top, top+bandH, pos, w)
		case fpp >= 1:
			raw, ok := inst.window(fromI, toI)
			if !ok {
				inst.emitPyramid(fromI, toI, ch, cols, colW, barW, yc, hh, gain, top, top+bandH, pos, w)
				continue
			}
			n := reduceColumns(raw, channels, ch, fppCol, inst.fMin[:cols], inst.fMax[:cols])
			inst.emitColumns(n, colW, barW, yc, hh, gain, top, top+bandH, pos)
		default:
			raw, ok := inst.window(fromI, min(toI+1, frames))
			if !ok {
				inst.emitPyramid(fromI, toI, ch, cols, colW, barW, yc, hh, gain, top, top+bandH, pos, w)
				continue
			}
			inst.emitSamples(raw, fromI, ch, channels, yc, hh, gain, pos)
		}
	}
}

// emitPyramid draws the columns of [fromI, toI) from the peaks pyramid and
// marks the columns whose peaks are not built yet with a hairline.
func (inst *Player) emitPyramid(fromI, toI int64, ch, cols int, colW, barW, yc, hh, gain, top, bottom float32, pos int64, w float32) {
	p := inst.tr.Peaks()
	n := p.Columns(fromI, toI, ch, inst.qMin[:cols], inst.qMax[:cols])
	for i := range n {
		inst.fMin[i] = float32(inst.qMin[i]) / 127
		inst.fMax[i] = float32(inst.qMax[i]) / 127
	}
	inst.emitColumns(n, colW, barW, yc, hh, gain, top, bottom, pos)
	if n < cols && !p.IsComplete() {
		x0 := float32(n) * colW
		c.PaintRectFilled(x0, yc-0.5, w, yc+0.5, 0, inst.vis.Unbuilt).Send()
	}
}

// window asks the track for raw frames [from, to); a miss is drawn from the
// pyramid this frame and asked again next frame (ADR-0208 SD3).
func (inst *Player) window(from, to int64) (raw []float32, ok bool) {
	raw, ok = inst.tr.Window(from, to)
	if !ok {
		inst.windowWait = true
	}
	return raw, ok
}

// emitColumns turns n min/max columns in fMin/fMax into one rect batch.
func (inst *Player) emitColumns(n int, colW, barW, yc, hh, gain, top, bottom float32, pos int64) {
	if n <= 0 {
		return
	}
	waveHex := inst.vis.Wave.Literal()
	progHex := inst.vis.Progress.Literal()
	view := inst.view
	for i := range n {
		x0 := float32(i) * colW
		x1 := x0 + barW
		yTop := yc - inst.fMax[i]*hh*gain
		yBot := yc - inst.fMin[i]*hh*gain
		if yBot-yTop < 1 {
			// A silent or near-silent column still shows a hairline.
			m := (yTop + yBot) / 2
			yTop, yBot = m-0.5, m+0.5
		}
		yTop = max(yTop, top)
		yBot = min(yBot, bottom)
		inst.minXs[i], inst.minYs[i], inst.maxXs[i], inst.maxYs[i] = x0, yTop, x1, yBot
		if view.XToFrame(x1) <= float64(pos) {
			inst.cols.SetHex(i, progHex)
		} else {
			inst.cols.SetHex(i, waveHex)
		}
	}
	c.PaintRectsFilled(inst.minXs[:n], inst.minYs[:n], inst.maxXs[:n], inst.maxYs[:n], inst.cols[:n]).Send()
}

// emitSamples draws the raw window as a polyline (and markers when a sample
// is wide enough), split at the playhead so the played part keeps the
// progress colour.
func (inst *Player) emitSamples(raw []float32, rawFrom int64, ch, channels int, yc, hh, gain float32, pos int64) {
	n := len(raw) / channels
	if n <= 0 {
		return
	}
	if cap(inst.xs) < n {
		inst.xs = make([]float32, n)
		inst.ys = make([]float32, n)
	}
	xs, ys := inst.xs[:n], inst.ys[:n]
	view := inst.view
	split := n
	for i := range n {
		f := rawFrom + int64(i)
		xs[i] = view.FrameToX(float64(f))
		ys[i] = yc - raw[i*channels+ch]*hh*gain
		if f < pos {
			split = i + 1
		}
	}
	stroke := styletokens.StrokeRegular
	if split > 1 {
		c.PaintPolyline(xs[:split], ys[:split], inst.vis.Progress, stroke).Send()
	}
	if split < n {
		start := max(split-1, 0)
		c.PaintPolyline(xs[start:], ys[start:], inst.vis.Wave, stroke).Send()
	}
	if view.FramesPerPx <= markerAtFpp {
		if split > 0 {
			c.PaintMarkers(xs[:split], ys[:split], 0, 2.5, inst.vis.Progress, stroke).Send()
		}
		if split < n {
			c.PaintMarkers(xs[split:], ys[split:], 0, 2.5, inst.vis.Wave, stroke).Send()
		}
	}
}

func (inst *Player) growScratch(cols int) {
	if cap(inst.minXs) >= cols {
		return
	}
	inst.minXs = make([]float32, cols)
	inst.minYs = make([]float32, cols)
	inst.maxXs = make([]float32, cols)
	inst.maxYs = make([]float32, cols)
	inst.cols = color.NewColors(cols)
	inst.qMin = make([]int8, cols)
	inst.qMax = make([]int8, cols)
	inst.fMin = make([]float32, cols)
	inst.fMax = make([]float32, cols)
}

func (inst *Player) paintRuler(w, waveH float32) {
	tb := inst.tr.TimeBase()
	view := inst.view
	fromD := tb.FrameToDuration(int64(math.Floor(view.FromFrame)))
	toD := tb.FrameToDuration(int64(math.Ceil(view.ToFrame(w))))
	span := toD - fromD
	if span <= 0 {
		return
	}
	step := pickDurationStep(span, w, rulerTargetPx)
	inst.tickBuf = durationTicks(fromD, toD, step, inst.tickBuf)
	inst.ticks = inst.ticks[:0]
	for _, t := range inst.tickBuf {
		x := float32(float64(t-fromD) / float64(span) * float64(w))
		var label string
		if tb.IsAbsolute() {
			label = formatClock(tb.Epoch.Add(t), step)
		} else {
			label = formatOffset(t, step)
		}
		inst.ticks = append(inst.ticks, axisruler.Tick{Pos: x, Label: label})
	}
	axisruler.Paint(axisruler.SideBottom, waveH+rulerGap, 0, w, inst.ticks, inst.vis.Ruler)
}

func (inst *Player) paintPlayhead(pos int64, w, waveH float32) {
	x := inst.view.FrameToX(float64(pos))
	if x < 0 || x > w {
		return
	}
	col := inst.vis.Playhead
	c.PaintLine(x, 0, x, waveH, col, styletokens.StrokeStrong).Send()
	half := playheadCaretW / 2
	x0, x1 := max(x-half, 0), min(x+half, w)
	c.PaintPolygonFilled([]float32{x0, x1, x}, []float32{0, 0, playheadCaretH}, col).Send()
}

func (inst *Player) paintHover(w, waveH float32) {
	if !inst.hoverOk {
		return
	}
	x := inst.view.FrameToX(float64(inst.hoverFrame))
	c.PaintLine(x, 0, x, waveH, inst.vis.Hover, styletokens.StrokeHair).Send()
	label := inst.formatFrame(inst.hoverFrame, inst.readoutStep())
	anchorH := uint8(0) // left of the hairline
	tx := x + 4
	if x > w*0.8 {
		anchorH, tx = 2, x-4 // right-anchored when near the right edge
	}
	c.PaintText(tx, playheadCaretH+2, anchorH, 0, label, inst.vis.HoverFontSize, inst.vis.HoverText).Send()
}

// readoutStep is the precision a readout of one frame needs at the current
// zoom: milliseconds until a pixel is narrower than one, then microseconds.
func (inst *Player) readoutStep() (step time.Duration) {
	tb := inst.tr.TimeBase()
	if !inst.viewInit || tb.Format.SampleRate == 0 {
		return time.Millisecond
	}
	pxDur := float64(inst.view.FramesPerPx) / float64(tb.Format.SampleRate) * float64(time.Second)
	if pxDur >= float64(time.Millisecond) {
		return time.Millisecond
	}
	return time.Microsecond
}

func (inst *Player) formatFrame(frame int64, step time.Duration) (s string) {
	tb := inst.tr.TimeBase()
	d := tb.FrameToDuration(frame)
	if tb.IsAbsolute() {
		return formatClock(tb.Epoch.Add(d), step)
	}
	return formatOffset(d, step)
}

func clampFrame(f, frames int64) (out int64) {
	return max(0, min(f, frames))
}

func isNaN32(v float32) (yes bool) { return math.IsNaN(float64(v)) }
