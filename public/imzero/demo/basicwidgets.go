//go:build !bootstrap

package demo

import (
	"fmt"

	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/logical"
)

type comboBoxState struct {
	items    []string
	selected int
}

var comboBoxS comboBoxState = comboBoxState{
	items:    []string{"<none>", "option a", "option b", "option c"},
	selected: 0,
}

var listS comboBoxState = comboBoxState{
	items:    []string{"<none>", "option a", "option b", "option c"},
	selected: 0,
}

func renderComboBox() {
	{
		s := comboBoxS.selected
		if imgui.BeginComboV("my combo", comboBoxS.items[s], imgui.ImGuiComboFlags_None) {
			for i, v := range comboBoxS.items {
				if imgui.SelectableV(v, i == s, 0, 0) {
					comboBoxS.selected = i
				}
			}
			imgui.EndCombo()
		}
	}

	{
		s := listS.selected
		if imgui.BeginListBoxV("list", 0) {
			for i, v := range listS.items {
				if imgui.SelectableV(v, i == s, 0, 0) {
					listS.selected = i
				}
			}
			imgui.EndListBox()
		}
	}
}

var buttonClicked int

func renderButton() {
	imgui.TextUnformatted("Button:")
	imgui.SameLine()
	if imgui.Button("Button") {
		buttonClicked++
	}
	if buttonClicked > 0 {
		imgui.TextUnformatted(fmt.Sprintf("button clicked %d times", buttonClicked))
	}
}

func renderTooltip() {
	imgui.TextUnformatted("Tooltips:")
	imgui.SameLine()
	imgui.SmallButton("Basic")
	if imgui.IsItemHovered() {
		if imgui.BeginTooltip() {
			imgui.TextUnformatted("I am a tooltip")
			imgui.EndTooltip()
		}
	}
	imgui.SameLine()
	imgui.SmallButton("Delayed")
	if imgui.IsItemHoveredV(imgui.ImGuiHoveredFlags_DelayNormal) {
		if imgui.BeginTooltip() {
			imgui.TextUnformatted("I am a delayed tooltip")
			imgui.EndTooltip()
		}
	}
}

func renderTree() {
	if imgui.TreeNode("solar system") {
		if imgui.TreeNode("sun") {
			imgui.TreePop()
		}
		if imgui.TreeNode("earth") {
			if imgui.TreeNode("europe") {
				imgui.Bullet()
				imgui.SameLine()
				imgui.TextUnformatted("switzerland")
				imgui.Bullet()
				imgui.SameLine()
				imgui.TextUnformatted("germany")
				imgui.Bullet()
				imgui.SameLine()
				imgui.TextUnformatted("france")
				imgui.TreePop()
			}
			if imgui.TreeNode("america") {
				imgui.TreePop()
			}
			if imgui.TreeNode("asia") {
				imgui.TreePop()
			}
			if imgui.TreeNode("africa") {
				imgui.TreePop()
			}
			imgui.TreePop()
		}
		imgui.TreePop()
	}
}

func renderCollapsingHeader() {
	if imgui.CollapsingHeaderV("header 1", 0) {
		imgui.TextUnformatted("content 1")
	}
	if imgui.CollapsingHeaderV("header 2", 0) {
		imgui.TextUnformatted("content 2")
	}
}

var toggle1 bool

var toggle2 bool

func RenderToggle() {
	toggle1, _ = imgui.Toggle("toggler", toggle1)
	toggle2, _ = imgui.ToggleV("toggler 2", toggle2, imgui.ImGuiToggleFlags_ShadowedFrame, 3.0, 0.33, 0.33, 0)
}

var knob = make([]float32, 8, 8)

var knobVariants = []imgui.ImGuiKnobVariant{imgui.ImGuiKnobVariant_Tick,
	imgui.ImGuiKnobVariant_Dot,
	imgui.ImGuiKnobVariant_Wiper,
	imgui.ImGuiKnobVariant_WiperOnly,
	imgui.ImGuiKnobVariant_WiperDot,
	imgui.ImGuiKnobVariant_Stepped,
	imgui.ImGuiKnobVariant_Space}

var knobLabels = []string{
	"my knob tick",
	"my knob dot",
	"my knob wiper",
	"my knob wiper only",
	"my knob wiper dot",
	"my knob wiper stepped",
	"my knob wiper space",
}

func RenderKnobs() {
	imgui.BeginGroup()
	for i, v := range knobVariants {
		knob[i], _ = imgui.KnobV(knobLabels[i], knob[i], 0.0, 100.0, 0.0, "%.3f", v, 0, 0, 10)
		imgui.SameLine()
	}
	imgui.EndGroup()
}

func MakeRenderBasicWidgets() func() {
	inputText := MakeRenderInputText()
	checkboxRenderer := MakeRenderCheckbox()
	return func() {
		imgui.SeparatorText("input text")
		inputText()

		imgui.SeparatorText("general")
		renderButton()
		renderTooltip()

		imgui.SeparatorText("combo box")
		renderComboBox()

		imgui.SeparatorText("tree")
		renderTree()

		imgui.SeparatorText("collapsing header")
		renderCollapsingHeader()

		imgui.SeparatorText("table")
		RenderSimpleTable()

		imgui.SeparatorText("toggle")
		RenderToggle()

		imgui.SeparatorText("knob")
		RenderKnobs()

		imgui.SeparatorText("checkbox")
		checkboxRenderer()
	}
}

func MakeRenderInputText() func() {
	text := ""
	text2 := ""
	return func() {
		text, _ = imgui.InputText("a label", text, 32)
		imgui.TextUnformatted(text)

		text2, _ = imgui.InputTextWithHint("another label", "a hint", text2, 32)
		imgui.TextUnformatted(text2)
	}
}

func MakeRenderCheckbox() func() {
	checkboxState := logical.TriNil
	return func() {
		checkboxState, _ = imgui.Checkbox("click me if you can", checkboxState)
		imgui.SameLine()
		if imgui.Button("reset to nil") {
			checkboxState = logical.TriNil
		}
	}
}
