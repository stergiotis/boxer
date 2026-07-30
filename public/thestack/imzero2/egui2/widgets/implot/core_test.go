package implot

import (
	"math"
	"strings"
	"testing"
)

func TestNiceNum(t *testing.T) {
	cases := []struct {
		in    float64
		round bool
		want  float64
	}{
		{1.0, false, 1}, {1.7, false, 2}, {4.9, false, 5}, {7.3, false, 10},
		{0.13, true, 0.1}, {0.34, true, 0.5}, {2.6, true, 2}, {8.1, true, 10},
		{130, true, 100}, {620, true, 500},
	}
	for _, tc := range cases {
		got := niceNum(tc.in, tc.round)
		if math.Abs(got-tc.want) > 1e-9*tc.want {
			t.Errorf("niceNum(%v, %v) = %v, want %v", tc.in, tc.round, got, tc.want)
		}
	}
}

func TestLocateTicksBasics(t *testing.T) {
	ticks := locateTicks(Range{0, 10}, 500, nil)
	if len(ticks) == 0 {
		t.Fatal("no ticks located")
	}
	majors := 0
	for _, tk := range ticks {
		if tk.value < 0 || tk.value > 10 {
			t.Errorf("tick %v outside range", tk.value)
		}
		if tk.major {
			majors++
			if tk.label == "" {
				t.Errorf("major tick %v without label", tk.value)
			}
		}
	}
	if majors < 2 {
		t.Errorf("expected ≥2 major ticks, got %d", majors)
	}
}

func TestTickLabelsExact(t *testing.T) {
	// Accumulated float walk must not leak into labels (0.30000000000000004).
	ticks := locateTicks(Range{0, 1}, 400, nil)
	for _, tk := range ticks {
		if !tk.major {
			continue
		}
		if strings.Contains(tk.label, "0000") || len(tk.label) > 8 {
			t.Errorf("suspicious tick label %q", tk.label)
		}
	}
}

func TestTransformRoundTrip(t *testing.T) {
	tr := newTransform(Range{-3, 7}, Range{10, 250}, 40, 20, 500, 300)
	for _, v := range []float64{-3, 0, 3.21, 7} {
		back := tr.plotX(tr.pxX(v))
		if math.Abs(back-v) > 1e-4 {
			t.Errorf("x round-trip %v -> %v", v, back)
		}
	}
	for _, v := range []float64{10, 42.5, 250} {
		back := tr.plotY(tr.pxY(v))
		if math.Abs(back-v) > 1e-3 {
			t.Errorf("y round-trip %v -> %v", v, back)
		}
	}
	// y inversion: larger plot value = smaller pixel y.
	if tr.pxY(250) >= tr.pxY(10) {
		t.Error("y axis not inverted")
	}
}

func TestSanitize(t *testing.T) {
	r := Range{5, 5}.sanitize()
	if r.Size() <= 0 {
		t.Error("degenerate range not widened")
	}
	r = Range{math.NaN(), 3}.sanitize()
	if r != (Range{0, 1}) {
		t.Errorf("NaN range not reset: %+v", r)
	}
	r = Range{4, 1}.sanitize()
	if r.Min != 1 || r.Max != 4 {
		t.Errorf("inverted range not swapped: %+v", r)
	}
}
