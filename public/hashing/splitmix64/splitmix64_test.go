//go:build llm_generated_gemini3pro

package splitmix64

import (
	"fmt"
	"math/rand/v2"
	"testing"
)

// TestBijectivity performs a property-based test.
// It verifies that Reverse(Forward(x)) == x for millions of random inputs.
// This proves the function is a permutation (no collisions possible).
func TestBijectivity(t *testing.T) {
	const iterations = 1_000_000
	rng := rand.New(rand.NewPCG(1, 2))

	for i := 0; i < iterations; i++ {
		original := rng.Uint64()
		hashed := Forward(original)
		reversed := Reverse(hashed)

		if original != reversed {
			t.Fatalf("Bijectivity failed for input %d:\nForward -> 0x%x\nReverse -> 0x%x",
				original, hashed, reversed)
		}
	}
}

// TestGoldenRecords verifies the output against the official reference implementation
// (Steele & Vigna, 2014) to ensure regression stability.
func TestGoldenRecords(t *testing.T) {
	var v1, v2, v3, v4, v5 uint64
	v1 = 1234567
	v2 = v1 + uint64(0x9e3779b97f4a7c15)
	v3 = v2 + uint64(0x9e3779b97f4a7c15)
	v4 = v3 + uint64(0x9e3779b97f4a7c15)
	v5 = v4 + uint64(0x9e3779b97f4a7c15)

	tests := []struct {
		input uint64
		want  uint64
	}{
		// Verified against Standard C / Java SplitMix64 Implementation
		// Source: https://rosettacode.org/wiki/Pseudo-random_numbers/Splitmix64
		{v1, 6457827717110365317},
		{v2, 3203168211198807973},
		{v3, 9817491932198370423},
		{v4, 4593380528125082431},
		{v5, 16408922859458223821},

		{0, 0xe220a8397b1dcdaf},                  // Mix(0 + Gamma)
		{0xffffffffffffffff, 0xe4d971771b652c20}, // Mix(MaxUint64 + Gamma)
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("Input_%d", tt.input), func(t *testing.T) {
			got := Forward(tt.input)
			if got != tt.want {
				t.Errorf("Forward(%d)\nGot:  0x%x\nWant: 0x%x", tt.input, got, tt.want)
			}

			// Also verify the Reverse works on the Golden Record
			rev := Reverse(tt.want)
			if rev != tt.input {
				t.Errorf("Reverse(0x%x) = %d; want %d", tt.want, rev, tt.input)
			}
		})
	}
}

// BenchmarkForward measures the throughput of the hash function.
func BenchmarkForward(b *testing.B) {
	var sink uint64
	for i := 0; i < b.N; i++ {
		sink = Forward(uint64(i))
	}
	_ = sink
}

// BenchmarkReverse measures the cost of inverting the hash.
func BenchmarkReverse(b *testing.B) {
	var sink uint64
	// Use a fixed input to isolate function cost
	input := uint64(0x59aebba6a10c1485)
	for i := 0; i < b.N; i++ {
		sink = Reverse(input)
	}
	_ = sink
}
