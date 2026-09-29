// Package bgjobrow draws a running bgjob as the standard job row — bar, share
// and time left, the job's note, Cancel — and cancels the job when Cancel is
// clicked. It is the one place a [bgjob.Snapshot] is mapped onto
// [jobprogress.Input], so every surface that waits on a [bgjob.Runner] or a
// [bgjob.Keyed] shows the same row and the same figures (ADR-0247), and gains
// what the row gains.
//
// jobprogress itself stays free of any producer; this package is the adapter
// for the producer most surfaces use.
package bgjobrow

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/jobprogress"
)

// JobI is what the row needs of a job; [bgjob.Runner] and [bgjob.Keyed]
// both are one.
type JobI interface {
	Snapshot() bgjob.Snapshot
	Cancel()
}

// Input places and labels the row. The job's own note wins over Note.
type Input struct {
	// Ids and ScopeKey are [jobprogress.Input]'s (ADR-0267 W4): required
	// when Cancel is set, otherwise Ids may be nil.
	Ids      *c.WidgetIdStack
	ScopeKey string
	// Job is the job the row waits on.
	Job JobI
	// Title is shown above a stacked row; not drawn Inline.
	Title string
	// Note is what the row says while the job has published no note of its
	// own — "searching…" for a job that only ever runs indeterminate.
	Note string
	// Cancel draws Cancel, which cancels Job when clicked; see
	// [jobprogress.Input.Cancel]. False draws a row that cannot be cancelled.
	Cancel bool
	// Inline, BarWidth and RateUnit are [jobprogress.Input]'s.
	Inline   bool
	BarWidth float32
	RateUnit string
}

// Result is what one Render reports.
type Result struct {
	// Running is whether the row drew: true while the job runs.
	Running bool
	// Cancelled is true on the frame Cancel was clicked; the job has been
	// told to cancel.
	Cancelled bool
}

// Render draws the row while Input.Job is running and nothing otherwise,
// and reports which. While it draws it keeps frames coming — the job runs on
// its own goroutine and cannot ask for one — and a click on Cancel cancels
// the job. What a cancelled or failed job then shows is the caller's: the
// row is for the wait.
func Render(in Input) (res Result) {
	snap := in.Job.Snapshot()
	if snap.State != bgjob.StateRunning {
		return
	}
	c.RequestRepaint()
	res.Running = true
	if jobprogress.Render(ToProgress(snap, in)).CancelClicked {
		in.Job.Cancel()
		res.Cancelled = true
	}
	return
}

// ToProgress is the mapping Render draws, for a caller that places the row
// itself.
func ToProgress(snap bgjob.Snapshot, in Input) jobprogress.Input {
	note := snap.Note
	if note == "" {
		note = in.Note
	}
	return jobprogress.Input{
		Ids:      in.Ids,
		ScopeKey: in.ScopeKey,
		Title:    in.Title,
		Fraction: snap.Fraction,
		EtaMs:    snap.EtaMs,
		Rate:     snap.Rate,
		RateUnit: in.RateUnit,
		Note:     note,
		Cancel:   in.Cancel,
		Inline:   in.Inline,
		BarWidth: in.BarWidth,
	}
}
