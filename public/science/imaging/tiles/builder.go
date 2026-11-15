package tiles

import (
	"context"
	"image"
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/levels"
)

// Builder fills a [Store] from an image's rows, top to bottom. It is used
// from one goroutine; the store it fills may be read from any.
type Builder struct {
	s      *Store
	band   []rows   // level 0: the rows of the band being gathered
	acc    [][]rows // acc[k][3*i+j]: level k's rows of plane i, j = mean, min, max
	accRow []int    // the tile row of each level being gathered
	added  int      // rows added so far
	digest *imaging.Digest
	opts   Options
}

// Options adjust a build.
type Options struct {
	// Levels is how many reduced levels to build; 0 takes what a viewer
	// reduces to (levels.CountFor with its defaults), and a value above 8
	// is held at 8, so that a 512-row band holds whole cells of every level.
	Levels int
	// Progress, when not nil, is told the rows added so far and the total.
	Progress func(done, total int64)
}

// maxSide and maxPixels bound the image a store takes: sides a plane can
// index, and a count of pixels whose samples a machine could hold. A
// decoder's own limit is normally far below.
const (
	maxSide   = 1 << 30
	maxPixels = 1 << 36
)

// maxLevels is the most levels a band of [Size] rows holds whole cells of
// at a cell height of 2^k: 2^9 = 512, held to the levels package's own
// default of 8.
const maxLevels = 8

// NewBuilder starts a store for an image of the given size, model and
// sample kind. transforms are the planes' value transforms (nil for the
// identity); prov is recorded, and its SampleHash set when the store is
// finished.
func NewBuilder(width, height int, model imaging.ColorModelE, kind imaging.SampleKindE, transforms []imaging.ValueTransform,
	prov imaging.Provenance, opts Options) (b *Builder, err error) {
	switch {
	case width <= 0 || height <= 0:
		return nil, eb.Build().Int("width", width).Int("height", height).Errorf("image has no size")
	case width > maxSide || height > maxSide || int64(width)*int64(height) > maxPixels:
		return nil, eb.Build().Int("width", width).Int("height", height).Errorf("image is larger than a store holds")
	case model.PlaneCount() == 0 || kind == 0:
		return nil, eb.Build().Errorf("image names no colour model or sample kind")
	case len(transforms) > model.PlaneCount():
		return nil, eb.Build().Int("transforms", len(transforms)).Errorf("more value transforms than planes")
	}
	reduced := opts.Levels
	if reduced <= 0 {
		reduced = levels.CountFor(width, height, levels.Options{})
	}
	reduced = min(reduced, maxLevels)
	n := model.PlaneCount()
	tr := make([]imaging.ValueTransform, n)
	for i := range tr {
		tr[i] = imaging.IdentityTransform
		if i < len(transforms) && transforms[i].Scale != 0 {
			tr[i] = transforms[i]
		}
	}
	s := &Store{width: width, height: height, model: model, kind: kind, transforms: tr, reduced: reduced,
		changed: make(chan struct{}), prov: prov,
		lo: make([]float64, n), hi: make([]float64, n), ok: make([]bool, n)}
	for i := range n {
		s.lo[i], s.hi[i] = math.Inf(1), math.Inf(-1)
	}
	s.prov.SampleHash = [32]byte{}
	s.levels = make([]*grid, reduced+1)
	for k := range s.levels {
		w, h := levels.Size(width, height, k)
		cols, rows := (w+Size-1)/Size, (h+Size-1)/Size
		s.levels[k] = &grid{w: w, h: h, cols: cols, rows: rows, tiles: make([]Tile, cols*rows)}
	}
	b = &Builder{s: s, band: make([]rows, n), acc: make([][]rows, reduced+1), accRow: make([]int, reduced+1),
		digest: imaging.NewDigest(model, kind, width, height), opts: opts}
	for i := range b.band {
		b.band[i] = rows{kind: kind, w: width}
	}
	for k := 1; k <= reduced; k++ {
		w, _ := levels.Size(width, height, k)
		b.acc[k] = make([]rows, 3*n)
		for j := range b.acc[k] {
			b.acc[k][j] = rows{kind: kind, w: w}
		}
	}
	return b, nil
}

// Store is the store being filled; it can be handed to readers at once.
func (inst *Builder) Store() *Store { return inst.s }

// Add takes the next rows of the image: an image as wide as the store, of
// its model and kind, any number of rows high. Rows past the image's height
// are refused.
func (inst *Builder) Add(r imaging.Image) error {
	if r.Model() != inst.s.model {
		return eb.Build().Stringer("model", r.Model()).Errorf("rows do not match the image being built")
	}
	planes := make([]imaging.Plane, r.PlaneCount())
	for i := range planes {
		planes[i] = r.Plane(i)
	}
	return inst.AddPlanes(planes)
}

// AddPlanes is [Builder.Add] with the rows given as one plane per plane of
// the model, all as wide as the store and equally high: what a decoder
// produces, without assembling an image around it.
func (inst *Builder) AddPlanes(planes []imaging.Plane) error {
	s := inst.s
	if len(planes) != s.model.PlaneCount() {
		return eb.Build().Int("planes", len(planes)).Stringer("model", s.model).Errorf("rows do not match the image being built")
	}
	h := planes[0].Height()
	for _, p := range planes {
		if p.Width() != s.width || p.Kind() != s.kind || p.Height() != h {
			return eb.Build().Int("width", p.Width()).Stringer("kind", p.Kind()).Int("height", p.Height()).
				Errorf("rows do not match the image being built")
		}
	}
	if inst.added+h > s.height {
		return eb.Build().Int("rows", inst.added+h).Int("height", s.height).Errorf("more rows than the image has")
	}
	for i := range s.model.PlaneCount() {
		p := planes[i]
		inst.digest.WritePlane(i, p)
		if lo, hi, ok := p.StoredRange(); ok {
			s.mu.Lock()
			s.lo[i], s.hi[i], s.ok[i] = min(s.lo[i], lo), max(s.hi[i], hi), true
			s.mu.Unlock()
		}
	}
	for y := 0; y < h; {
		take := min(Size-inst.band[0].count(), h-y)
		for i := range inst.band {
			inst.band[i].add(planes[i], y, y+take)
		}
		y += take
		inst.added += take
		if inst.band[0].count() == Size {
			if err := inst.flushBand(); err != nil {
				return err
			}
		}
	}
	if inst.opts.Progress != nil {
		inst.opts.Progress(int64(inst.added), int64(s.height))
	}
	return nil
}

// Finish builds what is left once every row is added, and marks the store
// finished with the digest of its samples.
func (inst *Builder) Finish() error {
	s := inst.s
	if inst.added != s.height {
		err := eb.Build().Int("rows", inst.added).Int("height", s.height).Errorf("image ended before its last row")
		inst.Fail(err)
		return err
	}
	if inst.band[0].count() > 0 {
		if err := inst.flushBand(); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.finished = true
	s.prov.SampleHash = inst.digest.Sum()
	inst.wakeLocked()
	s.mu.Unlock()
	return nil
}

// Fail ends the build with err: a reader waiting on a tile not built gets
// it, and so does every later one.
func (inst *Builder) Fail(err error) {
	s := inst.s
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	inst.wakeLocked()
	s.mu.Unlock()
}

// wakeLocked wakes every reader waiting on the store.
func (inst *Builder) wakeLocked() {
	close(inst.s.changed)
	inst.s.changed = make(chan struct{})
}

// flushBand cuts the band gathered into level-0 tiles, reduces it to every
// level, and cuts each level's rows into tiles once a tile row is complete.
// The band starts on a multiple of 512 rows, so every cell of every level up
// to 2^9 rows high lies wholly in one band, and the band's reduction is that
// of the whole image.
func (inst *Builder) flushBand() (err error) {
	s := inst.s
	n := s.model.PlaneCount()
	planes := make([]imaging.Plane, n)
	for i := range planes {
		if planes[i], err = inst.band[i].take(); err != nil {
			return err
		}
	}
	band, err := imaging.NewImage(s.model, planes, imaging.Provenance{})
	if err != nil {
		return err
	}
	last := inst.added == s.height
	tiles0 := make([]Tile, s.levels[0].cols)
	for col := range tiles0 {
		x0, x1 := col*Size, min((col+1)*Size, s.width)
		ps := make([]imaging.Plane, n)
		for i := range ps {
			if ps[i], err = cut(planes[i], x0, x1); err != nil {
				return err
			}
			ps[i], _ = ps[i].WithTransform(s.transforms[i])
		}
		if tiles0[col].Mean, err = imaging.NewImage(s.model, ps, imaging.Provenance{}); err != nil {
			return err
		}
	}
	inst.publish(0, inst.accRow[0], tiles0)
	inst.accRow[0]++
	if s.reduced == 0 {
		return nil
	}
	reductions, err := reduceBand(band, s.reduced)
	if err != nil {
		return err
	}
	for k := 1; k <= s.reduced; k++ {
		red := reductions[k-1]
		for i := range n {
			inst.acc[k][3*i].add(red.Mean.Plane(i), 0, red.Mean.Height())
			inst.acc[k][3*i+1].add(red.Min[i], 0, red.Mean.Height())
			inst.acc[k][3*i+2].add(red.Max[i], 0, red.Mean.Height())
		}
		if inst.acc[k][0].count() == Size || last {
			if err = inst.flushLevel(k); err != nil {
				return err
			}
		}
	}
	return nil
}

// flushLevel cuts level k's gathered rows into one row of tiles.
func (inst *Builder) flushLevel(k int) (err error) {
	s := inst.s
	n := s.model.PlaneCount()
	g := s.levels[k]
	whole := make([][3]imaging.Plane, n)
	for i := range n {
		for j := range 3 {
			if whole[i][j], err = inst.acc[k][3*i+j].take(); err != nil {
				return err
			}
		}
	}
	tiles := make([]Tile, g.cols)
	for col := range tiles {
		x0, x1 := col*Size, min((col+1)*Size, g.w)
		means := make([]imaging.Plane, n)
		t := Tile{Min: make([]imaging.Plane, n), Max: make([]imaging.Plane, n)}
		for i := range n {
			if means[i], err = cut(whole[i][0], x0, x1); err != nil {
				return err
			}
			means[i], _ = means[i].WithTransform(s.transforms[i])
			if t.Min[i], err = cut(whole[i][1], x0, x1); err != nil {
				return err
			}
			if t.Max[i], err = cut(whole[i][2], x0, x1); err != nil {
				return err
			}
			t.Min[i], _ = t.Min[i].WithTransform(s.transforms[i])
			t.Max[i], _ = t.Max[i].WithTransform(s.transforms[i])
		}
		if t.Mean, err = imaging.NewImage(s.model, means, imaging.Provenance{}); err != nil {
			return err
		}
		tiles[col] = t
	}
	inst.publish(k, inst.accRow[k], tiles)
	inst.accRow[k]++
	return nil
}

// publish stores a row of tiles of level k and wakes the readers.
func (inst *Builder) publish(k, row int, tiles []Tile) {
	s := inst.s
	g := s.levels[k]
	var cost int64
	for _, t := range tiles {
		px := int64(t.Mean.Width()) * int64(t.Mean.Height()) * int64(s.model.PlaneCount())
		if t.Min != nil {
			px *= 3
		}
		cost += px * sampleBytes(s.kind)
	}
	s.mu.Lock()
	copy(g.tiles[row*g.cols:], tiles)
	g.ready = row + 1
	s.bytes += cost
	inst.wakeLocked()
	s.mu.Unlock()
}

// reduceBand reduces a band to levels 1 to count, each cell exactly as
// levels.Build makes it from the whole image. A band too small for the
// build to reach a level — the last few rows of a narrow image — is reduced
// for that level directly.
func reduceBand(band imaging.Image, count int) (out []levels.Level, err error) {
	py, err := levels.Build(context.Background(), band, levels.Options{MinSide: 1, MaxLevels: count})
	if err != nil {
		return nil, err
	}
	out = make([]levels.Level, count)
	for k := 1; k <= count; k++ {
		if lv, ok := py.Level(k); ok {
			out[k-1] = lv
			continue
		}
		w, h := levels.Size(band.Width(), band.Height(), k)
		means := make([]imaging.Plane, band.PlaneCount())
		lv := levels.Level{Factor: 1 << k, Min: make([]imaging.Plane, band.PlaneCount()), Max: make([]imaging.Plane, band.PlaneCount())}
		for i := range means {
			if means[i], lv.Min[i], lv.Max[i], err = levels.Reduce(band.Plane(i), k, imageRect(w, h)); err != nil {
				return nil, err
			}
		}
		if lv.Mean, err = imaging.NewImage(band.Model(), means, imaging.Provenance{}); err != nil {
			return nil, err
		}
		out[k-1] = lv
	}
	return out, nil
}

// FromImage builds a store from an image in memory, all at once. The store
// holds a copy; the image may be dropped afterwards.
func FromImage(ctx context.Context, img imaging.Image, opts Options) (s *Store, err error) {
	if img.IsZero() {
		return nil, eb.Build().Errorf("no image to store")
	}
	tr := make([]imaging.ValueTransform, img.PlaneCount())
	for i := range tr {
		tr[i] = img.Plane(i).Transform()
	}
	b, err := NewBuilder(img.Width(), img.Height(), img.Model(), img.Kind(), tr, img.Provenance(), opts)
	if err != nil {
		return nil, err
	}
	for y := 0; y < img.Height(); y += Size {
		if err = ctx.Err(); err != nil {
			b.Fail(err)
			return nil, err
		}
		y1 := min(y+Size, img.Height())
		planes := make([]imaging.Plane, img.PlaneCount())
		for i := range planes {
			if planes[i], err = rowsOf(img.Plane(i), y, y1); err != nil {
				return nil, err
			}
		}
		band, berr := imaging.NewImage(img.Model(), planes, imaging.Provenance{})
		if berr != nil {
			return nil, berr
		}
		if err = b.Add(band); err != nil {
			return nil, err
		}
	}
	return b.Store(), b.Finish()
}

// rowsOf is rows [y0, y1) of p as a plane, sharing its samples.
func rowsOf(p imaging.Plane, y0, y1 int) (imaging.Plane, error) {
	w := p.Width()
	switch p.Kind() {
	case imaging.SampleKindU8:
		return imaging.NewPlaneU8(w, y1-y0, p.U8()[y0*w:y1*w])
	case imaging.SampleKindU16:
		return imaging.NewPlaneU16(w, y1-y0, p.U16()[y0*w:y1*w])
	}
	return imaging.NewPlaneF32(w, y1-y0, p.F32()[y0*w:y1*w])
}

// imageRect is the rectangle of a w × h grid from the origin.
func imageRect(w, h int) image.Rectangle { return image.Rect(0, 0, w, h) }
