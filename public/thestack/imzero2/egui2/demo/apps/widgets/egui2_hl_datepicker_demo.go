//go:build llm_generated_opus47

package widgets

import (
	"fmt"
	"time"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// datePickerDemoState carries the three packed-YYYYMMDD uint64 values
// the date-picker bindings write back into. Per-window so two open
// gallery windows have independent date pickers; the struct is
// heap-allocated once at Init so the &st.X pointers handed to
// SendRespVal stay stable across frames.
type datePickerDemoState struct {
	basic    uint64
	withOpts uint64
	narrow   uint64
}

func init() {
	registry.Register(registry.Demo{
		Name:        "date-picker",
		Category:    "Inputs & pickers",
		Title:       "date picker",
		Stage:       [2]float32{1024, 600},
		Kind:        registry.DemoKindUX,
		Description: "egui_extras DatePickerButton wired through FFFI2 with a packed YYYYMMDD u64 wire format and the standard SendRespVal one-frame-lag databinding.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			now := time.Now()
			today := c.PackDateYmd(now.Year(), int(now.Month()), now.Day())
			state = &datePickerDemoState{
				basic:    today,
				withOpts: today,
				narrow:   c.PackDateYmd(2000, 1, 1),
			}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoDatePicker(ids, state.(*datePickerDemoState))
		},
		SourceFunc: demoDatePicker,
	})
}

func demoDatePicker(ids *c.WidgetIdStack, st *datePickerDemoState) {
	for range c.CollapsingHeader(ids.PrepareStr("dp-basic"),
		c.WidgetText().Text("default").Keep()).DefaultOpen(true).KeepIter() {
		c.Label("Click the button to open the calendar; pick a date and watch the readout update on the next frame.").Send()
		c.DatePickerButton(ids.PrepareStr("basic"), st.basic).
			SendRespVal(&st.basic)
		y, m, d := c.UnpackDateYmd(st.basic)
		c.Label(fmt.Sprintf("selected: %04d-%02d-%02d (packed: %d)", y, m, d, st.basic)).Send()
	}

	for range c.CollapsingHeader(ids.PrepareStr("dp-options"),
		c.WidgetText().Text("with options (custom format, weekend highlight, no calendar week)").Keep()).KeepIter() {
		c.DatePickerButton(ids.PrepareStr("opts"), st.withOpts).
			Format("%A, %B %d, %Y").
			HighlightWeekends(true).
			CalendarWeek(false).
			ShowIcon(true).
			SendRespVal(&st.withOpts)
		y, m, d := c.UnpackDateYmd(st.withOpts)
		c.Label(fmt.Sprintf("selected: %04d-%02d-%02d", y, m, d)).Send()
	}

	for range c.CollapsingHeader(ids.PrepareStr("dp-narrow"),
		c.WidgetText().Text("year range constrained (1900-2030, no arrows)").Keep()).KeepIter() {
		c.DatePickerButton(ids.PrepareStr("narrow"), st.narrow).
			StartEndYears(1900, 2030).
			Arrows(false).
			SendRespVal(&st.narrow)
		y, m, d := c.UnpackDateYmd(st.narrow)
		c.Label(fmt.Sprintf("selected: %04d-%02d-%02d", y, m, d)).Send()
	}
}
