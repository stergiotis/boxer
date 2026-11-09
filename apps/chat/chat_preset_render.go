package chat

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// renderPresetEmpty is the head of a preset's empty transcript: its name,
// its greeting and its starters.
func (inst *App) renderPresetEmpty() {
	p := inst.preset
	for rt := range c.RichTextLabel(p.Manifest.Display) {
		rt.Heading()
	}
	if p.Greeting != "" {
		c.LabelAtoms(c.Atoms().Text(p.Greeting).Keep()).Wrap().Selectable(true).Send()
	}
	if len(p.Starters) > 0 {
		c.AddSpace(8)
		for _, s := range p.Starters {
			for range c.HoverText(s.Text).KeepIter() {
				if c.Button(inst.ids.PrepareStr("starter-"+s.Label), c.Atoms().Text(s.Label).Keep()).SendResp().HasPrimaryClicked() {
					inst.fillDraft(s.Text)
				}
			}
		}
		c.AddSpace(8)
	}
}

// tipDefinition says what the tab is, above the document.
const tipDefinition = "The document this window was made from: its body is what the model is told, and its properties are the settings it runs under."

// renderDefinitionTab is the preset's document, with its frontmatter last,
// where the helper is designed to sit.
func (inst *App) renderDefinitionTab() {
	weak(tipDefinition)
	for range c.IdScope(inst.ids.PrepareStr("definition-body")) {
		markdown.Render(markdown.Input{Ids: inst.ids, ScopeKey: "definition-doc", Doc: inst.definition})
		markdown.RenderFrontmatter(inst.definition)
	}
}
