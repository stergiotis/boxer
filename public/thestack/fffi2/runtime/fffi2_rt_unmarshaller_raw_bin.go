package runtime

import (
	"encoding/binary"
	"errors"
	"io"
	"math"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/unsafeperf"
)

type Unmarshaller struct {
	r              io.Reader
	endianness     binary.ByteOrder
	errHandler     func(err error)
	allocateBuffer func(l uint32) []byte
	buf            []byte
	read           int
	// err is the first read failure since the last SetInput or ClearErr.
	// The Read* methods cannot report failure through their results — they
	// return a zero value, which no caller can tell apart from a zero the
	// peer actually sent — so this is the only signal that decoded values
	// are not real. It is sticky, and it gates every subsequent read.
	err error
}

var _ UnmarshallReaderI = (*Unmarshaller)(nil)

func NewUnmarshaller(r io.Reader, bin binary.ByteOrder, errHandler func(err error), allocateBuffer func(l uint32) []byte) *Unmarshaller {
	if allocateBuffer == nil {
		allocateBuffer = func(l uint32) []byte {
			return make([]byte, l)
		}
	}
	if errHandler == nil {
		errHandler = func(err error) {
			log.Error().Err(err).Msg("error while unmarshalling fffi value")
		}
	}
	return &Unmarshaller{
		r:              r,
		endianness:     bin,
		buf:            make([]byte, 8, 8),
		errHandler:     errHandler,
		allocateBuffer: allocateBuffer,
		read:           0,
	}
}

func (inst *Unmarshaller) ResetReadBytes() {
	inst.read = 0
}

func (inst *Unmarshaller) GetReadBytes() int {
	return inst.read
}

// Err returns the first read failure since the last SetInput or ClearErr,
// or nil while the stream is intact. Every Read* method returns a zero
// value on failure, so a caller that must trust what it decoded has to
// check this — typically once per frame rather than per field.
func (inst *Unmarshaller) Err() (err error) {
	return inst.err
}

// ClearErr drops the sticky failure and re-arms reading. Only sound once
// the stream is known to be resynchronised, since a failed read leaves the
// position somewhere inside a frame. SetInput does this implicitly.
func (inst *Unmarshaller) ClearErr() {
	inst.err = nil
}

// fail records the first failure and reports it to the error handler. Later
// failures in the same epoch are not reported again: once the position is
// unknown every subsequent read fails the same way, which is what used to
// turn one broken pipe into a per-read flood on stderr.
func (inst *Unmarshaller) fail(err error) {
	if inst.err != nil {
		return
	}
	inst.err = err
	inst.handleError(err)
}

// SetInput attaches a new reader and re-arms reading: any previous failure
// described the stream being replaced.
func (inst *Unmarshaller) SetInput(r io.Reader) {
	inst.r = r
	inst.err = nil
}
func (inst *Unmarshaller) SetEndianness(endi binary.ByteOrder) {
	inst.endianness = endi
}
func (inst *Unmarshaller) SetErrorHandler(f func(err error)) {
	inst.errHandler = f
}
func (inst *Unmarshaller) SetAllocateBufferFunc(f func(l uint32) []byte) {
	inst.allocateBuffer = f
}

func (inst *Unmarshaller) ReadUInt8() (v uint8) {
	if inst.readBuf(1) {
		v = inst.buf[0]
	}
	return
}

func (inst *Unmarshaller) ReadUInt16() (v uint16) {
	if inst.readBuf(2) {
		v = inst.endianness.Uint16(inst.buf)
	}
	return
}
func (inst *Unmarshaller) ReadUInt32MostLikelyZero() (v uint32) {
	if inst.readBuf(4) {
		b := inst.buf
		if b[3] == 0 && b[2] == 0 && b[1] == 0 && b[0] == 0 {
			return
		}
		v = inst.endianness.Uint32(b)
	}
	return
}

func (inst *Unmarshaller) ReadUInt32() (v uint32) {
	if inst.readBuf(4) {
		v = inst.endianness.Uint32(inst.buf)
	}
	return
}

func (inst *Unmarshaller) ReadUInt64() (v uint64) {
	if inst.readBuf(8) {
		v = inst.endianness.Uint64(inst.buf)
	}
	return
}

func (inst *Unmarshaller) ReadInt8() (v int8) {
	return int8(inst.ReadUInt8())
}

func (inst *Unmarshaller) ReadInt16() (v int16) {
	return int16(inst.ReadUInt16())
}

func (inst *Unmarshaller) ReadInt32() (v int32) {
	return int32(inst.ReadUInt32())
}

func (inst *Unmarshaller) ReadInt64() (v int64) {
	return int64(inst.ReadUInt64())
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
	v = uintptr(inst.ReadUInt64())
	return
}

func (inst *Unmarshaller) handleError(err error) {
	if err != nil && inst.errHandler != nil {
		inst.errHandler(err)
	}
}

func (inst *Unmarshaller) readBuf(n int) (success bool) {
	if inst.err != nil {
		// A short read leaves the stream position inside a frame, so
		// reading on would decode whatever bytes follow as if they were
		// the next field. Stay stopped until the caller resynchronises.
		return
	}
	u, err := io.ReadFull(inst.r, inst.buf[:n])
	inst.read += u
	if err != nil {
		inst.fail(err)
		return
	}
	success = true
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
	if inst.err != nil {
		return
	}
	v = inst.allocateBuffer(l)
	if len(v) != int(l) {
		inst.fail(StringAllocationError)
		return nil
	}
	u, err := io.ReadFull(inst.r, v)
	inst.read += u
	if err != nil {
		inst.fail(err)
		// Handing back the buffer would return allocated-but-unwritten
		// bytes; through ReadString those become a run of NULs that reads
		// like a value the peer sent.
		return nil
	}
	return
}
func (inst *Unmarshaller) ReadBytes() (v []byte) {
	l := inst.ReadUInt32()
	if inst.err != nil {
		return nil
	}
	if l == math.MaxUint32 {
		// nil sentinel — WriteBytes(nil) encodes as MaxUint32
		return nil
	}
	if l == 0 {
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

// ReadSliceLength returns the element count, or isNil for the nil-slice
// sentinel. A failed read yields (0, false) — an empty, non-nil slice —
// which is a perfectly ordinary answer, so callers that must distinguish
// "the peer sent nothing" from "the stream died" have to consult Err.
func (inst *Unmarshaller) ReadSliceLength() (l int, isNil bool) {
	v := inst.ReadUInt32()
	if v == math.MaxUint32 {
		return 0, true
	}
	return int(v), false
}
