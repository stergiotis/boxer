//go:build llm_generated_opus47

package widgets

import (
	"fmt"
	"time"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// dateTimePickerDemoState carries the three packed-instant uint64s
// the datetime-picker bindings write back into. Per-window so two
// open gallery windows have independent pickers; the struct is
// heap-allocated once at Init so the &st.X pointers handed to
// SendRespVal stay stable across frames.
type dateTimePickerDemoState struct {
	basic    uint64
	withOpts uint64
	narrow   uint64
}

func init() {
	registry.Register(registry.Demo{
		Name:        "datetime-picker",
		Category:    "Inputs & pickers",
		Title:       "datetime picker",
		Stage:       [2]float32{1024, 600},
		Flags:       registry.DemoFlagNonDeterministic, // time-of-day digits drift per minute
		Kind:        registry.DemoKindUX,
		Description: "DatePickerButton + h:m:s drag-spinners as a single composite FFFI2 widget; wire is a u64 holding int64 epoch-ms bits (UTC). Phase 1 of ADR-0016 (Grafana time range picker port).",
		Init: func(_ *c.WidgetIdStack) (state any) {
			now := time.Now()
			seed := c.PackDateTimeUtc(now)
			state = &dateTimePickerDemoState{
				basic:    seed,
				withOpts: seed,
				narrow:   c.PackDateTimeUtc(time.Date(2000, 1, 1, 8, 30, 0, 0, time.UTC)),
			}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoDateTimePicker(ids, state.(*dateTimePickerDemoState))
		},
		SourceFunc: demoDateTimePicker,
	})
}

func demoDateTimePicker(ids *c.WidgetIdStack, st *dateTimePickerDemoState) {
	for range c.CollapsingHeader(ids.PrepareStr("dt-basic"),
		c.WidgetText().Text("default").Keep()).DefaultOpen(true).KeepIter() {
		c.Label("Calendar pop for the date, three drag-spinners for the time of day; UTC.").Send()
		c.DateTimePickerButton(ids.PrepareStr("basic"), st.basic).
			SendRespVal(&st.basic)
		t := c.UnpackDateTimeUtc(st.basic)
		c.Label(fmt.Sprintf("selected: %s (packed: %d)", t.Format(time.RFC3339), st.basic)).Send()
	}

	for range c.CollapsingHeader(ids.PrepareStr("dt-options"),
		c.WidgetText().Text("with options (custom date format, weekend highlight, no calendar week)").Keep()).KeepIter() {
		c.DateTimePickerButton(ids.PrepareStr("opts"), st.withOpts).
			Format("%A, %B %d, %Y").
			HighlightWeekends(true).
			CalendarWeek(false).
			ShowIcon(true).
			SendRespVal(&st.withOpts)
		t := c.UnpackDateTimeUtc(st.withOpts)
		c.Label(fmt.Sprintf("selected: %s", t.Format("Mon 02 Jan 2006 15:04:05 UTC"))).Send()
	}

	for range c.CollapsingHeader(ids.PrepareStr("dt-narrow"),
		c.WidgetText().Text("year range constrained (1900-2030, no arrows)").Keep()).KeepIter() {
		c.DateTimePickerButton(ids.PrepareStr("narrow"), st.narrow).
			StartEndYears(1900, 2030).
			Arrows(false).
			SendRespVal(&st.narrow)
		t := c.UnpackDateTimeUtc(st.narrow)
		c.Label(fmt.Sprintf("selected: %s", t.Format(time.RFC3339))).Send()
	}
}
