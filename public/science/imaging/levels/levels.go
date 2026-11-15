package levels

import (
	"context"
	"image"
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// Level is one reduction of an image. Mean is an image of the same colour
// model whose samples are the cell means; Min and Max hold, plane by plane,
// the extremes of what each cell covers.
type Level struct {
	// Factor is the side of the block of image samples one cell covers.
	Factor int
	Mean   imaging.Image
	Min    []imaging.Plane
	Max    []imaging.Plane
}

// Pyramid is an image's reduced levels. Level 0 is the image itself and is
// not stored here.
type Pyramid struct {
	levels []Level
}

// Count is the number of reduced levels: the highest k [Pyramid.Level]
// answers.
func (inst *Pyramid) Count() int {
	if inst == nil {
		return 0
	}
	return len(inst.levels)
}

// Level returns reduction k, from 1 for the first halving, and ok false for
// a level that was not built.
func (inst *Pyramid) Level(k int) (level Level, ok bool) {
	if inst == nil || k < 1 || k > len(inst.levels) {
		return Level{}, false
	}
	return inst.levels[k-1], true
}

// Options configures a build. The zero value is usable.
type Options struct {
	// MinSide stops the halving once the longer side of a level is at most
	// this; 0 means 512, the side of one tile.
	MinSide int
	// MaxLevels bounds the number of levels; 0 means 8.
	MaxLevels int
	// Progress, when set, is called from the building goroutine with the
	// samples reduced so far and the total.
	Progress func(done, total int64)
}

// Size is the width and height of level k of an image of the given size:
// each halving rounds up, so a level always covers the whole image.
func Size(width, height, k int) (w, h int) {
	f := 1 << k
	return (width + f - 1) / f, (height + f - 1) / f
}

// CountFor is how many levels a build makes for an image of the given size.
func CountFor(width, height int, opts Options) int {
	minSide, maxLevels := opts.MinSide, opts.MaxLevels
	if minSide <= 0 {
		minSide = 512
	}
	if maxLevels <= 0 {
		maxLevels = 8
	}
	k := 0
	for k < maxLevels {
		w, h := Size(width, height, k)
		if max(w, h) <= minSide {
			break
		}
		k++
	}
	return k
}

// Build reduces an image level by level. It can be cancelled through ctx,
// which it checks between planes and levels.
func Build(ctx context.Context, img imaging.Image, opts Options) (pyramid *Pyramid, err error) {
	if img.IsZero() {
		return nil, eb.Build().Errorf("no image to reduce")
	}
	count := CountFor(img.Width(), img.Height(), opts)
	pyramid = &Pyramid{levels: make([]Level, count)}
	var total, done int64
	for k := 1; k <= count; k++ {
		w, h := Size(img.Width(), img.Height(), k)
		total += int64(w) * int64(h) * int64(img.PlaneCount())
	}
	means := make([][]imaging.Plane, count)
	for k := range count {
		pyramid.levels[k] = Level{
			Factor: 1 << (k + 1),
			Min:    make([]imaging.Plane, img.PlaneCount()),
			Max:    make([]imaging.Plane, img.PlaneCount()),
		}
		means[k] = make([]imaging.Plane, img.PlaneCount())
	}
	for p := range img.PlaneCount() {
		var acc *accumulator
		for k := 1; k <= count; k++ {
			if err = ctx.Err(); err != nil {
				return nil, eb.Build().Int("level", k).Int("plane", p).Errorf("level build cancelled: %w", err)
			}
			if k == 1 {
				// The first level is summed straight from the samples: an
				// accumulator the size of the plane itself would cost
				// several times the plane's memory.
				w, h := Size(img.Width(), img.Height(), 1)
				acc = accumulate(img.Plane(p), 1, image.Rect(0, 0, w, h))
			} else {
				acc = acc.halve()
			}
			lv := &pyramid.levels[k-1]
			means[k-1][p], lv.Min[p], lv.Max[p], err = acc.planes(img.Plane(p).Transform())
			if err != nil {
				return nil, err
			}
			done += int64(acc.w) * int64(acc.h)
			if opts.Progress != nil {
				opts.Progress(done, total)
			}
		}
	}
	for k := range count {
		prov := img.Provenance()
		pyramid.levels[k].Mean, err = imaging.NewImage(img.Model(), means[k], prov)
		if err != nil {
			return nil, err
		}
	}
	return pyramid, nil
}

// accumulator is one plane at one level in the form the next level is made
// from: per cell the sum and count of the valid samples it covers and their
// extremes. Sums of integer samples are exact in 64 bits for any plane this
// package can be given; float sums are kept in float64.
type accumulator struct {
	kind  imaging.SampleKindE
	w, h  int
	sum   []float64 // float planes
	isum  []uint64  // integer planes
	count []uint32
	lo    []float32 // float planes
	hi    []float32
	ilo   []uint16 // integer planes
	ihi   []uint16
}

// halve combines each 2 × 2 block of cells into one; a block cut by the edge
// combines the cells it has.
func (inst *accumulator) halve() *accumulator {
	w, h := (inst.w+1)/2, (inst.h+1)/2
	n := w * h
	out := &accumulator{kind: inst.kind, w: w, h: h, count: make([]uint32, n)}
	integer := inst.kind.IsInteger()
	if integer {
		out.isum, out.ilo, out.ihi = make([]uint64, n), make([]uint16, n), make([]uint16, n)
	} else {
		out.sum, out.lo, out.hi = make([]float64, n), make([]float32, n), make([]float32, n)
	}
	for cy := range h {
		for cx := range w {
			o := cy*w + cx
			first := true
			for dy := range 2 {
				y := 2*cy + dy
				if y >= inst.h {
					continue
				}
				for dx := range 2 {
					x := 2*cx + dx
					if x >= inst.w {
						continue
					}
					i := y*inst.w + x
					if inst.count[i] == 0 {
						continue
					}
					out.count[o] += inst.count[i]
					if integer {
						out.isum[o] += inst.isum[i]
						if first {
							out.ilo[o], out.ihi[o] = inst.ilo[i], inst.ihi[i]
						} else {
							out.ilo[o], out.ihi[o] = min(out.ilo[o], inst.ilo[i]), max(out.ihi[o], inst.ihi[i])
						}
					} else {
						out.sum[o] += inst.sum[i]
						if first {
							out.lo[o], out.hi[o] = inst.lo[i], inst.hi[i]
						} else {
							out.lo[o], out.hi[o] = min(out.lo[o], inst.lo[i]), max(out.hi[o], inst.hi[i])
						}
					}
					first = false
				}
			}
		}
	}
	return out
}

// planes turns the accumulated cells into the level's mean, minimum and
// maximum planes, in the plane's own sample kind.
func (inst *accumulator) planes(transform imaging.ValueTransform) (mean, lo, hi imaging.Plane, err error) {
	n := inst.w * inst.h
	mk := func(p imaging.Plane, e error) (imaging.Plane, error) {
		if e != nil {
			return p, e
		}
		return p.WithTransform(transform)
	}
	switch inst.kind {
	case imaging.SampleKindU8:
		m, a, b := make([]uint8, n), make([]uint8, n), make([]uint8, n)
		for i := range m {
			m[i], a[i], b[i] = uint8(roundedMean(inst.isum[i], inst.count[i])), uint8(inst.ilo[i]), uint8(inst.ihi[i])
		}
		if mean, err = mk(imaging.NewPlaneU8(inst.w, inst.h, m)); err != nil {
			return
		}
		if lo, err = mk(imaging.NewPlaneU8(inst.w, inst.h, a)); err != nil {
			return
		}
		hi, err = mk(imaging.NewPlaneU8(inst.w, inst.h, b))
	case imaging.SampleKindU16:
		m, a, b := make([]uint16, n), make([]uint16, n), make([]uint16, n)
		for i := range m {
			m[i], a[i], b[i] = uint16(roundedMean(inst.isum[i], inst.count[i])), inst.ilo[i], inst.ihi[i]
		}
		if mean, err = mk(imaging.NewPlaneU16(inst.w, inst.h, m)); err != nil {
			return
		}
		if lo, err = mk(imaging.NewPlaneU16(inst.w, inst.h, a)); err != nil {
			return
		}
		hi, err = mk(imaging.NewPlaneU16(inst.w, inst.h, b))
	case imaging.SampleKindF32:
		nan := float32(math.NaN())
		m, a, b := make([]float32, n), make([]float32, n), make([]float32, n)
		for i := range m {
			if inst.count[i] == 0 {
				m[i], a[i], b[i] = nan, nan, nan
				continue
			}
			m[i], a[i], b[i] = float32(inst.sum[i]/float64(inst.count[i])), inst.lo[i], inst.hi[i]
		}
		if mean, err = mk(imaging.NewPlaneF32(inst.w, inst.h, m)); err != nil {
			return
		}
		if lo, err = mk(imaging.NewPlaneF32(inst.w, inst.h, a)); err != nil {
			return
		}
		hi, err = mk(imaging.NewPlaneF32(inst.w, inst.h, b))
	default:
		err = eb.Build().Stringer("kind", inst.kind).Errorf("no reduction for planes of this sample kind")
	}
	return
}

// roundedMean is sum over count, rounded to nearest with halves up.
func roundedMean(sum uint64, count uint32) uint64 {
	if count == 0 {
		return 0
	}
	return (sum + uint64(count)/2) / uint64(count)
}

// Reduce computes the cells of level k inside rect — given in that level's
// own cell coordinates — directly from the plane's samples, with the same
// result [Build] gives for them. The planes it returns are rect's size.
func Reduce(plane imaging.Plane, k int, rect image.Rectangle) (mean, lo, hi imaging.Plane, err error) {
	lw, lh := Size(plane.Width(), plane.Height(), k)
	if k < 0 || k > 30 || rect.Empty() || !rect.In(image.Rect(0, 0, lw, lh)) {
		return mean, lo, hi, eb.Build().Int("level", k).Stringer("region", rect).Int("levelWidth", lw).Int("levelHeight", lh).
			Errorf("region to reduce is empty or not inside the level")
	}
	return accumulate(plane, k, rect).planes(plane.Transform())
}

// accumulate sums the samples under each cell of level k inside rect.
func accumulate(plane imaging.Plane, k int, rect image.Rectangle) *accumulator {
	f := 1 << k
	a := &accumulator{kind: plane.Kind(), w: rect.Dx(), h: rect.Dy(), count: make([]uint32, rect.Dx()*rect.Dy())}
	n := a.w * a.h
	integer := plane.Kind().IsInteger()
	if integer {
		a.isum, a.ilo, a.ihi = make([]uint64, n), make([]uint16, n), make([]uint16, n)
	} else {
		a.sum, a.lo, a.hi = make([]float64, n), make([]float32, n), make([]float32, n)
	}
	pw := plane.Width()
	u8, u16, f32 := plane.U8(), plane.U16(), plane.F32()
	for cy := rect.Min.Y; cy < rect.Max.Y; cy++ {
		y1 := min((cy+1)*f, plane.Height())
		for cx := rect.Min.X; cx < rect.Max.X; cx++ {
			x1 := min((cx+1)*f, pw)
			o := (cy-rect.Min.Y)*a.w + (cx - rect.Min.X)
			first := true
			for y := cy * f; y < y1; y++ {
				for x := cx * f; x < x1; x++ {
					i := y*pw + x
					if integer {
						var v uint16
						if u8 != nil {
							v = uint16(u8[i])
						} else {
							v = u16[i]
						}
						a.isum[o] += uint64(v)
						if first {
							a.ilo[o], a.ihi[o] = v, v
						} else {
							a.ilo[o], a.ihi[o] = min(a.ilo[o], v), max(a.ihi[o], v)
						}
					} else {
						v := f32[i]
						if v != v {
							continue
						}
						a.sum[o] += float64(v)
						if first {
							a.lo[o], a.hi[o] = v, v
						} else {
							a.lo[o], a.hi[o] = min(a.lo[o], v), max(a.hi[o], v)
						}
					}
					a.count[o]++
					first = false
				}
			}
		}
	}
	return a
}
