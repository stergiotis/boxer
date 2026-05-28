package vlq

import "encoding/binary"

func VlqBijective(n uint32) (r uint64) {
	r, _ = VlqBijectiveV(n)
	return
}

// VlqBijectiveV "Git" style VLQ, see https://en.wikipedia.org/wiki/Variable-length_quantity
func VlqBijectiveV(n uint32) (r uint64, nBits int) {
	v := [8]byte{0, 0, 0, 0, 0, 0, 0, 0}
	p := len(v) - 1
	v[p] = byte(n & 0x7f)
	nBits = 8
	n >>= 7
	for n > 0 {
		nBits += 8
		p--
		n--
		v[p] = byte(0x80 | (n & 0x7f))
		n >>= 7
	}
	r = binary.BigEndian.Uint64(v[:])
	return
}
