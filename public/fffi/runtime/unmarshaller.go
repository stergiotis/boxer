package runtime

import (
	"encoding/binary"
	"errors"
	"io"
	"math"

	"github.com/stergiotis/boxer/public/unsafeperf"
)

type Unmarshaller struct {
	r              io.Reader
	bin            binary.ByteOrder
	errHandler     func(err error)
	allocateBuffer func(l uint32) []byte
	buf            []byte
}

func NewUnmarshaller(r io.Reader, bin binary.ByteOrder, errHandler func(err error), allocateBuffer func(l uint32) []byte) *Unmarshaller {
	return &Unmarshaller{
		r:              r,
		bin:            bin,
		buf:            make([]byte, 8, 8),
		errHandler:     errHandler,
		allocateBuffer: allocateBuffer,
	}
}

func (inst *Unmarshaller) ReadUInt8() (v uint8) {
	if inst.readBuf(1) {
		v = inst.buf[0]
	}
	return
}

func (inst *Unmarshaller) ReadUInt16() (v uint16) {
	if inst.readBuf(2) {
		v = inst.bin.Uint16(inst.buf)
	}
	return
}
func (inst *Unmarshaller) ReadUInt32MostLikelyZero() (v uint32) {
	if inst.readBuf(4) {
		b := inst.buf
		if b[3] == 0 && b[2] == 0 && b[1] == 0 && b[0] == 0 {
			return
		}
		v = inst.bin.Uint32(b)
	}
	return
}

func (inst *Unmarshaller) ReadUInt32() (v uint32) {
	if inst.readBuf(4) {
		v = inst.bin.Uint32(inst.buf)
	}
	return
}

func (inst *Unmarshaller) ReadUInt64() (v uint64) {
	if inst.readBuf(8) {
		v = inst.bin.Uint64(inst.buf)
	}
	return
}

func (inst *Unmarshaller) ReadUint() (v uint) {
	return uint(inst.ReadUInt32())
}

func (inst *Unmarshaller) ReadInt() (v int) {
	// sign-magnitude ILP32, LLP64, LP64
	const signBit uint32 = 1 << 31
	u := inst.ReadUInt32()
	if u&signBit != 0 {
		v = -int(u & ^signBit)
	} else {
		v = int(u)
	}
	return
}

func (inst *Unmarshaller) ReadInt8() (v int8) {
	const signBit uint8 = 1 << 7
	u := inst.ReadUInt8()
	if u&signBit != 0 {
		v = -int8(u & ^signBit)
	} else {
		v = int8(u)
	}
	return
}

func (inst *Unmarshaller) ReadInt16() (v int16) {
	const signBit uint16 = 1 << 15
	u := inst.ReadUInt16()
	if u&signBit != 0 {
		v = -int16(u & ^signBit)
	} else {
		v = int16(u)
	}
	return
}

func (inst *Unmarshaller) ReadInt32() (v int32) {
	const signBit uint32 = 1 << 31
	u := inst.ReadUInt32()
	if u&signBit != 0 {
		v = -int32(u & ^signBit)
	} else {
		v = int32(u)
	}
	return
}

func (inst *Unmarshaller) ReadInt64() (v int64) {
	const signBit uint64 = 1 << 63
	u := inst.ReadUInt64()
	if u&signBit != 0 {
		v = -int64(u & ^signBit)
	} else {
		v = int64(u)
	}
	return
}

func (inst *Unmarshaller) ReadFloat32() (v float32) {
	v = math.Float32frombits(inst.ReadUInt32())
	return
}

func (inst *Unmarshaller) ReadFloat64() (v float64) {
	v = math.Float64frombits(inst.ReadUInt64())
	return
}

func (inst *Unmarshaller) ReadComplex64() (v complex64) {
	r := inst.ReadFloat32()
	i := inst.ReadFloat32()
	v = complex(r, i)
	return
}

func (inst *Unmarshaller) ReadComplex128() (v complex128) {
	r := inst.ReadFloat64()
	i := inst.ReadFloat64()
	v = complex(r, i)
	return
}

func (inst *Unmarshaller) ReadUintptr() (v uintptr) {
	// TODO check size using unsafe.Sizeof(...) ?
	v = uintptr(inst.ReadUInt64())
	return
}

func (inst *Unmarshaller) handleError(err error) {
	if err != nil && inst.errHandler != nil {
		inst.errHandler(err)
	}
}

func (inst *Unmarshaller) readBuf(n int) (success bool) {
	_, err := io.ReadFull(inst.r, inst.buf[:n])
	inst.handleError(err)
	success = err == nil
	return
}

var StringAllocationError = errors.New("allocated string buffer does not have correct length")

func (inst *Unmarshaller) ReadString() (v string) {
	b := inst.ReadBytes()
	if len(b) == 0 {
		return ""
	}

	v = unsafeperf.UnsafeBytesToString(b)
	return
}
func (inst *Unmarshaller) ReadStringMostLikelyEmpty() (v string) {
	l := inst.ReadUInt32MostLikelyZero()
	if l == 0 {
		// fast path
		return ""
	}

	v = unsafeperf.UnsafeBytesToString(inst.readBytesNonEmpty(l))
	return
}

func (inst *Unmarshaller) readBytesNonEmpty(l uint32) (v []byte) {
	v = inst.allocateBuffer(l)
	if len(v) != int(l) {
		inst.handleError(StringAllocationError)
		return
	}
	_, err := io.ReadFull(inst.r, v)
	if err != nil {
		inst.handleError(err)
		return
	}
	return
}
func (inst *Unmarshaller) ReadBytes() (v []byte) {
	l := inst.ReadUInt32()
	if l == 0 {
		// TODO
		v = inst.allocateBuffer(0)
	} else {
		return inst.readBytesNonEmpty(l)
	}
	return
}

func (inst *Unmarshaller) ReadBool() (v bool) {
	v = inst.ReadUInt8() != 0
	return
}
