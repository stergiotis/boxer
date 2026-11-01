package trailviewer

import (
	"context"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker/evaluator"
)

// rangeRunner evaluates a picker expression the viewer does not read
// itself — "Yesterday", "Today so far", a typed expression — through the
// picker's evaluator (ADR-0016 Phase 4), off the frame, into a fixed
// window.
type rangeRunner struct {
	bus    app.BusI
	runner bgjob.Runner[Window]
}

const rangeTimeout = 5 * time.Second

func (inst *rangeRunner) start(from, to, tzName string) (err error) {
	ev, err := evaluator.NewEvaluator(inst.bus, timerangepicker.PoolName)
	if err != nil {
		return eh.Errorf("the time range %q to %q needs the expression evaluator, which this window cannot reach: %w", from, to, err)
	}
	tzId, err := timerangepicker.LookupTz(tzName)
	if err != nil {
		tzId = timerangepicker.TzIDUTC
	}
	if inst.runner.Running() {
		inst.runner.Invalidate()
	}
	inst.runner.Start(nil, bgjob.Spec{Kind: "trailviewer.range", Title: "Trail viewer time range"},
		func(ctx context.Context) (out *Window, err error) {
			ctx, cancel := context.WithTimeout(ctx, rangeTimeout)
			defer cancel()
			fromMs, toMs, err := ev.Eval(ctx, time.Now(), tzId, from, to)
			if err != nil {
				return
			}
			w := Window{From: time.UnixMilli(fromMs).UTC(), To: time.UnixMilli(toMs).UTC()}
			if err = w.validate(); err != nil {
				return nil, eh.Errorf("the time range evaluates to %s – %s: %w", w.From, w.To, err)
			}
			return &w, nil
		})
	return
}

// take returns a finished evaluation once.
func (inst *rangeRunner) take() (w Window, err error, done bool) {
	if res, _, ok := inst.runner.TakeResult(); ok {
		return *res, nil, true
	}
	if snap := inst.runner.Snapshot(); snap.State == bgjob.StateFailed && snap.Err != nil {
		inst.runner.Invalidate()
		return Window{}, snap.Err, true
	}
	return
}

func (inst *rangeRunner) close() { inst.runner.Invalidate() }
