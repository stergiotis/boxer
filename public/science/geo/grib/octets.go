package grib

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// rd is a forward cursor over one section's octets. A read past the end
// latches bad and yields zeros, so a template is read whole and checked
// once; every count taken from the file is bounded by its caller before it
// drives a loop or an allocation. GRIB is big-endian throughout.
type rd struct {
	b   []byte
	p   int
	bad bool
}

func (inst *rd) take(n int) (b []byte) {
	if inst.bad || n < 0 || n > len(inst.b)-inst.p {
		inst.bad = true
		return
	}
	b = inst.b[inst.p : inst.p+n : inst.p+n]
	inst.p += n
	return
}

func (inst *rd) skip(n int) {
	_ = inst.take(n)
}

func (inst *rd) remaining() (n int) {
	if inst.bad {
		return
	}
	n = len(inst.b) - inst.p
	return
}

func (inst *rd) rest() (b []byte) {
	b = inst.take(inst.remaining())
	return
}

func (inst *rd) u8() (v uint8) {
	b := inst.take(1)
	if b != nil {
		v = b[0]
	}
	return
}

func (inst *rd) u16() (v uint16) {
	v = uint16(inst.uN(2))
	return
}

func (inst *rd) u32() (v uint32) {
	v = uint32(inst.uN(4))
	return
}

func (inst *rd) u64() (v uint64) {
	v = inst.uN(8)
	return
}

func (inst *rd) uN(n int) (v uint64) {
	if n < 1 || n > 8 {
		inst.bad = true
		return
	}
	b := inst.take(n)
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return
}

// Signed octets in GRIB are sign-magnitude: the top bit is the sign, the
// rest the magnitude, and all ones is the missing value (ADR-0292 §R5). A
// two's-complement read turns a coded −1 into 255, and a coded "missing"
// into −127; both have shipped in decoders. The sN readers return the
// magnitude with its sign; whether the value was missing is a separate
// question answered by isMissing on the raw pattern.
func (inst *rd) s8() (v int8) {
	raw := inst.u8()
	v = int8(raw & 0x7f)
	if raw&0x80 != 0 {
		v = -v
	}
	return
}

func (inst *rd) s16() (v int16) {
	raw := inst.u16()
	v = int16(raw & 0x7fff)
	if raw&0x8000 != 0 {
		v = -v
	}
	return
}

func (inst *rd) s32() (v int32) {
	raw := inst.u32()
	v = int32(raw & 0x7fffffff)
	if raw&0x80000000 != 0 {
		v = -v
	}
	return
}

// s32m reads a signed 32-bit octet group and reports the all-ones pattern
// as missing rather than as −2 147 483 647.
func (inst *rd) s32m() (v int32, missing bool) {
	raw := inst.u32()
	if raw == math.MaxUint32 {
		missing = true
		return
	}
	v = int32(raw & 0x7fffffff)
	if raw&0x80000000 != 0 {
		v = -v
	}
	return
}

// u32m reads an unsigned 32-bit octet group and reports all ones as missing.
func (inst *rd) u32m() (v uint32, missing bool) {
	v = inst.u32()
	missing = v == math.MaxUint32
	return
}

func (inst *rd) u8m() (v uint8, missing bool) {
	v = inst.u8()
	missing = v == math.MaxUint8
	return
}

func (inst *rd) u16m() (v uint16, missing bool) {
	v = inst.u16()
	missing = v == math.MaxUint16
	return
}

// f32 reads an IEEE 754 single, the encoding of every reference value in
// edition 2.
func (inst *rd) f32() (v float32) {
	v = math.Float32frombits(inst.u32())
	return
}

func (inst *rd) truncation(what string) (err error) {
	if inst.bad {
		err = eb.Build().Str("structure", what).Int("length", len(inst.b)).Errorf("structure truncated: %w", ErrMalformed)
	}
	return
}

// bitReader pulls big-endian bit fields out of a data section. Widths run
// from 0 to 64; a width of 0 yields 0 and consumes nothing, which is how a
// constant field and a zero-width group both fall out of the same loop.
// Reading past the end latches bad, as with rd.
type bitReader struct {
	b   []byte
	pos uint64 // in bits
	bad bool
}

func (inst *bitReader) bits(width uint8) (v uint64) {
	if width == 0 || inst.bad {
		return
	}
	end := inst.pos + uint64(width)
	if end > uint64(len(inst.b))*8 {
		inst.bad = true
		return
	}
	for inst.pos < end {
		byteIdx := inst.pos >> 3
		bitOff := inst.pos & 7
		avail := 8 - bitOff
		need := end - inst.pos
		if need < avail {
			avail = need
		}
		chunk := uint64(inst.b[byteIdx]) >> (8 - bitOff - avail) & (1<<avail - 1)
		v = v<<avail | chunk
		inst.pos += avail
	}
	return
}

// alignByte moves to the next octet boundary, as the complex packing
// layout requires between its runs of fields.
func (inst *bitReader) alignByte() {
	inst.pos = (inst.pos + 7) &^ 7
}

func (inst *bitReader) bytePos() (n int) {
	n = int(inst.pos >> 3)
	return
}
