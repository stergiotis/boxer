package tiles

import "github.com/stergiotis/boxer/public/science/imaging"

// rows is a growing block of whole rows of one plane, of one sample kind.
type rows struct {
	kind imaging.SampleKindE
	w    int
	u8   []uint8
	u16  []uint16
	f32  []float32
}

func (inst *rows) count() int {
	switch inst.kind {
	case imaging.SampleKindU8:
		return len(inst.u8) / inst.w
	case imaging.SampleKindU16:
		return len(inst.u16) / inst.w
	}
	return len(inst.f32) / inst.w
}

// add appends rows [y0, y1) of p, which is inst.w wide.
func (inst *rows) add(p imaging.Plane, y0, y1 int) {
	a, b := y0*inst.w, y1*inst.w
	switch inst.kind {
	case imaging.SampleKindU8:
		inst.u8 = append(inst.u8, p.U8()[a:b]...)
	case imaging.SampleKindU16:
		inst.u16 = append(inst.u16, p.U16()[a:b]...)
	default:
		inst.f32 = append(inst.f32, p.F32()[a:b]...)
	}
}

// take hands over the block as a plane and starts an empty one.
func (inst *rows) take() (p imaging.Plane, err error) {
	h := inst.count()
	switch inst.kind {
	case imaging.SampleKindU8:
		p, err = imaging.NewPlaneU8(inst.w, h, inst.u8)
		inst.u8 = nil
	case imaging.SampleKindU16:
		p, err = imaging.NewPlaneU16(inst.w, h, inst.u16)
		inst.u16 = nil
	default:
		p, err = imaging.NewPlaneF32(inst.w, h, inst.f32)
		inst.f32 = nil
	}
	return
}

// cut copies columns [x0, x1) of every row of p into a plane of their own.
func cut(p imaging.Plane, x0, x1 int) (out imaging.Plane, err error) {
	w, h, cw := p.Width(), p.Height(), x1-x0
	switch p.Kind() {
	case imaging.SampleKindU8:
		s := make([]uint8, 0, cw*h)
		for y := range h {
			s = append(s, p.U8()[y*w+x0:y*w+x1]...)
		}
		return imaging.NewPlaneU8(cw, h, s)
	case imaging.SampleKindU16:
		s := make([]uint16, 0, cw*h)
		for y := range h {
			s = append(s, p.U16()[y*w+x0:y*w+x1]...)
		}
		return imaging.NewPlaneU16(cw, h, s)
	}
	s := make([]float32, 0, cw*h)
	for y := range h {
		s = append(s, p.F32()[y*w+x0:y*w+x1]...)
	}
	return imaging.NewPlaneF32(cw, h, s)
}

// sampleBytes is the size of one sample of a kind.
func sampleBytes(kind imaging.SampleKindE) int64 {
	switch kind {
	case imaging.SampleKindU16:
		return 2
	case imaging.SampleKindF32:
		return 4
	}
	return 1
}
