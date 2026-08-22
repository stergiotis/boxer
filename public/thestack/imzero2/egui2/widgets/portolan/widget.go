package portolan

import (
	"math"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Options configures a Map. The zero value is unusable without a Source; the
// rest default to Leaflet's behaviour with this tree's one deliberate
// difference, ZoomSnap 0 (continuous zoom).
type Options struct {
	// Source is where tiles come from; Loader how they are fetched.
	Source TileSource
	Loader LoaderOptions
	// Center and Zoom are the initial view; an invalid (NaN) Center leaves
	// the map unloaded until SetView is called.
	Center LatLng
	Zoom   float64
	// CRS defaults to EPSG3857.
	CRS CRSI
	// MinZoom/MaxZoom override the source's when Has*; MaxBounds keeps the
	// view inside an area; ZoomSnap/ZoomDelta as in ViewOptions.
	MinZoom, MaxZoom       float64
	HasMinZoom, HasMaxZoom bool
	MaxBounds              LatLngBounds
	ZoomSnap, ZoomDelta    float64
	// The interaction handlers are on unless disabled; Handlers carries
	// their numeric knobs (zero = DefaultHandlerOptions, i.e. Leaflet's).
	NoDragging, NoScrollWheelZoom, NoDoubleClickZoom bool
	NoPinchZoom, NoBoxZoom, NoKeyboard               bool
	NoZoomAnimation                                  bool
	Handlers                                         HandlerOptions
	// Background is the colour under the tiles (0 = a light grey).
	Background uint32
	// HideAttribution suppresses the source's attribution label.
	HideAttribution bool
	// NoTiles draws no basemap at all — background and overlays only; the
	// offline mode play uses. SetNoTiles toggles it later.
	NoTiles bool
}

// Projector is what an overlay callback paints with: conversions between
// geography and canvas pixels for the frame being drawn, and the helpers of
// overlay.go.
type Projector struct {
	view *View
	m    *Map
}

// ToCanvas projects a point to canvas pixels (origin top-left of the map).
func (p Projector) ToCanvas(ll LatLng) Point { return p.view.LatLngToContainerPoint(ll) }

// ToLatLng inverts ToCanvas.
func (p Projector) ToLatLng(px Point) LatLng { return p.view.ContainerPointToLatLng(px) }

// View is the frame's view, read-only by convention.
func (p Projector) View() *View { return p.view }

// Map is the slippy-map widget: a View, a Pyramid and a TileLoader behind a
// painter-lane canvas, with Leaflet's handlers between the lane's registers
// and the view. Create one per map instance and keep it; call Render every
// frame (ADR-0204 §SD1, §SD2).
type Map struct {
	ids     *c.WidgetIdStack
	opts    Options
	hopts   HandlerOptions
	view    *View
	pyramid *Pyramid
	loader  *TileLoader
	tracker *c.ImageVersionTracker[TileCoords]
	// overlayTracker is the send-once record of Projector.Image rasters.
	overlayTracker *c.ImageVersionTracker[string]
	// pixels holds decoded tiles by WRAPPED coords, shared by the unwrapped
	// tiles that show the same source tile; holders counts them so pixels go
	// when the last holder is pruned.
	pixels  map[TileCoords]*tilePixels
	holders map[TileCoords]int
	// wrappedOf remembers which wrapped tile each requested unwrapped tile
	// maps to, for the loader's arrivals and for Draw.
	wrappedOf map[TileCoords]TileCoords
	// srcGen counts tile sources; it is the tiles' image version, so a
	// switch re-uploads under the same ids instead of showing the old
	// source's pixels.
	srcGen uint64

	// handlers
	drag  dragHandler
	wheel wheelHandler
	pinch pinchHandler
	box   boxZoom
	// press bookkeeping for the drag's origin (M0's recipe)
	prevPosX, prevPosY         float32
	prevPosOk                  bool
	pressOriginX, pressOriginY float32
	pressOriginOk              bool
	keyFrameID                 uint64

	// per-frame readback
	events    ViewEvents
	size      Point
	hover     LatLng
	hoverOk   bool
	clicked   LatLng
	clickedOk bool
	// instrumentation
	bytesShipped uint64
	frames       uint64
	reships      uint64
}

type tilePixels struct {
	px   []uint32
	w, h int
}

const defaultBackgroundRGBA = 0xd8d8dcff

// mapKeyMask is what the map eats while focused: the arrows pan, Escape
// cancels a box zoom. The zoom keys wait for keycodes the vocabulary lacks.
var mapKeyMask = keycodes.MaskOf(keycodes.ArrowUp, keycodes.ArrowDown, keycodes.ArrowLeft, keycodes.ArrowRight, keycodes.Escape)

// New makes a map. ids scopes every id the widget derives; opts.Source is
// required.
func New(ids *c.WidgetIdStack, opts Options) *Map {
	if opts.Source.URLTemplate == "" {
		opts.Source = NewTileSource("https://tile.openstreetmap.org/{z}/{x}/{y}.png")
	} else {
		opts.Source = opts.Source.Normalized()
	}
	if opts.Background == 0 {
		opts.Background = defaultBackgroundRGBA
	}
	hopts := opts.Handlers
	if hopts == (HandlerOptions{}) {
		hopts = DefaultHandlerOptions()
	}
	view := NewView(ViewOptions{
		CRS: opts.CRS, MinZoom: opts.MinZoom, MaxZoom: opts.MaxZoom,
		HasMinZoom: opts.HasMinZoom, HasMaxZoom: opts.HasMaxZoom,
		ZoomSnap: opts.ZoomSnap, ZoomDelta: opts.ZoomDelta, MaxBounds: opts.MaxBounds,
	})
	view.SetLayerZoomLimits(opts.Source.MinZoom, opts.Source.MaxZoom, true, !math.IsInf(opts.Source.MaxZoom, 1))
	view.SetZoomAnimation(!opts.NoZoomAnimation)
	m := &Map{
		ids:            ids,
		opts:           opts,
		hopts:          hopts,
		view:           view,
		pyramid:        NewPyramid(opts.Source),
		loader:         NewTileLoader(opts.Loader),
		tracker:        c.NewImageVersionTracker[TileCoords](),
		overlayTracker: c.NewImageVersionTracker[string](),
		pixels:         make(map[TileCoords]*tilePixels, 64),
		holders:        make(map[TileCoords]int, 64),
		wrappedOf:      make(map[TileCoords]TileCoords, 64),
		srcGen:         1,
	}
	m.wirePyramid()
	return m
}

func (m *Map) wirePyramid() {
	m.pyramid.OnRequest = m.requestTile
	m.pyramid.OnAbort = func(coords TileCoords) { m.loader.Cancel(coords); m.dropHolder(coords) }
	m.pyramid.OnUnload = m.dropHolder
}

// Source is the tile source in use.
func (m *Map) Source() TileSource { return m.opts.Source }

// SetSource switches the tile source: requests to the old one are cancelled
// and its tiles dropped, the pyramid restarts on the new one at the current
// view, and the view's layer zoom limits follow the source. An empty template
// is ignored.
func (m *Map) SetSource(src TileSource) {
	if src.URLTemplate == "" {
		return
	}
	src = src.Normalized()
	for coords := range m.wrappedOf {
		m.loader.Cancel(coords)
	}
	m.loader.Drain()
	for wrapped := range m.pixels {
		m.tracker.Forget(wrapped)
	}
	m.pixels = make(map[TileCoords]*tilePixels, 64)
	m.holders = make(map[TileCoords]int, 64)
	m.wrappedOf = make(map[TileCoords]TileCoords, 64)
	m.srcGen++
	m.opts.Source = src
	m.pyramid = NewPyramid(src)
	m.wirePyramid()
	m.view.SetLayerZoomLimits(src.MinZoom, src.MaxZoom, true, !math.IsInf(src.MaxZoom, 1))
	if m.view.Loaded() && !m.opts.NoTiles {
		m.pyramid.Sync(m.view, ViewEvents{ViewReset: true})
	}
}

// Close stops the loader. The map must not be rendered afterwards.
func (m *Map) Close() { m.loader.Close() }

// View is the map's camera — use it to SetView/FlyTo/FitBounds/PanTo between
// frames; Render reads it.
func (m *Map) View() *View { return m.view }

// Events are the view events of the last rendered frame.
func (m *Map) Events() ViewEvents { return m.events }

// ViewHash changes whenever the view does — centre, zoom or size — for
// callers that debounce on a stable view rather than on events.
func (m *Map) ViewHash() uint64 {
	v := m.view
	h := uint64(14695981039346656037)
	mix := func(f float64) {
		h ^= math.Float64bits(f)
		h *= 1099511628211
	}
	mix(v.Center().Lat)
	mix(v.Center().Lng)
	mix(v.Zoom())
	mix(v.Size().X)
	mix(v.Size().Y)
	return h
}

// Hover is the geographic point under the pointer last frame, when the
// pointer was over the map.
func (m *Map) Hover() (LatLng, bool) { return m.hover, m.hoverOk }

// Clicked is the geographic point of a primary click last frame — a press
// and release without a drag in between.
func (m *Map) Clicked() (LatLng, bool) { return m.clicked, m.clickedOk }

// Loading reports tiles still on their way.
func (m *Map) Loading() bool { return m.pyramid.IsLoading() || m.loader.Pending() > 0 }

// Stats are the pyramid's counters; Health the loader's.
func (m *Map) Stats() PyramidStats  { return m.pyramid.Stats() }
func (m *Map) Health() LoaderHealth { return m.loader.Health() }

// BytesShipped is how many pixel bytes have gone through paintImage since
// construction; Reships how many tiles were sent a second time (starved
// textures).
func (m *Map) BytesShipped() uint64 { return m.bytesShipped }
func (m *Map) Reships() uint64      { return m.reships }

// SetNoTiles turns the basemap off (background and overlays only) or back
// on; turning it on again reloads the viewport's tiles.
func (m *Map) SetNoTiles(on bool) {
	if on == m.opts.NoTiles {
		return
	}
	m.opts.NoTiles = on
	if !on && m.view.Loaded() {
		m.pyramid.Sync(m.view, ViewEvents{ViewReset: true})
	}
}

func (m *Map) requestTile(coords, wrapped TileCoords) {
	m.wrappedOf[coords] = wrapped
	m.holders[wrapped]++
	if px, ok := m.pixels[wrapped]; ok && px != nil {
		// Already decoded for another unwrapped copy: ready at once.
		m.pyramid.TileReady(m.view, coords, false, time.Now())
		return
	}
	m.loader.Request(coords, wrapped, m.opts.Source.URL(wrapped, m.pyramid.GlobalRows()))
}

func (m *Map) dropHolder(coords TileCoords) {
	wrapped, ok := m.wrappedOf[coords]
	if !ok {
		return
	}
	delete(m.wrappedOf, coords)
	if m.holders[wrapped]--; m.holders[wrapped] <= 0 {
		delete(m.holders, wrapped)
		delete(m.pixels, wrapped)
		m.tracker.Forget(wrapped)
	}
}

// RenderFill is Render sized to the pane the map sits in, as reported by the
// layout probe one frame late; fallbackW/fallbackH are used on the first
// frame and whenever the probe has nothing (a hidden tab).
func (m *Map) RenderFill(fallbackW, fallbackH float32, overlay func(Projector)) {
	w, h := fallbackW, fallbackH
	if pw, ph, ok := c.CapturePaneSize(m.ids.PrepareStr("portolan-pane").Derive()); ok && pw > 0 && ph > 0 {
		w, h = pw, ph
	}
	m.Render(w, h, overlay)
}

// Render draws the map into a w×h canvas at the current layout position and
// applies the previous frame's input. overlay, when not nil, is called after
// the tiles and before the canvas is flushed, to paint on top with c.Paint*
// in canvas coordinates through the Projector.
func (m *Map) Render(w, h float32, overlay func(Projector)) {
	for range c.IdScope(m.ids.PrepareStr("portolan")) {
		// The key-capturing Frame around the body: while it has focus (a
		// click on the map gives it), the arrows pan and Escape cancels a box
		// zoom (ADR-0177).
		kf := c.Frame(m.ids.PrepareStr("portolan-keys")).CaptureKeys(uint64(mapKeyMask))
		m.keyFrameID = kf.Id()
		for range kf.KeepIter() {
			m.frame(w, h, overlay)
		}
	}
}

func (m *Map) frame(w, h float32, overlay func(Projector)) {
	sm := c.CurrentApplicationState.StateManager
	now := time.Now()
	m.frames++
	size := Point{float64(w), float64(h)}
	sizeChanged := size != m.size
	m.size = size
	m.view.SetSize(size)
	if !m.view.Loaded() && !math.IsNaN(m.opts.Center.Lat) && !math.IsNaN(m.opts.Center.Lng) {
		m.view.SetView(m.opts.Center, m.opts.Zoom)
	}
	// Running animations step first, so a handler that starts a new one this
	// frame starts it from the current view.
	m.view.Tick(now)

	canvasH := widgethandle.Make(m.ids.PrepareStr("portolan-canvas").Derive())
	areaH := widgethandle.Make(m.ids.PrepareStr("portolan-area").Derive())
	cur, live := sm.GetCanvasCursor(canvasH)
	flags := sm.GetResponse(areaH)
	areaCur, areaOk := sm.GetCanvasCursor(areaH)
	wheel := sm.GetCanvasWheel(canvasH)
	ptr := sm.GetPointer()

	m.hoverOk, m.clickedOk = false, false
	if live && m.view.Loaded() {
		m.handleInput(cur, areaCur, areaOk, flags, wheel, ptr, now)
		// Hover and click readback, for apps that pick points on the map.
		posX, posY, posOk := cur.PosX, cur.PosY, !isNaN32(cur.PosX) && !isNaN32(cur.PosY)
		if ptr.Valid && !isNaN32(ptr.X) && !isNaN32(ptr.Y) {
			posX, posY, posOk = ptr.X-cur.OriginX, ptr.Y-cur.OriginY, true
		}
		if posOk && flags.HasContainsPointer() {
			m.hover, m.hoverOk = m.view.ContainerPointToLatLng(Point{float64(posX), float64(posY)}), true
		}
		if posOk && flags.HasPrimaryClicked() {
			m.clicked, m.clickedOk = m.view.ContainerPointToLatLng(Point{float64(posX), float64(posY)}), true
			// The release of a click surrenders the focus the press asked
			// for (egui's SurrenderFocusOn::Clicks, and during a click only
			// the clicked widget counts as hovered), so it is asked for again
			// here — a drag has no click and keeps the press's focus.
			m.focusKeys()
		}
	}
	if m.view.Loaded() && !m.opts.NoKeyboard {
		m.handleKeys(sm)
	}

	// ---- view → pyramid ----
	ev := m.view.TakeEvents()
	if sizeChanged && m.view.Loaded() {
		ev.Move = true
	}
	again := false
	if !m.opts.NoTiles {
		m.pyramid.Sync(m.view, ev)
		// ---- arrivals ----
		for _, a := range m.loader.Drain() {
			if !a.Failed {
				m.pixels[a.Wrapped] = &tilePixels{px: a.Pixels, w: a.W, h: a.H}
			}
			m.pyramid.TileReady(m.view, a.Coords, a.Failed, now)
		}
		again = m.pyramid.Tick(m.view, now)
	}

	// ---- draw ----
	c.PaintClipPush(0, 0, w, h).Send()
	if !m.opts.NoTiles {
		for _, td := range m.pyramid.Draw(m.view, now) {
			px, ok := m.pixels[td.Wrapped]
			if !ok || px == nil {
				continue
			}
			id := m.ids.PrepareStr("tile-" + td.Wrapped.Key()).Derive()
			send := m.tracker.PixelsToSendFor(td.Wrapped, id, m.srcGen, px.px)
			if len(send) > 0 {
				m.bytesShipped += uint64(len(send)) * 4
			}
			r := td.Rect
			c.PaintImage(id, float32(r.Min.X), float32(r.Min.Y), float32(r.Max.X), float32(r.Max.Y),
				uint32(px.w), uint32(px.h), m.srcGen, send).Opacity(float32(td.Opacity)).Send()
		}
	}
	if overlay != nil {
		overlay(Projector{view: m.view, m: m})
	}
	if r, ok := m.box.rect(); ok {
		c.PaintRectFilled(float32(r.Min.X), float32(r.Min.Y), float32(r.Max.X), float32(r.Max.Y), 0, color.Hex(0xffffff40)).Send()
		c.PaintRectStroke(float32(r.Min.X), float32(r.Min.Y), float32(r.Max.X), float32(r.Max.Y), 0, color.Hex(0x3388ffff), 2).Send()
	}
	if !m.opts.HideAttribution && !m.opts.NoTiles && m.opts.Source.Attribution != "" {
		m.paintAttribution(w, h)
	}
	c.PaintClipPop().Send()

	// The drag-owning region last, so it wins the hit test, over a canvas that
	// senses click and hover only (ADR-0204 §SD6).
	c.PaintSenseRegion(m.ids.PrepareStr("portolan-area"), 0, 0, w, h).Send()
	c.PaintCanvas(m.ids.PrepareStr("portolan-canvas"), w, h).
		Background(color.Hex(m.opts.Background)).
		Sense(true, false, true).
		CaptureZoom().
		CaptureScroll().
		Send()

	if again || m.view.Animating() || m.wheel.hasStart || m.pinch.active ||
		(!m.opts.NoTiles && (m.loader.Pending() > 0 || m.pyramid.IsLoading())) {
		c.RequestRepaintAfter(1.0 / 60)
	}
	m.events = ev
}

func (m *Map) paintAttribution(w, h float32) {
	const fontSize = 10.0
	text := m.opts.Source.Attribution
	approxW := float32(len(text))*fontSize*0.55 + 8
	c.PaintRectFilled(w-approxW, h-fontSize-6, w, h, 0, color.Hex(0xffffffb0)).Send()
	c.PaintText(w-4, h-3, 2, 2, text, fontSize, color.Hex(0x333333ff)).Send()
}

// handleInput feeds last frame's registers to the handlers: the drag or a
// shift-drag box (the sense region's press origin, positions from the
// frame-end pointer — M0's recipe), the wheel, the pinch, the double-click.
func (m *Map) handleInput(cur, areaCur c.CanvasCursorValue, areaOk bool, flags c.ResponseFlagsE,
	wheel c.CanvasWheelValue, ptr c.PointerValue, now time.Time) {
	v := m.view
	posX, posY, posOk := cur.PosX, cur.PosY, !isNaN32(cur.PosX) && !isNaN32(cur.PosY)
	if ptr.Valid && !isNaN32(ptr.X) && !isNaN32(ptr.Y) {
		posX, posY, posOk = ptr.X-cur.OriginX, ptr.Y-cur.OriginY, true
	}
	pos := Point{float64(posX), float64(posY)}

	if flags.HasIsPointerButtonDown() && posOk && !m.drag.active && !m.box.active && !m.pressOriginOk {
		m.pressOriginX, m.pressOriginY, m.pressOriginOk = posX, posY, true
		// A press focuses the map, so the arrows work straight after without
		// a Tab. The capture Frame does not sense clicks (it must not — see
		// the tree widget's keys.go), so focus is asked for here.
		m.focusKeys()
	}
	if !flags.HasIsPointerButtonDown() && !m.drag.active && !m.box.active {
		m.pressOriginOk = false
	}
	if flags.HasDragStarted() && posOk {
		var origin Point
		switch {
		case areaOk && !isNaN32(areaCur.PosX) && !isNaN32(areaCur.PosY):
			// The sense region's drag-started row: the press origin.
			origin = Point{float64(areaCur.PosX), float64(areaCur.PosY)}
		case m.pressOriginOk:
			origin = Point{float64(m.pressOriginX), float64(m.pressOriginY)}
		case m.prevPosOk:
			origin = Point{float64(m.prevPosX), float64(m.prevPosY)}
		default:
			origin = pos
		}
		shift := areaCur.Shift()
		if !areaOk {
			shift = cur.Shift()
		}
		switch {
		case !m.opts.NoBoxZoom && shift:
			m.box.begin(origin)
		case !m.opts.NoDragging:
			m.drag.start(v, origin, now, m.hopts)
		}
	}
	switch {
	case m.box.active:
		if (flags.HasDragged() || flags.HasDragStopped()) && posOk {
			m.box.move(pos)
		}
		if flags.HasDragStopped() {
			m.box.finish(v)
			m.pressOriginOk = false
		}
	case m.drag.active:
		if (flags.HasDragged() || flags.HasDragStopped()) && posOk {
			m.drag.move(v, pos, now, m.hopts)
		}
		if flags.HasDragStopped() {
			m.drag.end(v, now, m.hopts)
			m.pressOriginOk = false
		}
	}
	m.prevPosX, m.prevPosY, m.prevPosOk = posX, posY, posOk && flags.HasContainsPointer()

	if !m.drag.active && !m.box.active {
		anchor := m.wheelAnchor(wheel)
		if !m.opts.NoPinchZoom && wheel.Zoom > 0 && wheel.Zoom != 1 {
			m.pinch.step(v, float64(wheel.Zoom), anchor, now, m.hopts)
		}
		if !m.opts.NoScrollWheelZoom && wheel.ScrollY != 0 {
			m.wheel.wheel(float64(wheel.ScrollY), anchor, now)
		}
	}
	m.wheel.tick(v, now, m.hopts)
	m.pinch.tick(v, now)

	if !m.opts.NoDoubleClickZoom && flags.HasDoubleClicked() && posOk {
		doubleClick(v, pos, cur.Shift())
	}
}

// focusKeys asks egui to focus the key-capturing Frame.
func (m *Map) focusKeys() {
	if !m.opts.NoKeyboard && m.keyFrameID != 0 {
		c.RequestFocus(m.keyFrameID)
	}
}

// handleKeys applies the keys the capture Frame ate while focused.
func (m *Map) handleKeys(sm *c.StateManager) {
	caps := sm.GetCapturedKeys(widgethandle.Make(m.keyFrameID))
	for _, k := range caps {
		switch k.Code {
		case keycodes.ArrowLeft:
			keyboardPan(m.view, -1, 0, k.Shift(), m.hopts)
		case keycodes.ArrowRight:
			keyboardPan(m.view, 1, 0, k.Shift(), m.hopts)
		case keycodes.ArrowUp:
			keyboardPan(m.view, 0, -1, k.Shift(), m.hopts)
		case keycodes.ArrowDown:
			keyboardPan(m.view, 0, 1, k.Shift(), m.hopts)
		case keycodes.Escape:
			m.box.cancel()
		}
	}
}

func (m *Map) wheelAnchor(wheel c.CanvasWheelValue) Point {
	if !isNaN32(wheel.HoverX) && !isNaN32(wheel.HoverY) {
		return Point{float64(wheel.HoverX), float64(wheel.HoverY)}
	}
	return m.size.DivideBy(2)
}

func isNaN32(v float32) bool { return math.IsNaN(float64(v)) }
