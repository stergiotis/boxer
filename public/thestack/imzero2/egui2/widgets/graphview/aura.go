package graphview

import (
	"hash/maphash"
	"math"
	"slices"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	cam "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/camera"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/legend"
)

// Auras (ADR-0224 §SD11): nodes that share an aura id are drawn over one
// translucent blob. Every visible member emanates a radial ramp on the
// screen, the ramps of one aura accumulate as a complementary product on a
// grid of CellSize screen pixels, and the iso-line at DrawLimit is traced,
// smoothed and filled beneath the graph. The behaviour re-derives the aura
// feature of ZoomCharts NetChart as measured in
// doc/adr-background-work/netchart-aura-analysis.md; the knob names keep
// that meaning, the constants are this widget's own.

// AuraParams configures the auras of a [View]. The zero value draws none;
// Enabled with the other fields zero takes the defaults noted per field.
type AuraParams struct {
	Enabled bool
	// CellSize is the field grid's cell in screen pixels, default 8. Smaller
	// cells follow the nodes more closely and cost more: the work per node
	// grows with the square of its aura radius over the cell.
	CellSize float32
	// Intensity is the ramp's outer radius as a multiple of the padded node
	// radius on screen, default 6.
	Intensity float32
	// DrawLimit is the accumulated value at which a cell is inside, 0..1,
	// default 0.8: higher draws tighter auras.
	DrawLimit float32
	// Pad is added to the node's screen radius before the ramp is sized, in
	// screen pixels, default 4, so a tiny node still carries a visible aura.
	Pad float32
	// Overlap lets auras cover the same cell; off, a cell belongs to the aura
	// with the largest accumulated value, ties to the lexicographically
	// smaller id.
	Overlap bool
	// Styles gives an aura's look by id; an aura without one takes the
	// qualitative cycle in id order at AuraFillAlpha.
	Styles map[string]AuraStyle
	// Legend says where the legend is drawn and who owns its clicks
	// (ADR-0224 §SD15). The zero value draws none.
	Legend AuraLegendModeE
	// LegendCorner is the canvas corner AuraLegendInside draws in, and
	// LegendInset the gap from both of that corner's edges in screen
	// pixels; zero takes 8. Neither is read in the other modes.
	LegendCorner CornerE
	LegendInset  float32
	LegendStyle  legend.Style
}

// AuraLegendModeE says where a View draws its aura legend (ADR-0224 §SD15).
// The rows themselves are built in every mode and published by
// [View.AuraLegendItems], so the mode chooses who paints them, not whether
// they exist.
type AuraLegendModeE uint8

const (
	// AuraLegendOff paints no legend. AuraLegendItems still reports the
	// rows, which is the difference between this and AuraParams.Enabled
	// being false.
	AuraLegendOff AuraLegendModeE = 0
	// AuraLegendInside paints the legend in the view's own canvas at
	// AuraParams.LegendCorner and takes its clicks: a click on a row toggles
	// that aura and reports EventKindAuraToggle.
	AuraLegendInside AuraLegendModeE = 1
	// AuraLegendExternal paints nothing and stamps no region. The caller
	// takes the rows from AuraLegendItems, paints them where it likes with
	// the legend package, and toggles with HideAura and ShowAura — which is
	// the only mode that works where the view does not own the canvas its
	// rows would be clicked in.
	AuraLegendExternal AuraLegendModeE = 2
)

// CornerE names a canvas corner, origin top-left.
type CornerE uint8

const (
	CornerTopLeft     CornerE = 0
	CornerTopRight    CornerE = 1
	CornerBottomLeft  CornerE = 2
	CornerBottomRight CornerE = 3
)

// AuraStyle is one aura's look. A zero Fill takes the cycle colour; a zero
// Line draws a hairline in the fill colour, which is what anti-aliases the
// mesh edge; Label defaults to the id.
type AuraStyle struct {
	Fill      color.Color
	Line      color.Color
	LineWidth float32 // screen pixels, default StrokeRegular when Line is set
	ZIndex    int32   // larger draws on top; ties in id order
	Label     string
	NoLegend  bool
}

// AuraFillAlpha is the alpha of a default aura fill.
const AuraFillAlpha = 0x66

func (inst AuraParams) withDefaults() AuraParams {
	def := func(v *float32, d float32) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&inst.CellSize, 8)
	def(&inst.Intensity, 6)
	def(&inst.DrawLimit, 0.8)
	def(&inst.Pad, 4)
	inst.DrawLimit = min(inst.DrawLimit, 1)
	return inst
}

// key is the part of the params a change of which invalidates the field.
type auraParamsKey struct {
	cellSize, intensity, drawLimit, pad float32
	overlap                             bool
}

func (inst AuraParams) key() auraParamsKey {
	return auraParamsKey{inst.CellSize, inst.Intensity, inst.DrawLimit, inst.Pad, inst.Overlap}
}

// auraKernel is one node's ramp on screen: 1 inside d0, linear to 0 at R.
type auraKernel struct{ d0, R float32 }

func kernelFor(radiusPx float32, p AuraParams) auraKernel {
	rp := radiusPx + p.Pad
	return auraKernel{d0: rp / 2, R: p.Intensity * rp}
}

func (k auraKernel) at(d float32) float32 {
	if d <= k.d0 {
		return 1
	}
	if d >= k.R {
		return 0
	}
	return (k.R - d) / (k.R - k.d0)
}

// extent is the distance at which a lone node's ramp equals drawLimit —
// the radius its aura has on screen.
func (k auraKernel) extent(drawLimit float32) float32 {
	return k.d0 + (1-drawLimit)*(k.R-k.d0)
}

// auraSet is the frame's aura table: the distinct ids in id order and each
// slot's memberships as a CSR over aura indices. hash changes when the
// membership does.
type auraSet struct {
	ids   []string
	index map[string]int32
	start []int32 // per slot, n+1
	list  []int32
	hash  uint64

	seed  maphash.Seed
	count []int32 // scratch
}

// build rebuilds the table from the declaration against g's slots and
// reports whether the membership changed.
func (a *auraSet) build(nodes []NodeSpec, g *graph) (changed bool) {
	if a.index == nil {
		a.index = make(map[string]int32, 8)
		a.seed = maphash.MakeSeed()
	}
	a.ids = a.ids[:0]
	clear(a.index)
	for i := range nodes {
		for _, id := range nodes[i].Auras {
			if id == "" {
				continue
			}
			if _, ok := a.index[id]; !ok {
				a.index[id] = 0
				a.ids = append(a.ids, id)
			}
		}
	}
	slices.Sort(a.ids)
	var h uint64
	for k, id := range a.ids {
		a.index[id] = int32(k)
		h += maphash.String(a.seed, id) * uint64(k+1)
	}
	n := g.n()
	a.count = growTo(a.count, n)
	clear(a.count)
	for i := range nodes {
		if s, ok := g.slot[nodes[i].Id]; ok {
			for _, id := range nodes[i].Auras {
				if id != "" {
					a.count[s]++
				}
			}
		}
	}
	a.start = growTo(a.start, n+1)
	var acc int32
	for s := 0; s < n; s++ {
		a.start[s] = acc
		acc += a.count[s]
	}
	a.start[n] = acc
	a.list = growTo(a.list, int(acc))
	copy(a.count, a.start[:n])
	for i := range nodes {
		s, ok := g.slot[nodes[i].Id]
		if !ok {
			continue
		}
		for _, id := range nodes[i].Auras {
			if id == "" {
				continue
			}
			k := a.index[id]
			a.list[a.count[s]] = k
			a.count[s]++
			h += mix64(nodes[i].Id) ^ maphash.String(a.seed, id)
		}
	}
	changed = h != a.hash
	a.hash = h
	return
}

func (a *auraSet) members(slot int32) []int32 {
	return a.list[a.start[slot]:a.start[slot+1]]
}

// auraBox is the sample-index bbox of an aura's non-zero cells, inclusive;
// empty when i1 < i0.
type auraBox struct{ i0, j0, i1, j1 int32 }

func (b *auraBox) reset()     { *b = auraBox{i0: math.MaxInt32, j0: math.MaxInt32, i1: -1, j1: -1} }
func (b auraBox) empty() bool { return b.i1 < b.i0 }
func (b *auraBox) add(i0, j0, i1, j1 int32) {
	b.i0, b.j0 = min(b.i0, i0), min(b.j0, j0)
	b.i1, b.j1 = max(b.i1, i1), max(b.j1, j1)
}

// auraField is the per-frame grid state: one accumulated grid per aura over
// the whole canvas, the ownership arrays for the no-overlap case, and the
// tracer scratch.
type auraField struct {
	cs      float32
	gw, gh  int32
	dl      float32
	overlap bool
	vals    [][]float32
	box     []auraBox
	best    []int32
	bestVal []float32
	hidden  []bool // per aura

	tr     tracer
	raw    rings
	sx, sy []float32
}

// compute accumulates every visible member's ramp into its auras' grids
// and, without overlap, assigns each cell to its strongest aura.
func (f *auraField) compute(g *graph, cm cam.Camera, set *auraSet, hidden map[string]struct{}, defaultRadius, w, h float32, p AuraParams) {
	f.cs = p.CellSize
	f.gw = int32(math.Ceil(float64(w / f.cs)))
	f.gh = int32(math.Ceil(float64(h / f.cs)))
	f.dl = p.DrawLimit
	f.overlap = p.Overlap
	na := len(set.ids)
	cells := int(f.gw * f.gh)
	if cap(f.vals) < na {
		f.vals = slices.Grow(f.vals, na-len(f.vals))
	}
	f.vals = f.vals[:na]
	f.box = growTo(f.box, na)
	f.hidden = growTo(f.hidden, na)
	for k := range na {
		f.vals[k] = growTo(f.vals[k], cells)
		clear(f.vals[k])
		f.box[k].reset()
		_, f.hidden[k] = hidden[set.ids[k]]
	}
	for s := range g.ids {
		mem := set.members(int32(s))
		if len(mem) == 0 {
			continue
		}
		r := g.radius[s]
		if r <= 0 {
			r = defaultRadius
		}
		sx, sy := cm.ToScreen(g.x[s], g.y[s])
		kern := kernelFor(r*cm.Zoom, p)
		R := kern.R
		if sx+R < 0 || sy+R < 0 || sx-R > w || sy-R > h {
			continue
		}
		i0 := max(int32(math.Floor(float64((sx-R)/f.cs))), 0)
		i1 := min(int32(math.Floor(float64((sx+R)/f.cs))), f.gw-1)
		j0 := max(int32(math.Floor(float64((sy-R)/f.cs))), 0)
		j1 := min(int32(math.Floor(float64((sy+R)/f.cs))), f.gh-1)
		if i1 < i0 || j1 < j0 {
			continue
		}
		for _, k := range mem {
			if f.hidden[k] {
				continue
			}
			f.box[k].add(i0, j0, i1, j1)
			vals := f.vals[k]
			for j := j0; j <= j1; j++ {
				dy := (float32(j)+0.5)*f.cs - sy
				row := vals[j*f.gw : (j+1)*f.gw]
				for i := i0; i <= i1; i++ {
					dx := (float32(i)+0.5)*f.cs - sx
					v := kern.at(float32(math.Sqrt(float64(dx*dx + dy*dy))))
					if v > 0 {
						a := row[i]
						row[i] = a + v - a*v
					}
				}
			}
		}
	}
	if f.overlap {
		return
	}
	f.best = growTo(f.best, cells)
	f.bestVal = growTo(f.bestVal, cells)
	for i := range f.best {
		f.best[i] = -1
	}
	clear(f.bestVal)
	// Aura order is id order, and the comparison is strict, so a tie goes to
	// the smaller id.
	for k := range na {
		b := f.box[k]
		if b.empty() {
			continue
		}
		vals := f.vals[k]
		for j := b.j0; j <= b.j1; j++ {
			for i := b.i0; i <= b.i1; i++ {
				idx := j*f.gw + i
				if v := vals[idx]; v > f.bestVal[idx] {
					f.bestVal[idx] = v
					f.best[idx] = int32(k)
				}
			}
		}
	}
}

// auraIso adapts one aura's grid to the tracer.
type auraIso struct {
	f *auraField
	k int32
}

func (s auraIso) value(i, j int32) float32 {
	if i < 0 || j < 0 || i >= s.f.gw || j >= s.f.gh {
		return 0
	}
	return s.f.vals[s.k][j*s.f.gw+i]
}

func (s auraIso) inside(i, j int32) bool {
	if i < 0 || j < 0 || i >= s.f.gw || j >= s.f.gh {
		return false
	}
	idx := j*s.f.gw + i
	return s.f.vals[s.k][idx] >= s.f.dl && (s.f.overlap || s.f.best[idx] == s.k)
}

// frac places the boundary where the ramp crosses the draw limit; toward a
// sample lost to another aura, whose own value is above the limit, it sits
// midway so the two auras' outlines meet.
func (s auraIso) frac(ai, aj, bi, bj int32) float32 {
	va, vb := s.value(ai, aj), s.value(bi, bj)
	if vb >= s.f.dl {
		return 0.5
	}
	t := (va - s.f.dl) / (va - vb)
	return min(max(t, 0.01), 1)
}

func (s auraIso) saddle(i, j int32) bool {
	mean := (s.value(i, j) + s.value(i+1, j) + s.value(i, j+1) + s.value(i+1, j+1)) / 4
	return mean >= s.f.dl
}

// contours appends aura k's outer rings, smoothed, to out. Holes are
// dropped: the concave fill takes one ring, so a pocket fills with the
// aura (ADR-0224 §SD11).
func (f *auraField) contours(k int32, out *rings) {
	b := f.box[k]
	if b.empty() || f.hidden[k] {
		return
	}
	f.tr.setup(f.gw, f.gh)
	f.raw.reset()
	f.tr.trace(auraIso{f, k}, f.cs, b.i0, b.j0, b.i1, b.j1, &f.raw)
	for r := range f.raw.count() {
		xs, ys := f.raw.ring(r)
		area := signedArea(xs, ys)
		if area > 0 || ringTooSmall(area, f.cs) {
			continue
		}
		f.sx, f.sy = appendSmoothed(xs, ys, out, f.sx, f.sy)
	}
}

// fill is the fill colour of the aura with id at position k of the id
// order: its style's, else the cycle colour at AuraFillAlpha.
func (inst AuraParams) fill(k int, id string) color.Color {
	if st, ok := inst.Styles[id]; ok && st.Fill.Kind() == color.ColorKindLiteral {
		return st.Fill
	}
	return color.Hex(styletokens.QualitativeCycle(k).AsHex()&^0xff | AuraFillAlpha)
}

// opaque returns the colour at full alpha, for a legend swatch.
func opaque(col color.Color) color.Color {
	if col.Kind() != color.ColorKindLiteral {
		return col
	}
	return color.Hex(col.Literal() | 0xff)
}
