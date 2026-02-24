//go:build llm_generated_gemini3pro

package splitmix64

// Package splitmix64 implements a bijective (reversible) 64-bit integer mixing function.
// It is based on the SplitMix64 algorithm (D. Steele, S. Vigna, 2014).
//
// Because the mapping is bijective, it acts as a Perfect Hash Function for the
// domain [0, 2^64-1], guaranteeing zero collisions.

const (
	// Gamma is the Weyl sequence constant (golden ratio point).
	Gamma = 0x9e3779b97f4a7c15

	// M1 Mixing constants (odd numbers, ensuring coprimality with 2^64).
	M1 = 0xbf58476d1ce4e5b9
	M2 = 0x94d049bb133111eb

	// InvM1 Modular Multiplicative Inverses of the mixing constants.
	// Calculated such that (M * InvM) == 1 (mod 2^64).
	// Used for the Reverse function.
	InvM1 = 0x96de1b173f119089
	InvM2 = 0x319642b2d24d8ec3

	// S1 Shift constants for the XOR-Shift steps.
	S1 = 30
	S2 = 27
	S3 = 31
)

// Forward applies the SplitMix64 permutation to x.
// It maps the input integer to a pseudo-random output unique to x.
func Forward(x uint64) uint64 {
	// 1. Weyl Sequence (Add Gamma)
	z := x + Gamma

	// 2. Mix (XOR-Shift * Constant)
	z = (z ^ (z >> S1)) * M1
	z = (z ^ (z >> S2)) * M2

	// 3. Final Avalanche
	return z ^ (z >> S3)
}

// Reverse applies the inverse of the SplitMix64 permutation.
// It guarantees that Reverse(Forward(x)) == x.
func Reverse(z uint64) uint64 {
	// 1. Undo Final Avalanche
	z = unxorShift(z, S3)

	// 2. Undo Mix Step 2
	z *= InvM2            // Multiply by inverse to undo multiplication
	z = unxorShift(z, S2) // Undo XOR-Shift

	// 3. Undo Mix Step 1
	z *= InvM1
	z = unxorShift(z, S1)

	// 4. Undo Weyl Sequence
	return z - Gamma
}

// unxorShift inverts the operation y = x ^ (x >> shift).
// This relies on the property that the top 'shift' bits are unchanged,
// allowing us to recover the bits iteratively from top to bottom.
func unxorShift(y uint64, shift int) uint64 {
	x := y
	for i := shift; i < 64; i += shift {
		x ^= (y >> i)
	}
	return x
}
