package play

import (
	"fmt"
	"math"
	"sort"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// The hover readout (ADR-0096 SD10's hover→info, 2026-10-02): with the
// readout checkbox on, the raster query returns, beside each non-empty
// pixel's colour, how many rows it counted and one figure the render names
// (rasterRender.readout, rounded to an Int32), and the
// panel keeps them in pos order. Under the pointer it finds the pixel and
// shows the two in the status line — no second query, and nothing for an
// empty pixel.

// mapReadout is one raster's per-pixel values, sorted by pos.
type mapReadout struct {
	pos    []uint32
	n      []uint32
	m      []float64 // nil when the render names no figure
	merc   mercBox   // the box the raster covers
	w, h   uint32
	label  string // the figure's unit or name, from the render
	factor uint32 // the level's sampling factor; counts are scaled by it
}

func (inst *mapReadout) bytes() int { return 8*len(inst.pos) + 8*len(inst.m) }

// readoutFromRecord copies the readout columns of a sparse raster record —
// (pos, r, g, b, a, n [, m]) — sorted by pos. A record without them (the
// dense form, an older template) gives none.
func readoutFromRecord(rec arrow.RecordBatch) (ro mapReadout, ok bool) {
	if rec.NumCols() < 6 {
		return
	}
	pos, ok1 := rec.Column(0).(*array.Uint32)
	n, ok2 := rec.Column(5).(*array.Uint32)
	if !ok1 || !ok2 {
		return
	}
	var m *array.Int32
	if rec.NumCols() >= 7 {
		m, _ = rec.Column(6).(*array.Int32)
	}
	rows := int(rec.NumRows())
	idx := make([]int, rows)
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return pos.Value(idx[a]) < pos.Value(idx[b]) })
	ro.pos, ro.n = make([]uint32, rows), make([]uint32, rows)
	if m != nil {
		ro.m = make([]float64, rows)
	}
	for k, i := range idx {
		ro.pos[k], ro.n[k] = pos.Value(i), n.Value(i)
		if m != nil {
			ro.m[k] = float64(m.Value(i))
		}
	}
	return ro, true
}

// at returns the readout under a geographic point: the raster's pixel there,
// on whichever world copy the point is, and whether it holds rows.
func (inst *mapReadout) at(ll portolan.LatLng) (n uint32, m float64, hasM bool, ok bool) {
	if inst.w == 0 || inst.h == 0 || inst.merc.maxX <= inst.merc.minX || inst.merc.maxY <= inst.merc.minY {
		return
	}
	center := (mercXToLon(float64(inst.merc.minX)) + mercXToLon(float64(inst.merc.maxX))) / 2
	lon := ll.Lng - 360*math.Round((ll.Lng-center)/360)
	x, y := lonToMercX(lon), latToMercY(ll.Lat)
	if x < inst.merc.minX || x >= inst.merc.maxX || y < inst.merc.minY || y >= inst.merc.maxY {
		return
	}
	px := uint64(x-inst.merc.minX) * uint64(inst.w) / uint64(inst.merc.maxX-inst.merc.minX)
	py := uint64(y-inst.merc.minY) * uint64(inst.h) / uint64(inst.merc.maxY-inst.merc.minY)
	p := uint32(min(py, uint64(inst.h-1))*uint64(inst.w) + min(px, uint64(inst.w-1)))
	k := sort.Search(len(inst.pos), func(i int) bool { return inst.pos[i] >= p })
	if k == len(inst.pos) || inst.pos[k] != p {
		return
	}
	n = inst.n[k]
	if inst.m != nil {
		m, hasM = inst.m[k], true
	}
	return n, m, hasM, true
}

// text is the status line's words for the readout under ll; empty over an
// empty pixel or off the raster.
func (inst *mapReadout) text(ll portolan.LatLng) string {
	n, m, hasM, ok := inst.at(ll)
	if !ok {
		return ""
	}
	count := humanize.Comma(int64(n)) + " positions"
	if inst.factor > 1 {
		count = "≈" + humanize.Comma(int64(n)*int64(inst.factor)) + " positions"
	}
	if !hasM || math.IsNaN(m) {
		return "under the pointer: " + count
	}
	return fmt.Sprintf("under the pointer: %s · %s %s", count, humanize.Comma(int64(math.Round(m))), inst.label)
}
