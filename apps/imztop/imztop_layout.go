package imztop

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// sectionHeader draws a panel-internal heading row: bold title with a
// modest size bump, a thin horizontal rule, and a small bottom margin.
// Per-panel CollapsingHeader was dropped because egui's panel/scroll
// surfaces already give each section a clear region; the toggle only
// added noise.
func (inst *App) sectionHeader(title string) {
	for rt := range c.RichTextLabel(title) {
		rt.Strong().Size(15)
	}
	c.AddSpace(inst.spaceHair())
	c.Separator().Horizontal().Send()
	c.AddSpace(inst.spaceTight())
}
