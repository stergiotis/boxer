package tiles

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// rows is a growing block of whole rows of one plane, of one sample kind.
// The plane it hands over declares inv invalid when hasInv is set.
type rows struct {
	kind   imaging.SampleKindE
	w      int
	inv    float64
	hasInv bool
	u8     []uint8
	u16    []uint16
	i16    []int16
	i32    []int32
	f32    []float32
	f64    []float64
}

// samples is the number of samples held.
func (inst *rows) samples() int {
	switch inst.kind {
	case imaging.SampleKindU8:
		return len(inst.u8)
	case imaging.SampleKindU16:
		return len(inst.u16)
	case imaging.SampleKindI16:
		return len(inst.i16)
	case imaging.SampleKindI32:
		return len(inst.i32)
	case imaging.SampleKindF32:
		return len(inst.f32)
	case imaging.SampleKindF64:
		return len(inst.f64)
	}
	return 0
}

func (inst *rows) count() int { return inst.samples() / inst.w }

// add appends rows [y0, y1) of p, which is inst.w wide and of inst.kind.
func (inst *rows) add(p imaging.Plane, y0, y1 int) {
	a, b := y0*inst.w, y1*inst.w
	switch inst.kind {
	case imaging.SampleKindU8:
		inst.u8 = append(inst.u8, p.U8()[a:b]...)
	case imaging.SampleKindU16:
		inst.u16 = append(inst.u16, p.U16()[a:b]...)
	case imaging.SampleKindI16:
		inst.i16 = append(inst.i16, p.I16()[a:b]...)
	case imaging.SampleKindI32:
		inst.i32 = append(inst.i32, p.I32()[a:b]...)
	case imaging.SampleKindF32:
		inst.f32 = append(inst.f32, p.F32()[a:b]...)
	case imaging.SampleKindF64:
		inst.f64 = append(inst.f64, p.F64()[a:b]...)
	}
}

// alloc sets the block to n zero samples, to be written in place.
func (inst *rows) alloc(n int) {
	switch inst.kind {
	case imaging.SampleKindU8:
		inst.u8 = make([]uint8, n)
	case imaging.SampleKindU16:
		inst.u16 = make([]uint16, n)
	case imaging.SampleKindI16:
		inst.i16 = make([]int16, n)
	case imaging.SampleKindI32:
		inst.i32 = make([]int32, n)
	case imaging.SampleKindF32:
		inst.f32 = make([]float32, n)
	case imaging.SampleKindF64:
		inst.f64 = make([]float64, n)
	}
}

// copyFrom copies n samples of p from src into the block at dst.
func (inst *rows) copyFrom(dst int, p imaging.Plane, src, n int) {
	switch inst.kind {
	case imaging.SampleKindU8:
		copy(inst.u8[dst:dst+n], p.U8()[src:src+n])
	case imaging.SampleKindU16:
		copy(inst.u16[dst:dst+n], p.U16()[src:src+n])
	case imaging.SampleKindI16:
		copy(inst.i16[dst:dst+n], p.I16()[src:src+n])
	case imaging.SampleKindI32:
		copy(inst.i32[dst:dst+n], p.I32()[src:src+n])
	case imaging.SampleKindF32:
		copy(inst.f32[dst:dst+n], p.F32()[src:src+n])
	case imaging.SampleKindF64:
		copy(inst.f64[dst:dst+n], p.F64()[src:src+n])
	}
}

// take hands over the block as a plane, declaring the block's invalid value,
// and starts an empty one.
func (inst *rows) take() (p imaging.Plane, err error) {
	h := inst.count()
	switch inst.kind {
	case imaging.SampleKindU8:
		p, err = imaging.NewPlaneU8(inst.w, h, inst.u8)
		inst.u8 = nil
	case imaging.SampleKindU16:
		p, err = imaging.NewPlaneU16(inst.w, h, inst.u16)
		inst.u16 = nil
	case imaging.SampleKindI16:
		p, err = imaging.NewPlaneI16(inst.w, h, inst.i16)
		inst.i16 = nil
	case imaging.SampleKindI32:
		p, err = imaging.NewPlaneI32(inst.w, h, inst.i32)
		inst.i32 = nil
	case imaging.SampleKindF32:
		p, err = imaging.NewPlaneF32(inst.w, h, inst.f32)
		inst.f32 = nil
	case imaging.SampleKindF64:
		p, err = imaging.NewPlaneF64(inst.w, h, inst.f64)
		inst.f64 = nil
	default:
		return p, eb.Build().Stringer("kind", inst.kind).Errorf("no rows of this sample kind")
	}
	if err == nil && inst.hasInv {
		p, err = p.WithInvalid(inst.inv)
	}
	return
}

// cut copies columns [x0, x1) of every row of p into a plane of their own,
// which declares what p declares and has its value transform.
func cut(p imaging.Plane, x0, x1 int) (out imaging.Plane, err error) {
	w, h, cw := p.Width(), p.Height(), x1-x0
	switch p.Kind() {
	case imaging.SampleKindU8:
		out, err = imaging.NewPlaneU8(cw, h, cutOf(p.U8(), w, h, x0, x1))
	case imaging.SampleKindU16:
		out, err = imaging.NewPlaneU16(cw, h, cutOf(p.U16(), w, h, x0, x1))
	case imaging.SampleKindI16:
		out, err = imaging.NewPlaneI16(cw, h, cutOf(p.I16(), w, h, x0, x1))
	case imaging.SampleKindI32:
		out, err = imaging.NewPlaneI32(cw, h, cutOf(p.I32(), w, h, x0, x1))
	case imaging.SampleKindF32:
		out, err = imaging.NewPlaneF32(cw, h, cutOf(p.F32(), w, h, x0, x1))
	case imaging.SampleKindF64:
		out, err = imaging.NewPlaneF64(cw, h, cutOf(p.F64(), w, h, x0, x1))
	default:
		return out, eb.Build().Stringer("kind", p.Kind()).Errorf("no plane of this sample kind to cut")
	}
	if err != nil {
		return
	}
	return like(out, p)
}

func cutOf[T any](s []T, w, h, x0, x1 int) []T {
	out := make([]T, 0, (x1-x0)*h)
	for y := range h {
		out = append(out, s[y*w+x0:y*w+x1]...)
	}
	return out
}

// like gives out the invalid value p declares and p's value transform.
func like(out, p imaging.Plane) (imaging.Plane, error) {
	var err error
	if inv, ok := p.Invalid(); ok {
		if out, err = out.WithInvalid(inv); err != nil {
			return out, err
		}
	}
	return out.WithTransform(p.Transform())
}

// sampleBytes is the size of one sample of a kind.
func sampleBytes(kind imaging.SampleKindE) int64 { return int64(kind.Bytes()) }
