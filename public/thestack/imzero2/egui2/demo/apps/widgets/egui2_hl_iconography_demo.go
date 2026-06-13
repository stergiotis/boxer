package widgets

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

func init() {
	registry.Register(registry.Demo{
		Name:        "iconography",
		Category:    "Design system",
		Title:       icons.IconPalette + " iconography",
		Stage:       [2]float32{1024, 600},
		Kind:        registry.DemoKindMixed,
		Description: "ADR-0044 iconography: Phosphor regular as the IDS icon font. Sections cover inline labels, icon buttons, and styled rich-text status icons. Brand / language marks were retired in the Slot B removal amendment — apps reach for plain text or Phosphor's own selective brand-mark coverage (Linux, GitHub, Apple, …).",
		Render:      func(ids *c.WidgetIdStack) { demoIconography(ids) },
		SourceFunc:  demoIconography,
	})
}

func demoIconography(ids *c.WidgetIdStack) {
	for range c.CollapsingHeader(ids.PrepareStr("ic-inline"), c.WidgetText().Text("inline icon usage").Keep()).DefaultOpen(true).KeepIter() {
		c.Label(icons.IconGear + " Settings").Send()
		c.Label(icons.IconFolderOpen + " Open project").Send()
		c.Label(icons.IconSave + " Save file").Send()
		c.Label(icons.IconSearch + " Search").Send()
		c.Label(icons.IconCheck + " All checks passed").Send()
		c.Label(icons.IconWarning + " Warning: disk usage high").Send()
		c.Label(icons.IconError + " Build failed").Send()
	}

	for range c.CollapsingHeader(ids.PrepareStr("ic-buttons"), c.WidgetText().Text("icon buttons").Keep()).DefaultOpen(true).KeepIter() {
		if c.Button(ids.PrepareStr("ic-btn-play"), c.Atoms().Text(icons.IconPlay+" Play").Keep()).SendResp().HasPrimaryClicked() {
		}
		if c.Button(ids.PrepareStr("ic-btn-pause"), c.Atoms().Text(icons.IconPause+" Pause").Keep()).SendResp().HasPrimaryClicked() {
		}
		if c.Button(ids.PrepareStr("ic-btn-stop"), c.Atoms().Text(icons.IconStop+" Stop").Keep()).SendResp().HasPrimaryClicked() {
		}
	}

	for range c.CollapsingHeader(ids.PrepareStr("ic-richtext"), c.WidgetText().Text("styled icons").Keep()).DefaultOpen(true).KeepIter() {
		green := color.Hex(styletokens.SuccessDefault.AsHex()).Keep()
		red := color.Hex(styletokens.ErrorDefault.AsHex()).Keep()
		yellow := color.Hex(styletokens.WarningDefault.AsHex()).Keep()
		bg := color.Transparent.Keep()

		atoms := c.Atoms()
		for rt := range atoms.StyledTextColored(green, bg, icons.IconCheck+" pass ") {
			_ = rt
		}
		for rt := range atoms.StyledTextColored(red, bg, icons.IconError+" fail ") {
			_ = rt
		}
		for rt := range atoms.StyledTextColored(yellow, bg, icons.IconWarning+" warn") {
			_ = rt
		}
		c.LabelAtoms(atoms.Keep()).Send()
	}

}
