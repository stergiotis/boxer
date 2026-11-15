package tiles

import (
	"context"
	"image"
	"math"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/stats"
)

// Size is the side of a tile, in the cells of its level (ADR-0373 §SD6).
const Size = 512

// Tile is one tile of a level: at level 0 the samples, and at a reduced
// level the means of the samples each cell covers, with their minima and
// maxima plane by plane. A tile is cut at the level's edge, so the last
// column and row of tiles may be smaller than [Size].
type Tile struct {
	Mean     imaging.Image
	Min, Max []imaging.Plane
}

// grid is one level's tiles, published a row of tiles at a time.
type grid struct {
	w, h, cols, rows int
	ready            int // tile rows published
	tiles            []Tile
}

// Store is an image as tiles and levels. Its methods may be called from any
// goroutine; a tile not yet built is waited for. A Store is made and filled
// by a [Builder].
type Store struct {
	width, height int
	model         imaging.ColorModelE
	kind          imaging.SampleKindE
	transforms    []imaging.ValueTransform
	reduced       int

	mu       sync.Mutex
	levels   []*grid
	changed  chan struct{}
	finished bool
	err      error
	prov     imaging.Provenance
	// lo and hi are each plane's smallest and largest valid stored sample,
	// as the builder saw them; ok says a plane had one.
	lo, hi []float64
	ok     []bool
	bytes  int64
}

// Width and Height are the image's size in pixels.
func (inst *Store) Width() int  { return inst.width }
func (inst *Store) Height() int { return inst.height }

// Model and Kind are what every tile's samples are.
func (inst *Store) Model() imaging.ColorModelE { return inst.model }
func (inst *Store) Kind() imaging.SampleKindE  { return inst.kind }

// Transform is plane i's value transform.
func (inst *Store) Transform(i int) imaging.ValueTransform {
	if i >= 0 && i < len(inst.transforms) {
		return inst.transforms[i]
	}
	return imaging.IdentityTransform
}

// Reduced is how many reduced levels the store holds, numbered 1 to Reduced.
func (inst *Store) Reduced() int { return inst.reduced }

// Provenance is where the samples came from. Its SampleHash is the digest
// of the samples once the store is finished, and zero before.
func (inst *Store) Provenance() imaging.Provenance {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.prov
}

// Finished reports whether every tile is built, and Err why building
// failed; a failed store answers every tile not built with that error.
func (inst *Store) Finished() bool {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.finished
}

func (inst *Store) Err() error {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.err
}

// Bytes is what the stored samples, means, minima and maxima cost.
func (inst *Store) Bytes() int64 {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.bytes
}

// LevelSize is level k's size in its cells.
func (inst *Store) LevelSize(k int) (w, h int) {
	if k < 0 || k > inst.reduced {
		return 0, 0
	}
	g := inst.levels[k]
	return g.w, g.h
}

// Wait returns once every tile is built, the build failed, or ctx ends.
func (inst *Store) Wait(ctx context.Context) error {
	for {
		inst.mu.Lock()
		finished, err, ch := inst.finished, inst.err, inst.changed
		inst.mu.Unlock()
		switch {
		case err != nil:
			return err
		case finished:
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
	}
}

// Tile is tile (col, row) of level k — 0 the samples, 1 to Reduced the
// reductions — waiting for it to be built when it is not yet.
func (inst *Store) Tile(ctx context.Context, k, col, row int) (t Tile, err error) {
	if k < 0 || k > inst.reduced {
		return t, eb.Build().Int("level", k).Int("reduced", inst.reduced).Errorf("store has no such level")
	}
	g := inst.levels[k]
	if col < 0 || row < 0 || col >= g.cols || row >= g.rows {
		return t, eb.Build().Int("level", k).Int("col", col).Int("row", row).Errorf("level has no such tile")
	}
	for {
		inst.mu.Lock()
		ready, failed, ch := g.ready, inst.err, inst.changed
		if row < ready {
			t = g.tiles[row*g.cols+col]
		}
		inst.mu.Unlock()
		switch {
		case row < ready:
			return t, nil
		case failed != nil:
			return t, failed
		}
		select {
		case <-ctx.Done():
			return t, ctx.Err()
		case <-ch:
		}
	}
}

// TryTile is [Store.Tile] without waiting: ok is false for a tile not built.
func (inst *Store) TryTile(k, col, row int) (t Tile, ok bool) {
	if k < 0 || k > inst.reduced {
		return t, false
	}
	g := inst.levels[k]
	if col < 0 || row < 0 || col >= g.cols || row >= g.rows {
		return t, false
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if row >= g.ready {
		return t, false
	}
	return g.tiles[row*g.cols+col], true
}

// tilesOver calls f for every level-0 tile that rect touches, with the part
// of rect in it in the tile's own pixels, waiting for each.
func (inst *Store) tilesOver(ctx context.Context, rect image.Rectangle, f func(t Tile, part image.Rectangle) error) error {
	for row := rect.Min.Y / Size; row*Size < rect.Max.Y; row++ {
		for col := rect.Min.X / Size; col*Size < rect.Max.X; col++ {
			t, err := inst.Tile(ctx, 0, col, row)
			if err != nil {
				return err
			}
			origin := image.Pt(col*Size, row*Size)
			part := rect.Intersect(image.Rect(origin.X, origin.Y, origin.X+t.Mean.Width(), origin.Y+t.Mean.Height())).Sub(origin)
			if part.Empty() {
				continue
			}
			if err = f(t, part); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkRect refuses a region that is empty or not inside the image.
func (inst *Store) checkRect(rect image.Rectangle) error {
	if rect.Empty() || !rect.In(image.Rect(0, 0, inst.width, inst.height)) {
		return eb.Build().Stringer("region", rect).Int("width", inst.width).Int("height", inst.height).
			Errorf("region is empty or not inside the image")
	}
	return nil
}

// Histogram counts plane i over rect from the level-0 tiles, as
// [stats.OfRegion] would over the whole image: one bin per value for an
// integer plane; for a float plane, 4096 bins over the region's own range,
// found by a first pass. Tiles not built yet are waited for.
func (inst *Store) Histogram(ctx context.Context, i int, rect image.Rectangle) (h stats.Histogram, err error) {
	if err = inst.checkRect(rect); err != nil {
		return
	}
	if i < 0 || i >= inst.model.PlaneCount() {
		return h, eb.Build().Int("plane", i).Errorf("image has no such plane")
	}
	binLo, binHi := 0.0, 0.0
	if inst.kind == imaging.SampleKindF32 {
		lo, hi := math.Inf(1), math.Inf(-1)
		err = inst.tilesOver(ctx, rect, func(t Tile, part image.Rectangle) error {
			th, terr := stats.OfRegion(t.Mean.Plane(i), part)
			if a, b, ok := th.StoredRange(); terr == nil && ok {
				lo, hi = min(lo, a), max(hi, b)
			}
			return terr
		})
		if err != nil {
			return
		}
		if lo <= hi {
			binLo, binHi = lo, hi
		}
	}
	err = inst.tilesOver(ctx, rect, func(t Tile, part image.Rectangle) error {
		th, terr := stats.OfRegionIn(t.Mean.Plane(i), part, binLo, binHi)
		if terr != nil {
			return terr
		}
		h, terr = stats.Merge(h, th)
		return terr
	})
	return
}

// Read copies the samples of rect out of the level-0 tiles into an image of
// its own, waiting for tiles not built yet. It costs the region's samples;
// a caller reading a whole large image this way holds it twice.
func (inst *Store) Read(ctx context.Context, rect image.Rectangle) (img imaging.Image, err error) {
	if err = inst.checkRect(rect); err != nil {
		return
	}
	n, w, h := inst.model.PlaneCount(), rect.Dx(), rect.Dy()
	dst := make([]rows, n)
	for i := range dst {
		dst[i] = rows{kind: inst.kind, w: w}
		switch inst.kind {
		case imaging.SampleKindU8:
			dst[i].u8 = make([]uint8, w*h)
		case imaging.SampleKindU16:
			dst[i].u16 = make([]uint16, w*h)
		default:
			dst[i].f32 = make([]float32, w*h)
		}
	}
	for row := rect.Min.Y / Size; row*Size < rect.Max.Y; row++ {
		for col := rect.Min.X / Size; col*Size < rect.Max.X; col++ {
			t, terr := inst.Tile(ctx, 0, col, row)
			if terr != nil {
				return img, terr
			}
			origin := image.Pt(col*Size, row*Size)
			abs := rect.Intersect(image.Rect(origin.X, origin.Y, origin.X+t.Mean.Width(), origin.Y+t.Mean.Height()))
			if abs.Empty() {
				continue
			}
			tw := t.Mean.Width()
			for i := range n {
				p := t.Mean.Plane(i)
				for y := abs.Min.Y; y < abs.Max.Y; y++ {
					src0 := (y-origin.Y)*tw + abs.Min.X - origin.X
					dst0 := (y-rect.Min.Y)*w + abs.Min.X - rect.Min.X
					cw := abs.Dx()
					switch inst.kind {
					case imaging.SampleKindU8:
						copy(dst[i].u8[dst0:dst0+cw], p.U8()[src0:src0+cw])
					case imaging.SampleKindU16:
						copy(dst[i].u16[dst0:dst0+cw], p.U16()[src0:src0+cw])
					default:
						copy(dst[i].f32[dst0:dst0+cw], p.F32()[src0:src0+cw])
					}
				}
			}
		}
	}
	planes := make([]imaging.Plane, n)
	for i := range planes {
		if planes[i], err = dst[i].take(); err != nil {
			return
		}
		planes[i], _ = planes[i].WithTransform(inst.Transform(i))
	}
	prov := inst.Provenance()
	return imaging.NewImage(inst.model, planes, prov)
}

// StoredRange is plane i's smallest and largest valid stored sample, as the
// builder saw them; ok is false before the store is finished and for a
// plane with no valid sample.
func (inst *Store) StoredRange(i int) (lo, hi float64, ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if !inst.finished || i < 0 || i >= len(inst.ok) || !inst.ok[i] {
		return 0, 0, false
	}
	return inst.lo[i], inst.hi[i], true
}
