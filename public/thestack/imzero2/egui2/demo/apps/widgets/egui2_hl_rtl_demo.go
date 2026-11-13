package widgets

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

// rtlProbes are the right-to-left strings the demo holds still, in logical
// (typed) order. Each isolates one thing egui has to get right: a lone RTL
// word, RTL with European digits, RTL then Latin, and Latin with an embedded
// RTL word. The scene in scenes/rtl-text.scene.md measures what egui does
// with them; nothing here works around it.
var rtlProbes = []string{
	"مرحبا",
	"عدد 123 مرحبا",
	"مرحبا World",
	"Hello مرحبا world",
}

// rtlEditSeed is the text the edit field starts from and resets to. It is
// the second probe, the string emilk/egui#8576 measures the caret with.
const rtlEditSeed = "عدد 123 مرحبا"

type rtlTextState struct {
	edit string
}

func init() {
	registry.Register(registry.Demo{
		Name:        "rtl-text",
		Category:    "Text & code",
		Title:       icons.IconGlobe + " right-to-left text (probe)",
		Stage:       [2]float32{1024, 500},
		Kind:        registry.DemoKindUX,
		Description: "Arabic strings as egui lays them out, in a label and in an editable field, with no workaround. The labels show word order across direction changes; the field shows where the caret goes. Needs a face with Arabic glyphs in the font chain. scenes/rtl-text.scene.md measures both.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &rtlTextState{edit: rtlEditSeed}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoRtlText(ids, state.(*rtlTextState))
		},
		SourceFunc: demoRtlText,
	})
}

func demoRtlText(ids *c.WidgetIdStack, st *rtlTextState) {
	stdSection("labels", "each line is one label, in logical order as typed")
	for _, s := range rtlProbes {
		for rt := range c.RichTextLabel(s) {
			rt.Size(26)
		}
	}

	stdSection("editable", "single-line TextEdit seeded with the second probe")
	for range c.Horizontal().KeepIter() {
		c.TextEdit(ids.PrepareStr("rtl-edit"), st.edit, false).
			DesiredWidth(360).SendRespVal(&st.edit)
		if c.Button(ids.PrepareStr("rtl-reset"), c.Atoms().Text("reset").Keep()).SendResp().HasPrimaryClicked() {
			st.edit = rtlEditSeed
		}
	}
}
