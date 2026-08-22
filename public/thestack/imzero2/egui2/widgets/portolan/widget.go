package portolan

import (
	"math"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Options configures a Map. The zero value is unusable without a Source; the
// rest default to Leaflet's behaviour with this tree's two deliberate
// differences (ZoomSnap 0, continuous zoom; no animation until M3).
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
	// The interaction handlers are on unless disabled.
	NoDragging, NoScrollWheelZoom, NoDoubleClickZoom bool
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
// painter-lane canvas. Create one per map instance and keep it; call Render
// every frame (ADR-0204 §SD1, §SD2).
type Map struct {
	ids     *c.WidgetIdStack
	opts    Options
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
	// unwrappedOf remembers which wrapped tile each requested unwrapped tile
	// maps to, for the loader's arrivals and for Draw.
	wrappedOf map[TileCoords]TileCoords

	// input
	dragging                   bool
	dragOriginX, dragOriginY   float32
	dragStartCenter            LatLng
	dragLastOffX, dragLastOffY float64
	prevPosX, prevPosY         float32
	prevPosOk                  bool
	pressOriginX, pressOriginY float32
	pressOriginOk              bool
	wheelDelta                 float64
	wheelStart                 time.Time
	wheelPos                   Point

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

// Wheel constants — Leaflet's ScrollWheelZoomHandler.
const (
	wheelDebounce         = 40 * time.Millisecond
	wheelPxPerZoom        = 60.0
	defaultBackgroundRGBA = 0xd8d8dcff
)

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
	view := NewView(ViewOptions{
		CRS: opts.CRS, MinZoom: opts.MinZoom, MaxZoom: opts.MaxZoom,
		HasMinZoom: opts.HasMinZoom, HasMaxZoom: opts.HasMaxZoom,
		ZoomSnap: opts.ZoomSnap, ZoomDelta: opts.ZoomDelta, MaxBounds: opts.MaxBounds,
	})
	view.SetLayerZoomLimits(opts.Source.MinZoom, opts.Source.MaxZoom, true, !math.IsInf(opts.Source.MaxZoom, 1))
	m := &Map{
		ids:            ids,
		opts:           opts,
		view:           view,
		pyramid:        NewPyramid(opts.Source),
		loader:         NewTileLoader(opts.Loader),
		tracker:        c.NewImageVersionTracker[TileCoords](),
		overlayTracker: c.NewImageVersionTracker[string](),
		pixels:         make(map[TileCoords]*tilePixels, 64),
		holders:        make(map[TileCoords]int, 64),
		wrappedOf:      make(map[TileCoords]TileCoords, 64),
	}
	m.pyramid.OnRequest = m.requestTile
	m.pyramid.OnAbort = func(coords TileCoords) { m.loader.Cancel(coords); m.dropHolder(coords) }
	m.pyramid.OnUnload = m.dropHolder
	return m
}

// Close stops the loader. The map must not be rendered afterwards.
func (m *Map) Close() { m.loader.Close() }

// View is the map's camera — use it to SetView/FitBounds/PanTo between
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

// Render draws the map into a w×h canvas at the current layout position and
// applies the previous frame's input. overlay, when not nil, is called after
// the tiles and before the canvas is flushed, to paint on top with c.Paint*
// in canvas coordinates through the Projector.
func (m *Map) Render(w, h float32, overlay func(Projector)) {
	for range c.IdScope(m.ids.PrepareStr("portolan")) {
		m.frame(w, h, overlay)
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
		}
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
			send := m.tracker.PixelsToSendFor(td.Wrapped, id, 1, px.px)
			if len(send) > 0 {
				m.bytesShipped += uint64(len(send)) * 4
			}
			r := td.Rect
			c.PaintImage(id, float32(r.Min.X), float32(r.Min.Y), float32(r.Max.X), float32(r.Max.Y),
				uint32(px.w), uint32(px.h), 1, send).Opacity(float32(td.Opacity)).Send()
		}
	}
	if overlay != nil {
		overlay(Projector{view: m.view, m: m})
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

	if again || (!m.opts.NoTiles && (m.loader.Pending() > 0 || m.pyramid.IsLoading())) || m.wheelDelta != 0 {
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

// handleInput applies last frame's gestures to the view: the drag (M0's
// recipe — the sense region's press origin, positions from the frame-end
// pointer, the view at the press plus the offset), the wheel (Leaflet's
// handler: accumulate, 40 ms debounce, sigmoid, anchored), the pinch as a zoom
// factor, and the double-click.
func (m *Map) handleInput(cur, areaCur c.CanvasCursorValue, areaOk bool, flags c.ResponseFlagsE,
	wheel c.CanvasWheelValue, ptr c.PointerValue, now time.Time) {
	posX, posY, posOk := cur.PosX, cur.PosY, !isNaN32(cur.PosX) && !isNaN32(cur.PosY)
	if ptr.Valid && !isNaN32(ptr.X) && !isNaN32(ptr.Y) {
		posX, posY, posOk = ptr.X-cur.OriginX, ptr.Y-cur.OriginY, true
	}

	if !m.opts.NoDragging {
		if flags.HasIsPointerButtonDown() && posOk && !m.dragging && !m.pressOriginOk {
			m.pressOriginX, m.pressOriginY, m.pressOriginOk = posX, posY, true
		}
		if !flags.HasIsPointerButtonDown() && !m.dragging {
			m.pressOriginOk = false
		}
		if flags.HasDragStarted() && posOk {
			m.dragging = true
			switch {
			case areaOk && !isNaN32(areaCur.PosX) && !isNaN32(areaCur.PosY):
				m.dragOriginX, m.dragOriginY = areaCur.PosX, areaCur.PosY
			case m.pressOriginOk:
				m.dragOriginX, m.dragOriginY = m.pressOriginX, m.pressOriginY
			case m.prevPosOk:
				m.dragOriginX, m.dragOriginY = m.prevPosX, m.prevPosY
			default:
				m.dragOriginX, m.dragOriginY = posX, posY
			}
			m.dragStartCenter = m.view.Center()
			m.dragLastOffX, m.dragLastOffY = 0, 0
			m.view.MoveStart(false)
		}
		if m.dragging && (flags.HasDragged() || flags.HasDragStopped()) && posOk {
			offX := float64(posX - m.dragOriginX)
			offY := float64(posY - m.dragOriginY)
			if offX != m.dragLastOffX || offY != m.dragLastOffY {
				start := m.view.Project(m.dragStartCenter)
				m.view.MoveTo(m.view.Unproject(start.Subtract(Point{offX, offY})), m.view.Zoom())
				m.dragLastOffX, m.dragLastOffY = offX, offY
			}
		}
		if flags.HasDragStopped() && m.dragging {
			m.dragging = false
			m.pressOriginOk = false
			m.view.MoveEnd(false)
		}
	}
	m.prevPosX, m.prevPosY, m.prevPosOk = posX, posY, posOk && flags.HasContainsPointer()

	if !m.opts.NoScrollWheelZoom && !m.dragging {
		// ctrl+wheel and a pinch arrive as a zoom factor: apply at once,
		// anchored at the pointer.
		if wheel.Zoom > 0 && wheel.Zoom != 1 {
			anchor := m.wheelAnchor(wheel)
			z := m.view.LimitZoom(m.view.Zoom() + math.Log2(float64(wheel.Zoom)))
			if z != m.view.Zoom() {
				m.view.SetZoomAround(anchor, z)
			}
		}
		// A plain wheel accumulates, and zooms 40 ms after the first notch —
		// Leaflet's ScrollWheelZoomHandler.
		if wheel.ScrollY != 0 {
			m.wheelDelta += float64(wheel.ScrollY)
			m.wheelPos = m.wheelAnchor(wheel)
			if m.wheelStart.IsZero() {
				m.wheelStart = now
			}
		}
		if m.wheelDelta != 0 && now.Sub(m.wheelStart) >= wheelDebounce {
			m.performWheelZoom()
		}
	}

	if !m.opts.NoDoubleClickZoom && flags.HasDoubleClicked() && posOk {
		delta := m.view.ZoomDelta()
		if cur.Shift() {
			delta = -delta
		}
		m.view.SetZoomAround(Point{float64(posX), float64(posY)}, m.view.Zoom()+delta)
	}
}

func (m *Map) wheelAnchor(wheel c.CanvasWheelValue) Point {
	if !isNaN32(wheel.HoverX) && !isNaN32(wheel.HoverY) {
		return Point{float64(wheel.HoverX), float64(wheel.HoverY)}
	}
	return m.size.DivideBy(2)
}

// performWheelZoom is Leaflet's _performZoom: the accumulated delta through a
// sigmoid, snapped, applied about the pointer.
func (m *Map) performWheelZoom() {
	delta := m.wheelDelta
	m.wheelDelta = 0
	m.wheelStart = time.Time{}
	zoom := m.view.Zoom()
	snap := m.view.ZoomSnap()
	d2 := delta / (wheelPxPerZoom * 4)
	d3 := 4 * math.Log(2/(1+math.Exp(-math.Abs(d2)))) / math.Ln2
	d4 := d3
	if snap != 0 {
		d4 = math.Ceil(d3/snap) * snap
	}
	if delta < 0 {
		d4 = -d4
	}
	dz := m.view.LimitZoom(zoom+d4) - zoom
	if dz == 0 {
		return
	}
	m.view.SetZoomAround(m.wheelPos, zoom+dz)
}

func isNaN32(v float32) bool { return math.IsNaN(float64(v)) }
