package runtime

import (
	"encoding/binary"
	"io"
	"math"

	"github.com/stergiotis/boxer/public/unsafeperf"
)

type Marshaller struct {
	w          io.Writer
	bin        binary.ByteOrder
	errHandler func(err error)
	buf        []byte
	written    int
}

func NewMarshaller(w io.Writer, bin binary.ByteOrder, errHandler func(err error)) *Marshaller {
	return &Marshaller{
		w:          w,
		bin:        bin,
		buf:        make([]byte, 8, 8),
		errHandler: errHandler,
		written:    0,
	}
}

func (inst *Marshaller) ResetWrittenBytes() {
	inst.written = 0
}

func (inst *Marshaller) GetWrittenBytes() int {
	return inst.written
}

func (inst *Marshaller) WriteUInt8(v uint8) {
	inst.buf[0] = v
	inst.writeBuf(1)
}

func (inst *Marshaller) WriteBool(v bool) {
	if v {
		inst.buf[0] = 1
	} else {
		inst.buf[0] = 0
	}
	inst.writeBuf(1)
}

func (inst *Marshaller) WriteUInt(v uint) {
	// FIXME static assert sizeof?
	inst.bin.PutUint32(inst.buf, uint32(v))
	inst.writeBuf(4)
}

func (inst *Marshaller) WriteUInt16(v uint16) {
	inst.bin.PutUint16(inst.buf, v)
	inst.writeBuf(2)
}

func (inst *Marshaller) WriteUInt32(v uint32) {
	inst.bin.PutUint32(inst.buf, v)
	inst.writeBuf(4)
}

func (inst *Marshaller) WriteUInt64(v uint64) {
	inst.bin.PutUint64(inst.buf, v)
	inst.writeBuf(8)
}

func (inst *Marshaller) WriteInt(v int) {
	// sign-magnitude ILP32, LLP64, LP64
	if v < 0 {
		inst.WriteUInt32(1<<31 | uint32(-v))
	} else {
		inst.WriteUInt32(uint32(v))
	}
}

func (inst *Marshaller) WriteUint(v uint) {
	// ILP32, LLP64, LP64
	inst.WriteUInt32(uint32(v))
}

func (inst *Marshaller) WriteInt8(v int8) {
	// sign-magnitude
	if v < 0 {
		inst.WriteUInt8(1<<7 | uint8(-v))
	} else {
		inst.WriteUInt8(uint8(v))
	}
}

func (inst *Marshaller) WriteInt16(v int16) {
	// sign-magnitude
	if v < 0 {
		inst.WriteUInt16(1<<15 | uint16(-v))
	} else {
		inst.WriteUInt16(uint16(v))
	}
}

func (inst *Marshaller) WriteInt32(v int32) {
	// sign-magnitude
	if v < 0 {
		inst.WriteUInt32(1<<31 | uint32(-v))
	} else {
		inst.WriteUInt32(uint32(v))
	}
}

func (inst *Marshaller) WriteInt64(v int64) {
	// sign-magnitude
	if v < 0 {
		inst.WriteUInt64(1<<63 | uint64(-v))
	} else {
		inst.WriteUInt64(uint64(v))
	}
}

func (inst *Marshaller) WriteFloat32(v float32) {
	inst.WriteUInt32(math.Float32bits(v))
}

func (inst *Marshaller) WriteFloat64(v float64) {
	inst.WriteUInt64(math.Float64bits(v))
}

func (inst *Marshaller) WriteComplex64(v complex64) {
	inst.WriteFloat32(real(v))
	inst.WriteFloat32(imag(v))
}

func (inst *Marshaller) WriteComplex128(v complex128) {
	inst.WriteFloat64(real(v))
	inst.WriteFloat64(imag(v))
}

func (inst *Marshaller) writeBuf(n int) {
	u, err := inst.w.Write(inst.buf[:n])
	inst.written += u
	inst.handleError(err)
}

func (inst *Marshaller) handleError(err error) {
	if err != nil && inst.errHandler != nil {
		inst.errHandler(err)
	}
}

func (inst *Marshaller) WriteString(v string) {
	inst.WriteSliceLength(len(v))
	n, err := inst.w.Write(unsafeperf.UnsafeStringToByte(v))
	inst.written += 4 + n
	inst.handleError(err)
}

func (inst *Marshaller) WriteBytes(v []byte) {
	if v == nil {
		inst.WriteNilSlice()
		return
	}
	inst.WriteSliceLength(len(v))
	n, err := inst.w.Write(v)
	inst.written += 4 + n
	inst.handleError(err)
}

func (inst *Marshaller) WriteSliceLength(l int) {
	inst.WriteUInt32(uint32(l))
}

func (inst *Marshaller) WriteNilSlice() {
	inst.WriteUInt32(math.MaxUint32)
}
