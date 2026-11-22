package widgets

import (
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

func init() {
	registry.Register(registry.Demo{
		Name:     "selector",
		Category: "Layout & widgets",
		Title:    icons.PhToggleLeft + " selector",
		Stage:    [2]float32{1024, 520},
		Kind:     registry.DemoKindMixed,
		Description: "widgets/selector: a segmented bar is one control under one outline, the selected option filled " +
			"(ADR-0404 SD5), set beside a row of ordinary command buttons. The outline takes the theme's button radius " +
			"and the segments sit a hairline apart, so a hovered one does not touch its neighbour (SD8). Then the same " +
			"choice inline, vertical, with a gap, frameless, as selectable labels and as radio buttons.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &selectorDemoState{view: 1, lens: 2, density: 1, gapped: 0, frameless: 0, chip: 1, radio: 0}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoSelector(ids, state.(*selectorDemoState))
		},
		SourceFunc: demoSelector,
	})
}

// selectorDemoState holds one value per bar.
type selectorDemoState struct {
	view, lens, density, gapped, frameless, chip, radio int
	clicks                                              int
	wrap                                                bool
}

func demoSelector(ids *c.WidgetIdStack, st *selectorDemoState) {
	d := styletokens.ActiveDensity()
	gap := styletokens.GapSections(d)
	c.Label("A segmented bar beside a row of command buttons:").Send()
	for rt := range c.RichTextLabel("The outline's radius is the " + styletokens.ActiveTheme().String() + " theme's button radius, " +
		strconv.FormatFloat(float64(styletokens.RoundingButton()), 'f', -1, 32) + ", plus the inset. Hover a segment: it does not touch its neighbour.") {
		rt.Small().Weak()
	}
	for range c.HorizontalTop().KeepIter() {
		selector.Segmented(ids, "view", &st.view).
			Option(0, "Document").Option(1, "Source").Option(2, "Revisions").Option(3, "Lint").
			Send()
		c.AddSpace(gap)
		for _, label := range []string{"Refresh", "Copy", "Export"} {
			if c.Button(ids.PrepareStr(label), c.Atoms().Text(label).Keep()).SendResp().HasPrimaryClicked() {
				st.clicks++
			}
		}
	}
	c.Separator().Send()
	for range c.HorizontalTop().KeepIter() {
		c.Label("Inline, in a row with a label:").Send()
		selector.Segmented(ids, "lens", &st.lens).Inline().
			Option(0, "statement").Option(1, "ast").Option(2, "plan").Option(3, "lineage").
			Send()
		c.Checkbox(ids.PrepareStr("wrap"), st.wrap, "wrap").SendRespVal(&st.wrap)
	}
	for range c.HorizontalTop().KeepIter() {
		c.Label("With Gap:").Send()
		selector.Segmented(ids, "gapped", &st.gapped).Gap(styletokens.GapInline(d)).
			Option(0, "Day").Option(1, "Week").Option(2, "Month").
			Send()
	}
	for range c.HorizontalTop().KeepIter() {
		c.Label("Frameless (for a popup that states the selection itself):").Send()
		selector.Segmented(ids, "frameless", &st.frameless).Frameless().
			Option(0, "Day").Option(1, "Week").Option(2, "Month").
			Send()
	}
	for range c.HorizontalTop().KeepIter() {
		c.Label("StyleSelectable:").Send()
		selector.Segmented(ids, "chip", &st.chip).Style(selector.StyleSelectable).
			Option(0, "Day").Option(1, "Week").Option(2, "Month").
			Send()
	}
	c.Separator().Send()
	for range c.HorizontalTop().KeepIter() {
		for range c.Vertical().KeepIter() {
			c.Label("Vertical:").Send()
			selector.Segmented(ids, "density", &st.density).Vertical().
				Option(0, "Tight").Option(1, "Standard").Option(2, "Roomy").
				Send()
		}
		c.AddSpace(gap)
		for range c.Vertical().KeepIter() {
			c.Label("StyleRadio, vertical:").Send()
			selector.Segmented(ids, "radio", &st.radio).Style(selector.StyleRadio).Vertical().
				Option(0, "Tight").Option(1, "Standard").Option(2, "Roomy").
				Send()
		}
	}
}
