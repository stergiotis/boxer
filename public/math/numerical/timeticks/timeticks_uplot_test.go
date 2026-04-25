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

func TestTimeTicks_ContextLabels_DayBoundary(t *testing.T) {
	// 48h span with 6h ticks → 9 ticks (00, 06, 12, 18 across 2 days, plus closing 00)
	// → 3 context ranges, one per calendar date.
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
	if len(layout.ContextLabels) != 3 {
		t.Errorf("expected 3 context labels, got %d: %+v", len(layout.ContextLabels), layout.ContextLabels)
	}
	wantLabels := []string{"Apr 25 2026", "Apr 26 2026", "Apr 27 2026"}
	for i, want := range wantLabels {
		if i >= len(layout.ContextLabels) {
			break
		}
		if layout.ContextLabels[i].Label != want {
			t.Errorf("context label %d: got %q, want %q", i, layout.ContextLabels[i].Label, want)
		}
	}
}

func TestTimeTicks_NoContextForYearStep(t *testing.T) {
	dataMin := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	dataMax := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	layout := TimeTicks(dataMin, dataMax, TimeTickOptions{PanelWidthPx: 600})
	if layout.Step.Unit != TimeStepUnitYear {
		t.Fatalf("expected year step, got %+v", layout.Step)
	}
	if len(layout.ContextLabels) != 0 {
		t.Errorf("year step should produce no context labels, got %d", len(layout.ContextLabels))
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
