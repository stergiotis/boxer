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
	//
	// Wider than the flat list needed. The outline renders through the tree
	// widget now, so the column pays for a disclosure control, a per-level
	// indent and the trailing count before the heading text starts.
	galleryOutlineW = float32(190)
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
//
// The heading levels go three deep on one branch and two on another, so the
// outline column has a hierarchy to draw rather than a flat run — a forest of
// same-level headings would show the tree widget's indent and disclosure
// controls doing nothing.
const sampleDoc = `# Release notes

Some **bold** prose with *emphasis*, a ` + "`code span`" + `, and a
[link](https://example.com) to somewhere else.

## What changed

- the preview reparses as you type
- the caret drives the preview's scroll
- copy leaves through the clipboard broker

### Why the caret leads

Following the caret keeps both panes on the same section without a scroll sync
that fights the reader for the scrollbar.

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
	// The scene shows the outline column — the heading tree, its indents and
	// its disclosure controls — and the gallery stage is wide enough to earn
	// it. Left fully expanded, which is the state a reader first meets.
	inst.showOutline = true
	// Pin the caret at the top of the document. Left alone, the TextEdit
	// reports whatever cursor it starts with — the end of the buffer — and the
	// caret-follows-preview sync then does its job and scrolls both panes past
	// the title. Focus is not requested: the scene wants the caret placed, not
	// the editor made the focused widget.
	inst.requestCaret(0, 0, false)
	// …and a find query that hits twice, so the capture carries M3's two tones
	// — the current match and the rest — and would show a regression in the
	// colour-section split as a background bleeding across the prose around a
	// match. The query alone is the whole gesture: find has no on/off.
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
	// The same call renderBody makes, and for the scene the same reason: the
	// outline highlights whichever section the caret is in, so without it the
	// column draws with nothing selected and the capture is missing the row
	// chrome. Deterministic here — the caret starts at offset 0, which is the
	// first heading.
	inst.trackCaretSection()
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
