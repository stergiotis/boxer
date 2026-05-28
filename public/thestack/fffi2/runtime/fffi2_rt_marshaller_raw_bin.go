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

var _ MarshallWriterI = (*Marshaller)(nil)

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

func (inst *Marshaller) WriteUint8(v uint8) {
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

func (inst *Marshaller) WriteUint16(v uint16) {
	inst.bin.PutUint16(inst.buf, v)
	inst.writeBuf(2)
}

func (inst *Marshaller) WriteUint32(v uint32) {
	inst.bin.PutUint32(inst.buf, v)
	inst.writeBuf(4)
}

func (inst *Marshaller) WriteUint64(v uint64) {
	inst.bin.PutUint64(inst.buf, v)
	inst.writeBuf(8)
}

func (inst *Marshaller) WriteInt8(v int8) {
	inst.WriteUint8(uint8(v))
}

func (inst *Marshaller) WriteInt16(v int16) {
	inst.WriteUint16(uint16(v))
}

func (inst *Marshaller) WriteInt32(v int32) {
	inst.WriteUint32(uint32(v))
}

func (inst *Marshaller) WriteInt64(v int64) {
	inst.WriteUint64(uint64(v))
}

func (inst *Marshaller) WriteFloat32(v float32) {
	inst.WriteUint32(math.Float32bits(v))
}

func (inst *Marshaller) WriteFloat64(v float64) {
	inst.WriteUint64(math.Float64bits(v))
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

func (inst *Marshaller) WriteVerbatim(v []byte) {
	n, err := inst.w.Write(v)
	inst.written += n
	inst.handleError(err)
}
func (inst *Marshaller) WriteBytes(v []byte) {
	if v == nil {
		inst.WriteNilSlice()
		return
	}
	inst.WriteSliceLength(len(v))
	inst.WriteVerbatim(v)
}

func (inst *Marshaller) WriteSliceLength(l int) {
	inst.WriteUint32(uint32(l))
}

func (inst *Marshaller) WriteNilSlice() {
	inst.WriteUint32(math.MaxUint32)
}
