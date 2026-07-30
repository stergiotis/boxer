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
	tr := newTransform(Range{-3, 7}, Range{10, 250}, ScaleLinear, ScaleLinear, 40, 20, 500, 300)
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

func TestLogTransformRoundTrip(t *testing.T) {
	tr := newTransform(Range{0.1, 1000}, Range{1, 100}, ScaleLog10, ScaleLog10, 40, 20, 500, 300)
	for _, v := range []float64{0.1, 1, 32.5, 1000} {
		back := tr.plotX(tr.pxX(v))
		if math.Abs(back-v)/v > 1e-4 {
			t.Errorf("log x round-trip %v -> %v", v, back)
		}
	}
	// Equal pixel spacing per decade: 1→10 and 10→100 must span the same px.
	d1 := tr.pxX(10) - tr.pxX(1)
	d2 := tr.pxX(100) - tr.pxX(10)
	if math.Abs(float64(d1-d2)) > 0.01 {
		t.Errorf("decades not equally spaced: %v vs %v", d1, d2)
	}
}

func TestSymLogRoundTrip(t *testing.T) {
	tr := newTransform(Range{-1000, 1000}, Range{0, 1}, ScaleSymLog, ScaleLinear, 0, 0, 600, 100)
	for _, v := range []float64{-1000, -1, 0, 2.5, 1000} {
		back := tr.plotX(tr.pxX(v))
		if math.Abs(back-v) > math.Max(1e-3, math.Abs(v)*1e-3) {
			t.Errorf("symlog round-trip %v -> %v", v, back)
		}
	}
	// Symmetry: ±v sit mirrored about the center.
	if math.Abs(float64(tr.pxX(0)-300)) > 0.01 {
		t.Errorf("symlog zero not centered: %v", tr.pxX(0))
	}
}

func TestLog10Ticks(t *testing.T) {
	ticks := locateTicksLog10(Range{0.5, 2000}, nil)
	var majors []float64
	for _, tk := range ticks {
		if tk.major {
			majors = append(majors, tk.value)
		}
	}
	want := []float64{1, 10, 100, 1000}
	if len(majors) != len(want) {
		t.Fatalf("majors = %v, want %v", majors, want)
	}
	for i := range want {
		if math.Abs(majors[i]-want[i]) > 1e-9 {
			t.Errorf("major[%d] = %v, want %v", i, majors[i], want[i])
		}
	}
}

func TestTimeTicks(t *testing.T) {
	// 48 h window starting mid-hour: ticks must snap to unit boundaries.
	t0 := float64(1_780_000_000) // fixed epoch, keeps the test deterministic
	ticks := locateTicksTime(Range{t0, t0 + 48*3600}, 500, nil)
	if len(ticks) < 2 {
		t.Fatalf("too few time ticks: %d", len(ticks))
	}
	if len(ticks) > 8 {
		t.Errorf("too many time ticks for the density: %d", len(ticks))
	}
	step := ticks[1].value - ticks[0].value
	for i := 1; i < len(ticks); i++ {
		if d := ticks[i].value - ticks[i-1].value; math.Abs(d-step) > 1 {
			t.Errorf("uneven time step: %v vs %v", d, step)
		}
	}
	for _, tk := range ticks {
		if tk.label == "" {
			t.Errorf("time tick %v without label", tk.value)
		}
		if int64(tk.value)%3600 != 0 {
			t.Errorf("time tick %v not on an hour boundary", tk.value)
		}
	}
}

func TestSanitizeScaledLog(t *testing.T) {
	r := sanitizeScaled(Range{-5, 100}, ScaleLog10)
	if r.Min <= 0 {
		t.Errorf("log sanitize left non-positive min: %+v", r)
	}
	r = sanitizeScaled(Range{-5, -1}, ScaleLog10)
	if r.Min <= 0 || r.Max <= 0 {
		t.Errorf("log sanitize of all-negative range: %+v", r)
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

func TestBinSamples(t *testing.T) {
	samples := []float64{0, 0.1, 0.2, 0.5, 0.9, 1.0, math.NaN()}
	counts, lo, width, n := binSamples(samples, 4, false)
	if n != 6 {
		t.Fatalf("n = %d, want 6", n)
	}
	if lo != 0 || math.Abs(width-0.25) > 1e-12 {
		t.Errorf("lo=%v width=%v", lo, width)
	}
	sum := 0.0
	for _, cn := range counts {
		sum += cn
	}
	if sum != 6 {
		t.Errorf("counts sum %v, want 6 (max sample must clamp into last bin)", sum)
	}
	// Density integrates to one.
	dcounts, _, dw, _ := binSamples(samples, 4, true)
	integ := 0.0
	for _, cn := range dcounts {
		integ += cn * dw
	}
	if math.Abs(integ-1) > 1e-9 {
		t.Errorf("density integral %v, want 1", integ)
	}
	// Sturges default: n=6 → ceil(log2 6)+1 = 4 bins.
	sc, _, _, _ := binSamples(samples, 0, false)
	if len(sc) != 4 {
		t.Errorf("sturges bins = %d, want 4", len(sc))
	}
}

func TestBin2DOrientation(t *testing.T) {
	// One point at max-y must land in row 0 (top), one at min-y in the last row.
	xs := []float64{0, 1}
	ys := []float64{0, 1}
	values, _, _, _, _, ok := bin2D(xs, ys, 2, 2)
	if !ok {
		t.Fatal("bin2D failed")
	}
	// (x=1, y=1) → top-right = row 0, col 1; (x=0, y=0) → bottom-left = row 1, col 0.
	if values[0*2+1] != 1 || values[1*2+0] != 1 {
		t.Errorf("orientation wrong: %v", values)
	}
}

func TestHashF64s(t *testing.T) {
	a := []float64{1, 2, 3}
	b := []float64{1, 2, 3}
	cc := []float64{1, 2, 3.0000001}
	if hashF64s(a) != hashF64s(b) {
		t.Error("hash not stable")
	}
	if hashF64s(a) == hashF64s(cc) {
		t.Error("hash not sensitive")
	}
}
