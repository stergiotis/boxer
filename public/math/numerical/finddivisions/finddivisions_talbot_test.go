package finddivisions

import (
	"math"
	"strconv"
	"strings"
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
	opts := TalbotOptions{
		Weights:   DefaultWeights,
		OnlyLoose: false,
		FastMode:  false,
		Qs:        DefaultQ,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts.OnlyLoose = tt.loose
			res := Talbot(tt.dmin, tt.dmax, tt.m, opts, SimpleLegibilityScorer{})

			if len(res.TickValues) == 0 {
				t.Fatal("No ticks generated")
			}

			// Check loose constraint
			if tt.loose {
				if res.ViewMin > tt.dmin || res.ViewMax < tt.dmax {
					t.Errorf("Loose constraint failed. Data [%v, %v], Labels [%v, %v]",
						tt.dmin, tt.dmax, res.ViewMin, res.ViewMax)
				}
			}

			// Check step size approximation
			if math.Abs(res.Step-tt.wantStep) > 1e-5 {
				t.Logf("Note: Got step %v, expected approx %v", res.Step, tt.wantStep)
			}

			// Check legibility (bounds sanity)
			if res.ViewMin > res.ViewMax {
				t.Error("Min > Max")
			}
		})
	}
}

// A Talbot step is a nice number by construction, and the ticks and labels
// built from it must be nice at the bit level too. Two things used to break
// that: math.Pow(10, z) is inexact for z < 0, so a step of 3*10^-1 arrived as
// 0.30000000000000004 before a single tick existed; and start+i*step leaves
// noise of its own (3*0.4 is 1.2000000000000002), which the shortest-round-
// trip label format then printed in full.
func TestTalbotLabelsCarryNoFloatNoise(t *testing.T) {
	opts := TalbotOptions{Weights: DefaultWeights, FastMode: true, OnlyLoose: true}

	// The y axis behind the report: an imztop MiB/s history padded to 1.5.
	res := Talbot(0, 1.5, 5, opts, SimpleLegibilityScorer{})
	wantValues := []float64{0, 0.4, 0.8, 1.2, 1.6}
	wantLabels := []string{"0", "0.4", "0.8", "1.2", "1.6"}
	if len(res.TickValues) != len(wantValues) {
		t.Fatalf("got %d ticks %v, want %d", len(res.TickValues), res.TickValues, len(wantValues))
	}
	for i := range wantValues {
		if res.TickValues[i] != wantValues[i] { // exact: the noise is the bug
			t.Errorf("tick %d: got %v, want %v (exactly)", i, res.TickValues[i], wantValues[i])
		}
		if res.TickLabels[i] != wantLabels[i] {
			t.Errorf("label %d: got %q, want %q", i, res.TickLabels[i], wantLabels[i])
		}
	}

	// q=3 with z=-1 — the step that used to be dirty before it was used.
	if res := Talbot(0, 1.1, 5, opts, SimpleLegibilityScorer{}); res.Step != 0.3 {
		t.Errorf("step: got %v, want 0.3 (exactly)", res.Step)
	}

	// Across the plausible range of a rate axis, no label may run to the
	// width of float noise, and every label must still name its own tick.
	for i := 1; i <= 2000; i++ {
		dmax := float64(i) * 0.01
		res := Talbot(0, dmax, 5, opts, SimpleLegibilityScorer{})
		for j, s := range res.TickLabels {
			if strings.ContainsAny(s, "eE") { // scientific: 3 significant digits by design
				continue
			}
			if n := len(strings.TrimLeft(strings.Replace(s, ".", "", 1), "-0")); n > 6 {
				t.Fatalf("dmax=%v label %d = %q: %d significant digits is float noise", dmax, j, s, n)
			}
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				t.Fatalf("dmax=%v label %d = %q: unparseable: %v", dmax, j, s, err)
			}
			if d := math.Abs(v - res.TickValues[j]); d > math.Abs(res.Step)*1e-6 {
				t.Fatalf("dmax=%v label %d = %q but its tick is %v (off by %v)", dmax, j, s, res.TickValues[j], d)
			}
		}
	}
}
