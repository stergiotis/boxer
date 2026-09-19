package bgjobrow

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/jobprogress"
)

var (
	_ JobI = (*bgjob.Runner[int])(nil)
	_ JobI = (*bgjob.Keyed[int])(nil)
)

func TestToProgress(t *testing.T) {
	in := Input{Note: "searching…", Inline: true, BarWidth: 120, RateUnit: "rows"}
	got := ToProgress(bgjob.Snapshot{State: bgjob.StateRunning, Fraction: -1}, in)
	if got.Note != "searching…" || got.Fraction >= 0 || !got.Inline || got.BarWidth != 120 {
		t.Errorf("a job with no note of its own shows the caller's: %+v", got)
	}
	got = ToProgress(bgjob.Snapshot{State: bgjob.StateRunning, Fraction: 0.25, EtaMs: 2000, Rate: 10, Note: "65 directories"}, in)
	if got.Note != "65 directories" {
		t.Errorf("the job's note wins: %q", got.Note)
	}
	if s := jobprogress.StatusLine(got); s == "" {
		t.Error("expected a status line")
	}
}
