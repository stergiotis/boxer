//go:build llm_generated_opus47

package widgets

import (
	"context"
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker/evaluator"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker/presets"
)

// timeRangePickerInstanceState is the per-picker (UTC + Tokyo) draft +
// evaluation-result state. The Phase 4 demo renders two stacked picker
// instances against different timezones to exercise the Tz builder and
// show how the same expression yields different epoch-ms bounds under
// different IANA zones.
type timeRangePickerInstanceState struct {
	tzName       string
	tzID         uint16
	intervalMs   uint32
	idSlot       string
	packedRange  string
	fromExpr     string
	toExpr       string
	lastEval     timerangepicker.EvaluatedRange
	lastEvalErr  error
}

type timeRangePickerDemoState struct {
	utc       *timeRangePickerInstanceState
	tokyo     *timeRangePickerInstanceState
	evaluator *evaluator.Evaluator
	presets   *presets.Registry
	// evalErr is set when NewEvaluator fails (bus not wired); the demo
	// falls back to picker-only rendering so the UI still demos.
	evalErr error
}

func init() {
	registry.Register(registry.Demo{
		Name:        "timerange-picker",
		Category:    "Inputs & pickers",
		Title:       "time range picker",
		Stage:       [2]float32{1200, 720},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindUX,
		Description: "Composite picker: from/to expression fields + Apply + Grafana 7.5 presets. Phase 4 of ADR-0016: evaluator routes through ch.local.exec.timerangepicker (ADR-0028) and the same expression evaluates against two timezones to demonstrate the Tz builder.",
		BusInit: func(_ *c.WidgetIdStack, bus runtimeapp.BusI) (state any) {
			tokyoID, tzErr := timerangepicker.LookupTz("Asia/Tokyo")
			if tzErr != nil {
				tokyoID = timerangepicker.TzIDUTC
			}
			st := &timeRangePickerDemoState{
				presets: presets.DefaultGrafana75(),
				utc: &timeRangePickerInstanceState{
					tzName:     "UTC",
					tzID:       timerangepicker.TzIDUTC,
					intervalMs: 5000,
					idSlot:     "trp-utc",
					fromExpr:   "anchor_now - INTERVAL 1 HOUR",
					toExpr:     "anchor_now",
				},
				tokyo: &timeRangePickerInstanceState{
					tzName:     "Asia/Tokyo",
					tzID:       tokyoID,
					intervalMs: 30000,
					idSlot:     "trp-tokyo",
					fromExpr:   "toStartOfDay(anchor_now)",
					toExpr:     "anchor_now",
				},
			}
			ev, evErr := evaluator.NewEvaluator(bus, timerangepicker.PoolName)
			if evErr != nil {
				st.evalErr = evErr
			} else {
				st.evaluator = ev
			}
			state = st
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoTimeRangePicker(ids, state.(*timeRangePickerDemoState))
		},
		SourceFunc: demoTimeRangePicker,
	})
}

func demoTimeRangePicker(ids *c.WidgetIdStack, st *timeRangePickerDemoState) {
	for range c.CollapsingHeader(ids.PrepareStr("trp-default"),
		c.WidgetText().Text("Grafana 7.5 presets + ClickHouse SQL via chlocalbroker").Keep()).DefaultOpen(true).KeepIter() {
		c.Label("Two pickers, same expressions, different timezones. Apply or click a preset; the evaluator routes through ch.local.exec.timerangepicker and the resolved bounds appear under each picker.").Send()
		if st.evalErr != nil {
			c.Label(fmt.Sprintf("evaluator: %v (no bus wired — picker UI works, evaluation disabled)", st.evalErr)).Send()
		}
		renderTimeRangePickerInstance(ids, st, st.utc)
		c.Separator().Horizontal().Send()
		renderTimeRangePickerInstance(ids, st, st.tokyo)
	}
}

func renderTimeRangePickerInstance(ids *c.WidgetIdStack, st *timeRangePickerDemoState, inst *timeRangePickerInstanceState) {
	c.Label(fmt.Sprintf("Timezone: %s   |   Refresh: %d ms", inst.tzName, inst.intervalMs)).Send()
	fluid := c.TimeRangePicker(ids.PrepareStr(inst.idSlot), inst.fromExpr, inst.toExpr).
		Tz(inst.tzName).
		RefreshInterval(inst.intervalMs)
	// Feed the last evaluated wall-clock bounds back into the picker so
	// the closed trigger button renders human-readable time instead of
	// the raw SQL. Skipped before the first successful Eval — the Rust
	// widget then falls back to the truncated SQL form.
	if inst.lastEval.FromEpochMS != 0 || inst.lastEval.ToEpochMS != 0 {
		fluid = fluid.EvaluatedBounds(inst.lastEval.FromEpochMS, inst.lastEval.ToEpochMS)
	}
	for _, p := range st.presets.All() {
		fluid = fluid.AddPreset(p.Label(), p.FromSQL(), p.ToSQL())
	}
	fluid.SendRespVal(&inst.packedRange)

	tzWire, from, to := timerangepicker.UnpackRange(inst.packedRange)
	if inst.packedRange != "" && (from != inst.fromExpr || to != inst.toExpr || tzWire != inst.tzName) {
		inst.fromExpr, inst.toExpr = from, to
		// The dropdown carries the user's tz pick on the wire; fall back
		// to the picker's configured TzID when the wire is empty (legacy
		// 2-segment payload or an uninitialised binding).
		effectiveTzID := inst.tzID
		if tzWire != "" {
			if id, err := timerangepicker.LookupTz(tzWire); err == nil {
				effectiveTzID = id
				inst.tzID = id
				inst.tzName = tzWire
			}
		}
		if st.evaluator != nil {
			anchor := time.Now()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			fromMs, toMs, err := st.evaluator.Eval(ctx, anchor, effectiveTzID, from, to)
			cancel()
			if err != nil {
				inst.lastEvalErr = err
				inst.lastEval = timerangepicker.EvaluatedRange{}
			} else {
				inst.lastEval = timerangepicker.EvaluatedRange{
					FromEpochMS: fromMs,
					ToEpochMS:   toMs,
					TzID:        effectiveTzID,
				}
				inst.lastEvalErr = nil
			}
		} else {
			inst.lastEvalErr = eh.Errorf("evaluator not wired; ch.local.exec broker unavailable")
		}
	}

	switch {
	case inst.lastEvalErr != nil:
		c.Label(fmt.Sprintf("error: %v", inst.lastEvalErr)).Send()
	case inst.lastEval.FromEpochMS == 0 && inst.lastEval.ToEpochMS == 0:
		c.Label("(click Apply or a preset to evaluate)").Send()
	default:
		c.Label(fmt.Sprintf("from:     %s", inst.lastEval.AsFromTime().Format(time.RFC3339))).Send()
		c.Label(fmt.Sprintf("to:       %s", inst.lastEval.AsToTime().Format(time.RFC3339))).Send()
		c.Label(fmt.Sprintf("duration: %s", inst.lastEval.Duration())).Send()
	}
}
