//go:build llm_generated_opus47

package timeticks

import (
	"testing"
	"time"
)

func TestTimeTicks_StepSelection(t *testing.T) {
	tests := []struct {
		name       string
		span       time.Duration
		panelWidth int32
		wantStep   TimeStep
	}{
		{"30min/600px → 5min", 30 * time.Minute, 600, TimeStep{TimeStepUnitMinute, 5}},
		{"1h/600px → 5min", time.Hour, 600, TimeStep{TimeStepUnitMinute, 5}},
		{"1d/800px → 2h", 24 * time.Hour, 800, TimeStep{TimeStepUnitHour, 2}},
		{"30d/800px → 2d", 30 * 24 * time.Hour, 800, TimeStep{TimeStepUnitDay, 2}},
		{"1y/800px → 1mo", 365 * 24 * time.Hour, 800, TimeStep{TimeStepUnitMonth, 1}},
		{"10y/800px → 1y", 10 * 365 * 24 * time.Hour, 800, TimeStep{TimeStepUnitYear, 1}},
	}

	dataMin := time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layout := TimeTicks(dataMin, dataMin.Add(tt.span), TimeTickOptions{
				PanelWidthPx: tt.panelWidth,
			})
			if layout.Step != tt.wantStep {
				t.Errorf("step: got %+v, want %+v", layout.Step, tt.wantStep)
			}
			if len(layout.TickValues) == 0 {
				t.Errorf("no ticks generated")
			}
			if len(layout.TickValues) != len(layout.TickLabels) {
				t.Errorf("tick/label length mismatch: %d vs %d", len(layout.TickValues), len(layout.TickLabels))
			}
		})
	}
}

func TestTimeTicks_DegenerateSpan(t *testing.T) {
	dataMin := time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC)
	layout := TimeTicks(dataMin, dataMin, TimeTickOptions{PanelWidthPx: 600})
	if len(layout.TickValues) != 0 {
		t.Errorf("expected empty ticks for zero span, got %d", len(layout.TickValues))
	}
	if layout.Algorithm != "uplot-ladder" {
		t.Errorf("expected algorithm marker even on degenerate span, got %q", layout.Algorithm)
	}
}

func TestTimeTicks_RolloverRows_HourStepAcrossDays(t *testing.T) {
	// 48h span with 6h ticks → 9 ticks across 3 calendar dates within one year.
	// bucketHour rollover stack is [Year, Day], so:
	//   - Year row: 1 label "2026" spanning all 9 ticks
	//   - Day row: 3 labels (Apr 25 2026, Apr 26 2026, Apr 27 2026)
	dataMin := time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC)
	dataMax := dataMin.Add(48 * time.Hour)
	layout := TimeTicks(dataMin, dataMax, TimeTickOptions{
		PanelWidthPx:    600,
		TargetSpacingPx: 75,
	})
	if layout.Step != (TimeStep{TimeStepUnitHour, 6}) {
		t.Fatalf("expected 6h step, got %+v", layout.Step)
	}
	if got := len(layout.TickValues); got != 9 {
		t.Errorf("expected 9 ticks, got %d", got)
	}
	if len(layout.RolloverRows) != 2 {
		t.Fatalf("expected 2 rollover rows (year, day), got %d", len(layout.RolloverRows))
	}

	yearRow := layout.RolloverRows[0]
	if yearRow.Boundary != BoundaryYear {
		t.Errorf("row 0 boundary: got %v, want BoundaryYear", yearRow.Boundary)
	}
	if len(yearRow.Labels) != 1 || yearRow.Labels[0].Label != "2026" {
		t.Errorf("year row: got %+v, want single label \"2026\"", yearRow.Labels)
	}

	dayRow := layout.RolloverRows[1]
	if dayRow.Boundary != BoundaryDay {
		t.Errorf("row 1 boundary: got %v, want BoundaryDay", dayRow.Boundary)
	}
	wantDays := []string{"Apr 25 2026", "Apr 26 2026", "Apr 27 2026"}
	if len(dayRow.Labels) != len(wantDays) {
		t.Fatalf("day row: got %d labels, want %d", len(dayRow.Labels), len(wantDays))
	}
	for i, want := range wantDays {
		if dayRow.Labels[i].Label != want {
			t.Errorf("day label %d: got %q, want %q", i, dayRow.Labels[i].Label, want)
		}
	}
}

func TestTimeTicks_NoRolloverForYearStep(t *testing.T) {
	dataMin := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	dataMax := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	layout := TimeTicks(dataMin, dataMax, TimeTickOptions{PanelWidthPx: 600})
	if layout.Step.Unit != TimeStepUnitYear {
		t.Fatalf("expected year step, got %+v", layout.Step)
	}
	if len(layout.RolloverRows) != 0 {
		t.Errorf("year step should produce no rollover rows, got %d", len(layout.RolloverRows))
	}
}

func TestTimeTicks_RolloverRows_SecondStepThreeRows(t *testing.T) {
	// 5-minute span at 1-second-resolution panels: bucketSecond rollover
	// stack is [Year, Day, Hour]. The span stays inside one hour, so each
	// row collapses to a single label spanning every tick — but all three
	// rows must be present.
	dataMin := time.Date(2026, 4, 25, 12, 30, 0, 0, time.UTC)
	dataMax := dataMin.Add(5 * time.Minute)
	layout := TimeTicks(dataMin, dataMax, TimeTickOptions{
		PanelWidthPx:    1200,
		TargetSpacingPx: 50,
	})
	if layout.Step.Unit != TimeStepUnitSecond {
		t.Fatalf("expected second step, got %+v", layout.Step)
	}
	if len(layout.RolloverRows) != 3 {
		t.Fatalf("expected 3 rollover rows (year, day, hour) for second-step, got %d", len(layout.RolloverRows))
	}
	want := []struct {
		boundary BoundaryE
		label    string
	}{
		{BoundaryYear, "2026"},
		{BoundaryDay, "Apr 25 2026"},
		{BoundaryHour, "Apr 25 2026 12:00"},
	}
	for i, w := range want {
		row := layout.RolloverRows[i]
		if row.Boundary != w.boundary {
			t.Errorf("row %d boundary: got %v, want %v", i, row.Boundary, w.boundary)
		}
		if len(row.Labels) != 1 {
			t.Errorf("row %d (%v): expected 1 label spanning all ticks, got %d", i, w.boundary, len(row.Labels))
			continue
		}
		if row.Labels[0].Label != w.label {
			t.Errorf("row %d (%v): got %q, want %q", i, w.boundary, row.Labels[0].Label, w.label)
		}
	}
}

func TestTimeTicks_RolloverRows_MinuteStepWithinOneDay(t *testing.T) {
	// 90-minute span at minute-step crosses an hour boundary but stays
	// inside one day. bucketMinute rollover stack is [Year, Day]; expect
	// exactly two rows, with the day row collapsed to one label.
	dataMin := time.Date(2026, 4, 25, 11, 50, 0, 0, time.UTC)
	dataMax := dataMin.Add(90 * time.Minute)
	layout := TimeTicks(dataMin, dataMax, TimeTickOptions{
		PanelWidthPx:    1200,
		TargetSpacingPx: 50,
	})
	if layout.Step.Unit != TimeStepUnitMinute {
		t.Fatalf("expected minute step, got %+v", layout.Step)
	}
	if len(layout.RolloverRows) != 2 {
		t.Fatalf("expected 2 rollover rows for minute-step, got %d", len(layout.RolloverRows))
	}
	dayRow := layout.RolloverRows[1]
	if dayRow.Boundary != BoundaryDay {
		t.Errorf("row 1 boundary: got %v, want BoundaryDay", dayRow.Boundary)
	}
	if len(dayRow.Labels) != 1 {
		t.Errorf("day row: expected 1 label (span stays in one day), got %d", len(dayRow.Labels))
	}
}

func TestTimeTicks_DSTFallBack(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("America/New_York unavailable")
	}
	// 2024-11-03 is the US fall-back day: 02:00 EDT → 01:00 EST.
	dataMin := time.Date(2024, 11, 3, 0, 0, 0, 0, loc)
	dataMax := time.Date(2024, 11, 3, 23, 0, 0, 0, loc)
	layout := TimeTicks(dataMin, dataMax, TimeTickOptions{
		PanelWidthPx:    1200,
		TargetSpacingPx: 50,
		Location:        loc,
	})
	for _, tick := range layout.TickValues {
		local := tick.In(loc)
		_, mi, sec := local.Clock()
		if mi != 0 || sec != 0 {
			t.Errorf("tick %v not on local-hour boundary (mi=%d sec=%d)", local, mi, sec)
		}
	}
	// 2024-11-03 in NY has 25 hours; we span 0..23 inclusive → ≥ 24 ticks.
	if len(layout.TickValues) < 24 {
		t.Errorf("expected ≥24 hourly ticks across DST day, got %d", len(layout.TickValues))
	}
}

func TestTimeTicks_HalfHourTimezone(t *testing.T) {
	// India Standard Time is UTC+05:30. With Hour×1 step, only local-clock
	// snapping puts ticks on minute :00 in IST; UTC truncation would put
	// them on :30 because the offset is half an hour.
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Skip("Asia/Kolkata unavailable")
	}
	dataMin := time.Date(2026, 4, 25, 9, 17, 0, 0, loc)
	dataMax := dataMin.Add(12 * time.Hour)
	layout := TimeTicks(dataMin, dataMax, TimeTickOptions{
		PanelWidthPx:    600,
		TargetSpacingPx: 50,
		Location:        loc,
	})
	if layout.Step != (TimeStep{TimeStepUnitHour, 1}) {
		t.Fatalf("expected Hour×1 step, got %+v", layout.Step)
	}
	for _, tick := range layout.TickValues {
		local := tick.In(loc)
		if mi := local.Minute(); mi != 0 {
			t.Errorf("tick %v has minute %d (want :00 in local time, exercising +05:30 offset)", local, mi)
		}
	}
}

func TestTimeTicks_Hysteresis(t *testing.T) {
	// 70-minute span at 600px / 50px target → natural pick is Minute×10 (5min<5.83min<10min).
	// With prev=Minute×5: prev_ticks=14, target=12, within (1±0.2)*12 = [9.6, 14.4] → keep prev.
	dataMin := time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC)
	dataMax := dataMin.Add(70 * time.Minute)

	noHys := TimeTicks(dataMin, dataMax, TimeTickOptions{PanelWidthPx: 600})
	if noHys.Step != (TimeStep{TimeStepUnitMinute, 10}) {
		t.Fatalf("baseline natural pick: got %+v, want Minute×10", noHys.Step)
	}

	withHys := TimeTicks(dataMin, dataMax, TimeTickOptions{
		PanelWidthPx: 600,
		PrevStep:     TimeStep{TimeStepUnitMinute, 5},
	})
	if withHys.Step != (TimeStep{TimeStepUnitMinute, 5}) {
		t.Errorf("expected hysteresis to retain Minute×5, got %+v", withHys.Step)
	}

	// far-away prev (different rung distance) should NOT trigger hysteresis.
	farPrev := TimeTicks(dataMin, dataMax, TimeTickOptions{
		PanelWidthPx: 600,
		PrevStep:     TimeStep{TimeStepUnitMinute, 1},
	})
	if farPrev.Step == (TimeStep{TimeStepUnitMinute, 1}) {
		t.Errorf("far prev should not stick: got %+v", farPrev.Step)
	}
}

func TestPickTimeStep_AgreesWithTimeTicks(t *testing.T) {
	dataMin := time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC)
	cases := []time.Duration{
		30 * time.Minute,
		24 * time.Hour,
		30 * 24 * time.Hour,
		365 * 24 * time.Hour,
	}
	for _, span := range cases {
		opts := TimeTickOptions{PanelWidthPx: 800}
		picked := PickTimeStep(dataMin, dataMin.Add(span), opts)
		layout := TimeTicks(dataMin, dataMin.Add(span), opts)
		if picked != layout.Step {
			t.Errorf("span %v: PickTimeStep=%+v, TimeTicks.Step=%+v", span, picked, layout.Step)
		}
	}
}

func TestPickTimeStep_DegenerateReturnsSmallestLadder(t *testing.T) {
	dataMin := time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC)
	step := PickTimeStep(dataMin, dataMin, TimeTickOptions{PanelWidthPx: 600})
	if step.IsZero() {
		t.Errorf("expected non-zero step for degenerate span (so callers iterating with Add don't loop)")
	}
}

func TestTimeStep_AddAndApproxDuration(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("America/New_York unavailable")
	}
	// Spring-forward 2024-03-10: 02:00 EST → 03:00 EDT. Day-step Add must
	// land on midnight of the next calendar day, not 23h or 25h later.
	dst := time.Date(2024, 3, 10, 0, 0, 0, 0, loc)
	next := TimeStep{TimeStepUnitDay, 1}.Add(dst)
	if next.Day() != 11 || next.Hour() != 0 {
		t.Errorf("Day+1 across spring-forward: got %v, want 2024-03-11 00:00 local", next)
	}

	// ApproxDuration should be exact for sub-day, approximate for month/year.
	if got := (TimeStep{TimeStepUnitMinute, 5}).ApproxDuration(); got != 5*time.Minute {
		t.Errorf("Minute×5 ApproxDuration: got %v, want 5m", got)
	}
	if got := (TimeStep{TimeStepUnitYear, 1}).ApproxDuration(); got < 364*24*time.Hour || got > 366*24*time.Hour {
		t.Errorf("Year×1 ApproxDuration outside [364d, 366d]: got %v", got)
	}
}

func TestTimeAxisLayout_MapToScreen(t *testing.T) {
	dataMin := time.Date(2026, 4, 25, 0, 0, 0, 0, time.UTC)
	dataMax := dataMin.Add(24 * time.Hour)
	layout := TimeTicks(dataMin, dataMax, TimeTickOptions{PanelWidthPx: 800})

	if got := layout.MapToScreen(dataMin, 0, 800); got != 0 {
		t.Errorf("ViewMin maps to %v, want 0", got)
	}
	if got := layout.MapToScreen(dataMax, 0, 800); got != 800 {
		t.Errorf("ViewMax maps to %v, want 800", got)
	}
	mid := dataMin.Add(12 * time.Hour)
	if got := layout.MapToScreen(mid, 0, 800); got != 400 {
		t.Errorf("midpoint maps to %v, want 400", got)
	}
	// degenerate layout: span = 0
	deg := TimeAxisLayout{ViewMin: dataMin, ViewMax: dataMin}
	if got := deg.MapToScreen(dataMin, 100, 200); got != 100 {
		t.Errorf("degenerate layout maps to %v, want axisStartPx (100)", got)
	}
}

func TestTimeTicks_MonthlyTicksAlignedToFirstOfMonth(t *testing.T) {
	dataMin := time.Date(2026, 1, 17, 12, 30, 0, 0, time.UTC)
	dataMax := dataMin.AddDate(0, 6, 0)
	layout := TimeTicks(dataMin, dataMax, TimeTickOptions{
		PanelWidthPx:    600,
		TargetSpacingPx: 75,
	})
	if layout.Step.Unit != TimeStepUnitMonth {
		t.Fatalf("expected month step, got %+v", layout.Step)
	}
	for _, tick := range layout.TickValues {
		if tick.Day() != 1 || tick.Hour() != 0 || tick.Minute() != 0 {
			t.Errorf("month tick %v not snapped to 1st-of-month at 00:00", tick)
		}
	}
}
