//go:build integration

package maptiles

import (
	"container/list"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// The viewport every arm sees, in logical points (README §2).
const viewW, viewH = 1024, 600

// step is one settled view of the scripted sequence. Either a SetView
// (center + zoom) or a pan by a pixel offset from the previous view.
type step struct {
	name     string
	lat, lon float64
	zoom     float64
	panX     float64
	panY     float64
}

var zurich = [2]float64{47.45, 8.56}
var slice = [2]float64{47.25, 8.75}

// sequence is the scripted pan/zoom path (README §2).
var sequence = []step{
	{name: "s01-overview-z7", lat: slice[0], lon: slice[1], zoom: 7},
	{name: "s02-zoom-in-z8", lat: slice[0], lon: slice[1], zoom: 8},
	{name: "s03-zurich-z9", lat: zurich[0], lon: zurich[1], zoom: 9},
	{name: "s04-pan-east-third", panX: viewW / 3},
	{name: "s05-pan-east-two-thirds", panX: 2 * viewW / 3},
	{name: "s06-pan-south-half", panY: viewH / 2},
	{name: "s07-pan-back-to-s03", lat: zurich[0], lon: zurich[1], zoom: 9},
	{name: "s08-zoom-out-z8", lat: zurich[0], lon: zurich[1], zoom: 8},
	{name: "s09-zoom-in-z9-revisit", lat: zurich[0], lon: zurich[1], zoom: 9},
	{name: "s10-zoom-in-z10", lat: zurich[0], lon: zurich[1], zoom: 10},
	{name: "s11-back-to-s01", lat: slice[0], lon: slice[1], zoom: 7},
}

// query is one raster query an arm issues to the server.
type query struct {
	arm  string
	step int
	n    int // order within the arm
	lvl  int
	b    box
	tile string // z/x/y for a tile arm
}

// viewBox is the panel's request for a view: the bbox of the view bounds and
// the clamped raster size (no antimeridian or pole folding is needed over the
// slice; the harness asserts that).
func viewBox(v *portolan.View) box {
	b := v.Bounds()
	if b.GetWest() < -180 || b.GetEast() > 180 {
		panic("view crosses the antimeridian; the harness does not fold")
	}
	return box{
		minX: uint64(lonToMercX(b.GetWest())), maxX: uint64(lonToMercX(b.GetEast())),
		minY: uint64(latToMercY(b.GetNorth())), maxY: uint64(latToMercY(b.GetSouth())),
		w: viewW, h: viewH,
	}
}

type stepLog struct {
	step                              int
	requested, pyramidHeld, cacheHits int
	issued                            int
	detail                            string
}

// simulation drives the real portolan View (and, for a tile arm, the real
// Pyramid) through the sequence and returns what each arm would send.
type armSpec struct {
	name      string
	tileSize  int // 0: bbox arm
	zoomOff   int
	byteCache int // tile arms: loader byte-cache capacity, in tiles (0 = none)
}

var arms = []armSpec{
	{name: "bbox-sd1"},
	{name: "tile1024", tileSize: 1024, zoomOff: -2, byteCache: 512},
	{name: "tile512", tileSize: 512, zoomOff: -1, byteCache: 512},
	{name: "tile1024-nocache", tileSize: 1024, zoomOff: -2},
	{name: "tile512-nocache", tileSize: 512, zoomOff: -1},
}

func armByName(name string) armSpec {
	for _, a := range arms {
		if a.name == name {
			return a
		}
	}
	panic(name)
}

func newView() *portolan.View {
	return portolan.NewView(portolan.ViewOptions{Size: portolan.Point{X: viewW, Y: viewH}, ZoomSnap: 1})
}

func applyStep(v *portolan.View, s step) {
	if s.panX != 0 || s.panY != 0 {
		v.PanBy(portolan.Point{X: s.panX, Y: s.panY})
		return
	}
	v.SetView(portolan.LL(s.lat, s.lon), s.zoom)
}

// simulate returns the arm's issued queries and a per-step log.
func simulate(a armSpec) (qs []query, logs []stepLog) {
	if a.tileSize == 0 {
		return simulateBBox(a)
	}
	return simulateTiles(a)
}

// simulateBBox is SD1 as built: each settled view restarts the ladder at the
// coarsest level and climbs to the full table (every level is under the 3 s
// budget on the local slice — the cost run checks it), on one lane whose memo
// holds the last (SQL, params).
func simulateBBox(a armSpec) (qs []query, logs []stepLog) {
	v := newView()
	memo := ""
	for i, s := range sequence {
		applyStep(v, s)
		v.TakeEvents()
		b := viewBox(v)
		lg := stepLog{step: i, detail: b.String()}
		for li := range ladder {
			key := fmt.Sprintf("%d|%s", li, b)
			lg.requested++
			if key == memo {
				lg.cacheHits++
				continue
			}
			memo = key
			qs = append(qs, query{arm: a.name, step: i, n: len(qs), lvl: li, b: b})
			lg.issued++
		}
		logs = append(logs, lg)
	}
	return
}

// lru is the loader's count-bounded byte cache, keyed by tile URL.
type lru struct {
	cap   int
	order *list.List
	items map[string]*list.Element
}

func newLRU(n int) *lru { return &lru{cap: n, order: list.New(), items: map[string]*list.Element{}} }

func (c *lru) get(k string) bool {
	if c.cap == 0 {
		return false
	}
	if el, ok := c.items[k]; ok {
		c.order.MoveToFront(el)
		return true
	}
	return false
}

func (c *lru) put(k string) {
	if c.cap == 0 {
		return
	}
	if el, ok := c.items[k]; ok {
		c.order.MoveToFront(el)
		return
	}
	c.items[k] = c.order.PushFront(k)
	if c.order.Len() > c.cap {
		old := c.order.Back()
		c.order.Remove(old)
		delete(c.items, old.Value.(string))
	}
}

// simulateTiles drives a second Pyramid over the same View, the way a raster
// layer beside the basemap would. Each tile the pyramid requests goes to a
// loader with a byte cache; a miss runs the per-tile ladder (three queries,
// the last kept), a hit runs nothing. Tiles arrive at once; the clock then
// runs a second so the fade ends and the prune happens.
func simulateTiles(a armSpec) (qs []query, logs []stepLog) {
	src := portolan.NewTileSource("ch://{z}/{x}/{y}")
	src.TileSize = portolan.Point{X: float64(a.tileSize), Y: float64(a.tileSize)}
	src.ZoomOffset = a.zoomOff
	src.MaxZoom = 20
	src = src.Normalized()
	v := newView()
	v.SetLayerZoomLimits(src.MinZoom, src.MaxZoom, true, true)
	p := portolan.NewPyramid(src)
	var pending []portolan.TileCoords
	var pendingWrapped []portolan.TileCoords
	p.OnRequest = func(c, w portolan.TileCoords) {
		pending = append(pending, c)
		pendingWrapped = append(pendingWrapped, w)
	}
	cache := newLRU(a.byteCache)
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	first := true
	for i, s := range sequence {
		applyStep(v, s)
		if first {
			v.TakeEvents()
			p.SetView(v, false, false)
			first = false
		} else {
			p.Sync(v, v.TakeEvents())
		}
		lg := stepLog{step: i}
		var tiles []string
		for k, c := range pending {
			w := pendingWrapped[k]
			z := src.ZoomForURL(w.Z)
			id := fmt.Sprintf("%d/%d/%d", z, w.X, w.Y)
			tiles = append(tiles, id)
			lg.requested++
			if cache.get(id) {
				lg.cacheHits++
			} else {
				for li := range ladder {
					qs = append(qs, query{arm: a.name, step: i, n: len(qs), lvl: li, b: tileBox(z, w.X, w.Y, uint32(a.tileSize)), tile: id})
					lg.issued++
				}
				cache.put(id)
			}
			p.TileReady(v, c, false, now)
		}
		pending, pendingWrapped = pending[:0], pendingWrapped[:0]
		for range 20 {
			now = now.Add(100 * time.Millisecond)
			p.Tick(v, now)
		}
		lg.pyramidHeld = p.TileCount()
		lg.detail = strings.Join(tiles, " ")
		logs = append(logs, lg)
	}
	return
}

// TestSequenceCounts counts, per arm and step, what is requested, what the
// client side answers, and what reaches the server. No server needed.
func TestSequenceCounts(t *testing.T) {
	runDir(t) // skips unless MTA_RUN_DIR is set: the trial, not the integration lane, runs these
	var rows []string
	for _, a := range arms {
		qs, logs := simulate(a)
		for _, lg := range logs {
			rows = append(rows, fmt.Sprintf("%s\t%s\t%d\t%d\t%d\t%d\t%s", a.name, sequence[lg.step].name,
				lg.requested, lg.cacheHits, lg.issued, lg.pyramidHeld, lg.detail))
		}
		t.Logf("%-18s issued %3d queries", a.name, len(qs))
		for _, q := range qs {
			if q.b.maxX > mercUnitMax || q.b.maxY > mercUnitMax {
				t.Fatalf("%s: tile %s ends at 2^32, outside the UInt32 slots", a.name, q.tile)
			}
		}
	}
	tsv(t, "counts.tsv", "arm\tstep\trequested\tclient_hits\tissued\tpyramid_tiles_after\tdetail", rows)
}
