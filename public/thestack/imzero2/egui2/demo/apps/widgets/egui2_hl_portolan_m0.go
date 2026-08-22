package widgets

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg" // tile decoders: raster basemaps serve PNG, aerial layers JPEG
	_ "image/png"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/basemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// =============================================================================
// portolan M0 spike (ADR-0204 §SD8, M0) — a fixed-camera slippy map drawn in
// Go on the painter lane: every visible tile is one paintImage at a computed
// rect, the pan comes from the canvas's R24 pointer row, the zoom from its R23
// wheel row, both one frame behind. No pyramid, no animation, no walkers.
//
// It exists to measure two things before the Leaflet port starts: whether the
// one-frame input lag of a Go-side map is perceptible (desktop host, and over
// the carrier), and how many bytes a first paint ships through paintImage.
// The readout under the canvas reports both; the host log carries the same
// numbers. It is deliberately not a widget — nothing should build on it.
// =============================================================================

const (
	portolanM0TileSize       = 256
	portolanM0CanvasW        = 720
	portolanM0CanvasH        = 460
	portolanM0MinZoom        = 1.0
	portolanM0MaxZoom        = 19.0
	portolanM0Workers        = 6 // Leaflet's and walkers' per-host budget
	portolanM0WheelPxPerZoom = 60.0
	portolanM0UserAgent      = "boxer-portolan-m0-spike/0.1 (+https://github.com/stergiotis/boxer)"
	portolanM0MaxTileBytes   = 4 << 20
)

type portolanTileKey struct{ z, x, y int }

func (k portolanTileKey) String() string {
	return strconv.Itoa(k.z) + "/" + strconv.Itoa(k.x) + "/" + strconv.Itoa(k.y)
}

type portolanTile struct {
	pixels []uint32 // 0xRRGGBBAA row-major, the paintImage packing
	w, h   int
	ready  bool
	failed bool
}

type portolanM0State struct {
	// view
	lat, lon float64
	zoom     float64

	// tiles — the mutex covers tiles and inflight; workers write, the frame reads
	mu       sync.Mutex
	tiles    map[portolanTileKey]*portolanTile
	inflight map[portolanTileKey]struct{}
	queue    chan portolanTileKey
	client   *http.Client
	urlTpl   string
	tracker  *c.ImageVersionTracker[portolanTileKey]
	sent     map[portolanTileKey]struct{}

	// gestures — Leaflet's formulation: the view during a drag is the view at
	// the press plus the pointer's offset from the press origin, never a sum of
	// per-frame deltas, so a frame whose position is not seen loses nothing.
	dragging                   bool
	dragOriginX, dragOrigY     float32
	dragStartLat, dragStartLon float64
	dragLastOffX, dragLastOffY float64
	// prevPos is last frame's pointer over the canvas (canvas-relative, from
	// R20 when it is valid, else the R24 row). On the drag-started frame the
	// rows already carry the first moved position, so the press origin is the
	// position of the frame before — Leaflet's _startPoint.
	prevPosX, prevPosY float32
	prevPosOk          bool
	// pressOrigin is the pointer on the first frame the primary button is
	// down on the canvas (R7 IsPointerButtonDown), before egui's click/drag
	// threshold has turned the press into a drag — Leaflet's _startPoint.
	pressOriginX, pressOriginY float32
	pressOriginOk              bool

	// instrumentation
	startedAt        time.Time
	frame            uint64
	lastFrameAt      time.Time
	frameDtMs        float64
	dragEvents       uint64 // drag-started frames
	dragFrames       uint64 // frames that moved the view
	wheelFrames      uint64
	bytesShipped     uint64
	reships          uint64
	firstPaintDone   bool
	firstPaintBytes  uint64
	firstPaintFrames uint64
	firstPaintMs     float64
	failedTiles      uint64
}

func newPortolanM0State() *portolanM0State {
	st := &portolanM0State{
		lat:       walkersDemoCenterLat,
		lon:       walkersDemoCenterLon,
		zoom:      12,
		tiles:     make(map[portolanTileKey]*portolanTile, 256),
		inflight:  make(map[portolanTileKey]struct{}, 64),
		queue:     make(chan portolanTileKey, 1024),
		client:    &http.Client{Timeout: 30 * time.Second},
		urlTpl:    strings.TrimSpace(basemap.TileURL.Get()),
		tracker:   c.NewImageVersionTracker[portolanTileKey](),
		sent:      make(map[portolanTileKey]struct{}, 256),
		startedAt: time.Now(),
	}
	for i := 0; i < portolanM0Workers; i++ {
		go st.worker()
	}
	return st
}

// worker drains the request queue: fetch, decode, publish. Decoding stays
// off the frame thread, which is ADR-0165's second constraint.
func (st *portolanM0State) worker() {
	for key := range st.queue {
		t := st.fetch(key)
		st.mu.Lock()
		delete(st.inflight, key)
		st.tiles[key] = t
		st.mu.Unlock()
	}
}

func (st *portolanM0State) fetch(key portolanTileKey) (t *portolanTile) {
	t = &portolanTile{}
	url := strings.NewReplacer(
		"{z}", strconv.Itoa(key.z), "{x}", strconv.Itoa(key.x), "{y}", strconv.Itoa(key.y),
	).Replace(st.urlTpl)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.failed = true
		return
	}
	req.Header.Set("User-Agent", portolanM0UserAgent)
	resp, err := st.client.Do(req)
	if err != nil {
		log.Warn().Err(err).Str("url", url).Msg("portolan m0: tile fetch failed")
		t.failed = true
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		log.Warn().Int("status", resp.StatusCode).Str("url", url).Msg("portolan m0: tile fetch refused")
		t.failed = true
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, portolanM0MaxTileBytes))
	if err != nil {
		t.failed = true
		return
	}
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		log.Warn().Err(err).Str("url", url).Msg("portolan m0: tile could not be decoded")
		t.failed = true
		return
	}
	b := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	w, h := b.Dx(), b.Dy()
	px := make([]uint32, w*h)
	for i := range px {
		o := i * 4
		px[i] = uint32(rgba.Pix[o])<<24 | uint32(rgba.Pix[o+1])<<16 | uint32(rgba.Pix[o+2])<<8 | uint32(rgba.Pix[o+3])
	}
	t.pixels, t.w, t.h, t.ready = px, w, h, true
	return
}

// ensure returns the tile if it has arrived (ready or failed) and otherwise
// queues it once. The queue is bounded; a full queue just retries next frame.
func (st *portolanM0State) ensure(key portolanTileKey) (t *portolanTile) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if t = st.tiles[key]; t != nil {
		return
	}
	if _, ok := st.inflight[key]; ok {
		return nil
	}
	select {
	case st.queue <- key:
		st.inflight[key] = struct{}{}
	default:
	}
	return nil
}

// Web Mercator in tile pixels at a (fractional) zoom, 256 px per tile.
func portolanProject(lat, lon, zoom float64) (x, y float64) {
	n := portolanM0TileSize * math.Exp2(zoom)
	x = (lon + 180) / 360 * n
	latRad := lat * math.Pi / 180
	y = (1 - math.Log(math.Tan(latRad)+1/math.Cos(latRad))/math.Pi) / 2 * n
	return
}

func portolanUnproject(x, y, zoom float64) (lat, lon float64) {
	n := portolanM0TileSize * math.Exp2(zoom)
	lon = x/n*360 - 180
	lat = math.Atan(math.Sinh(math.Pi*(1-2*y/n))) * 180 / math.Pi
	lat = math.Max(-85.0511, math.Min(85.0511, lat))
	return
}

// zoomAround changes the zoom keeping the geographic point under the anchor
// (canvas coordinates) where it is — Leaflet's setZoomAround.
func (st *portolanM0State) zoomAround(ax, ay, w, h, newZoom float64) {
	cx, cy := portolanProject(st.lat, st.lon, st.zoom)
	glat, glon := portolanUnproject(cx+(ax-w/2), cy+(ay-h/2), st.zoom)
	gx, gy := portolanProject(glat, glon, newZoom)
	st.lat, st.lon = portolanUnproject(gx-(ax-w/2), gy-(ay-h/2), newZoom)
	st.zoom = newZoom
}

func isNaN32(v float32) bool { return math.IsNaN(float64(v)) }

func demoPortolanM0(ids *c.WidgetIdStack, st *portolanM0State) {
	for range c.IdScope(ids.PrepareStr("portolan-m0")) {
		sm := c.CurrentApplicationState.StateManager
		canvasH := widgethandle.Make(ids.PrepareStr("portolan-m0-canvas").Derive())
		w, h := float64(portolanM0CanvasW), float64(portolanM0CanvasH)

		st.frame++
		now := time.Now()
		if !st.lastFrameAt.IsZero() {
			dt := float64(now.Sub(st.lastFrameAt).Microseconds()) / 1000
			if st.frameDtMs == 0 {
				st.frameDtMs = dt
			} else {
				st.frameDtMs = st.frameDtMs*0.9 + dt*0.1
			}
		}
		st.lastFrameAt = now

		// ---- input, one frame behind: R7 flags, R24 cursor, R23 wheel ----
		cur, live := sm.GetCanvasCursor(canvasH)
		wheel := sm.GetCanvasWheel(canvasH)
		// The drag is owned by a sense region over the whole canvas (the ImPlot
		// recipe): its R24 row on the drag-started frame carries the press
		// origin, which the canvas's own row does not — the press and the first
		// move share a host frame more often than not, and then the canvas's
		// press position is already the moved one.
		areaH := widgethandle.Make(ids.PrepareStr("portolan-m0-area").Derive())
		flags := sm.GetResponse(areaH)
		areaCur, areaOk := sm.GetCanvasCursor(areaH)
		if live {
			// Position: the frame-end pointer (R20) minus the canvas origin is
			// the freshest sample — it leads the canvas's own R24 row by one
			// event — so it drives the gesture, as the ImPlot port does; the
			// R24 row is the fallback and the liveness signal.
			r24Ok := !isNaN32(cur.PosX) && !isNaN32(cur.PosY)
			ptr := sm.GetPointer()
			posX, posY, posOk := cur.PosX, cur.PosY, r24Ok
			if ptr.Valid && !isNaN32(ptr.X) && !isNaN32(ptr.Y) {
				posX, posY, posOk = ptr.X-cur.OriginX, ptr.Y-cur.OriginY, true
			}
			if flags.HasIsPointerButtonDown() && posOk && !st.dragging && !st.pressOriginOk {
				st.pressOriginX, st.pressOriginY, st.pressOriginOk = posX, posY, true
				log.Info().Uint64("frame", st.frame).Float32("x", posX).Float32("y", posY).Msg("portolan m0: press")
			}
			if !flags.HasIsPointerButtonDown() && !st.dragging {
				st.pressOriginOk = false
			}
			if flags.HasDragStarted() && posOk {
				st.dragging = true
				// The press origin: the button-down frame's position when we
				// saw one; else last frame's position; else this frame's —
				// whose rows already include the movement that turned the
				// press into a drag.
				switch {
				case areaOk && !isNaN32(areaCur.PosX) && !isNaN32(areaCur.PosY):
					// The sense region's drag-started row: the press origin.
					st.dragOriginX, st.dragOrigY = areaCur.PosX, areaCur.PosY
				case st.pressOriginOk:
					st.dragOriginX, st.dragOrigY = st.pressOriginX, st.pressOriginY
				case st.prevPosOk:
					st.dragOriginX, st.dragOrigY = st.prevPosX, st.prevPosY
				default:
					st.dragOriginX, st.dragOrigY = posX, posY
				}
				st.dragStartLat, st.dragStartLon = st.lat, st.lon
				st.dragLastOffX, st.dragLastOffY = 0, 0
				st.dragEvents++
				log.Info().Uint64("frame", st.frame).Float32("r24x", cur.PosX).Float32("r24y", cur.PosY).
					Float32("r20x", ptr.X-cur.OriginX).Float32("r20y", ptr.Y-cur.OriginY).
					Bool("areaOk", areaOk).Float32("areaX", areaCur.PosX).Float32("areaY", areaCur.PosY).
					Float32("originX", st.dragOriginX).Float32("originY", st.dragOrigY).
					Msg("portolan m0: drag started")
			}
			// Dragged frames and the release frame both carry a position worth
			// applying; the release is where the last movement lands.
			if st.dragging && (flags.HasDragged() || flags.HasDragStopped()) && posOk {
				offX := float64(posX - st.dragOriginX)
				offY := float64(posY - st.dragOrigY)
				if offX != st.dragLastOffX || offY != st.dragLastOffY {
					cx, cy := portolanProject(st.dragStartLat, st.dragStartLon, st.zoom)
					st.lat, st.lon = portolanUnproject(cx-offX, cy-offY, st.zoom)
					st.dragFrames++
					log.Info().Uint64("frame", st.frame).Float32("r24x", cur.PosX).Float32("r24y", cur.PosY).
						Float32("r20x", ptr.X-cur.OriginX).Float32("r20y", ptr.Y-cur.OriginY).
						Float64("offX", offX).Float64("offY", offY).Msg("portolan m0: dragged")
					st.dragLastOffX, st.dragLastOffY = offX, offY
				}
			}
			if flags.HasDragStopped() && st.dragging {
				st.dragging = false
				st.pressOriginOk = false
				log.Info().Uint64("frame", st.frame).Float64("offX", st.dragLastOffX).Float64("offY", st.dragLastOffY).
					Msg("portolan m0: drag stopped")
			}
			st.prevPosX, st.prevPosY, st.prevPosOk = posX, posY, posOk && flags.HasContainsPointer()
			// ctrl+wheel and pinch arrive as a zoom factor, a plain wheel as
			// scroll pixels; Leaflet maps 60 px to one level. Anchored at the
			// hover position when the row carries one.
			dz := 0.0
			if wheel.Zoom > 0 && wheel.Zoom != 1 {
				dz += math.Log2(float64(wheel.Zoom))
			}
			if wheel.ScrollY != 0 {
				dz += float64(wheel.ScrollY) / portolanM0WheelPxPerZoom
			}
			if dz != 0 {
				ax, ay := w/2, h/2
				if !isNaN32(wheel.HoverX) && !isNaN32(wheel.HoverY) {
					ax, ay = float64(wheel.HoverX), float64(wheel.HoverY)
				}
				nz := math.Max(portolanM0MinZoom, math.Min(portolanM0MaxZoom, st.zoom+dz))
				if nz != st.zoom {
					st.zoomAround(ax, ay, w, h, nz)
					st.wheelFrames++
				}
			}
		}

		// ---- tiles: the integer level nearest the zoom, scaled to it ----
		z := int(math.Round(st.zoom))
		scale := math.Exp2(st.zoom - float64(z))
		n := 1 << uint(z)
		cx, cy := portolanProject(st.lat, st.lon, float64(z))
		tlx, tly := cx-w/(2*scale), cy-h/(2*scale)
		tx0, tx1 := int(math.Floor(tlx/portolanM0TileSize)), int(math.Floor((tlx+w/scale)/portolanM0TileSize))
		ty0, ty1 := int(math.Floor(tly/portolanM0TileSize)), int(math.Floor((tly+h/scale)/portolanM0TileSize))
		type slot struct {
			key    portolanTileKey
			tx, ty int
			d2     float64
		}
		slots := make([]slot, 0, (tx1-tx0+1)*(ty1-ty0+1))
		for ty := ty0; ty <= ty1; ty++ {
			if ty < 0 || ty >= n {
				continue
			}
			for tx := tx0; tx <= tx1; tx++ {
				wx := ((tx % n) + n) % n
				dxc := (float64(tx)+0.5)*portolanM0TileSize - cx
				dyc := (float64(ty)+0.5)*portolanM0TileSize - cy
				slots = append(slots, slot{key: portolanTileKey{z, wx, ty}, tx: tx, ty: ty, d2: dxc*dxc + dyc*dyc})
			}
		}
		// Request and draw centre-out, Leaflet's load order.
		sort.Slice(slots, func(i, j int) bool { return slots[i].d2 < slots[j].d2 })

		visible, ready, failed := 0, 0, 0
		c.PaintClipPush(0, 0, float32(w), float32(h)).Send()
		for _, s := range slots {
			visible++
			sx := (float64(s.tx)*portolanM0TileSize - tlx) * scale
			sy := (float64(s.ty)*portolanM0TileSize - tly) * scale
			sw := portolanM0TileSize * scale
			t := st.ensure(s.key)
			switch {
			case t != nil && t.ready:
				ready++
				id := ids.PrepareStr("portolan-m0-tile-" + s.key.String()).Derive()
				px := st.tracker.PixelsToSendFor(s.key, id, 1, t.pixels)
				if len(px) > 0 {
					st.bytesShipped += uint64(len(px)) * 4
					if _, seen := st.sent[s.key]; seen {
						st.reships++
					}
					st.sent[s.key] = struct{}{}
				}
				c.PaintImage(id, float32(sx), float32(sy), float32(sx+sw), float32(sy+sw),
					uint32(t.w), uint32(t.h), 1, px).Send()
			case t != nil && t.failed:
				failed++
				c.PaintRectFilled(float32(sx), float32(sy), float32(sx+sw), float32(sy+sw), 0, color.Hex(0xe8c8c8ff)).Send()
			default:
				c.PaintRectFilled(float32(sx), float32(sy), float32(sx+sw), float32(sy+sw), 0, color.Hex(0xd8d8dcff)).Send()
			}
		}
		c.PaintClipPop().Send()
		// A centre mark, so a capture shows where the view centre sits.
		c.PaintLine(float32(w/2-8), float32(h/2), float32(w/2+8), float32(h/2), color.Hex(0xff3030ff), 1.5).Send()
		c.PaintLine(float32(w/2), float32(h/2-8), float32(w/2), float32(h/2+8), color.Hex(0xff3030ff), 1.5).Send()

		// The drag-owning region, emitted last so it wins the hit test
		// (emission order is priority), over a canvas that senses click and
		// hover only.
		c.PaintSenseRegion(ids.PrepareStr("portolan-m0-area"), 0, 0, float32(w), float32(h)).Send()
		c.PaintCanvas(ids.PrepareStr("portolan-m0-canvas"), portolanM0CanvasW, portolanM0CanvasH).
			Background(color.Hex(0xd8d8dcff)).
			Sense(true, false, true).
			CaptureZoom().
			CaptureScroll().
			Send()

		if !st.firstPaintDone && visible > 0 && ready+failed == visible {
			st.firstPaintDone = true
			st.firstPaintBytes = st.bytesShipped
			st.firstPaintFrames = st.frame
			st.firstPaintMs = float64(time.Since(st.startedAt).Milliseconds())
			st.failedTiles = uint64(failed)
			log.Info().Int("tiles", visible).Int("failed", failed).Uint64("bytes", st.firstPaintBytes).
				Uint64("frames", st.firstPaintFrames).Float64("ms", st.firstPaintMs).
				Msg("portolan m0: first full paint")
		}
		pending := visible - ready - failed
		if pending > 0 {
			c.RequestRepaintAfter(0.05)
		}

		// ---- readout: the measurement, as labels and in the host log ----
		c.Label(fmt.Sprintf("centre %.5f, %.5f   zoom %.2f (level %d x%.2f)   tiles %d visible · %d ready · %d pending · %d failed",
			st.lat, st.lon, st.zoom, z, scale, visible, ready, pending, failed)).Send()
		drag := "idle"
		if st.dragging {
			drag = "dragging"
		}
		c.Label(fmt.Sprintf("frame %d   Δt %.1f ms   %s · drags %d · frames moved %d · wheel frames %d",
			st.frame, st.frameDtMs, drag, st.dragEvents, st.dragFrames, st.wheelFrames)).Send()
		first := "first full paint: pending"
		if st.firstPaintDone {
			first = fmt.Sprintf("first full paint: %.2f MB in %d frames / %.0f ms (%d failed)",
				float64(st.firstPaintBytes)/(1<<20), st.firstPaintFrames, st.firstPaintMs, st.failedTiles)
		}
		c.Label(fmt.Sprintf("shipped %.2f MB through paintImage · %s · re-ships %d",
			float64(st.bytesShipped)/(1<<20), first, st.reships)).Send()
		c.Label("ADR-0204 M0 spike: drag to pan, wheel or pinch to zoom (anchored at the pointer). " +
			"Tiles are paintImage rects at a fixed camera; pan and zoom read R24/R23 one frame behind. " +
			"Source: " + st.urlTpl).Wrap().Send()
	}
}
