package decode

import (
	"encoding/binary"
	"math"
)

// bytesPerSample is the width of one f32le sample. Assumes ffmpeg's `-f f32le`
// output, which is little-endian IEEE binary32 regardless of the host.
const bytesPerSample int = 4

// f32Assembler turns the byte chunks an ffmpeg pipe hands over into samples.
// A chunk ends wherever the pipe ended it, which is not necessarily on a
// sample boundary, so the bytes of a sample split across two chunks are
// carried into the next call — as are whole samples the destination had no
// room for, which keeps the type total rather than imposing a size
// precondition on its caller.
//
// The zero value is ready to use. It is not safe for concurrent use; the
// [pcm.SourceI] contract has one goroutine reading at a time.
type f32Assembler struct {
	carry []byte
}

// Decode converts as much of src as fits into dst and returns the number of
// samples written. Whatever is left over — an incomplete sample, or samples
// dst could not take — is emitted by the following call, in order.
func (inst *f32Assembler) Decode(src []byte, dst []float32) (n int) {
	if len(inst.carry) > 0 {
		inst.carry = append(inst.carry, src...)
		n = decodeF32LE(inst.carry, dst)
		inst.carry = inst.carry[:copy(inst.carry, inst.carry[n*bytesPerSample:])]
		return n
	}
	n = decodeF32LE(src, dst)
	if rest := src[n*bytesPerSample:]; len(rest) > 0 {
		inst.carry = append(inst.carry[:0], rest...)
	}
	return n
}

// Pending is how many bytes are carried into the next [f32Assembler.Decode].
func (inst *f32Assembler) Pending() (n int) {
	return len(inst.carry)
}

// Reset drops the carry. A restarted process delivers a different position's
// bytes, so its predecessor's tail must not be prepended to them.
func (inst *f32Assembler) Reset() {
	inst.carry = inst.carry[:0]
}

func decodeF32LE(src []byte, dst []float32) (n int) {
	n = min(len(src)/bytesPerSample, len(dst))
	for i := range n {
		dst[i] = math.Float32frombits(binary.LittleEndian.Uint32(src[i*bytesPerSample:]))
	}
	return n
}
