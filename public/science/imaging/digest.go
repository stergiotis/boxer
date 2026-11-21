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
// patterns are hashed little-endian; signed integers as their two's
// complement bits.
type Digest struct {
	head   [10]byte
	planes []*blake3.Hasher
	buf    []byte
}

// NewDigest starts a digest of an image of the given model, kind and size.
func NewDigest(model ColorModelE, kind SampleKindE, width, height int) *Digest {
	d := &Digest{planes: make([]*blake3.Hasher, model.PlaneCount()), buf: make([]byte, 0, digestBufBytes)}
	d.head = imageHead(model, kind, width, height)
	for i := range d.planes {
		d.planes[i] = blake3.New(32, nil)
	}
	return d
}

// digestBufBytes is the size of the buffer samples are encoded into before
// they are hashed.
const digestBufBytes = 1 << 16

func imageHead(model ColorModelE, kind SampleKindE, width, height int) (head [10]byte) {
	head[0], head[1] = byte(model), byte(kind)
	binary.LittleEndian.PutUint32(head[2:], uint32(width))
	binary.LittleEndian.PutUint32(head[6:], uint32(height))
	return
}

// WriteU8 adds 8-bit samples to plane i.
func (inst *Digest) WriteU8(i int, s []uint8) { _, _ = inst.planes[i].Write(s) }

// WriteU16 adds 16-bit samples to plane i.
func (inst *Digest) WriteU16(i int, s []uint16) { inst.buf = hashU16(inst.planes[i], inst.buf, s) }

// WriteF32 adds float samples to plane i.
func (inst *Digest) WriteF32(i int, s []float32) { inst.buf = hashF32(inst.planes[i], inst.buf, s) }

// WriteI16 adds signed 16-bit samples to plane i.
func (inst *Digest) WriteI16(i int, s []int16) { inst.buf = hashI16(inst.planes[i], inst.buf, s) }

// WriteI32 adds signed 32-bit samples to plane i.
func (inst *Digest) WriteI32(i int, s []int32) { inst.buf = hashI32(inst.planes[i], inst.buf, s) }

// WriteF64 adds 64-bit float samples to plane i.
func (inst *Digest) WriteF64(i int, s []float64) { inst.buf = hashF64(inst.planes[i], inst.buf, s) }

// WritePlane adds every sample of p to plane i.
func (inst *Digest) WritePlane(i int, p Plane) { inst.buf = hashPlane(inst.planes[i], inst.buf, p) }

// Sum is the digest of what was written.
func (inst *Digest) Sum() (sum [32]byte) {
	digests := make([][32]byte, len(inst.planes))
	for i, p := range inst.planes {
		p.Sum(digests[i][:0])
	}
	return sumOver(inst.head[:], digests)
}

// planeDigest is a plane's own digest of its samples, the unit [Image.Hash]
// and [Stack.Hash] are made of.
func planeDigest(p Plane) (sum [32]byte) {
	h := blake3.New(32, nil)
	hashPlane(h, make([]byte, 0, min(digestBufBytes, p.Width()*p.Height()*p.kind.Bytes())), p)
	h.Sum(sum[:0])
	return
}

// sumImage is [Image.Hash] from the planes' own digests.
func sumImage(model ColorModelE, kind SampleKindE, width, height int, digests [][32]byte) [32]byte {
	head := imageHead(model, kind, width, height)
	return sumOver(head[:], digests)
}

func sumOver(head []byte, digests [][32]byte) (sum [32]byte) {
	h := blake3.New(32, nil)
	_, _ = h.Write(head)
	for i := range digests {
		_, _ = h.Write(digests[i][:])
	}
	h.Sum(sum[:0])
	return
}

func hashPlane(h *blake3.Hasher, buf []byte, p Plane) []byte {
	switch p.kind {
	case SampleKindU8:
		_, _ = h.Write(p.u8)
	case SampleKindU16:
		buf = hashU16(h, buf, p.u16)
	case SampleKindF32:
		buf = hashF32(h, buf, p.f32)
	case SampleKindI16:
		buf = hashI16(h, buf, p.i16)
	case SampleKindI32:
		buf = hashI32(h, buf, p.i32)
	case SampleKindF64:
		buf = hashF64(h, buf, p.f64)
	}
	return buf
}

// chunk is how many samples of size bytes fit in buf, which is grown to
// the digest's buffer size when it is too small for one.
func chunk(buf []byte, size int) ([]byte, int) {
	if cap(buf) < size {
		buf = make([]byte, 0, digestBufBytes)
	}
	return buf, cap(buf) / size
}

func hashU16(h *blake3.Hasher, buf []byte, s []uint16) []byte {
	buf, per := chunk(buf, 2)
	for start := 0; start < len(s); start += per {
		buf = buf[:0]
		for _, v := range s[start:min(start+per, len(s))] {
			buf = binary.LittleEndian.AppendUint16(buf, v)
		}
		_, _ = h.Write(buf)
	}
	return buf
}

func hashI16(h *blake3.Hasher, buf []byte, s []int16) []byte {
	buf, per := chunk(buf, 2)
	for start := 0; start < len(s); start += per {
		buf = buf[:0]
		for _, v := range s[start:min(start+per, len(s))] {
			buf = binary.LittleEndian.AppendUint16(buf, uint16(v))
		}
		_, _ = h.Write(buf)
	}
	return buf
}

func hashI32(h *blake3.Hasher, buf []byte, s []int32) []byte {
	buf, per := chunk(buf, 4)
	for start := 0; start < len(s); start += per {
		buf = buf[:0]
		for _, v := range s[start:min(start+per, len(s))] {
			buf = binary.LittleEndian.AppendUint32(buf, uint32(v))
		}
		_, _ = h.Write(buf)
	}
	return buf
}

func hashF32(h *blake3.Hasher, buf []byte, s []float32) []byte {
	buf, per := chunk(buf, 4)
	for start := 0; start < len(s); start += per {
		buf = buf[:0]
		for _, v := range s[start:min(start+per, len(s))] {
			buf = binary.LittleEndian.AppendUint32(buf, math.Float32bits(v))
		}
		_, _ = h.Write(buf)
	}
	return buf
}

func hashF64(h *blake3.Hasher, buf []byte, s []float64) []byte {
	buf, per := chunk(buf, 8)
	for start := 0; start < len(s); start += per {
		buf = buf[:0]
		for _, v := range s[start:min(start+per, len(s))] {
			buf = binary.LittleEndian.AppendUint64(buf, math.Float64bits(v))
		}
		_, _ = h.Write(buf)
	}
	return buf
}
