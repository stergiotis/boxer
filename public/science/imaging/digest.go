package imaging

import (
	"encoding/binary"
	"math"

	"lukechampine.com/blake3"
)

// Digest computes [Image.Hash] from samples written plane by plane, in each
// plane's storage order, in as many writes as the caller likes — row by row
// as a decoder produces them, or a plane at once. The digest is BLAKE3 over
// a header (model, kind, width, height) followed by each plane's own BLAKE3
// digest, so the planes can be written interleaved. Integers and float bit
// patterns are hashed little-endian.
type Digest struct {
	head   [10]byte
	planes []*blake3.Hasher
	buf    []byte
}

// NewDigest starts a digest of an image of the given model, kind and size.
func NewDigest(model ColorModelE, kind SampleKindE, width, height int) *Digest {
	d := &Digest{planes: make([]*blake3.Hasher, model.PlaneCount()), buf: make([]byte, 0, 1<<16)}
	d.head[0], d.head[1] = byte(model), byte(kind)
	binary.LittleEndian.PutUint32(d.head[2:], uint32(width))
	binary.LittleEndian.PutUint32(d.head[6:], uint32(height))
	for i := range d.planes {
		d.planes[i] = blake3.New(32, nil)
	}
	return d
}

// WriteU8 adds 8-bit samples to plane i.
func (inst *Digest) WriteU8(i int, s []uint8) { _, _ = inst.planes[i].Write(s) }

// WriteU16 adds 16-bit samples to plane i.
func (inst *Digest) WriteU16(i int, s []uint16) {
	for start := 0; start < len(s); start += cap(inst.buf) / 2 {
		inst.buf = inst.buf[:0]
		for _, v := range s[start:min(start+cap(inst.buf)/2, len(s))] {
			inst.buf = binary.LittleEndian.AppendUint16(inst.buf, v)
		}
		_, _ = inst.planes[i].Write(inst.buf)
	}
}

// WriteF32 adds float samples to plane i.
func (inst *Digest) WriteF32(i int, s []float32) {
	for start := 0; start < len(s); start += cap(inst.buf) / 4 {
		inst.buf = inst.buf[:0]
		for _, v := range s[start:min(start+cap(inst.buf)/4, len(s))] {
			inst.buf = binary.LittleEndian.AppendUint32(inst.buf, math.Float32bits(v))
		}
		_, _ = inst.planes[i].Write(inst.buf)
	}
}

// WritePlane adds every sample of p to plane i.
func (inst *Digest) WritePlane(i int, p Plane) {
	switch p.kind {
	case SampleKindU8:
		inst.WriteU8(i, p.u8)
	case SampleKindU16:
		inst.WriteU16(i, p.u16)
	case SampleKindF32:
		inst.WriteF32(i, p.f32)
	}
}

// Sum is the digest of what was written.
func (inst *Digest) Sum() (sum [32]byte) {
	h := blake3.New(32, nil)
	_, _ = h.Write(inst.head[:])
	for _, p := range inst.planes {
		_, _ = h.Write(p.Sum(nil))
	}
	h.Sum(sum[:0])
	return
}
