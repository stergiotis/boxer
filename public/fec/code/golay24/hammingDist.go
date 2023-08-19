package golay24

import "math/bits"

func HammingDistance32(a uint32, b uint32) int {
	return bits.OnesCount32(a ^ b)
}
