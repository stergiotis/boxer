package portolan

import (
	"math"
	"sort"
	"time"
)

// Pyramid is the tile bookkeeping of src/layer/tile/GridLayer.js without its
// DOM: which tiles of which levels exist, which are current for the view,
// which are kept so an already-loaded level keeps covering the viewport
// while the current one loads, when each was loaded (for the fade), and when
// the rest are pruned. It does not fetch — it asks for tiles through
// OnRequest and is told of arrivals through TileReady — and it does not draw:
// Draw returns where every tile goes on screen and how opaque it is, for a
// caller with a painter.
//
// Coordinates: tiles are keyed by UNWRAPPED coordinates (the same tile can
// appear twice on a wide view of a wrapping world); requests and draws carry
// the wrapped coordinates a source is addressed by.
type Pyramid struct {
	src TileSource

	tiles   map[TileCoords]*tile
	levels  map[int]struct{}
	hasZoom bool
	zoom    int // the level tiles are requested for (Leaflet's _tileZoom)

	hasGlobalRange  bool
	globalTileRange Bounds
	wrapX, wrapY    *[2]int

	loading bool
	noPrune bool
	// fade is Leaflet's fadeAnimation: tiles come up over 200 ms and the
	// prune after a full load waits for it; off, a tile is active the moment
	// it arrives and the prune follows on the next Tick.
	fade bool
	// pruneAt is that deferred prune, pending while prunePending.
	pruneAt      time.Time
	prunePending bool
	fading       bool

	stats PyramidStats

	// OnRequest is called for every tile the pyramid wants; the receiver
	// loads it and calls TileReady. wrapped is what to ask the source for.
	OnRequest func(coords, wrapped TileCoords)
	// OnAbort is called for a requested tile the pyramid no longer wants
	// before it arrived (a zoom change); loading it can stop.
	OnAbort func(coords TileCoords)
	// OnUnload is called when a tile leaves the pyramid; its texture can go.
	OnUnload func(coords TileCoords)
}

// PyramidStats counts Leaflet's tile events, which is how GridLayerSpec pins
// the pyramid's behaviour ("loads 32, unloads 16 tiles zooming in 10→11").
type PyramidStats struct {
	Loading, Load                                         int
	TileLoadStart, TileLoad, TileError, TileUnload, Abort int
}

type tile struct {
	coords   TileCoords
	wrapped  TileCoords
	current  bool
	loaded   bool
	loadedAt time.Time
	active   bool
	retain   bool
	failed   bool
}

// TileDraw is one tile's place on screen this frame: Rect in viewport pixels,
// Opacity after the fade and the layer opacity, and the wrapped coordinates
// whose texture to draw. Draw returns them back to front.
type TileDraw struct {
	Coords, Wrapped TileCoords
	Rect            Bounds
	Opacity         float64
}

// Fade is how long a tile takes to fade in, Leaflet's 200 ms; FadeThenPrune
// is the extra wait before pruning after a full load.
const (
	tileFade      = 200 * time.Millisecond
	tileFadePrune = 250 * time.Millisecond
)

// NewPyramid makes an empty pyramid for a source.
func NewPyramid(src TileSource) *Pyramid {
	return &Pyramid{
		src:    src,
		tiles:  make(map[TileCoords]*tile, 64),
		levels: make(map[int]struct{}, 4),
		fade:   true,
	}
}

// SetFade turns the 200 ms fade-in on or off (Leaflet's fadeAnimation; on by
// default).
func (p *Pyramid) SetFade(on bool) { p.fade = on }

// Sync applies a frame's view events the way GridLayer's getEvents wires them:
// a hard view change or a zoom re-selects the level (SetView), a move or its
// end re-requests the viewport's tiles (Update). Call it after the view has
// changed for the frame; the pinch/fly "animating" flags of M3 are not yet
// passed through.
func (p *Pyramid) Sync(v *View, ev ViewEvents) {
	if !v.Loaded() {
		return
	}
	if ev.ViewReset || ev.Zoom {
		p.SetView(v, false, false)
	}
	if ev.MoveEnd || ev.Move {
		p.Update(v)
	}
}

// Source is the pyramid's tile source.
func (p *Pyramid) Source() TileSource { return p.src }

// Stats are the event counts so far.
func (p *Pyramid) Stats() PyramidStats { return p.stats }

// IsLoading reports tiles requested and not yet arrived.
func (p *Pyramid) IsLoading() bool { return p.loading }

// TileCount is the number of tiles the pyramid holds, loaded or not.
func (p *Pyramid) TileCount() int { return len(p.tiles) }

// HasTile reports whether a tile (by unwrapped coordinates) is held.
func (p *Pyramid) HasTile(c TileCoords) bool { _, ok := p.tiles[c]; return ok }

// Levels are the zoom levels that currently hold tiles.
func (p *Pyramid) Levels() (zs []int) {
	for z := range p.levels {
		zs = append(zs, z)
	}
	sort.Ints(zs)
	return
}

// Reset forgets every tile and level (Leaflet's _invalidateAll).
func (p *Pyramid) Reset() {
	p.removeAllTiles()
	p.levels = make(map[int]struct{}, 4)
	p.hasZoom = false
}

// SetView is Leaflet's _setView: called when the view changes zoom (or on
// every view change with updateWhenZooming), it picks the level, drops
// unloaded tiles of other levels, recomputes the grid, requests what the
// viewport needs and prunes what it no longer does. noPrune keeps every
// loaded tile (during a pinch or a zoom animation); noUpdate skips the
// request pass unless the level changed.
func (p *Pyramid) SetView(v *View, noPrune, noUpdate bool) {
	z := int(jsRound(v.Zoom()))
	hasZoom := true
	if float64(z) > p.src.MaxZoom || float64(z) < p.src.MinZoom {
		hasZoom = false
	} else {
		z = p.src.ClampZoom(float64(z))
	}
	zoomChanged := hasZoom != p.hasZoom || z != p.zoom
	if !noUpdate || zoomChanged {
		p.hasZoom, p.zoom = hasZoom, z
		p.abortLoading()
		p.updateLevels()
		p.resetGrid(v)
		if p.hasZoom {
			p.Update(v)
		}
		if !noPrune {
			p.pruneTiles(v)
		}
		p.noPrune = noPrune
	}
}

// updateLevels drops levels that no longer hold tiles and registers the
// current one (Leaflet's _updateLevels).
func (p *Pyramid) updateLevels() {
	if !p.hasZoom {
		return
	}
	counts := make(map[int]int, len(p.levels))
	for _, t := range p.tiles {
		counts[t.coords.Z]++
	}
	for z := range p.levels {
		if counts[z] == 0 && z != p.zoom {
			delete(p.levels, z)
			p.removeTilesAtZoom(z)
		}
	}
	p.levels[p.zoom] = struct{}{}
}

// resetGrid recomputes the world's tile range and the wrap ranges for the
// level (Leaflet's _resetGrid).
func (p *Pyramid) resetGrid(v *View) {
	crs := v.CRS()
	tileSize := p.src.TileSize
	zoom := float64(p.zoom)
	if b, ok := v.PixelWorldBounds(zoom); ok {
		p.globalTileRange, p.hasGlobalRange = pxBoundsToTileRange(b, tileSize), true
	} else {
		p.hasGlobalRange = false
	}
	p.wrapX, p.wrapY = nil, nil
	if lo, hi, ok := crs.WrapLng(); ok && !p.src.NoWrap {
		p.wrapX = &[2]int{
			int(math.Floor(v.ProjectAt(LL(0, lo), zoom).X / tileSize.X)),
			int(math.Ceil(v.ProjectAt(LL(0, hi), zoom).X / tileSize.Y)),
		}
	}
	if lo, hi, ok := crs.WrapLat(); ok && !p.src.NoWrap {
		p.wrapY = &[2]int{
			int(math.Floor(v.ProjectAt(LL(lo, 0), zoom).Y / tileSize.X)),
			int(math.Ceil(v.ProjectAt(LL(hi, 0), zoom).Y / tileSize.Y)),
		}
	}
}

func pxBoundsToTileRange(b Bounds, tileSize Point) Bounds {
	return BoundsOf(b.Min.UnscaleBy(tileSize).Floor(), b.Max.UnscaleBy(tileSize).Ceil().Subtract(Point{1, 1}))
}

// tiledPixelBounds is the viewport in the level's pixels (Leaflet's
// _getTiledPixelBounds, without the zoom-animation branch).
func (p *Pyramid) tiledPixelBounds(v *View, center LatLng) Bounds {
	scale := v.ZoomScale(v.Zoom(), float64(p.zoom))
	pixelCenter := v.ProjectAt(center, float64(p.zoom)).Floor()
	halfSize := v.Size().DivideBy(scale * 2)
	return BoundsOf(pixelCenter.Subtract(halfSize), pixelCenter.Add(halfSize))
}

// Update is Leaflet's _update: marks the tiles the viewport needs as current,
// requests the missing ones from the centre outward, and hands off to SetView
// when the view's zoom has drifted more than a level from the tiles'.
func (p *Pyramid) Update(v *View) {
	if !p.hasZoom {
		return
	}
	zoom := p.src.ClampZoom(v.Zoom())
	center := v.Center()
	pixelBounds := p.tiledPixelBounds(v, center)
	tileRange := pxBoundsToTileRange(pixelBounds, p.src.TileSize)
	tileCenter := tileRange.GetCenter()
	margin := float64(p.src.KeepBuffer)
	noPruneRange := BoundsOf(
		tileRange.GetBottomLeft().Subtract(Point{margin, -margin}),
		tileRange.GetTopRight().Add(Point{margin, -margin}))
	if math.IsInf(tileRange.Min.X, 0) || math.IsInf(tileRange.Min.Y, 0) ||
		math.IsInf(tileRange.Max.X, 0) || math.IsInf(tileRange.Max.Y, 0) ||
		math.IsNaN(tileRange.Min.X) || math.IsNaN(tileRange.Max.Y) {
		panic("portolan: attempted to load an infinite number of tiles")
	}
	for _, t := range p.tiles {
		c := t.coords
		if c.Z != p.zoom || !noPruneRange.Contains(Point{float64(c.X), float64(c.Y)}) {
			t.current = false
		}
	}
	// _update just loads more tiles. If the tile zoom level differs too much
	// from the map's, let _setView reset levels and prune old tiles.
	if math.Abs(float64(zoom-p.zoom)) > 1 {
		p.SetView(v, false, false)
		return
	}
	type slot struct {
		c TileCoords
		d float64
	}
	var queue []slot
	for j := int(tileRange.Min.Y); j <= int(tileRange.Max.Y); j++ {
		for i := int(tileRange.Min.X); i <= int(tileRange.Max.X); i++ {
			c := TileCoords{i, j, p.zoom}
			if !p.isValidTile(v, c) {
				continue
			}
			if t, ok := p.tiles[c]; ok {
				t.current = true
				continue
			}
			queue = append(queue, slot{c, Point{float64(i), float64(j)}.DistanceTo(tileCenter)})
		}
	}
	// load tiles in order of their distance to center
	sort.SliceStable(queue, func(a, b int) bool { return queue[a].d < queue[b].d })
	if len(queue) != 0 {
		if !p.loading {
			p.loading = true
			p.stats.Loading++
		}
		for _, s := range queue {
			p.addTile(s.c)
		}
	}
}

func (p *Pyramid) isValidTile(v *View, c TileCoords) bool {
	crs := v.CRS()
	if !crs.Infinite() && p.hasGlobalRange {
		// don't load tile if it's out of bounds and not wrapped
		b := p.globalTileRange
		_, _, wrapLng := crs.WrapLng()
		_, _, wrapLat := crs.WrapLat()
		if (!wrapLng && (float64(c.X) < b.Min.X || float64(c.X) > b.Max.X)) ||
			(!wrapLat && (float64(c.Y) < b.Min.Y || float64(c.Y) > b.Max.Y)) {
			return false
		}
	}
	if !p.src.Bounds.IsValid() {
		return true
	}
	// don't load tile if it doesn't intersect the bounds in options
	return p.src.Bounds.Overlaps(p.tileCoordsToBounds(v, c))
}

// TileBounds is the geographic extent of a tile (wrapped into range unless
// NoWrap).
func (p *Pyramid) TileBounds(v *View, c TileCoords) LatLngBounds { return p.tileCoordsToBounds(v, c) }

func (p *Pyramid) tileCoordsToBounds(v *View, c TileCoords) LatLngBounds {
	tileSize := p.src.TileSize
	nwPoint := Point{float64(c.X), float64(c.Y)}.ScaleBy(tileSize)
	sePoint := nwPoint.Add(tileSize)
	nw := v.UnprojectAt(nwPoint, float64(c.Z))
	se := v.UnprojectAt(sePoint, float64(c.Z))
	b := LatLngBoundsOf(nw, se)
	if !p.src.NoWrap {
		b = v.WrapLatLngBounds(b)
	}
	return b
}

// WrapCoords brings a tile's X (and Y, for a CRS that wraps latitude) into
// the world's range — the coordinates a source is addressed by.
func (p *Pyramid) WrapCoords(c TileCoords) TileCoords {
	if p.wrapX != nil {
		c.X = wrapInt(c.X, p.wrapX[0], p.wrapX[1])
	}
	if p.wrapY != nil {
		c.Y = wrapInt(c.Y, p.wrapY[0], p.wrapY[1])
	}
	return c
}

// wrapInt is Util.wrapNum on integers, excluding max.
func wrapInt(x, min, max int) int {
	d := max - min
	return ((x-min)%d+d)%d + min
}

// GlobalRows is the number of tile rows at the current level, 0 when the
// CRS is infinite; TileSource.URL wants it for {-y} and TMS.
func (p *Pyramid) GlobalRows() int {
	if !p.hasGlobalRange {
		return 0
	}
	return int(p.globalTileRange.Max.Y) + 1
}

func (p *Pyramid) addTile(c TileCoords) {
	t := &tile{coords: c, wrapped: p.WrapCoords(c), current: true}
	p.tiles[c] = t
	p.stats.TileLoadStart++
	if p.OnRequest != nil {
		p.OnRequest(c, t.wrapped)
	}
}

// TileReady is what a loader calls when a tile has arrived, or failed (the
// failed tile stays, drawn as the source's error tile if any). It is
// Leaflet's _tileReady: with the fade on the tile starts transparent and Tick
// brings it up; without it the tile is active at once and a prune follows.
// When nothing is left to load, a prune is scheduled — after the fade, or on
// the next Tick without one.
func (p *Pyramid) TileReady(v *View, c TileCoords, failed bool, now time.Time) {
	if failed {
		p.stats.TileError++
	}
	t, ok := p.tiles[c]
	if !ok {
		return
	}
	t.loaded, t.loadedAt, t.failed = true, now, failed
	if p.fade {
		t.active = false
		p.fading = true
	} else {
		t.active = true
		p.pruneTiles(v)
	}
	if !failed {
		p.stats.TileLoad++
	}
	if p.noTilesToLoad() {
		p.loading = false
		p.stats.Load++
		if p.fade {
			// Wait a bit more than the fade before pruning.
			p.pruneAt = now.Add(tileFadePrune)
		} else {
			p.pruneAt = now
		}
		p.prunePending = true
	}
}

// Tick is Leaflet's _updateOpacity and its prune timer: call it once per
// frame with the clock. It reports whether another frame is needed — a fade
// or a pending prune.
func (p *Pyramid) Tick(v *View, now time.Time) (again bool) {
	if p.fading {
		nextFrame, willPrune := false, false
		for _, t := range p.tiles {
			if !t.current || !t.loaded {
				continue
			}
			fade := math.Min(1, float64(now.Sub(t.loadedAt))/float64(tileFade))
			if fade < 1 {
				nextFrame = true
			} else {
				if t.active {
					willPrune = true
				}
				t.active = true
			}
		}
		if willPrune && !p.noPrune {
			p.pruneTiles(v)
		}
		if !nextFrame {
			p.fading = false
		}
		again = nextFrame
	}
	if p.prunePending {
		if !now.Before(p.pruneAt) {
			p.prunePending = false
			p.pruneTiles(v)
		} else {
			again = true
		}
	}
	return again
}

func (p *Pyramid) noTilesToLoad() bool {
	for _, t := range p.tiles {
		if !t.loaded {
			return false
		}
	}
	return true
}

// pruneTiles is Leaflet's _pruneTiles: keep the current tiles, keep the
// loaded ancestors (up to five levels up) or descendants (two down) of any
// current tile that is not yet active so the view stays covered, drop the
// rest.
func (p *Pyramid) pruneTiles(v *View) {
	zoom := v.Zoom()
	if zoom > p.src.MaxZoom || zoom < p.src.MinZoom {
		p.removeAllTiles()
		return
	}
	for _, t := range p.tiles {
		t.retain = t.current
	}
	for _, t := range p.tiles {
		if t.current && !t.active {
			c := t.coords
			if !p.retainParent(c.X, c.Y, c.Z, c.Z-5) {
				p.retainChildren(c.X, c.Y, c.Z, c.Z+2)
			}
		}
	}
	for c, t := range p.tiles {
		if !t.retain {
			p.removeTile(c)
		}
	}
}

func floorDiv2(x int) int { return int(math.Floor(float64(x) / 2)) }

func (p *Pyramid) retainParent(x, y, z, minZoom int) bool {
	x2, y2, z2 := floorDiv2(x), floorDiv2(y), z-1
	t, ok := p.tiles[TileCoords{x2, y2, z2}]
	if ok && t.active {
		t.retain = true
		return true
	} else if ok && t.loaded {
		t.retain = true
	}
	if z2 > minZoom {
		return p.retainParent(x2, y2, z2, minZoom)
	}
	return false
}

func (p *Pyramid) retainChildren(x, y, z, maxZoom int) {
	for i := 2 * x; i < 2*x+2; i++ {
		for j := 2 * y; j < 2*y+2; j++ {
			t, ok := p.tiles[TileCoords{i, j, z + 1}]
			if ok && t.active {
				t.retain = true
				continue
			} else if ok && t.loaded {
				t.retain = true
			}
			if z+1 < maxZoom {
				p.retainChildren(i, j, z+1, maxZoom)
			}
		}
	}
}

// abortLoading is TileLayer's _abortLoading: unloaded tiles of other levels
// are dropped outright when the level changes.
func (p *Pyramid) abortLoading() {
	for c, t := range p.tiles {
		if t.coords.Z != p.zoom && !t.loaded {
			delete(p.tiles, c)
			p.stats.Abort++
			if p.OnAbort != nil {
				p.OnAbort(c)
			}
		}
	}
}

func (p *Pyramid) removeTilesAtZoom(z int) {
	for c, t := range p.tiles {
		if t.coords.Z == z {
			p.removeTile(c)
		}
	}
}

func (p *Pyramid) removeAllTiles() {
	for c := range p.tiles {
		p.removeTile(c)
	}
}

func (p *Pyramid) removeTile(c TileCoords) {
	if _, ok := p.tiles[c]; !ok {
		return
	}
	delete(p.tiles, c)
	p.stats.TileUnload++
	if p.OnUnload != nil {
		p.OnUnload(c)
	}
}

// Draw returns the loaded tiles' screen rects and opacities for the view,
// back to front: levels farther from the view's zoom first (Leaflet's
// z-index is maxZoom − |zoom − z|), then coarser before finer, then by
// coordinates for a stable order. A tile's rect is its coordinates scaled
// by 2^(zoom − z) minus the view's pixel origin, the same arithmetic as
// Leaflet's per-level CSS transform.
func (p *Pyramid) Draw(v *View, now time.Time) (out []TileDraw) {
	zoom := v.Zoom()
	origin := v.PixelOrigin()
	tileSize := p.src.TileSize
	out = make([]TileDraw, 0, len(p.tiles))
	for _, t := range p.tiles {
		if !t.loaded {
			continue
		}
		scale := v.ZoomScale(zoom, float64(t.coords.Z))
		min := Point{float64(t.coords.X), float64(t.coords.Y)}.ScaleBy(tileSize).MultiplyBy(scale).Subtract(origin)
		max := min.Add(tileSize.MultiplyBy(scale))
		opacity := p.src.Opacity
		if !t.active {
			opacity *= math.Min(1, float64(now.Sub(t.loadedAt))/float64(tileFade))
		}
		out = append(out, TileDraw{Coords: t.coords, Wrapped: t.wrapped, Rect: BoundsOf(min, max), Opacity: opacity})
	}
	level := func(d TileDraw) float64 { return math.Abs(zoom - float64(d.Coords.Z)) }
	sort.Slice(out, func(a, b int) bool {
		la, lb := level(out[a]), level(out[b])
		if la != lb {
			return la > lb
		}
		ca, cb := out[a].Coords, out[b].Coords
		if ca.Z != cb.Z {
			return ca.Z < cb.Z
		}
		if ca.Y != cb.Y {
			return ca.Y < cb.Y
		}
		return ca.X < cb.X
	})
	return out
}

// Failed reports whether a held tile failed to load.
func (p *Pyramid) Failed(c TileCoords) bool {
	t, ok := p.tiles[c]
	return ok && t.failed
}
