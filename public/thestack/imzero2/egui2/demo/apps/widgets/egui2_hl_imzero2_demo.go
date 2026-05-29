//go:build llm_generated_opus47

package widgets

import (
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// imzero2DemoState carries the per-window state for the catch-all
// imzero2 widget showcase: counters, slider/checkbox values, current
// combobox selection, radio choice, frame counter and the text-edit /
// drag-value bindings. Two open gallery windows tick these
// independently — and the checkboxVal pointer handed to
// statemanager.OverrideDatabindingBPtr stays stable for the lifetime
// of the App instance because the struct is heap-allocated once in
// Init.
type imzero2DemoState struct {
	n                int
	sliderVal        float64
	checkboxVal      bool
	frame            uint64
	myText           string
	myDragFloat      float64
	dropDownSelected int
	radioChoice      uint8
}

func init() {
	registry.Register(registry.Demo{
		Name:        "imzero2",
		Category:    "Layout & widgets",
		Title:       icons.IconLightning + " imzero2 (catch-all)",
		Stage:       [2]float32{1024, 700},
		Flags:       registry.DemoFlagNonDeterministic, // renders live time.Date(...) timestamp
		Kind:        registry.DemoKindDX,
		Description: "Mixed widget showcase: buttons, text-edit, slider, checkbox, radio, combobox, grid, scroll area, tree.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &imzero2DemoState{
				dropDownSelected: -1,
			}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			renderImzero2Demo(ids, state.(*imzero2DemoState))
		},
		SourceFunc: renderImzero2Demo,
	})
}

// renderImzero2Demo is the body of the "imzero2" window — the main
// widget showcase. State is per-window so two open gallery windows
// have independent counters and slider/checkbox/radio values.
func renderImzero2Demo(ids *c.WidgetIdStack, st *imzero2DemoState) {
	statemanager := c.CurrentApplicationState.StateManager
	incrementLabelAtoms := c.Atoms().Text("increment/decrement").Keep()
	c.Label(time.Now().GoString()).Send()
	{
		r := c.Button(c.MakeAbsoluteIdHighEntropy(0xdeadbeef), incrementLabelAtoms).SendResp()
		if r.HasPrimaryClicked() {
			st.n++
		} else if r.HasSecondaryClicked() {
			st.n--
		}
	}
	for range c.IdScope(ids.PrepareStr("myscope")) {
		c.TextEdit(ids.PrepareStr("textedit"), st.myText, false).SendRespVal(&st.myText)
		c.DragValueF64(ids.PrepareStr("dragvalue"), st.myDragFloat).SendRespVal(&st.myDragFloat)
	}
	for range c.ComboBox(ids.PrepareStr("combobox"), c.WidgetText().Text("combobox").Keep(), c.WidgetText().Text(fmt.Sprintf("option %d", st.dropDownSelected)).Keep()).KeepIter() {
		for i := 0; i < 10; i++ {
			selected := i == st.dropDownSelected
			if c.Button(ids.PrepareSeq(uint64(0x1111+i)), c.Atoms().Text(fmt.Sprintf("option %d", i)).Keep()).Selected(selected).FrameWhenInactive(!selected).Frame(true).SendResp().HasPrimaryClicked() {
				st.dropDownSelected = i
			}
		}
	}
	// RadioButton's HasPrimaryClicked fires without user input (CLAUDE.md
	// footgun); use Button.Selected(...) for the same visual + reliable click,
	// matching the combobox option pattern above.
	if c.Button(ids.PrepareStr("radio-1"), c.Atoms().Text("radio 1").Keep()).Selected(st.radioChoice == 1).SendResp().HasPrimaryClicked() {
		st.radioChoice = 1
	}
	if c.Button(ids.PrepareStr("radio-2"), c.Atoms().Text("radio 2").Keep()).Selected(st.radioChoice == 2).SendResp().HasPrimaryClicked() {
		st.radioChoice = 2
	}
	if c.Button(ids.PrepareStr("radio-3"), c.Atoms().Text("radio 3").Keep()).Selected(st.radioChoice == 3).SendResp().HasPrimaryClicked() {
		st.radioChoice = 3
	}

	c.Label(fmt.Sprintf("%d", st.n)).Selectable(false).Send()
	c.Separator().Send()

	c.SliderF64(ids.PrepareStr("slider"), st.sliderVal, 0.0, 100.0).
		Text("my text").
		SendRespVal(&st.sliderVal)
	c.Label(fmt.Sprintf("checked=%v", st.checkboxVal)).Send()
	if c.Checkbox(ids.PrepareStr("checkbox"), st.checkboxVal, "my checkbox").SendRespVal(&st.checkboxVal).HasChanged() {
		log.Info().Bool("value", st.checkboxVal).Msg("checkbox has changed")
	}

	if c.Button(ids.PrepareStr("set-true"), c.Atoms().Text("set to true").Keep()).SendResp().HasPrimaryClicked() {
		st.checkboxVal = true
		statemanager.OverrideDatabindingBPtr(&st.checkboxVal)
	}
	if c.Button(ids.PrepareStr("set-false"), c.Atoms().Text("set to false").Keep()).SendResp().HasPrimaryClicked() {
		statemanager.OverrideDatabindingBPtr(&st.checkboxVal)
		st.checkboxVal = false
	}
	{
		c.Label(fmt.Sprintf("frame=%d", st.frame)).Send()
		c.Passthrough(ids.PrepareStr("frame-passthrough"), st.frame)
		st.frame += 2
	}

	for range c.VerticalCenteredJustified().KeepIter() {
		c.Label("A").Send()
		c.Label("B").Send()
		c.Label("C").Send()
	}
	for range c.Grid(ids.PrepareStr("demo-grid")).NumColumns(3).KeepIter() {
		c.Label("A").Send()
		c.Label("B").Send()
		c.Label("C").Send()
		c.EndRow()
		c.Label("D").Send()
		c.Label("E").Send()
		c.Label("F").Send()
	}

	for range c.NodeDir(ids.PrepareStr("d0"), c.WidgetText().Text("dir 0").Keep()).SendIter() {
		for range c.NodeDir(ids.PrepareStr("d1"), c.WidgetText().Text("dir 1").Keep()).SendIter() {
			if c.NodeLeaf(ids.PrepareStr("l0"), c.WidgetText().Text("leaf 0").Keep()).SendResp().HasNodelikeSelected() {
				c.NodeLeaf(ids.PrepareStr("l0s"), c.WidgetText().Text("--- leaf 0 has is selected ---").Keep()).Send()
			}
			c.NodeLeaf(ids.PrepareStr("l1"), c.WidgetText().Text("leaf 1").Keep()).Send()
			c.NodeLeaf(ids.PrepareStr("l2"), c.WidgetText().Text("leaf 2").Keep()).Send()
		}
		for range c.NodeDir(ids.PrepareStr("d2"), c.WidgetText().Text("dir 2").Keep()).SendIter() {
			c.NodeLeaf(ids.PrepareStr("l3"), c.WidgetText().Text("leaf 3").Keep()).Send()
			c.NodeLeaf(ids.PrepareStr("l4"), c.WidgetText().Text("leaf 4").Keep()).Send()
		}
	}
	for range c.ScrollArea().Vscroll(true).KeepIter() {
		for range c.ScrollArea().Vscroll(true).KeepIter() {
			c.Tree(ids.PrepareStr("tree")).Send()
		}

		for range c.CollapsingHeader(ids.PrepareStr("section1"), c.WidgetText().Text("section 1").Keep()).KeepIter() {
			c.Label("Hello section1").Send()
			r := c.Button(ids.PrepareStr("section1-btn"), incrementLabelAtoms).SendResp()
			if r.HasPrimaryClicked() {
				st.n++
			} else if r.HasSecondaryClicked() {
				st.n--
			}
		}
	}
}
