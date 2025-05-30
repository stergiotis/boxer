package mixedradix

import (
	"github.com/rs/zerolog/log"
)

func ToDigits(radixii []uint64, n uint64) (digits []uint64) {
	return ToDigitsPrealloc(radixii, n, make([]uint64, 0, len(radixii)))
}
func ToDigitsPrealloc(radixii []uint64, n uint64, preallocDigits []uint64) (digits []uint64) {
	digits = preallocDigits[:0]
	for _, l := range radixii {
		digits = append(digits, n%l)
		n = n / l
	}
	return
}
func FromDigits(radixii []uint64, digits []uint64) (n uint64) {
	l := len(radixii)
	if l != len(digits) {
		log.Panic().Msg("radixii and digits needs to be of same radixii")
	}
	n = digits[0]
	for i, d := range digits[1:] {
		if d != 0 {
			b := uint64(1)
			for j := 0; j < i+1; j++ {
				b *= radixii[j]
			}
			n += d * b
		}
	}

	return
}
