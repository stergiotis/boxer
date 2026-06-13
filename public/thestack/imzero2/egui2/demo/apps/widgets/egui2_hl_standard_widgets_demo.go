package widgets

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// =============================================================================
// Standard egui widgets — UX showcase + DX example.
//
// Each section is one widget kind with multiple variants and a state readout
// at the bottom. Sections are wrapped in CollapsingHeaders so the catalog is
// browsable and only what the user opens pays the render cost.
//
// State lives in standardWidgetsDemoState — every counter, slider value,
// checkbox flag and text-edit buffer is per-window so two open gallery
// instances each have their own working state. Constants (the fruit-name
// menu) stay package-level since they are not mutated by the demo.
// =============================================================================

// standardWidgetsDemoState bundles every Slider/DragValue/TextEdit/
// Checkbox/Radio binding the demo writes back into via SendRespVal.
// The struct is heap-allocated once in Init so the &st.X pointers
// handed to SendRespVal/OverrideDatabindingBPtr stay stable for the
// lifetime of the gallery window.
type standardWidgetsDemoState struct {
	// Buttons
	counter int

	// Selection (Checkbox / Radio / SelectableLabel)
	checkBasic  bool
	checkIndet  bool
	radioChoice uint8
	radioTheme  uint8
	selectables [3]bool

	// Sliders
	sliderF     float64
	sliderInt   float64 // SendRespVal only on F64; format with .Integer()
	sliderLog   float64
	sliderVert  float64
	sliderFmt   float64
	sliderTrail float64
	sliderHex   float64

	// DragValue
	dragF float64
	dragU uint64

	// TextEdit
	textSingle    string
	textHint      string
	textLimited   string
	textPassword  string
	textReadonly  string
	textMultiline string
	textCode      string

	// ComboBox
	comboFruit  int
	comboNarrow int
}

// swFruitNames is the (immutable) option list for the fruit ComboBox.
// Kept package-level because it's a constant the readout also reads
// against st.comboFruit.
var swFruitNames = []string{"apple", "banana", "cherry", "dragonfruit", "elderberry", "fig"}

func init() {
	registry.Register(registry.Demo{
		Name:        "standard-widgets",
		Category:    "Layout & widgets",
		Title:       "standard widgets",
		Stage:       [2]float32{1024, 800},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindMixed,
		Description: "Catalog of standard egui widgets (Button, Label, Checkbox, RadioButton, SelectableLabel, Slider, DragValue, TextEdit, ComboBox) with variants, event handling and a live state readout.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &standardWidgetsDemoState{
				radioChoice:   1,
				selectables:   [3]bool{true, false, false},
				sliderF:       50.0,
				sliderInt:     7.0,
				sliderLog:     1.0,
				sliderVert:    0.4,
				sliderFmt:     0.5,
				sliderTrail:   0.7,
				sliderHex:     float64(0xCAFE),
				dragU:         100,
				textReadonly:  "this field is read-only",
				textMultiline: "first line\nsecond line\nthird line",
				textCode:      "fn main() {\n    println!(\"hello, imzero2\");\n}",
			}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoStandardWidgets(ids, state.(*standardWidgetsDemoState))
		},
		SourceFunc: demoStandardWidgets,
	})
}

// stdSection emits the standard "Separator + bold title + caption + small
// spacer" block shared by every widget section in this file. Mirrors the
// badgeSection helper without coupling the two demos.
func stdSection(title, caption string) {
	c.Separator().Send()
	for rt := range c.RichTextLabel(title) {
		rt.Strong()
	}
	if caption != "" {
		for rt := range c.RichTextLabel(caption) {
			rt.Italics()
		}
	}
	c.AddSpace(padHair())
}

// -----------------------------------------------------------------------------
// Top-level demo — one CollapsingHeader per widget kind.
// -----------------------------------------------------------------------------

func demoStandardWidgets(ids *c.WidgetIdStack, st *standardWidgetsDemoState) {
	for range c.CollapsingHeader(ids.PrepareStr("sw-buttons"),
		c.WidgetText().Text("Button — variants and event handling").Keep()).
		KeepIter() {
		swButtonsSection(ids, st)
	}
	for range c.CollapsingHeader(ids.PrepareStr("sw-labels"),
		c.WidgetText().Text("Label — text styling and wrap behaviour").Keep()).
		KeepIter() {
		swLabelsSection(ids)
	}
	for range c.CollapsingHeader(ids.PrepareStr("sw-selection"),
		c.WidgetText().Text("Checkbox / Radio / SelectableLabel").Keep()).
		DefaultOpen(true).KeepIter() {
		swSelectionSection(ids, st)
	}
	for range c.CollapsingHeader(ids.PrepareStr("sw-sliders"),
		c.WidgetText().Text("Slider — range, log, formatted, vertical").Keep()).
		KeepIter() {
		swSlidersSection(ids, st)
	}
	for range c.CollapsingHeader(ids.PrepareStr("sw-dragvalue"),
		c.WidgetText().Text("DragValue — drag-or-type numeric input").Keep()).
		KeepIter() {
		swDragValueSection(ids, st)
	}
	for range c.CollapsingHeader(ids.PrepareStr("sw-textedit"),
		c.WidgetText().Text("TextEdit — single, multiline, hint, password, code").Keep()).
		KeepIter() {
		swTextEditSection(ids, st)
	}
	for range c.CollapsingHeader(ids.PrepareStr("sw-combobox"),
		c.WidgetText().Text("ComboBox — dropdown selection").Keep()).
		KeepIter() {
		swComboBoxSection(ids, st)
	}
	for range c.CollapsingHeader(ids.PrepareStr("sw-readout"),
		c.WidgetText().Text("Live state readout").Keep()).
		KeepIter() {
		swStateReadoutSection(ids, st)
	}
}

// -----------------------------------------------------------------------------
// Button section
// -----------------------------------------------------------------------------

func swButtonsSection(ids *c.WidgetIdStack, st *standardWidgetsDemoState) {
	stdSection("event handling",
		"left click increments, right click decrements")
	for range c.Horizontal().KeepIter() {
		r := c.Button(ids.PrepareStr("btn-counter"),
			c.Atoms().Text("counter").Keep()).SendResp()
		if r.HasPrimaryClicked() {
			st.counter++
		} else if r.HasSecondaryClicked() {
			st.counter--
		}
		c.Label(fmt.Sprintf("count = %d", st.counter)).Send()
	}

	stdSection("frame and size variants",
		"Frame(false) gives a clickable label; Small() compacts to caption height")
	for range c.Horizontal().KeepIter() {
		c.Button(ids.PrepareStr("btn-default"),
			c.Atoms().Text("default").Keep()).Send()
		c.Button(ids.PrepareStr("btn-noframe"),
			c.Atoms().Text("Frame(false)").Keep()).Frame(false).Send()
		c.Button(ids.PrepareStr("btn-small"),
			c.Atoms().Text("Small()").Keep()).Small().Send()
		c.Button(ids.PrepareStr("btn-selected"),
			c.Atoms().Text("Selected(true)").Keep()).Selected(true).Send()
	}

	stdSection("atoms — leading icons and trailing slots",
		"Atoms() composes glyphs and rich text; RightText / ShortcutText sit on the right edge")
	for range c.Horizontal().KeepIter() {
		c.Button(ids.PrepareStr("btn-icon-save"),
			c.Atoms().Text(icons.IconSave+"  save").Keep()).Send()
		c.Button(ids.PrepareStr("btn-icon-play"),
			c.Atoms().Text(icons.IconPlay+"  run").Keep()).Send()
		c.Button(ids.PrepareStr("btn-icon-search"),
			c.Atoms().Text(icons.IconSearch+"  search").Keep()).Send()
	}
	for range c.Horizontal().KeepIter() {
		c.Button(ids.PrepareStr("btn-rt"),
			c.Atoms().Text("open").Keep()).RightText("→").Send()
		c.Button(ids.PrepareStr("btn-shortcut-save"),
			c.Atoms().Text(icons.IconSave+"  save").Keep()).
			ShortcutText("Ctrl+S").Send()
		c.Button(ids.PrepareStr("btn-shortcut-find"),
			c.Atoms().Text(icons.IconSearch+"  find…").Keep()).
			ShortcutText("Ctrl+F").Send()
	}

	stdSection("long label — wrap vs truncate",
		"max width is constrained for both buttons so the difference is visible")
	for range c.Vertical().KeepIter() {
		c.UiSetMaxWidth(220)
		c.Button(ids.PrepareStr("btn-wrap"),
			c.Atoms().Text("a button with a deliberately long label that wraps").Keep()).
			Wrap().Send()
		c.AddSpace(padInner())
		c.Button(ids.PrepareStr("btn-trunc"),
			c.Atoms().Text("a button with a deliberately long label that truncates").Keep()).
			Truncate().Send()
	}
}

// -----------------------------------------------------------------------------
// Label section — no per-window state, so swLabelsSection keeps the simple
// ids-only signature.
// -----------------------------------------------------------------------------

func swLabelsSection(ids *c.WidgetIdStack) {
	stdSection("text styles via RichTextLabel",
		"Heading / Strong / Weak / Italics / Underline / Strikethrough / Code / Monospace / Small / Size")
	for rt := range c.RichTextLabel("Heading") {
		rt.Heading()
	}
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel("Strong") {
			rt.Strong()
		}
		for rt := range c.RichTextLabel("Weak") {
			rt.Weak()
		}
		for rt := range c.RichTextLabel("Italic") {
			rt.Italics()
		}
		for rt := range c.RichTextLabel("Underline") {
			rt.Underline()
		}
		for rt := range c.RichTextLabel("Strike") {
			rt.Strikethrough()
		}
	}
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel("inline code") {
			rt.Code()
		}
		for rt := range c.RichTextLabel("monospace") {
			rt.Monospace()
		}
		for rt := range c.RichTextLabel("small") {
			rt.Small()
		}
		for rt := range c.RichTextLabel("size 20") {
			rt.Size(20)
		}
	}

	stdSection("colored text",
		"RichTextLabelColored emits a single coloured label; transparent bg keeps the row baseline")
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabelColored(
			color.Hex(styletokens.SuccessDefault.AsHex()).Keep(),
			color.Transparent.Keep(),
			"success") {
			rt.Strong()
		}
		for rt := range c.RichTextLabelColored(
			color.Hex(styletokens.WarningDefault.AsHex()).Keep(),
			color.Transparent.Keep(),
			"warning") {
			rt.Strong()
		}
		for rt := range c.RichTextLabelColored(
			color.Hex(styletokens.ErrorDefault.AsHex()).Keep(),
			color.Transparent.Keep(),
			"error") {
			rt.Strong()
		}
	}

	stdSection("selectable",
		"plain Label is selectable by default; Selectable(false) opts out")
	c.Label("Default Label — drag to select me").Send()
	c.Label("This Label has Selectable(false)").Selectable(false).Send()

	stdSection("wrap / truncate / extend",
		"all three labels live in a 220px-wide Ui so the long string forces a layout decision")
	for range c.Vertical().KeepIter() {
		c.UiSetMaxWidth(220)
		c.Label("Default — egui's default wrap mode for the layout").Send()
		c.AddSpace(padHair())
		c.Label("Wrap() — explicit wrap, breaks at word boundaries when the line overflows").Wrap().Send()
		c.AddSpace(padHair())
		c.Label("Truncate() — clipped with an ellipsis when the line overflows").Truncate().Send()
		c.AddSpace(padHair())
		c.Label("Extend() — single line, may overflow the parent rect").Extend().Send()
	}
}

// -----------------------------------------------------------------------------
// Selection section — Checkbox, Radio (workaround), SelectableLabel
// -----------------------------------------------------------------------------

func swSelectionSection(ids *c.WidgetIdStack, st *standardWidgetsDemoState) {
	stateMgr := c.CurrentApplicationState.StateManager

	stdSection("Checkbox — basic",
		"SendRespVal writes back next frame; HasChanged() is the change edge")
	if c.Checkbox(ids.PrepareStr("ck-basic"), st.checkBasic, "enable feature").
		SendRespVal(&st.checkBasic).HasChanged() {
		// edge — useful for triggering side effects
	}

	stdSection("Checkbox — indeterminate",
		"third visual state independent of true/false; toggle independently")
	if c.Checkbox(ids.PrepareStr("ck-indet"), st.checkIndet, "indeterminate visual").
		Indeterminate(true).SendRespVal(&st.checkIndet).HasChanged() {
	}

	stdSection("override the binding",
		"OverrideDatabindingBPtr forces Rust state to match Go — needed when external code mutates the var between frames")
	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("ck-set-true"),
			c.Atoms().Text("set basic = true").Keep()).SendResp().HasPrimaryClicked() {
			st.checkBasic = true
			stateMgr.OverrideDatabindingBPtr(&st.checkBasic)
		}
		if c.Button(ids.PrepareStr("ck-set-false"),
			c.Atoms().Text("set basic = false").Keep()).SendResp().HasPrimaryClicked() {
			st.checkBasic = false
			stateMgr.OverrideDatabindingBPtr(&st.checkBasic)
		}
	}

	stdSection("RadioButton — exclusive selection group",
		"an integer tracks the winner; each radio's checked visual derives from `st.radioChoice == i`. egui's RadioButton doesn't own state (it never calls mark_changed), so the Go side detects the click via HasPrimaryClicked() rather than HasChanged() — that's the apply-side gate per ADR-0013")
	for range c.Horizontal().KeepIter() {
		for i := uint8(1); i <= 3; i++ {
			var clicked bool
			if c.RadioButton(ids.PrepareSeq(uint64(0xab0001)+uint64(i)),
				c.Atoms().Text(fmt.Sprintf("option %d", i)).Keep(),
				st.radioChoice == i).
				SendRespVal(&clicked).HasPrimaryClicked() {
				st.radioChoice = i
			}
		}
	}

	stdSection("RadioButton — vertical settings-style group",
		"same pattern, vertical layout; labels can carry icons or descriptions via Atoms")
	for range c.Vertical().KeepIter() {
		themes := []struct {
			label string
			icon  string
		}{
			{"System default", icons.IconSliders},
			{"Light", icons.IconLightning},
			{"Dark", icons.IconColorMode},
		}
		for i, t := range themes {
			var clicked bool
			if c.RadioButton(ids.PrepareSeq(uint64(0xab1000)+uint64(i)),
				c.Atoms().Text(t.icon+"  "+t.label).Keep(),
				st.radioTheme == uint8(i)).
				SendRespVal(&clicked).HasPrimaryClicked() {
				st.radioTheme = uint8(i)
			}
		}
	}

	stdSection("SelectableLabel — toggle without a checkbox glyph",
		"useful for filter chips, tag pickers, multi-select lists")
	for range c.Horizontal().KeepIter() {
		labels := []string{"alpha", "beta", "gamma"}
		for i, name := range labels {
			if c.SelectableLabel(ids.PrepareSeq(uint64(0x5e1ec1+uint64(i))),
				st.selectables[i], name).
				SendResp().HasPrimaryClicked() {
				st.selectables[i] = !st.selectables[i]
			}
		}
	}
}

// -----------------------------------------------------------------------------
// Slider section
// -----------------------------------------------------------------------------

func swSlidersSection(ids *c.WidgetIdStack, st *standardWidgetsDemoState) {
	stdSection("float slider, 0..100",
		"Text() places a label to the right of the track")
	c.SliderF64(ids.PrepareStr("sl-f"), st.sliderF, 0.0, 100.0).
		Text("temperature").SendRespVal(&st.sliderF)

	stdSection("integer slider — Integer() format",
		"SendRespVal only exists on the F64 slider; .Integer() snaps and renders without decimals")
	c.SliderF64(ids.PrepareStr("sl-int"), st.sliderInt, 0.0, 16.0).
		Integer().Text("count").SendRespVal(&st.sliderInt)

	stdSection("logarithmic slider, 1e-3..1e3",
		"useful for orders-of-magnitude inputs (frequencies, gains, sample sizes)")
	c.SliderF64(ids.PrepareStr("sl-log"), st.sliderLog, 0.001, 1000.0).
		Logarithmic(true).MaxDecimals(3).Text("ratio").
		SendRespVal(&st.sliderLog)

	stdSection("vertical slider",
		"Vertical() rotates the track 90°; useful for mixing-board UIs")
	for range c.Horizontal().KeepIter() {
		c.SliderF64(ids.PrepareStr("sl-vert"), st.sliderVert, 0.0, 1.0).
			Vertical().FixedDecimals(2).
			SendRespVal(&st.sliderVert)
		c.AddSpace(gapItems())
		c.Label("← drag the vertical track").Send()
	}

	stdSection("formatting — prefix, suffix, fixed decimals, trailing fill",
		"prefix/suffix render inline; TrailingFill highlights the consumed range")
	c.SliderF64(ids.PrepareStr("sl-fmt"), st.sliderFmt, 0.0, 1.0).
		Prefix("$").Suffix("/unit").FixedDecimals(2).
		Text("price").SendRespVal(&st.sliderFmt)
	c.SliderF64(ids.PrepareStr("sl-trail"), st.sliderTrail, 0.0, 1.0).
		TrailingFill(true).FixedDecimals(2).Text("progress").
		SendRespVal(&st.sliderTrail)

	stdSection("hexadecimal slider",
		"Hexadecimal(minWidth, twosComplement, upper) formats the value as fixed-width hex")
	c.SliderF64(ids.PrepareStr("sl-hex"), st.sliderHex, 0.0, 65535.0).
		Hexadecimal(4, false, true).Text("address").
		SendRespVal(&st.sliderHex)
}

// -----------------------------------------------------------------------------
// DragValue section
// -----------------------------------------------------------------------------

func swDragValueSection(ids *c.WidgetIdStack, st *standardWidgetsDemoState) {
	stdSection("DragValue — float",
		"drag horizontally to scrub, double-click to type a value; Speed sets units-per-pixel")
	for range c.Horizontal().KeepIter() {
		c.DragValueF64(ids.PrepareStr("dv-f"), st.dragF).
			Speed(0.1).FixedDecimals(2).
			SendRespVal(&st.dragF)
		c.Label(fmt.Sprintf("= %.2f", st.dragF)).Send()
	}

	stdSection("DragValue — unsigned integer with prefix/suffix",
		"DragValueU64 covers integer use-cases; Prefix/Suffix wrap the rendered value")
	for range c.Horizontal().KeepIter() {
		c.DragValueU64(ids.PrepareStr("dv-u"), st.dragU).
			Speed(1.0).Prefix("⌀ ").Suffix(" px").
			SendRespVal(&st.dragU)
		c.Label(fmt.Sprintf("= %d", st.dragU)).Send()
	}

	stdSection("DragValue — hex format on float backend",
		"Hexadecimal(minWidth, twosComplement, upper) — same formatter family as Slider")
	c.DragValueF64(ids.PrepareStr("dv-hex"), st.dragF).
		Hexadecimal(4, false, true).Speed(1.0).
		SendRespVal(&st.dragF)
}

// -----------------------------------------------------------------------------
// TextEdit section
// -----------------------------------------------------------------------------

func swTextEditSection(ids *c.WidgetIdStack, st *standardWidgetsDemoState) {
	stdSection("single-line",
		"smallest TextEdit form; SendRespVal binds directly to a *string")
	c.TextEdit(ids.PrepareStr("te-single"), st.textSingle, false).
		DesiredWidth(280).SendRespVal(&st.textSingle)

	stdSection("with hint text (placeholder)",
		"HintText shows when the field is empty and unfocused")
	c.TextEdit(ids.PrepareStr("te-hint"), st.textHint, false).
		HintText("type a search term…").DesiredWidth(280).
		SendRespVal(&st.textHint)

	stdSection("character limit",
		"CharLimit truncates further input once the cap is reached")
	for range c.Horizontal().KeepIter() {
		c.TextEdit(ids.PrepareStr("te-limit"), st.textLimited, false).
			CharLimit(20).HintText("max 20 chars").DesiredWidth(220).
			SendRespVal(&st.textLimited)
		c.Label(fmt.Sprintf("%d/20", len(st.textLimited))).Send()
	}

	stdSection("password mode",
		"Password(true) renders dots regardless of contents; binding still gets the plain value")
	c.TextEdit(ids.PrepareStr("te-pw"), st.textPassword, false).
		Password(true).HintText("password").DesiredWidth(220).
		SendRespVal(&st.textPassword)

	stdSection("read-only",
		"Interactive(false) disables editing while keeping selection / copy")
	c.TextEdit(ids.PrepareStr("te-ro"), st.textReadonly, false).
		Interactive(false).DesiredWidth(280).
		SendRespVal(&st.textReadonly)

	stdSection("multiline",
		"second factory arg = true; DesiredRows hints at initial height")
	c.TextEdit(ids.PrepareStr("te-multi"), st.textMultiline, true).
		DesiredRows(4).DesiredWidth(420).
		SendRespVal(&st.textMultiline)

	stdSection("code editor",
		"CodeEditor() flips on monospace + lock-focus + tabs-as-spaces")
	c.TextEdit(ids.PrepareStr("te-code"), st.textCode, true).
		CodeEditor().DesiredRows(5).DesiredWidth(420).
		SendRespVal(&st.textCode)
}

// -----------------------------------------------------------------------------
// ComboBox section
// -----------------------------------------------------------------------------

func swComboBoxSection(ids *c.WidgetIdStack, st *standardWidgetsDemoState) {
	stdSection("ComboBox — bound to a slice index",
		"options are emitted as Buttons inside the dropdown; Selected() highlights the current pick")
	current := "(none)"
	if st.comboFruit >= 0 && st.comboFruit < len(swFruitNames) {
		current = swFruitNames[st.comboFruit]
	}
	for range c.ComboBox(ids.PrepareStr("cb-fruit"),
		c.WidgetText().Text("fruit").Keep(),
		c.WidgetText().Text(current).Keep()).KeepIter() {
		for i, name := range swFruitNames {
			selected := i == st.comboFruit
			if c.Button(ids.PrepareSeq(uint64(0xc0ffee+uint64(i))),
				c.Atoms().Text(name).Keep()).
				Selected(selected).
				FrameWhenInactive(!selected).
				Frame(true).
				SendResp().HasPrimaryClicked() {
				st.comboFruit = i
			}
		}
	}

	stdSection("ComboBox — narrow with truncate",
		"Width() caps the closed widget; Truncate() keeps a long label readable")
	narrowOptions := []string{
		"a really really long option label one",
		"medium label two",
		"short three",
	}
	current2 := narrowOptions[st.comboNarrow]
	for range c.ComboBox(ids.PrepareStr("cb-narrow"),
		c.WidgetText().Text("narrow").Keep(),
		c.WidgetText().Text(current2).Keep()).
		Width(180).Truncate().KeepIter() {
		for i, name := range narrowOptions {
			selected := i == st.comboNarrow
			if c.Button(ids.PrepareSeq(uint64(0xbeef00+uint64(i))),
				c.Atoms().Text(name).Keep()).
				Selected(selected).
				FrameWhenInactive(!selected).
				SendResp().HasPrimaryClicked() {
				st.comboNarrow = i
			}
		}
	}
}

// -----------------------------------------------------------------------------
// Live state readout — mirrors every bound var so the user can see the
// databinding round-trip working as they interact with the sections above.
// -----------------------------------------------------------------------------

func swStateReadoutSection(ids *c.WidgetIdStack, st *standardWidgetsDemoState) {
	stdSection("bound values",
		"this panel reads the same state fields the widgets above write to")
	for range c.Grid(ids.PrepareStr("sw-state-grid")).NumColumns(2).KeepIter() {
		swReadoutRow("counter", fmt.Sprintf("%d", st.counter))
		swReadoutRow("checkbox basic", fmt.Sprintf("%v", st.checkBasic))
		swReadoutRow("checkbox indeterminate", fmt.Sprintf("%v", st.checkIndet))
		swReadoutRow("radio choice", fmt.Sprintf("%d", st.radioChoice))
		themeNames := [3]string{"system", "light", "dark"}
		swReadoutRow("radio theme", themeNames[st.radioTheme])
		swReadoutRow("selectables", fmt.Sprintf("%v", st.selectables))
		swReadoutRow("slider float", fmt.Sprintf("%.3f", st.sliderF))
		swReadoutRow("slider integer", fmt.Sprintf("%.0f", st.sliderInt))
		swReadoutRow("slider log", fmt.Sprintf("%.4f", st.sliderLog))
		swReadoutRow("slider vertical", fmt.Sprintf("%.2f", st.sliderVert))
		swReadoutRow("slider formatted", fmt.Sprintf("%.2f", st.sliderFmt))
		swReadoutRow("slider trailing", fmt.Sprintf("%.2f", st.sliderTrail))
		swReadoutRow("slider hex", fmt.Sprintf("0x%04X", uint32(st.sliderHex)))
		swReadoutRow("drag float", fmt.Sprintf("%.2f", st.dragF))
		swReadoutRow("drag uint", fmt.Sprintf("%d", st.dragU))
		swReadoutRow("textedit single", fmt.Sprintf("%q", st.textSingle))
		swReadoutRow("textedit hint", fmt.Sprintf("%q", st.textHint))
		swReadoutRow("textedit limited", fmt.Sprintf("%q (%d)", st.textLimited, len(st.textLimited)))
		swReadoutRow("textedit password", fmt.Sprintf("%d chars", len(st.textPassword)))
		swReadoutRow("textedit multiline", fmt.Sprintf("%d bytes", len(st.textMultiline)))
		swReadoutRow("combo fruit", fmt.Sprintf("[%d] %s", st.comboFruit, swFruitNames[st.comboFruit]))
		swReadoutRow("combo narrow", fmt.Sprintf("[%d]", st.comboNarrow))
	}
}

func swReadoutRow(name, value string) {
	for rt := range c.RichTextLabel(name) {
		rt.Weak()
	}
	for rt := range c.RichTextLabel(value) {
		rt.Monospace()
	}
	c.EndRow()
}
