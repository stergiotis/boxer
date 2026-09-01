package coverage

import (
	"encoding/binary"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// byteReader is a bounds-checked cursor over a decoded blob. Every read
// returns an error instead of panicking so that corrupt or truncated input
// (a torn snapshot, a foreign file) degrades into a decode error.
type byteReader struct {
	b   []byte
	off int
}

func (r *byteReader) remaining() int {
	return len(r.b) - r.off
}

func (r *byteReader) take(n int) (b []byte, err error) {
	if n < 0 || r.remaining() < n {
		return nil, eb.Build().Int("need", n).Int("offset", r.off).Int("have", r.remaining()).Errorf("truncated input")
	}
	b = r.b[r.off : r.off+n]
	r.off += n
	return
}

func (r *byteReader) skip(n int) (err error) {
	_, err = r.take(n)
	return
}

func (r *byteReader) u8() (v uint8, err error) {
	b, err := r.take(1)
	if err != nil {
		return
	}
	v = b[0]
	return
}

func (r *byteReader) u32() (v uint32, err error) {
	b, err := r.take(4)
	if err != nil {
		return
	}
	v = binary.LittleEndian.Uint32(b)
	return
}

func (r *byteReader) u64() (v uint64, err error) {
	b, err := r.take(8)
	if err != nil {
		return
	}
	v = binary.LittleEndian.Uint64(b)
	return
}

func (r *byteReader) uleb() (v uint64, err error) {
	var shift uint
	for {
		var b uint8
		b, err = r.u8()
		if err != nil {
			return 0, err
		}
		if shift >= 64 {
			return 0, eb.Build().Int("off", r.off).Errorf("malformed ULEB128 at offset: exceeds 64 bits")
		}
		v |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return
		}
		shift += 7
	}
}

// padTo4 skips padding so the cursor lands on a 4-byte boundary relative to
// the blob start — the counter format pads on absolute offset.
func (r *byteReader) padTo4() (err error) {
	if rem := r.off % 4; rem != 0 {
		err = r.skip(4 - rem)
	}
	return
}
