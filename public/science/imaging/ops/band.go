package ops

import (
	"context"
	"image"
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// Band is the input of one band of output rows, as values (ADR-0400 §SD4):
// every plane of every input, for the band's rows and the plan's halo around
// them, the stored samples through their value transforms and NaN for every
// invalid one. Around the image the halo holds what the border rule puts
// there: NaN under [BorderInvalid], the nearest edge sample under
// [BorderNearest]. A kernel reads it and does not write it.
type Band struct {
	y0, y1        int
	width, height int
	halo          int
	stride        int
	// planes[i][p] is plane p of input i: rows y0−halo … y1+halo−1 of
	// stride values each, the first at column −halo.
	planes [][][]float64
}

// Rows are the image rows [y0, y1) of the output band.
func (inst *Band) Rows() (y0, y1 int) { return inst.y0, inst.y1 }

// Width and Height are the image's size.
func (inst *Band) Width() int  { return inst.width }
func (inst *Band) Height() int { return inst.height }

// Halo is the rows and columns held on each side of the band.
func (inst *Band) Halo() int { return inst.halo }

// Row is row y of plane p of input i — y within [y0−Halo, y1+Halo) — as
// Width+2·Halo values, element k being column k−Halo.
func (inst *Band) Row(i, p, y int) []float64 {
	r := y - inst.y0 + inst.halo
	return inst.planes[i][p][r*inst.stride : (r+1)*inst.stride]
}

// newBand makes a band able to hold up to rows output rows of inputs with
// the given plane counts.
func newBand(width, height, halo, rows int, planeCounts []int) *Band {
	b := &Band{width: width, height: height, halo: halo, stride: width + 2*halo, planes: make([][][]float64, len(planeCounts))}
	n := (rows + 2*halo) * b.stride
	for i, c := range planeCounts {
		b.planes[i] = make([][]float64, c)
		for p := range c {
			b.planes[i][p] = make([]float64, n)
		}
	}
	return b
}

// load fills the band for output rows [y0, y1) from the sources.
func (inst *Band) load(ctx context.Context, srcs []SourceI, y0, y1 int, border BorderE) error {
	inst.y0, inst.y1 = y0, y1
	h, w := inst.halo, inst.width
	r0, r1 := max(0, y0-h), min(inst.height, y1+h)
	for i, src := range srcs {
		var img imaging.Image
		base := 0
		if s, direct := src.(imageSource); direct {
			img = s.img
		} else {
			var err error
			if img, err = src.Read(ctx, image.Rect(0, r0, w, r1)); err != nil {
				return err
			}
			base = r0
		}
		for p := range inst.planes[i] {
			plane := img.Plane(p)
			if plane.Width() != w {
				return eb.Build().Int("input", i).Int("plane", p).Int("width", plane.Width()).Int("want", w).
					Errorf("a source read a region of another width than asked")
			}
			for y := r0; y < r1; y++ {
				row := inst.Row(i, p, y)
				rowValues(plane, y-base, row[h:h+w])
				var left, right float64
				if border == BorderNearest {
					left, right = row[h], row[h+w-1]
				} else {
					left, right = nan, nan
				}
				for x := range h {
					row[x], row[h+w+x] = left, right
				}
			}
			for y := y0 - h; y < y1+h; y++ {
				if y >= r0 && y < r1 {
					continue
				}
				row := inst.Row(i, p, y)
				if border == BorderNearest {
					copy(row, inst.Row(i, p, min(max(y, 0), inst.height-1)))
					continue
				}
				for x := range row {
					row[x] = nan
				}
			}
		}
	}
	return nil
}

// rowValues writes row y of p into dst as values: the stored samples
// through the value transform, NaN for each invalid one.
func rowValues(p imaging.Plane, y int, dst []float64) {
	w := p.Width()
	a, b := y*w, (y+1)*w
	t := p.Transform()
	inv, hasInv := p.Invalid()
	switch p.Kind() {
	case imaging.SampleKindU8:
		convertRow(p.U8()[a:b], dst, t, inv, hasInv)
	case imaging.SampleKindU16:
		convertRow(p.U16()[a:b], dst, t, inv, hasInv)
	case imaging.SampleKindI16:
		convertRow(p.I16()[a:b], dst, t, inv, hasInv)
	case imaging.SampleKindI32:
		convertRow(p.I32()[a:b], dst, t, inv, hasInv)
	case imaging.SampleKindF32:
		convertRow(p.F32()[a:b], dst, t, inv, hasInv)
	case imaging.SampleKindF64:
		convertRow(p.F64()[a:b], dst, t, inv, hasInv)
	}
}

type sampleI interface {
	uint8 | uint16 | int16 | int32 | float32 | float64
}

func convertRow[T sampleI](src []T, dst []float64, t imaging.ValueTransform, inv float64, hasInv bool) {
	identity := t.IsIdentity()
	for x, s := range src {
		v := float64(s)
		switch {
		case v != v || (hasInv && v == inv):
			v = nan
		case !identity:
			v = v*t.Scale + t.Offset
		}
		dst[x] = v
	}
}

// invalidF32 is the one NaN an output plane of 32-bit floats holds, so that
// every invalid sample has the same bits whatever made it invalid.
var invalidF32 = float32(math.NaN())

// outputPlane converts band rows of values into a plane of the plan's kind:
// a value that is not finite, as a float32 or float64, is the invalid NaN,
// and under [BorderInvalid] so is every sample within the halo of the
// image's edge, whose neighbourhood reaches outside it.
func outputPlane(plan *Plan, y0 int, values []float64) (imaging.Plane, error) {
	w := plan.Width
	rows := len(values) / w
	mask := plan.Border == BorderInvalid && plan.Halo > 0
	h := plan.Halo
	edge := func(x, y int) bool {
		return mask && (x < h || x >= w-h || y < h || y >= plan.Height-h)
	}
	if plan.Kind == imaging.SampleKindF32 {
		out := make([]float32, len(values))
		for r := range rows {
			y := y0 + r
			for x := range w {
				f := float32(values[r*w+x])
				if f-f != 0 || edge(x, y) {
					f = invalidF32
				}
				out[r*w+x] = f
			}
		}
		return imaging.NewPlaneF32(w, rows, out)
	}
	out := make([]float64, len(values))
	for r := range rows {
		y := y0 + r
		for x := range w {
			f := values[r*w+x]
			if f-f != 0 || edge(x, y) {
				f = nan
			}
			out[r*w+x] = f
		}
	}
	return imaging.NewPlaneF64(w, rows, out)
}
