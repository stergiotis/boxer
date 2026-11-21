package regions

import (
	"context"
	"image"
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/stats"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
)

// MethodId identifies the rules every result of this package was produced
// under: the inclusion rule and its tie rule (ADR-0399 §SD3), coverage by
// area (§SD4), the statistics (§SD7) and the profile (§SD8). A change to any
// of them is a new identifier (§SD9).
const MethodId = "regions/1"

// DefaultMaxPixels is the largest rectangle, in pixels (64 × 2^20), a
// measurement copies out of a source that copies to answer, when the
// options give none.
const DefaultMaxPixels = 64 << 20

// SourceI is where a measurement's samples come from: an image in memory
// ([ImageSource]) or a tiled store ([StoreSource]). Every read is of the
// samples at full resolution (ADR-0373 §SD8).
type SourceI interface {
	// Size is the image's width and height in pixels.
	Size() (width, height int)
	// Placement is where the image's pixels are; the zero placement when
	// nothing says so.
	Placement() imaging.Placement
	// PlaneLabels names each plane in storage order.
	PlaneLabels() []string
	// Read is an image holding at least the pixels of rect, which lies
	// inside the image, and the position in the source of its pixel (0, 0).
	// A source that copies samples to answer refuses a rect of more than
	// maxPixels pixels, naming the limit.
	Read(ctx context.Context, rect image.Rectangle, maxPixels int64) (img imaging.Image, origin image.Point, err error)
}

// ImageSource is an image in memory as a source; a read copies nothing.
func ImageSource(img imaging.Image) SourceI { return imageSource{img: img} }

type imageSource struct {
	img imaging.Image
}

var _ SourceI = imageSource{}

func (inst imageSource) Size() (width, height int)    { return inst.img.Width(), inst.img.Height() }
func (inst imageSource) Placement() imaging.Placement { return inst.img.Placement() }
func (inst imageSource) PlaneLabels() []string        { return inst.img.PlaneLabels() }

func (inst imageSource) Read(ctx context.Context, rect image.Rectangle, _ int64) (img imaging.Image, origin image.Point, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	if inst.img.IsZero() || !rect.In(image.Rect(0, 0, inst.img.Width(), inst.img.Height())) {
		return img, origin, eb.Build().Stringer("rect", rect).Int("width", inst.img.Width()).Int("height", inst.img.Height()).
			Errorf("rectangle to read is not inside the image")
	}
	return inst.img, image.Point{}, nil
}

// StoreSource is a tiled store as a source; a read copies the rectangle's
// level-0 samples out of the tiles ([tiles.Store.Read]), waiting for tiles
// not built yet.
func StoreSource(s *tiles.Store) SourceI { return storeSource{s: s} }

type storeSource struct {
	s *tiles.Store
}

var _ SourceI = storeSource{}

func (inst storeSource) Size() (width, height int)    { return inst.s.Width(), inst.s.Height() }
func (inst storeSource) Placement() imaging.Placement { return inst.s.Placement() }
func (inst storeSource) PlaneLabels() []string        { return inst.s.PlaneLabels() }

func (inst storeSource) Read(ctx context.Context, rect image.Rectangle, maxPixels int64) (img imaging.Image, origin image.Point, err error) {
	if n := int64(rect.Dx()) * int64(rect.Dy()); n > maxPixels {
		return img, origin, eb.Build().Stringer("rect", rect).Int64("pixels", n).Int64("limit", maxPixels).
			Errorf("rectangle to read from the store is over the pixel limit")
	}
	img, err = inst.s.Read(ctx, rect)
	return img, rect.Min, err
}

// MeasureOptions are what a measurement may vary. The zero value measures
// by centre, with the default percentiles, tolerance and pixel limit.
type MeasureOptions struct {
	// Area adds, for an area shape, the figures of coverage by area
	// (ADR-0399 §SD4) beside those by centre.
	Area bool
	// Percentiles are the percentiles reported, each in [0, 100]; nil is
	// 1, 50 and 99.
	Percentiles []float64
	// Tolerance is the tolerance, in pixels, of a shape approximated on the
	// way to pixels; zero is [DefaultTolerance].
	Tolerance float64
	// MaxPixels bounds the rectangle a source that copies may copy; zero is
	// [DefaultMaxPixels].
	MaxPixels int64
}

func (inst MeasureOptions) withDefaults() (o MeasureOptions, err error) {
	o = inst
	if o.Percentiles == nil {
		o.Percentiles = []float64{1, 50, 99}
	}
	for _, p := range o.Percentiles {
		if !(p >= 0 && p <= 100) {
			return o, eb.Build().Float64("percentile", p).Errorf("a percentile lies in [0, 100]")
		}
	}
	if o.MaxPixels == 0 {
		o.MaxPixels = DefaultMaxPixels
	}
	if o.MaxPixels < 0 {
		return o, eb.Build().Int64("maxPixels", o.MaxPixels).Errorf("the pixel limit must be positive")
	}
	return o, nil
}

// Measurement is a region's statistics over one image, plane by plane
// (ADR-0399 §SD7), with what produced them.
type Measurement struct {
	// Method is [MethodId].
	Method   string
	RegionId string
	// Level is the level the samples were read at: always 0, full
	// resolution.
	Level int
	// Mapping is how the region reached the image's pixels, and Shape the
	// shape it became there, which is what was measured.
	Mapping Mapping
	Shape   Shape
	// Rule is the coverage every figure but the area figures was computed
	// over, and Pixels the number of pixels it covers in the image.
	Rule   CoverageRuleE
	Pixels int64
	Planes []PlaneFigures
}

// PlaneFigures is one plane's figures, in values — the stored samples
// through the plane's value transform — as the statistics of a rectangle
// report them (ADR-0373 §M4).
type PlaneFigures struct {
	Label string
	// Rule is the coverage the figures below were computed over; Area says
	// its own.
	Rule CoverageRuleE
	// Histogram is the stored samples counted ([stats.OfSpans]); every other
	// figure here is read from it.
	Histogram stats.Histogram
	// Valid and Invalid count the covered samples: invalid ones — NaN, the
	// declared invalid value — are counted and left out of every other
	// figure.
	Valid, Invalid uint64
	// Min, Max, Mean and StdDev (divisor n) are NaN when no valid sample is
	// covered.
	Min, Max, Mean, StdDev float64
	// Percentiles are nearest-rank: sample values for a kind with codes,
	// located within a bin otherwise (Exact false, BinWidth in stored
	// units).
	Percentiles []Percentile
	Exact       bool
	BinWidth    float64
	// Area is nil unless coverage by area was asked for and the shape is an
	// area shape.
	Area *AreaFigures
}

// Percentile is the value below or at which P percent of the valid covered
// samples lie; NaN when there are none.
type Percentile struct {
	P, Value float64
}

// AreaFigures are a plane's figures under coverage by area (ADR-0399 §SD4):
// every pixel the shape covers in part, weighted by the fraction covered.
// The sums are taken in float64 in row-major order with a compensated
// (Neumaier) sum, so they do not depend on how the work is split.
type AreaFigures struct {
	// Rule is [CoverageRuleArea].
	Rule CoverageRuleE
	// Area is the covered area of valid samples, in pixels; InvalidArea that
	// of invalid ones, which are left out of Sum and Mean.
	Area, InvalidArea float64
	// Sum is Σ fraction·value; Mean is Sum / Area, NaN when Area is zero.
	Sum, Mean float64
}

// Measure computes a region's statistics over an image from a source
// (ADR-0399 §SD7): the region is brought to the image's pixels
// ([ToPixels]), the pixels it covers are chosen by [CoverageOf], and every
// plane is counted over them at full resolution with [stats.OfSpans], whose
// definitions — exact for a kind with codes, binned otherwise — are the
// engine's. With opts.Area, the figures of coverage by area are added for
// an area shape. The rectangle read is the one holding every covered pixel;
// a source that copies refuses one over opts.MaxPixels. The anchor is not
// read: which image of a stack a region applies to is the caller's choice
// ([Anchor.AppliesAt]).
func Measure(ctx context.Context, src SourceI, r Region, opts MeasureOptions) (m Measurement, err error) {
	if opts, err = opts.withDefaults(); err != nil {
		return
	}
	px, mapping, err := r.ToPixels(src.Placement(), opts.Tolerance)
	if err != nil {
		return
	}
	w, h := src.Size()
	cov, err := CoverageOf(px, w, h)
	if err != nil {
		return
	}
	m = Measurement{Method: MethodId, RegionId: r.Id, Mapping: mapping, Shape: px, Rule: cov.rule, Pixels: cov.pixels}
	labels := src.PlaneLabels()
	rect := cov.Bounds()
	var area areaCells
	withArea := opts.Area && px.kind.IsArea()
	if withArea {
		if area, err = collectArea(ctx, px, w, h); err != nil {
			return Measurement{}, err
		}
		rect = rect.Union(area.bounds)
	}
	var img imaging.Image
	var origin image.Point
	if !rect.Empty() {
		if img, origin, err = src.Read(ctx, rect, opts.MaxPixels); err != nil {
			return Measurement{}, err
		}
	}
	spans := make([]stats.RowSpan, len(cov.spans))
	for i, s := range cov.spans {
		spans[i] = stats.RowSpan{Row: s.Row - origin.Y, Min: s.Min - origin.X, Max: s.Max - origin.X}
	}
	m.Planes = make([]PlaneFigures, len(labels))
	for i := range m.Planes {
		if err = ctx.Err(); err != nil {
			return Measurement{}, err
		}
		pf := PlaneFigures{Label: labels[i], Rule: cov.rule}
		if !rect.Empty() {
			if pf.Histogram, err = stats.OfSpans(img.Plane(i), spans); err != nil {
				return Measurement{}, err
			}
		}
		pf.fill(opts.Percentiles)
		if withArea {
			pf.Area = area.figures(img.Plane(i), origin)
		}
		m.Planes[i] = pf
	}
	return m, nil
}

func (inst *PlaneFigures) fill(percentiles []float64) {
	h := inst.Histogram
	inst.Valid, inst.Invalid = h.Valid(), h.Invalid()
	inst.Exact, inst.BinWidth = h.IsExact(), h.BinWidth()
	nan := math.NaN()
	inst.Min, inst.Max, inst.Mean, inst.StdDev = nan, nan, nan, nan
	if lo, hi, ok := h.Range(); ok {
		inst.Min, inst.Max = lo, hi
	}
	if v, ok := h.Mean(); ok {
		inst.Mean = v
	}
	if v, ok := h.StdDev(); ok {
		inst.StdDev = v
	}
	inst.Percentiles = make([]Percentile, len(percentiles))
	for i, p := range percentiles {
		v, ok := h.Percentile(p)
		if !ok {
			v = nan
		}
		inst.Percentiles[i] = Percentile{P: p, Value: v}
	}
}

// areaCells is coverage by area collected once for every plane: the
// pixels in row-major order and their fractions.
type areaCells struct {
	cols, rows []int
	fractions  []float64
	bounds     image.Rectangle
}

func collectArea(ctx context.Context, s Shape, w, h int) (a areaCells, err error) {
	seq, err := AreaFractions(s, w, h)
	if err != nil {
		return a, err
	}
	n := 0
	for p, f := range seq {
		if n++; n%4096 == 0 {
			if err = ctx.Err(); err != nil {
				return areaCells{}, err
			}
		}
		a.cols = append(a.cols, p.X)
		a.rows = append(a.rows, p.Y)
		a.fractions = append(a.fractions, f)
		a.bounds = a.bounds.Union(image.Rect(p.X, p.Y, p.X+1, p.Y+1))
	}
	return a, nil
}

func (inst areaCells) figures(p imaging.Plane, origin image.Point) *AreaFigures {
	var area, invalid, sum neumaier
	for i, f := range inst.fractions {
		v, ok := p.ValueAt(inst.cols[i]-origin.X, inst.rows[i]-origin.Y)
		if !ok {
			invalid.add(f)
			continue
		}
		area.add(f)
		sum.add(f * v)
	}
	out := &AreaFigures{Rule: CoverageRuleArea, Area: area.value(), InvalidArea: invalid.value(), Sum: sum.value(), Mean: math.NaN()}
	if out.Area > 0 {
		out.Mean = out.Sum / out.Area
	}
	return out
}

// neumaier is a compensated sum: the running sum and the low-order part
// each addition lost.
type neumaier struct {
	sum, c float64
}

func (inst *neumaier) add(v float64) {
	t := inst.sum + v
	if math.Abs(inst.sum) >= math.Abs(v) {
		inst.c += (inst.sum - t) + v
	} else {
		inst.c += (v - t) + inst.sum
	}
	inst.sum = t
}

func (inst neumaier) value() float64 { return inst.sum + inst.c }
