package numerical

import (
	"math"
	"testing"
)

func TestFindDivisionsNelder(t *testing.T) {
	tests := []struct {
		name     string
		min, max float64
		n        int
	}{
		{"Standard", 0, 10, 5},
		{"Small range", 0, 0.1, 5},
		{"Negative", -10, -5, 5},
		{"Crossing Zero", -5, 5, 11},
		{"Odd Steps", 0, 10, 4}, // might trigger 2.5 or 3.33 logic
		{"Large Numbers", 1000, 2000, 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := FindDivisionsNelder(tt.min, tt.max, tt.n, nil)

			// Basic Validation
			if res.Step <= 0 {
				t.Errorf("Step must be positive, got %v", res.Step)
			}

			// Calculate actual tick positions
			ticks := res.GenerateTicks()

			// Check if ticks cover the data
			firstTick := ticks[0]
			lastTick := ticks[len(ticks)-1]

			// Allow slight float error
			if firstTick > tt.min+1e-9 {
				t.Errorf("First tick %v starts after min %v", firstTick, tt.min)
			}
			if lastTick < tt.max-1e-9 {
				t.Errorf("Last tick %v ends before max %v", lastTick, tt.max)
			}

			// Check if we got roughly the requested number of ticks
			// Nelder's algorithm is strict on Step size, but loose on exact count.
			// It ensures we have *at least* N ticks usually, or close to it.
			if len(ticks) < tt.n-2 || len(ticks) > tt.n+2 {
				t.Logf("Notice: requested %d ticks, got %d. (This is normal for Nelder's constraint)", tt.n, len(ticks))
			}
		})
	}
}

func TestEdgeCasesGo(t *testing.T) {
	// Test the specific Q logic (using 1.2 or 1.6)
	// Range 0 to 1.2, 2 ticks requested -> Raw step 1.2. Should match exactly.
	res := FindDivisionsNelder(0, 1.2, 2, nil)
	// Raw step = 1.2. Normalized = 1.2. Matches 1.2 in Q.
	if math.Abs(res.Step-1.2) > 1e-9 {
		t.Errorf("Expected step 1.2, got %v", res.Step)
	}

	// Zero Range
	res = FindDivisionsNelder(5, 5, 5, nil)
	if res.Step == 0 {
		t.Error("Zero range should produce non-zero step")
	}
}
