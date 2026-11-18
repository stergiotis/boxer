package portolan

import (
	"math"
	"sort"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/camera"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Options configures a Map. The zero value is unusable without a Source; the
// rest default to Leaflet's behaviour with this tree's one deliberate
// difference, ZoomSnap 0 (continuous zoom).
type Options struct {
	// Source is where tiles come from; Loader how they are fetched.
	Source TileSource
	Loader LoaderOptions
	// Center and Zoom are the initial view, set on the first frame; with
	// NoInitialView the map stays unloaded until View().SetView (or
	// FitBounds, FlyTo, …) is called.
	Center        LatLng
	Zoom          float64
	NoInitialView bool
	// CRS defaults to EPSG3857.
	CRS CRSI
	// MinZoom/MaxZoom override the source's when Has*; MaxBounds keeps the
	// view inside an area; ZoomSnap/ZoomDelta as in ViewOptions.
	MinZoom, MaxZoom       float64
	HasMinZoom, HasMaxZoom bool
	MaxBounds              LatLngBounds
	ZoomSnap, ZoomDelta    float64
	// The interaction handlers are on unless disabled; Handlers carries
	// their knobs, zero meaning Leaflet's value field by field.
	NoDragging, NoScrollWheelZoom, NoDoubleClickZoom bool
	NoPinchZoom, NoBoxZoom, NoKeyboard               bool
	// HostKeys are keys the map takes while it has focus and hands to its
	// host in [Events.Keys] instead of acting on them — Page Up and Page
	// Down for a document, the arrows when the host moves by them. A key in
	// both sets is the host's. A press on the map focuses it for them even
	// with NoKeyboard; [Map.SetHostKeys] changes them later.
	HostKeys keycodes.Mask
	// WheelPans makes the wheel pan the view, up and down and — with the
	// shift key, or a horizontal wheel — sideways, as a document scrolls;
	// the wheel then zooms only with the zoom modifier, which egui reports
	// as a zoom, as it does a pinch. It overrides the wheel's zoom.
	WheelPans       bool
	NoZoomAnimation bool
	Handlers        HandlerOptions
	// Background is the colour under the tiles, 0xRRGGBBAA; 0 takes the
	// design system's panel background (styletokens.NeutralBgPanel), which is
	// also the sea under landoverlay's default land.
	Background uint32
	// HideAttribution suppresses the source's attribution label.
	HideAttribution bool
	// NoTiles draws no basemap at all — background and overlays only; the
	// offline mode play uses. SetNoTiles toggles it later.
	NoTiles bool
	// NearestTiles draws tiles without interpolation, so a magnified tile
	// shows its pixels as squares. SetNearestTiles changes it later.
	NearestTiles bool
	// UploadBudgetBytes bounds the tile pixels shipped to the host in one
	// frame; 0 is no bound. Once a frame has shipped this much, a tile that
	// would need an upload is drawn from the texture the host already holds
	// — or not at all, if it has none — and ships on a later frame. Tiles
	// ship in the order they are drawn, back to front, so the levels that
	// stand in beneath the current one go first. A frame always ships at
	// least one tile, so the bound is passed by at most one tile's size.
	UploadBudgetBytes int64
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

// Camera is the frame's transform as the shared 2D camera, for a widget
// drawn over the map inside this overlay — see [View.Camera], including its
// note on which recipe stays exact at high zoom.
func (p Projector) Camera(refZoom float64) camera.Camera { return p.view.Camera(refZoom) }

// CameraAt is [View.CameraAt]: the frame's transform with world units
// measured from a local origin, which is the form to prefer.
func (p Projector) CameraAt(refZoom float64, origin Point) camera.Camera {
	return p.view.CameraAt(refZoom, origin)
}

// Map is the slippy-map widget: a View, a Pyramid and a TileLoader behind a
// painter-lane canvas, with Leaflet's handlers between the lane's registers
// and the view. Create one per map instance and keep it; call Render every
// frame (ADR-0204 §SD1, §SD2).
type Map struct {
	ids     *c.WidgetIdStack
	key     string // the id scope every derivation sits under
	opts    Options
	hopts   HandlerOptions
	view    *View
	pyramid *Pyramid
	loader  *TileLoader
	tracker *c.ImageVersionTracker[TileCoords]
	// overlayTracker is the send-once record of Projector.Image rasters.
	overlayTracker *c.ImageVersionTracker[string]
	// scratch is the vector overlays' working storage, reused across rings
	// and frames.
	scratch overlayScratch
	// pixels holds decoded tiles by WRAPPED coords, shared by the unwrapped
	// tiles that show the same source tile; holders counts them so pixels go
	// when the last holder is pruned.
	pixels  map[TileCoords]*tilePixels
	holders map[TileCoords]int
	// wrappedOf remembers which wrapped tile each requested unwrapped tile
	// maps to, for the loader's arrivals and for Draw.
	wrappedOf map[TileCoords]TileCoords
	// errTiles marks wrapped tiles whose pixels are the source's error image,
	// so a later request for them goes to the loader again instead of
	// reusing the error image as if it were the tile.
	errTiles map[TileCoords]struct{}
	// srcGen counts tile sources; it is the high half of the tiles' image
	// version (tileVersion), so a switch re-uploads under the same ids
	// instead of showing the old source's pixels.
	srcGen uint64
	// contentRev counts InvalidateTiles and SetSource calls: the revision a
	// pixel-source tile is requested under. sourceRev is the revision the
	// last SetSource moved it to; a tile answered under an older one was
	// answered by a source no longer shown. refreshing holds the tiles asked
	// for again while their previous pixels stay on screen.
	contentRev uint64
	sourceRev  uint64
	refreshing map[TileCoords]struct{}

	// pointerVeto is set for one frame by SetPointerVeto: a widget drawn
	// over the map has taken this frame's pointer (ADR-0228 §SD2).
	pointerVeto bool

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
	// keys are the host's keys taken this frame (Options.HostKeys).
	keys []c.CapturedKey

	// per-frame readback
	events    ViewEvents
	size      Point
	originX   float32 // the canvas's screen origin last frame (R24), for scenes
	originY   float32
	originOk  bool
	hover     LatLng
	hoverOk   bool
	clicked   LatLng
	clickedOk bool
	// boxSelect turns a plain drag into a box that is reported rather than
	// zoomed to (SetBoxSelect); boxSelecting marks a box begun that way, and
	// selected/selectedOk the box a finished one reported this frame.
	boxSelect    bool
	boxSelecting bool
	selected     LatLngBounds
	selectedOk   bool
	// instrumentation
	bytesShipped uint64
	frames       uint64
	reships      uint64
}

type tilePixels struct {
	px   []uint32
	w, h int
	// gen counts the pixels stored under this wrapped key — an error image
	// replaced by the tile, or the reverse — so the host's texture, which
	// re-uploads only on a version change, follows.
	gen uint64
}

// tileVersion is the tile's paintImage content version: the source
// generation above the per-tile pixel generation.
func (m *Map) tileVersion(t *tilePixels) uint64 { return m.srcGen<<32 | t.gen }

// noPixels is the non-nil empty slice that asks the host to draw the texture
// it holds.
var noPixels = []uint32{}

// mapKeyMask is what the map eats while focused: the arrows pan, Escape
// cancels a box zoom. The zoom keys wait for keycodes the vocabulary lacks.
var mapKeyMask = keycodes.MaskOf(keycodes.ArrowUp, keycodes.ArrowDown, keycodes.ArrowLeft, keycodes.ArrowRight, keycodes.Escape)

// New makes a map. Every id the widget derives is scoped under scopeKey on
// ids, so two maps on one stack differ by scopeKey alone; empty takes
// "portolan". opts.Source defaults to OpenStreetMap.
func New(ids *c.WidgetIdStack, scopeKey string, opts Options) *Map {
	if scopeKey == "" {
		scopeKey = "portolan"
	}
	if opts.Source.URLTemplate == "" && opts.Loader.Pixels == nil {
		opts.Source = NewTileSource("https://tile.openstreetmap.org/{z}/{x}/{y}.png")
	} else {
		opts.Source = opts.Source.Normalized()
	}
	if opts.Background == 0 {
		opts.Background = styletokens.NeutralBgPanel.AsHex()
	}
	hopts := opts.Handlers.withDefaults()
	view := NewView(ViewOptions{
		CRS: opts.CRS, MinZoom: opts.MinZoom, MaxZoom: opts.MaxZoom,
		HasMinZoom: opts.HasMinZoom, HasMaxZoom: opts.HasMaxZoom,
		ZoomSnap: opts.ZoomSnap, ZoomDelta: opts.ZoomDelta, MaxBounds: opts.MaxBounds,
	})
	view.SetLayerZoomLimits(opts.Source.MinZoom, opts.Source.MaxZoom, true, !math.IsInf(opts.Source.MaxZoom, 1))
	view.SetZoomAnimation(!opts.NoZoomAnimation)
	m := &Map{
		ids:            ids,
		key:            scopeKey,
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
		errTiles:       make(map[TileCoords]struct{}, 8),
		refreshing:     make(map[TileCoords]struct{}, 16),
		srcGen:         1,
	}
	m.wirePyramid()
	return m
}

func (m *Map) wirePyramid() {
	m.pyramid.OnRequest = m.requestTile
	m.pyramid.OnAbort = func(coords TileCoords) { m.loader.Cancel(coords); m.dropHolder(coords) }
	m.pyramid.OnUnload = func(coords TileCoords) { m.loader.Cancel(coords); m.dropHolder(coords) }
}

// Handles returns the canvas and area handles this map's sense surfaces use,
// for a widget hosted in its overlay that reads the same registers
// (ADR-0228 §SD1). The canvas carries containment, hover and the cursor row;
// the area is the drag-owning region the map emits last. They are derived,
// not remembered, so this is valid before the first Render.
func (m *Map) Handles() (canvas, area widgethandle.WidgetHandle) {
	for range c.IdScope(m.ids.PrepareStr(m.key)) {
		// Render draws the canvas inside its key-capturing Frame, which
		// scopes the ids beneath it; the same scope is opened here so these
		// are the handles the frame's registers are keyed by.
		for range c.IdScope(m.ids.PrepareStr("portolan-keys")) {
			canvas = widgethandle.Make(m.ids.PrepareStr("portolan-canvas").Derive())
			area = widgethandle.Make(m.ids.PrepareStr("portolan-area").Derive())
		}
	}
	return
}

// SetPointerVeto tells the map that a widget drawn over it has taken this
// frame's pointer gesture (ADR-0228 §SD2), so the map starts no drag, no box
// zoom and no double-click zoom from it. A gesture already in flight
// finishes, and the wheel, the pinch and the keyboard are untouched — they
// are the host's in every case. The flag is cleared by the Render it applies
// to, so set it every frame it should hold.
func (m *Map) SetPointerVeto(on bool) { m.pointerVeto = on }

// SetHostKeys sets [Options.HostKeys] from here on.
func (m *Map) SetHostKeys(keys keycodes.Mask) { m.opts.HostKeys = keys }

// SetWheelPans sets [Options.WheelPans] from here on.
func (m *Map) SetWheelPans(on bool) { m.opts.WheelPans = on }

// SetBoxSelect turns box selection on or off. On, a plain drag draws the
// box-zoom rectangle and, when released, reports its bounds in
// [Events.Selected] instead of zooming to it; panning by drag is off while it
// holds, and the wheel, the pinch, the keys and Escape (which drops a box in
// progress) work as before. The setting holds until changed.
func (m *Map) SetBoxSelect(on bool) { m.boxSelect = on }

// Source is the tile source in use.
func (m *Map) Source() TileSource { return m.opts.Source }

// SetSource switches the tile source: requests to the old one are cancelled
// and its tiles dropped, the pyramid restarts on the new one at the current
// view, and the view's layer zoom limits follow the source. An empty template
// is ignored, except on a map whose tiles come from a pixel source, where
// the template is not used. On such a map the switch is also a new content
// revision: a tile the pixel source answers for a request made before it is
// not shown, and is asked for again — the pixel source is one object across
// switches, so only the revision tells its answers apart. The pixel source
// must therefore answer for the new content by the time SetSource is called.
func (m *Map) SetSource(src TileSource) {
	if src.URLTemplate == "" && m.opts.Loader.Pixels == nil {
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
	m.errTiles = make(map[TileCoords]struct{}, 8)
	m.refreshing = make(map[TileCoords]struct{}, 16)
	m.srcGen++
	m.contentRev++
	m.sourceRev = m.contentRev
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
// Events is what one Render produced from the previous frame's input
// (ADR-0267 W3): the view's events and a primary click, if there was one.
// Hover is continuous and stays a getter.
type Events struct {
	ViewEvents
	// Clicked is the geographic point of a primary click last frame — a
	// press and release without a drag in between; ClickedOk says there
	// was one.
	Clicked   LatLng
	ClickedOk bool
	// Selected is the box a drag drew and released last frame while box
	// selection was on (SetBoxSelect); SelectedOk says there was one.
	Selected   LatLngBounds
	SelectedOk bool
	// Boxing is the box being dragged, as it stands this frame, whether it
	// will be selected or zoomed to; BoxingOk says one is being dragged.
	Boxing   LatLngBounds
	BoxingOk bool
	// Keys are the presses of [Options.HostKeys] the map took last frame,
	// auto-repeats included, in order. The slice is the map's and is valid
	// until the next Render.
	Keys []c.CapturedKey
}

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

// CanvasOrigin is the screen position of the canvas's top-left corner as the
// host reported it last frame — what a headless scene needs to aim a pointer
// at the map, since the canvas is painter-only and absent from the
// accessibility tree. ok is false before the first frame with a live canvas.
func (m *Map) CanvasOrigin() (x, y float32, ok bool) { return m.originX, m.originY, m.originOk }

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
	if _, isErr := m.errTiles[wrapped]; !isErr {
		if px, ok := m.pixels[wrapped]; ok && px != nil {
			// Already decoded for another unwrapped copy: ready at once.
			m.pyramid.TileReady(m.view, coords, false, time.Now())
			return
		}
	}
	if m.opts.Loader.Pixels != nil {
		m.loader.RequestPixels(coords, wrapped, m.contentRev)
		return
	}
	m.loader.Request(coords, wrapped, m.opts.Source.URL(wrapped, m.pyramid.GlobalRows()))
}

// InvalidateTiles says that every tile's content is out of date: what a
// pixel source returns has changed (ADR-0373 §SD7). The tiles on screen stay
// there and each is asked for again, coarser levels first; a tile is
// replaced when its new pixels arrive. On a map whose tiles are fetched by
// URL it does nothing — a URL's content is the source's business.
func (m *Map) InvalidateTiles() {
	if m.opts.Loader.Pixels == nil {
		return
	}
	m.contentRev++
	held := make([]TileCoords, 0, len(m.wrappedOf))
	for coords, wrapped := range m.wrappedOf {
		if px := m.pixels[wrapped]; px != nil {
			held = append(held, coords)
		}
	}
	sort.Slice(held, func(a, b int) bool {
		ca, cb := held[a], held[b]
		if ca.Z != cb.Z {
			return ca.Z < cb.Z
		}
		if ca.Y != cb.Y {
			return ca.Y < cb.Y
		}
		return ca.X < cb.X
	})
	for _, coords := range held {
		m.refreshing[coords] = struct{}{}
		m.loader.RequestPixels(coords, m.wrappedOf[coords], m.contentRev)
	}
}

// SetNearestTiles turns interpolation of the tiles off or back on.
func (m *Map) SetNearestTiles(on bool) { m.opts.NearestTiles = on }

func (m *Map) dropHolder(coords TileCoords) {
	wrapped, ok := m.wrappedOf[coords]
	if !ok {
		return
	}
	delete(m.wrappedOf, coords)
	delete(m.refreshing, coords)
	if m.holders[wrapped]--; m.holders[wrapped] <= 0 {
		delete(m.holders, wrapped)
		delete(m.pixels, wrapped)
		delete(m.errTiles, wrapped)
		m.tracker.Forget(wrapped)
	}
}

// absorbArrivals stores the loader's decoded tiles and tells the pyramid.
// A failed tile with an ErrorTileURL is re-pointed at it first (TileLayer's
// _tileOnError) and is ready — as failed — when the error image arrives; the
// byte cache makes that image one fetch.
func (m *Map) absorbArrivals(arrivals []TileArrival, now time.Time) {
	errURL := m.opts.Source.ErrorTileURL
	var again, unseen []TileArrival
	defer func() {
		for _, a := range again {
			if _, held := m.wrappedOf[a.Coords]; held {
				m.refreshing[a.Coords] = struct{}{}
				m.loader.RequestPixels(a.Coords, a.Wrapped, m.contentRev)
			}
		}
		for _, a := range unseen {
			if wrapped, held := m.wrappedOf[a.Coords]; held {
				m.loader.RequestPixels(a.Coords, wrapped, m.contentRev)
			}
		}
	}()
	for _, a := range arrivals {
		if _, held := m.wrappedOf[a.Coords]; !held {
			// Requested under a source since switched: nothing holds it.
			continue
		}
		if m.opts.Loader.Pixels != nil && a.Revision < m.sourceRev {
			// Answered for the source before the last switch, at coordinates
			// the new one holds as well: these are not its pixels. The tile
			// is still waited for, so it is asked of the new source.
			unseen = append(unseen, a)
			continue
		}
		if a.Failed && errURL != "" && a.URL != errURL {
			m.loader.Request(a.Coords, a.Wrapped, errURL)
			continue
		}
		isErrImage := errURL != "" && a.URL == errURL
		// A pixel-source tile asked for again keeps its place in the pyramid
		// and only swaps its pixels; one that answers an older revision than
		// the current one is asked for once more.
		_, refresh := m.refreshing[a.Coords]
		stale := m.opts.Loader.Pixels != nil && a.Revision < m.contentRev
		if refresh {
			delete(m.refreshing, a.Coords)
		}
		if stale {
			again = append(again, a)
		}
		if refresh && a.Failed {
			continue
		}
		if !a.Failed {
			var gen uint64
			if old := m.pixels[a.Wrapped]; old != nil {
				gen = old.gen + 1
			}
			m.pixels[a.Wrapped] = &tilePixels{px: a.Pixels, w: a.W, h: a.H, gen: gen}
			if isErrImage {
				m.errTiles[a.Wrapped] = struct{}{}
			} else {
				delete(m.errTiles, a.Wrapped)
			}
		}
		if refresh {
			continue
		}
		m.pyramid.TileReady(m.view, a.Coords, a.Failed || isErrImage, now)
	}
}

// RenderFill is Render sized to the pane the map sits in, as reported by the
// layout probe one frame late; fallbackW/fallbackH are used on the first
// frame and whenever the probe has nothing (a hidden tab).
func (m *Map) RenderFill(fallbackW, fallbackH float32, overlay func(Projector)) (ev Events) {
	w, h := fallbackW, fallbackH
	// The probe's slot is keyed under the map's own scope (ADR-0267 W7); the
	// scope is opened for the derivation alone and emits nothing.
	for range c.IdScope(m.ids.PrepareStr(m.key)) {
		if pw, ph, ok := c.CapturePaneSize(m.ids.ProbeSeq("pane")); ok && pw > 0 && ph > 0 {
			w, h = pw, ph
		}
	}
	return m.Render(w, h, overlay)
}

// Render draws the map into a w×h canvas at the current layout position and
// applies the previous frame's input. overlay, when not nil, is called after
// the tiles and before the canvas is flushed, to paint on top with c.Paint*
// in canvas coordinates through the Projector.
func (m *Map) Render(w, h float32, overlay func(Projector)) (ev Events) {
	defer func() {
		ev = Events{ViewEvents: m.events, Clicked: m.clicked, ClickedOk: m.clickedOk, Selected: m.selected, SelectedOk: m.selectedOk, Keys: m.keys}
		if r, ok := m.box.rect(); ok && m.view.Loaded() {
			ev.Boxing = LatLngBoundsOf(m.view.ContainerPointToLatLng(r.Min), m.view.ContainerPointToLatLng(r.Max))
			ev.BoxingOk = true
		}
	}()
	for range c.IdScope(m.ids.PrepareStr(m.key)) {
		// The key-capturing Frame around the body: while it has focus (a
		// click on the map gives it), the arrows pan and Escape cancels a box
		// zoom (ADR-0177), and the host's keys are taken for it.
		kf := c.Frame(m.ids.PrepareStr("portolan-keys")).CaptureKeys(uint64(m.keyMask()))
		m.keyFrameID = kf.Id()
		for range kf.KeepIter() {
			m.frame(w, h, overlay)
		}
	}
	return
}

func (m *Map) frame(w, h float32, overlay func(Projector)) {
	sm := c.CurrentApplicationState.StateManager
	now := time.Now()
	m.frames++
	size := Point{float64(w), float64(h)}
	sizeChanged := size != m.size
	m.size = size
	m.view.SetSize(size)
	if !m.view.Loaded() && !m.opts.NoInitialView {
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

	m.hoverOk, m.clickedOk, m.selectedOk = false, false, false
	if live && !isNaN32(cur.OriginX) && !isNaN32(cur.OriginY) {
		m.originX, m.originY, m.originOk = cur.OriginX, cur.OriginY, true
	}
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
	m.keys = m.keys[:0]
	if m.takesKeys() {
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
		m.absorbArrivals(m.loader.Drain(), now)
		again = m.pyramid.Tick(m.view, now)
	}

	// ---- draw ----
	c.PaintClipPush(0, 0, w, h).Send()
	if !m.opts.NoTiles {
		var shipped int64
		for _, td := range m.pyramid.Draw(m.view, now) {
			px, ok := m.pixels[td.Wrapped]
			if !ok || px == nil {
				continue
			}
			id := m.tileId(td.Wrapped)
			version := m.tileVersion(px)
			if m.opts.UploadBudgetBytes > 0 && shipped >= m.opts.UploadBudgetBytes {
				sent, was := m.tracker.LastSent(td.Wrapped)
				held := was && !sm.TextureStarved(id)
				if !held || sent != version {
					// Over this frame's budget and in need of an upload:
					// the host's texture stands in, and the next frame
					// carries on.
					again = true
					if held {
						r := td.Rect
						c.PaintImage(id, float32(r.Min.X), float32(r.Min.Y), float32(r.Max.X), float32(r.Max.Y),
							uint32(px.w), uint32(px.h), sent, noPixels).Opacity(float32(td.Opacity)).Nearest(m.opts.NearestTiles).Send()
					}
					continue
				}
			}
			send := m.tracker.PixelsToSendFor(td.Wrapped, id, version, px.px)
			shipped += int64(len(send)) * 4
			if len(send) > 0 {
				m.bytesShipped += uint64(len(send)) * 4
			}
			r := td.Rect
			c.PaintImage(id, float32(r.Min.X), float32(r.Min.Y), float32(r.Max.X), float32(r.Max.Y),
				uint32(px.w), uint32(px.h), version, send).Opacity(float32(td.Opacity)).Nearest(m.opts.NearestTiles).Send()
		}
	}
	if overlay != nil {
		overlay(Projector{view: m.view, m: m})
	}
	if r, ok := m.box.rect(); ok {
		c.PaintRectFilled(float32(r.Min.X), float32(r.Min.Y), float32(r.Max.X), float32(r.Max.Y), 0, color.Hex(styletokens.AccentDefault.AsHex()&^0xff|0x40)).Send()
		c.PaintRectStroke(float32(r.Min.X), float32(r.Min.Y), float32(r.Max.X), float32(r.Max.Y), 0, color.Hex(styletokens.AccentDefault.AsHex()), styletokens.StrokeStrong).Send()
	}
	if !m.opts.HideAttribution && !m.opts.NoTiles && m.opts.Source.Attribution != "" {
		m.paintAttribution(w, h)
	}
	c.PaintClipPop().Send()

	// The drag-owning region last, so it wins the hit test, over a canvas that
	// senses click and hover only (ADR-0204 §SD6).
	c.PaintSenseRegion(m.ids.PrepareStr("portolan-area"), 0, 0, w, h).Send()
	canvas := c.PaintCanvas(m.ids.PrepareStr("portolan-canvas"), w, h).
		Background(color.Hex(m.opts.Background)).
		Sense(true, false, true).
		CaptureZoom()
	if !m.opts.NoScrollWheelZoom || m.opts.WheelPans {
		// A map the wheel neither zooms nor pans leaves it to what encloses
		// it: a page scrolls under the pointer wherever it is.
		canvas = canvas.CaptureScroll()
	}
	canvas.Send()

	m.pointerVeto = false
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
	c.PaintRectFilled(w-approxW, h-fontSize-6, w, h, 0, color.Hex(styletokens.NeutralBgSurface.AsHex()&^0xff|0xb0)).Send()
	c.PaintText(w-4, h-3, 2, 2, text, fontSize, color.Hex(styletokens.NeutralTextPrimary.AsHex())).Send()
}

// handleInput feeds last frame's registers to the handlers: the drag or a
// shift-drag box (the sense region's press origin, positions from the
// frame-end pointer — M0's recipe), the wheel, the pinch, the double-click.
func (m *Map) handleInput(cur, areaCur c.CanvasCursorValue, areaOk bool, flags c.ResponseFlagsE,
	wheel c.CanvasWheelValue, ptr c.PointerValue, now time.Time) {
	v := m.view
	// A guest that claimed the pointer this frame stops a new gesture from
	// starting here; one already in flight is the map's to finish.
	veto := m.pointerVeto
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
	if flags.HasDragStarted() && posOk && !veto {
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
		case m.boxSelect || (!m.opts.NoBoxZoom && shift):
			m.box.begin(origin)
			m.boxSelecting = m.boxSelect
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
			if m.boxSelecting {
				m.selected, m.selectedOk = m.box.end(v)
			} else {
				m.box.finish(v)
			}
			m.boxSelecting = false
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
		switch {
		case m.opts.WheelPans:
			// The content follows the wheel: a scroll down shows what lies
			// below, as on a page.
			if wheel.ScrollX != 0 || wheel.ScrollY != 0 {
				v.PanBy(Point{X: -float64(wheel.ScrollX), Y: -float64(wheel.ScrollY)})
			}
		case !m.opts.NoScrollWheelZoom && wheel.ScrollY != 0:
			m.wheel.wheel(float64(wheel.ScrollY), anchor, now)
		}
	}
	m.wheel.tick(v, now, m.hopts)
	m.pinch.tick(v, now)

	if !m.opts.NoDoubleClickZoom && flags.HasDoubleClicked() && posOk && !veto {
		doubleClick(v, pos, cur.Shift())
	}
}

// keyMask is what the capture Frame eats while focused: the map's own keys
// unless NoKeyboard, and the host's.
func (m *Map) keyMask() keycodes.Mask {
	mask := m.opts.HostKeys
	if !m.opts.NoKeyboard {
		mask |= mapKeyMask
	}
	return mask
}

// takesKeys reports whether the map has any keys to take.
func (m *Map) takesKeys() bool { return m.keyMask() != 0 }

// focusKeys asks egui to focus the key-capturing Frame.
func (m *Map) focusKeys() {
	if m.takesKeys() && m.keyFrameID != 0 {
		c.RequestFocus(m.keyFrameID)
	}
}

// handleKeys applies the keys the capture Frame ate while focused, and
// keeps the host's for Events.Keys.
func (m *Map) handleKeys(sm *c.StateManager) {
	caps := sm.GetCapturedKeys(widgethandle.Make(m.keyFrameID))
	for _, k := range caps {
		if m.opts.HostKeys.Has(k.Code) {
			if k.Down() {
				m.keys = append(m.keys, k)
			}
			continue
		}
		if m.opts.NoKeyboard || !m.view.Loaded() {
			continue
		}
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

// tileId is a tile's texture id: its coordinate key under a "tile" scope, so
// no tile can collide with another id of the map's.
func (m *Map) tileId(coords TileCoords) (id uint64) {
	for range c.IdScope(m.ids.PrepareStr("tile")) {
		id = m.ids.PrepareStr(coords.Key()).Derive()
	}
	return
}
