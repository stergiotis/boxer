package regions

import (
	"context"
	"image"
	"iter"
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// ProfileOptions are what a profile may vary. The zero value takes the
// default tolerance and pixel limit.
type ProfileOptions struct {
	// Tolerance is as for [MeasureOptions]; a line maps exactly in every
	// frame, so it changes nothing.
	Tolerance float64
	// MaxPixels bounds each rectangle a source that copies may copy; zero is
	// [DefaultMaxPixels]. A profile is read in bands of rows, so a long
	// diagonal line is not refused for the size of its bounding rectangle.
	MaxPixels int64
}

// profileBand is the most pixels one read of a profile's samples spans,
// below the pixel limit.
const profileBand = 1 << 20

// ProfileStep is one pixel of a profile.
type ProfileStep struct {
	Col, Row int
	// Length is the length of the line inside the pixel, and Distance the
	// distance along the line from its start to the middle of that length,
	// both in pixels.
	Length, Distance float64
	// FrameDistance is Distance in the unit of the source's linear frame;
	// NaN when it has none ([LineProfile.FrameUnit]).
	FrameDistance float64
}

// LineProfile is a line's profile (ADR-0399 §SD8): the pixels the line
// passes through, in order from its start, each once, with every plane's
// stored sample there. Nothing is interpolated and no spacing is chosen.
type LineProfile struct {
	// Method is [MethodId]; Level is always 0.
	Method   string
	RegionId string
	Level    int
	// Mapping is how the line reached the image's pixels, and Line the line
	// it became there.
	Mapping Mapping
	Line    Shape
	// Rule is [CoverageRuleLine].
	Rule CoverageRuleE
	// Length is the sum of the steps' lengths: the length, in pixels, of
	// the part of the line inside the image.
	Length float64
	// Labels names each plane.
	Labels []string

	frameUnit          string
	hasFrame           bool
	frameScale         float64
	cols, rows         []int
	lengths, distances []float64
	stored             [][]float64
	valid              [][]bool
	transforms         []imaging.ValueTransform
}

// Len is the number of pixels in the profile.
func (inst LineProfile) Len() int { return len(inst.cols) }

// FrameUnit is the unit [ProfileStep.FrameDistance] is in, and ok false
// when the source has no placement whose frame is linear — physical or a
// reference system — with one unit for both coordinates. The distance is
// Euclidean in the frame's coordinates: for a reference system in degrees
// it is not a distance on the ground.
func (inst LineProfile) FrameUnit() (unit string, ok bool) { return inst.frameUnit, inst.hasFrame }

// Step is pixel i of the profile.
func (inst LineProfile) Step(i int) ProfileStep {
	s := ProfileStep{Col: inst.cols[i], Row: inst.rows[i], Length: inst.lengths[i], Distance: inst.distances[i], FrameDistance: math.NaN()}
	if inst.hasFrame {
		s.FrameDistance = s.Distance * inst.frameScale
	}
	return s
}

// All is the profile's pixels in order.
func (inst LineProfile) All() iter.Seq2[int, ProfileStep] {
	return func(yield func(int, ProfileStep) bool) {
		for i := range inst.cols {
			if !yield(i, inst.Step(i)) {
				return
			}
		}
	}
}

// Sample is plane's sample at pixel i of the profile: as stored, and the
// value it stands for through the plane's value transform; valid is false
// for an invalid sample — NaN, the declared invalid value — whose stored
// and value are then zero.
func (inst LineProfile) Sample(plane, i int) (stored, value float64, valid bool) {
	if !inst.valid[plane][i] {
		return 0, 0, false
	}
	stored = inst.stored[plane][i]
	return stored, inst.transforms[plane].Apply(stored), true
}

// Profile is the profile of a line region over an image from a source
// (ADR-0399 §SD8). The line is brought to the image's pixels ([ToPixels])
// and traversed exactly: it is cut where it crosses a whole column or row
// coordinate, and each part of positive length is in one pixel — a part
// running along a pixel edge in the pixel below or right of it, whose index
// is the floor of the edge's coordinate (ADR-0373 §SD3); a line through a
// pixel corner passes from one diagonal neighbour to the other, the two
// beside the corner not passed through; a line of zero length is the pixel
// containing its point, with length zero; parts outside the image are left
// out. For each pixel it gives the length of the line inside it, the
// distance from the line's start to the middle of that length, and every
// plane's stored sample. Where the source's placement is linear with one
// unit, distances are also given in that unit.
func Profile(ctx context.Context, src SourceI, r Region, opts ProfileOptions) (p LineProfile, err error) {
	if r.Shape.kind != ShapeKindLine {
		return p, eb.Build().Str("region", r.Id).Stringer("kind", r.Shape.kind).Errorf("a profile is of a line")
	}
	maxPixels := opts.MaxPixels
	if maxPixels == 0 {
		maxPixels = DefaultMaxPixels
	}
	if maxPixels < 0 {
		return p, eb.Build().Int64("maxPixels", maxPixels).Errorf("the pixel limit must be positive")
	}
	pl := src.Placement()
	line, mapping, err := r.ToPixels(pl, opts.Tolerance)
	if err != nil {
		return
	}
	w, h := src.Size()
	p = LineProfile{Method: MethodId, RegionId: r.Id, Mapping: mapping, Line: line, Rule: CoverageRuleLine, Labels: src.PlaneLabels()}
	a, b := line.a, line.b
	length := math.Hypot(b.X-a.X, b.Y-a.Y)
	traverse(a, b, w, h, func(col, row int, t0, t1 float64) bool {
		p.cols = append(p.cols, col)
		p.rows = append(p.rows, row)
		p.lengths = append(p.lengths, (t1-t0)*length)
		p.distances = append(p.distances, (t0+t1)/2*length)
		return true
	})
	var sum neumaier
	for _, l := range p.lengths {
		sum.add(l)
	}
	p.Length = sum.value()
	if f := pl.Frame; (f.Kind == imaging.FrameKindPhysical || f.Kind == imaging.FrameKindReferenceSystem) &&
		f.Units[0] != "" && f.Units[0] == f.Units[1] && pl.Validate() == nil {
		p.frameUnit, p.hasFrame = f.Units[0], true
		if length > 0 {
			dx, dy := pl.Affine[1]*(b.X-a.X)+pl.Affine[2]*(b.Y-a.Y), pl.Affine[4]*(b.X-a.X)+pl.Affine[5]*(b.Y-a.Y)
			p.frameScale = math.Hypot(dx, dy) / length
		}
	}
	n := len(p.Labels)
	p.stored, p.valid, p.transforms = make([][]float64, n), make([][]bool, n), make([]imaging.ValueTransform, n)
	for i := range n {
		p.stored[i], p.valid[i] = make([]float64, len(p.cols)), make([]bool, len(p.cols))
		p.transforms[i] = imaging.IdentityTransform
	}
	limit := min(maxPixels, profileBand)
	for lo := 0; lo < len(p.cols); {
		if err = ctx.Err(); err != nil {
			return LineProfile{}, err
		}
		rect := image.Rect(p.cols[lo], p.rows[lo], p.cols[lo]+1, p.rows[lo]+1)
		hi := lo + 1
		for ; hi < len(p.cols); hi++ {
			next := rect.Union(image.Rect(p.cols[hi], p.rows[hi], p.cols[hi]+1, p.rows[hi]+1))
			if int64(next.Dx())*int64(next.Dy()) > limit {
				break
			}
			rect = next
		}
		img, origin, rerr := src.Read(ctx, rect, maxPixels)
		if rerr != nil {
			return LineProfile{}, rerr
		}
		for i := range n {
			plane := img.Plane(i)
			p.transforms[i] = plane.Transform()
			for j := lo; j < hi; j++ {
				p.stored[i][j], p.valid[i][j] = plane.StoredAt(p.cols[j]-origin.X, p.rows[j]-origin.Y)
				if !p.valid[i][j] {
					p.stored[i][j] = 0
				}
			}
		}
		lo = hi
	}
	return p, nil
}
