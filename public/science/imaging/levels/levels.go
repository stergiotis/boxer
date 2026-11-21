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
// which it checks between planes and levels. A plane's declared invalid value
// (ADR-0398 §SD2) is left out of every cell and declared on every plane the
// levels hold.
func Build(ctx context.Context, img imaging.Image, opts Options) (pyramid *Pyramid, err error) {
	if img.IsZero() {
		return nil, eb.Build().Errorf("no image to reduce")
	}
	count := CountFor(img.Width(), img.Height(), opts)
	if err = checkCellSize(img.Width(), img.Height(), count); err != nil {
		return nil, err
	}
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
		plane := img.Plane(p)
		var acc accumulatorI
		for k := 1; k <= count; k++ {
			if err = ctx.Err(); err != nil {
				return nil, eb.Build().Int("level", k).Int("plane", p).Errorf("level build cancelled: %w", err)
			}
			if k == 1 {
				// The first level is summed straight from the samples: an
				// accumulator the size of the plane itself would cost
				// several times the plane's memory.
				w, h := Size(img.Width(), img.Height(), 1)
				if acc, err = accumulate(plane, 1, image.Rect(0, 0, w, h)); err != nil {
					return nil, err
				}
			} else {
				acc = acc.halve()
			}
			lv := &pyramid.levels[k-1]
			means[k-1][p], lv.Min[p], lv.Max[p], err = acc.planes(plane)
			if err != nil {
				return nil, err
			}
			cw, ch := acc.size()
			done += int64(cw) * int64(ch)
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

// checkCellSize refuses a level whose cells could cover 2^32 samples or
// more, which a cell's count does not hold: an image of over four billion
// pixels reduced sixteen times or more.
func checkCellSize(width, height, k int) error {
	f := int64(1) << min(k, 62)
	if min(f, int64(width))*min(f, int64(height)) >= 1<<32 {
		return eb.Build().Int("level", k).Int("width", width).Int("height", height).
			Errorf("a cell of this level would cover more samples than it can count")
	}
	return nil
}

// sampleI is the Go type of a plane's samples, one per sample kind.
type sampleI interface {
	uint8 | uint16 | int16 | int32 | float32 | float64
}

// sumI is what a cell's samples are summed in: int64 for an integer kind,
// float64 for a float kind.
type sumI interface {
	int64 | float64
}

// accumulatorI is one plane at one level in the form the next level is made
// from, whatever its sample kind.
type accumulatorI interface {
	// halve combines each 2 × 2 block of cells into one.
	halve() accumulatorI
	// planes turns the cells into the level's mean, minimum and maximum
	// planes, in the sample kind of the plane reduced, carrying its value
	// transform and declared invalid value.
	planes(of imaging.Plane) (mean, lo, hi imaging.Plane, err error)
	size() (w, h int)
}

// accumulator is one plane at one level: per cell the sum and count of the
// valid samples it covers and their extremes. Sums of integer samples are
// int64, exact for every cell of fewer than 2^32 samples (checkCellSize):
// |sum| ≤ 2^31 · 2^32 = 2^63. Float sums are kept in float64. The extremes
// are of the plane's own type, so they become the level's minimum and
// maximum planes without a conversion.
type accumulator[T sampleI, S sumI] struct {
	w, h   int
	sum    []S
	count  []uint32
	lo, hi []T
}

var _ accumulatorI = (*accumulator[uint8, int64])(nil)

func newAccumulator[T sampleI, S sumI](w, h int) *accumulator[T, S] {
	n := w * h
	return &accumulator[T, S]{w: w, h: h, sum: make([]S, n), count: make([]uint32, n), lo: make([]T, n), hi: make([]T, n)}
}

func (inst *accumulator[T, S]) size() (w, h int) { return inst.w, inst.h }

// halve combines each 2 × 2 block of cells into one; a block cut by the edge
// combines the cells it has, and a cell with no valid sample adds nothing.
func (inst *accumulator[T, S]) halve() accumulatorI {
	w, h := (inst.w+1)/2, (inst.h+1)/2
	out := newAccumulator[T, S](w, h)
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
					out.sum[o] += inst.sum[i]
					if first {
						out.lo[o], out.hi[o] = inst.lo[i], inst.hi[i]
					} else {
						out.lo[o], out.hi[o] = min(out.lo[o], inst.lo[i]), max(out.hi[o], inst.hi[i])
					}
					first = false
				}
			}
		}
	}
	return out
}

// planes turns the accumulated cells into the level's mean, minimum and
// maximum planes, in the plane's own sample kind (ADR-0398 §SD1, §SD2). An
// integer mean is the sum over the count rounded to nearest, halves up. A
// cell with no valid sample is invalid in all three: NaN in a float plane,
// the declared value in an integer one. A mean that would land on the
// declared value — a rounded integer mean, or a float mean that came out
// equal to it — is moved to the neighbouring representable value on the
// side of the cell's exact mean, up when the exact mean is the declared value
// itself. The cell's samples are not the declared value, so its mean can
// only land there when samples lie on both sides of it, and both neighbours
// are within the cell's minimum and maximum. A level is for drawing; no
// number is read from it (ADR-0373 §SD8).
func (inst *accumulator[T, S]) planes(of imaging.Plane) (mean, lo, hi imaging.Plane, err error) {
	n := inst.w * inst.h
	inv, hasInv := of.Invalid()
	m := make([]T, n)
	a, b := inst.lo, inst.hi
	switch sums := any(inst.sum).(type) {
	case []int64:
		for i, c := range inst.count {
			if c == 0 {
				// Only a plane with a declared value has a cell with no valid
				// sample.
				m[i], a[i], b[i] = T(inv), T(inv), T(inv)
				continue
			}
			q, below := roundedMean(sums[i], c)
			if hasInv && float64(q) == inv {
				if below {
					q--
				} else {
					q++
				}
			}
			m[i] = T(q)
		}
	case []float64:
		var zero T
		_, single := any(zero).(float32)
		nan := T(math.NaN())
		for i, c := range inst.count {
			if c == 0 {
				m[i], a[i], b[i] = nan, nan, nan
				continue
			}
			exact := sums[i] / float64(c)
			v := T(exact)
			if hasInv && float64(v) == inv {
				dir := math.Inf(1)
				if exact < inv {
					dir = math.Inf(-1)
				}
				if single {
					v = T(math.Nextafter32(float32(inv), float32(dir)))
				} else {
					v = T(math.Nextafter(inv, dir))
				}
			}
			m[i] = v
		}
	}
	mk := func(s []T) (p imaging.Plane, e error) {
		if p, e = newPlane(inst.w, inst.h, s); e != nil {
			return
		}
		if hasInv {
			if p, e = p.WithInvalid(inv); e != nil {
				return
			}
		}
		return p.WithTransform(of.Transform())
	}
	if mean, err = mk(m); err != nil {
		return
	}
	if lo, err = mk(a); err != nil {
		return
	}
	hi, err = mk(b)
	return
}

// roundedMean is sum over count rounded to nearest with halves up, and
// whether the exact mean is below that. It divides with the remainder
// rather than adding half the count to the sum, which could leave int64.
func roundedMean(sum int64, count uint32) (q int64, below bool) {
	c := int64(count)
	q, r := sum/c, sum%c
	if r < 0 {
		q, r = q-1, r+c
	}
	// The exact mean is q + r/c with 0 ≤ r < c.
	if 2*r >= c {
		return q + 1, true
	}
	return q, false
}

// newPlane makes a plane of the kind of T.
func newPlane[T sampleI](w, h int, s []T) (imaging.Plane, error) {
	switch v := any(s).(type) {
	case []uint8:
		return imaging.NewPlaneU8(w, h, v)
	case []uint16:
		return imaging.NewPlaneU16(w, h, v)
	case []int16:
		return imaging.NewPlaneI16(w, h, v)
	case []int32:
		return imaging.NewPlaneI32(w, h, v)
	case []float32:
		return imaging.NewPlaneF32(w, h, v)
	case []float64:
		return imaging.NewPlaneF64(w, h, v)
	}
	return imaging.Plane{}, eb.Build().Errorf("no plane for this sample type")
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
	if err = checkCellSize(plane.Width(), plane.Height(), k); err != nil {
		return
	}
	acc, err := accumulate(plane, k, rect)
	if err != nil {
		return
	}
	return acc.planes(plane)
}

// accumulate sums the samples under each cell of level k inside rect. A
// sample kind it does not know is refused by name.
func accumulate(plane imaging.Plane, k int, rect image.Rectangle) (accumulatorI, error) {
	inv, hasInv := plane.Invalid()
	pw, ph := plane.Width(), plane.Height()
	switch plane.Kind() {
	case imaging.SampleKindU8:
		return accumulateOf[uint8, int64](plane.U8(), pw, ph, k, rect, inv, hasInv), nil
	case imaging.SampleKindU16:
		return accumulateOf[uint16, int64](plane.U16(), pw, ph, k, rect, inv, hasInv), nil
	case imaging.SampleKindI16:
		return accumulateOf[int16, int64](plane.I16(), pw, ph, k, rect, inv, hasInv), nil
	case imaging.SampleKindI32:
		return accumulateOf[int32, int64](plane.I32(), pw, ph, k, rect, inv, hasInv), nil
	case imaging.SampleKindF32:
		return accumulateOf[float32, float64](plane.F32(), pw, ph, k, rect, inv, hasInv), nil
	case imaging.SampleKindF64:
		return accumulateOf[float64, float64](plane.F64(), pw, ph, k, rect, inv, hasInv), nil
	}
	return nil, eb.Build().Stringer("kind", plane.Kind()).Errorf("no reduction for planes of this sample kind")
}

// accumulateOf is accumulate over the samples of one Go type. NaN and the
// declared invalid value are left out.
func accumulateOf[T sampleI, S sumI](samples []T, pw, ph, k int, rect image.Rectangle, inv float64, hasInv bool) *accumulator[T, S] {
	f := 1 << k
	a := newAccumulator[T, S](rect.Dx(), rect.Dy())
	// The declared value is one the kind holds exactly (Plane.WithInvalid),
	// so the conversion is exact.
	skip := T(inv)
	for cy := rect.Min.Y; cy < rect.Max.Y; cy++ {
		y1 := min((cy+1)*f, ph)
		for cx := rect.Min.X; cx < rect.Max.X; cx++ {
			x1 := min((cx+1)*f, pw)
			o := (cy-rect.Min.Y)*a.w + (cx - rect.Min.X)
			first := true
			var sum S
			for y := cy * f; y < y1; y++ {
				for _, v := range samples[y*pw+cx*f : y*pw+x1] {
					if v != v || (hasInv && v == skip) {
						continue
					}
					sum += S(v)
					if first {
						a.lo[o], a.hi[o] = v, v
					} else {
						a.lo[o], a.hi[o] = min(a.lo[o], v), max(a.hi[o], v)
					}
					a.count[o]++
					first = false
				}
			}
			a.sum[o] = sum
		}
	}
	return a
}
