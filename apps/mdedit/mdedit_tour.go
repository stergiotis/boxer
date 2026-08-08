package mdedit

// mdedit_tour.go enrols one synthetic scene into the imzero2 demo registry
// (ADR-0057) so the central TestDriver captures the editor in the widgets
// tour. The live App is a windowed AppI; the tour renders the same two panes
// on a fixed starting document so the scene is deterministic and
// network-free. Screenshot scaffolding only.

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
)

const (
	// gallerySrcW pins the source column in the demo scene. The live app uses
	// a resizable side panel instead; see renderGallery for why the two hosts
	// cannot share a layout.
	gallerySrcW = float32(400)
	// galleryOutlineW pins the outline column. The live app derives it from
	// the window instead; here the stage width is fixed, so it is too.
	galleryOutlineW = float32(150)
	// galleryPaneH bounds both columns. The interactive gallery host is a
	// scroll area of unbounded height, in which an unbounded child does not
	// know where to stop. It has to leave room for the demo chrome the driver
	// draws above the scene (title, badges, description, source link — about
	// 200px) AND for the three bar rows, inside the Stage height below.
	galleryPaneH = float32(380)

	// tourQuery is the scene's find query. Two occurrences in sampleDoc, which
	// is the smallest number that shows both match tones at once.
	tourQuery = "preview"
)

// sampleDoc is the scene's document. Small on purpose, and chosen to exercise
// the lowering paths a reader will actually look at in a screenshot: headings
// (which are also what the caret-follows-preview sync keys on), inline
// styling, a list, a fenced block, and a callout.
const sampleDoc = `# Release notes

Some **bold** prose with *emphasis*, a ` + "`code span`" + `, and a
[link](https://example.com) to somewhere else.

## What changed

- the preview reparses as you type
- the caret drives the preview's scroll
- copy leaves through the clipboard broker

## Notes

Filed under #release/notes — and note that C#sharp and issue #4 are prose,
not tags.

> [!note] No file I/O
> Text arrives by paste and leaves by copy.

` + "```go" + `
func main() {
	println("hello")
}
` + "```" + `
`

func init() {
	registry.Register(registry.Demo{
		Name:     "mdedit-split",
		Category: "UX",
		Title:    icons.PhMarkdownLogo + " mdedit — source beside preview",
		Stage:    [2]float32{980, 660},
		Flags:    registry.DemoFlagNeedsLargeArea,
		Kind:     registry.DemoKindMixed,
		Description: "A markdown editing surface composed from pieces that already existed: the source is a " +
			"plain monospace TextEdit, the preview is a markdown.Doc reparsed whenever the buffer changes, " +
			"and moving the caret into a different section scrolls the preview to it. The live app puts the " +
			"two in a resizable split and adds a dirty marker and a clipboard export.",
		Init:           mdTourInit,
		RenderStateful: mdTourRender,
		SourceFunc:     (*App).renderPreview,
	})
}

func mdTourInit(ids *c.WidgetIdStack) (state any) {
	inst := newApp()
	inst.ids = ids
	inst.src = sampleDoc
	inst.saved = sampleDoc
	// The scene shows the outline column: it is half of what M2 added, and the
	// gallery stage is wide enough to earn it.
	inst.showOutline = true
	// …and an open find bar with a query that hits twice, so the capture
	// carries M3's two tones — the current match and the rest — and would show
	// a regression in the colour-section split as a background bleeding across
	// the prose around a match.
	inst.find.show = true
	inst.find.query = tourQuery
	return inst
}

func mdTourRender(ids *c.WidgetIdStack, state any) {
	if inst, ok := state.(*App); ok && inst != nil {
		inst.ids = ids
		inst.renderGallery()
	}
}

// renderGallery is the split laid out for a demo host rather than for the
// app's own window.
//
// It deliberately does not reuse renderBody. The gallery's interactive driver
// wraps each demo in an unbounded scroll area with no CentralPanel region, and
// egui side panels size against a CentralPanel — inside a bare scroll area
// they collapse to a sliver and clip their contents. The tour's driver bounds
// its rect and would hide that entirely, so the panels would look correct in
// every screenshot and be broken in the gallery. A pinned-width column beside
// an unconstrained one behaves in both.
func (inst *App) renderGallery() {
	inst.refreshDerived()
	inst.renderBar()
	// The size constraints sit on the columns, OUTSIDE the scroll areas. Set
	// inside, they size the scrolled CONTENT instead of the viewport — the
	// pane then scrolls a 380px-tall content block inside whatever height was
	// left over, which reads as a pane opened partway down its own document.
	for range c.Horizontal().KeepIter() {
		for range c.Vertical().KeepIter() {
			c.UiSetMinWidth(gallerySrcW)
			c.UiSetMaxWidth(gallerySrcW)
			c.UiSetMinHeight(galleryPaneH)
			c.UiSetMaxHeight(galleryPaneH)
			inst.renderSource()
		}
		for range c.Vertical().KeepIter() {
			c.UiSetMinWidth(galleryOutlineW)
			c.UiSetMaxWidth(galleryOutlineW)
			c.UiSetMinHeight(galleryPaneH)
			c.UiSetMaxHeight(galleryPaneH)
			inst.renderOutline()
		}
		for range c.Vertical().KeepIter() {
			c.UiSetMinHeight(galleryPaneH)
			c.UiSetMaxHeight(galleryPaneH)
			for range c.ScrollArea().Hscroll(true).Vscroll(true).AutoShrink(false, false).KeepIter() {
				inst.renderPreview()
			}
		}
	}
}
