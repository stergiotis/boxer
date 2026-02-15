//go:build llm_generated_gemini3pro

package finddivisions

import "testing"

func TestCalculateTicks(t *testing.T) {
	tests := []struct {
		name         string
		min, max     float64
		desiredTicks int
		wantSpacing  float64
	}{
		{"Standard range", 0, 10, 5, 2.5},    // Range 10, roughly 2.5 steps (will likely snap to 2 or 5)
		{"0 to 1", 0, 1.0, 5, 0.2},           // Should give 0.0, 0.2, ...
		{"Negative range", -10, 10, 5, 5.0},  // -10, -5, 0, 5, 10
		{"Small numbers", 0.0, 0.1, 5, 0.02}, // 0.00, 0.02, ...
		{"Large numbers", 0, 10000, 5, 2500}, // Likely 2000 or 2500 or 5000 depending on round
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalculateTicks(tt.min, tt.max, tt.desiredTicks)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Basic sanity checks
			if len(got.Ticks) < 2 {
				t.Errorf("Too few ticks generated: %d", len(got.Ticks))
			}
			if got.Ticks[0] > tt.min {
				t.Errorf("First tick %f is greater than min %f", got.Ticks[0], tt.min)
			}
			if got.Ticks[len(got.Ticks)-1] < tt.max {
				t.Errorf("Last tick %f is less than max %f", got.Ticks[len(got.Ticks)-1], tt.max)
			}
		})
	}
}

func TestEdgeCases(t *testing.T) {
	// Case: Min equals Max
	res, err := CalculateTicks(10, 10, 5)
	if err != nil {
		t.Fatalf("Error on equal min/max: %v", err)
	}
	if len(res.Ticks) != 1 || res.Ticks[0] != 10 {
		t.Error("Should return single tick for equal min/max")
	}

	// Case: Desired ticks too low
	_, err = CalculateTicks(0, 100, 1)
	if err == nil {
		t.Error("Should error if desiredTicks < 2")
	}
}
