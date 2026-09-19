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
	// native says the wire's byte order is the machine's, so a slice's memory
	// is already its encoding.
	native bool
	// chunk is the scratch a slice is encoded into when it is not, allocated
	// on first use.
	chunk []byte
}

var _ MarshallWriterI = (*Marshaller)(nil)
var _ MarshallSliceWriterI = (*Marshaller)(nil)

// chunkBytes is how much of a slice is encoded per write when its memory
// cannot be written as it is.
const chunkBytes = 8192

// nativeLittleEndian is the machine's byte order, found once.
var nativeLittleEndian = binary.NativeEndian.Uint16([]byte{1, 0}) == 1

func NewMarshaller(w io.Writer, bin binary.ByteOrder, errHandler func(err error)) *Marshaller {
	return &Marshaller{
		w:          w,
		bin:        bin,
		buf:        make([]byte, 8),
		errHandler: errHandler,
		written:    0,
		native: (bin == binary.ByteOrder(binary.LittleEndian) && nativeLittleEndian) ||
			(bin == binary.ByteOrder(binary.BigEndian) && !nativeLittleEndian),
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

// WriteUint16Elements implements [MarshallSliceWriterI].
func (inst *Marshaller) WriteUint16Elements(vs []uint16) {
	if inst.native {
		if b, ok := unsafeperf.UnsafeSliceToBytes(vs); ok {
			inst.WriteVerbatim(b)
			return
		}
	}
	for len(vs) > 0 {
		n := min(len(vs), chunkBytes/2)
		b := inst.scratch()
		for i, v := range vs[:n] {
			inst.bin.PutUint16(b[2*i:], v)
		}
		inst.WriteVerbatim(b[:2*n])
		vs = vs[n:]
	}
}

// WriteUint32Elements implements [MarshallSliceWriterI].
func (inst *Marshaller) WriteUint32Elements(vs []uint32) {
	if inst.native {
		if b, ok := unsafeperf.UnsafeSliceToBytes(vs); ok {
			inst.WriteVerbatim(b)
			return
		}
	}
	for len(vs) > 0 {
		n := min(len(vs), chunkBytes/4)
		b := inst.scratch()
		for i, v := range vs[:n] {
			inst.bin.PutUint32(b[4*i:], v)
		}
		inst.WriteVerbatim(b[:4*n])
		vs = vs[n:]
	}
}

// WriteUint64Elements implements [MarshallSliceWriterI].
func (inst *Marshaller) WriteUint64Elements(vs []uint64) {
	if inst.native {
		if b, ok := unsafeperf.UnsafeSliceToBytes(vs); ok {
			inst.WriteVerbatim(b)
			return
		}
	}
	for len(vs) > 0 {
		n := min(len(vs), chunkBytes/8)
		b := inst.scratch()
		for i, v := range vs[:n] {
			inst.bin.PutUint64(b[8*i:], v)
		}
		inst.WriteVerbatim(b[:8*n])
		vs = vs[n:]
	}
}

func (inst *Marshaller) scratch() []byte {
	if inst.chunk == nil {
		inst.chunk = make([]byte, chunkBytes)
	}
	return inst.chunk
}
