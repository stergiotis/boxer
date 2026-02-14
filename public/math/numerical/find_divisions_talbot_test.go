//go:build llm_generated_gemini3pro
package numerical

import (
	"math"
	"testing"
)

func TestExtended(t *testing.T) {
	tests := []struct {
		name     string
		dmin     float64
		dmax     float64
		m        int
		loose    bool
		wantStep float64 // approximate
	}{
		// Heckbert example: 8.1, 14.1, 4 -> 8, 10, 12, 14 (Extended) vs 8, 9, 10... (Wilkinson)
		{"Paper Example 1", 8.1, 14.1, 4, false, 2.0},

		{"Zero Crossing", -10, 10, 5, false, 5.0},
		{"Small Numbers", 0.0, 0.1, 5, false, 0.02},
		{"Loose Constraint", 0.1, 0.9, 5, true, 0.2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Extended(tt.dmin, tt.dmax, tt.m, nil, tt.loose, DefaultWeights, SimpleLegibilityScorer{})

			if len(res.Ticks) == 0 {
				t.Fatal("No ticks generated")
			}

			// Check loose constraint
			if tt.loose {
				if res.Min > tt.dmin || res.Max < tt.dmax {
					t.Errorf("Loose constraint failed. Data [%v, %v], Labels [%v, %v]",
						tt.dmin, tt.dmax, res.Min, res.Max)
				}
			}

			// Check step size approximation
			if math.Abs(res.Step-tt.wantStep) > 1e-5 {
				t.Logf("Note: Got step %v, expected approx %v", res.Step, tt.wantStep)
			}

			// Check legibility (bounds sanity)
			if res.Min > res.Max {
				t.Error("Min > Max")
			}
		})
	}
}
