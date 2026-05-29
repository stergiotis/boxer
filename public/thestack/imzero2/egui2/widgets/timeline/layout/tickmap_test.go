//go:build llm_generated_opus47

package layout

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/math/numerical/timeticks"
)

func TestComputeTickMap_DegenerateRange_Empty(t *testing.T) {
	now := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	tm := ComputeTickMap(now, now, 0, 1000, nil, timeticks.TimeStep{})
	if tm.Ticks != nil || tm.RolloverRows != nil {
		t.Errorf("degenerate range: got non-empty map (%d ticks, %d rows)", len(tm.Ticks), len(tm.RolloverRows))
	}
}

func TestComputeTickMap_ZeroWidth_Empty(t *testing.T) {
	t0 := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	tm := ComputeTickMap(t0, t1, 100, 100, nil, timeticks.TimeStep{})
	if tm.Ticks != nil {
		t.Errorf("zero-width axis: got %d ticks want nil", len(tm.Ticks))
	}
}

func TestComputeTickMap_NegativeWidth_Empty(t *testing.T) {
	t0 := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	tm := ComputeTickMap(t0, t1, 500, 100, nil, timeticks.TimeStep{})
	if tm.Ticks != nil {
		t.Errorf("negative-width axis: got %d ticks want nil", len(tm.Ticks))
	}
}

func TestComputeTickMap_OneDayRange_MapsToAxis(t *testing.T) {
	t0 := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(24 * time.Hour)
	tm := ComputeTickMap(t0, t1, 0, 1000, nil, timeticks.TimeStep{})
	if len(tm.Ticks) == 0 {
		t.Fatal("one-day range: got 0 ticks")
	}
	for i, tk := range tm.Ticks {
		if tk.X < 0 || tk.X > 1000 {
			t.Errorf("tick %d at %v: X=%v outside [0,1000]", i, tk.T, tk.X)
		}
		if tk.Label == "" {
			t.Errorf("tick %d: empty label", i)
		}
	}
}

func TestComputeTickMap_TicksMonotonicX(t *testing.T) {
	t0 := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(7 * 24 * time.Hour)
	tm := ComputeTickMap(t0, t1, 0, 800, nil, timeticks.TimeStep{})
	if len(tm.Ticks) < 2 {
		t.Skipf("need >=2 ticks to test monotonicity, got %d", len(tm.Ticks))
	}
	for i := 1; i < len(tm.Ticks); i++ {
		if tm.Ticks[i].X <= tm.Ticks[i-1].X {
			t.Errorf("tick X not monotonic at i=%d: %v -> %v", i, tm.Ticks[i-1].X, tm.Ticks[i].X)
		}
		if !tm.Ticks[i].T.After(tm.Ticks[i-1].T) {
			t.Errorf("tick T not monotonic at i=%d: %v -> %v", i, tm.Ticks[i-1].T, tm.Ticks[i].T)
		}
	}
}

func TestComputeTickMap_YearRange_HasContextRows(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	tm := ComputeTickMap(t0, t1, 0, 1200, nil, timeticks.TimeStep{})
	if len(tm.Ticks) == 0 {
		t.Fatal("year range: 0 ticks")
	}
	if len(tm.RolloverRows) == 0 {
		t.Fatal("year range with month-ish ticks: expected ≥1 rollover row (year context)")
	}
	for r, row := range tm.RolloverRows {
		if len(row.Runs) == 0 {
			t.Errorf("rollover row %d: empty runs", r)
		}
		for j, run := range row.Runs {
			if run.EndX <= run.StartX {
				t.Errorf("rollover row %d run %d: EndX %v <= StartX %v", r, j, run.EndX, run.StartX)
			}
			if run.Label == "" {
				t.Errorf("rollover row %d run %d: empty label", r, j)
			}
		}
	}
}

func TestMapTimeToX_LinearScale(t *testing.T) {
	t0 := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(10 * time.Second)
	tm := TickMap{
		ViewMin:     t0,
		ViewMax:     t1,
		AxisStartPx: 100,
		AxisEndPx:   200,
	}
	cases := []struct {
		t    time.Time
		want float64
	}{
		{t0, 100},
		{t1, 200},
		{t0.Add(5 * time.Second), 150},
		{t0.Add(time.Second), 110},
	}
	for _, tc := range cases {
		if got := tm.MapTimeToX(tc.t); got != tc.want {
			t.Errorf("MapTimeToX(%v): got %v want %v", tc.t, got, tc.want)
		}
	}
}

func TestMapTimeToX_DegenerateView_ReturnsStart(t *testing.T) {
	t0 := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	tm := TickMap{
		ViewMin:     t0,
		ViewMax:     t0,
		AxisStartPx: 100,
		AxisEndPx:   200,
	}
	if got := tm.MapTimeToX(t0.Add(time.Hour)); got != 100 {
		t.Errorf("degenerate view: got %v want 100 (AxisStartPx)", got)
	}
}

func TestMapXToMS_RoundTripsMapMSToX(t *testing.T) {
	t0 := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	tm := TickMap{
		ViewMin:     t0,
		ViewMax:     t1,
		AxisStartPx: 100,
		AxisEndPx:   1100,
	}
	for _, frac := range []float64{0.0, 0.25, 0.5, 0.75, 1.0} {
		want := t0.Add(time.Duration(frac * float64(time.Hour))).UnixMilli()
		px := tm.MapMSToX(want)
		got := tm.MapXToMS(px)
		if got != want {
			t.Errorf("frac=%v round-trip: ms→px→ms got %d want %d", frac, got, want)
		}
	}
}

func TestMapXToMS_LeftOfAxisExtrapolatesBelowViewMin(t *testing.T) {
	t0 := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	tm := TickMap{
		ViewMin:     t0,
		ViewMax:     t0.Add(time.Hour),
		AxisStartPx: 100,
		AxisEndPx:   1100,
	}
	// px=90 is 10 px left of axisStart → 10/1000 of an hour BELOW viewMin
	// → -36000 ms relative to viewMin.
	got := tm.MapXToMS(90)
	want := t0.UnixMilli() - 36000
	if got != want {
		t.Errorf("MapXToMS(90): got %d want %d (must extrapolate LEFT of viewMin, not toward it)", got, want)
	}
}

func TestMapXToMS_DegenerateAxis_ReturnsViewMin(t *testing.T) {
	t0 := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	tm := TickMap{
		ViewMin:     t0,
		ViewMax:     t0.Add(time.Hour),
		AxisStartPx: 100,
		AxisEndPx:   100,
	}
	if got := tm.MapXToMS(500); got != t0.UnixMilli() {
		t.Errorf("degenerate axis: got %d want %d", got, t0.UnixMilli())
	}
}

func TestMapMSToX_AgreesWithMapTimeToX(t *testing.T) {
	t0 := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	tm := TickMap{
		ViewMin:     t0,
		ViewMax:     t1,
		AxisStartPx: 0,
		AxisEndPx:   1000,
	}
	mid := t0.Add(30 * time.Minute)
	wantX := tm.MapTimeToX(mid)
	gotX := tm.MapMSToX(mid.UnixMilli())
	if wantX != gotX {
		t.Errorf("MapMSToX vs MapTimeToX: got %v want %v", gotX, wantX)
	}
}

func TestComputeTickMap_AxisStartOffset_PreservedInTickX(t *testing.T) {
	t0 := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(24 * time.Hour)
	tm := ComputeTickMap(t0, t1, 200, 800, nil, timeticks.TimeStep{})
	if len(tm.Ticks) == 0 {
		t.Fatal("no ticks")
	}
	for i, tk := range tm.Ticks {
		if tk.X < 200 || tk.X > 800 {
			t.Errorf("tick %d: X=%v outside axis [200,800]", i, tk.X)
		}
	}
}
