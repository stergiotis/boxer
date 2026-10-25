package widgets

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/mdblocks"
	"strconv"
)

func init() {
	registry.Register(registry.Demo{
		Name:     "mdblocks",
		Category: "Layout & widgets",
		Title:    icons.PhTextAa + " markdown blocks",
		Stage:    [2]float32{900, 700},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "widgets/mdblocks (ADR-0334): a markdown document edited as a list of blocks over its own source. " +
			"Click a block to edit it as text; Enter at the end of a paragraph starts a new block, Enter in a list continues it, " +
			"Backspace at a block's start removes its marker and then joins it to the block above, Escape selects the block, " +
			"Ctrl+Z and Ctrl+Y walk the document's own undo. The readout under the editor is the source as it stands.",
		Init: func(_ *c.WidgetIdStack) (state any) {
			state = &mdblocksDemoState{}
			return
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoMdblocks(ids, state.(*mdblocksDemoState))
		},
		SourceFunc: demoMdblocks,
	})
}

const mdblocksSample = `# A document in blocks

Every paragraph, heading and list the parse finds is a block. Click one to
edit it; the others stay rendered.

- a list is one block
- Enter continues it
- Enter on an empty item leaves it

> A quote, with **bold** and ` + "`code`" + ` inside.

` + "```go\nfunc main() {}\n```" + `

| a | b |
| - | - |
| 1 | 2 |

The last paragraph.
`

type mdblocksDemoState struct {
	ed      *mdblocks.Editor
	changes int
	copied  string
}

// demoMdblocks hosts the editor over a fixed document and shows, under it,
// what a scene can assert on: the change count, the caret, and the source.
func demoMdblocks(ids *c.WidgetIdStack, st *mdblocksDemoState) {
	if st.ed == nil {
		// Built on the first frame: the registry's Init has no id stack.
		st.ed = mdblocks.New(ids, "mdblocks-demo", mdblocks.Opts{})
		st.ed.SetSource(mdblocksSample)
	}
	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("reset"), c.Atoms().Text("Reset").Keep()).SendResp().HasPrimaryClicked() {
			st.ed.SetSource(mdblocksSample)
			st.changes = 0
		}
		if c.Button(ids.PrepareStr("undo"), c.Atoms().Text("Undo").Keep()).SendResp().HasPrimaryClicked() {
			st.ed.Undo()
		}
		if c.Button(ids.PrepareStr("redo"), c.Atoms().Text("Redo").Keep()).SendResp().HasPrimaryClicked() {
			st.ed.Redo()
		}
	}
	var ev mdblocks.Events
	for range c.ScrollArea().Hscroll(true).Vscroll(true).AutoShrink(false, false).MaxHeight(420).KeepIter() {
		ev = st.ed.Render()
	}
	if ev.Changed {
		st.changes++
	}
	if ev.CopyRequest != "" {
		st.copied = ev.CopyRequest
	}
	c.Separator().Send()
	undo := "no"
	if st.ed.CanUndo() {
		undo = "yes"
	}
	c.Label("changes: " + strconv.Itoa(st.changes) + ", caret: " + strconv.Itoa(ev.Caret) + ", undo: " + undo).Selectable(false).Send()
	c.Label(st.ed.Source()).Selectable(true).Send()
}
