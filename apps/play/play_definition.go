package play

// play_definition.go is the applet definition view: the markdown document an
// embedded instance was defined BY, rendered inside the instance as a
// right-hand drawer the top bar's "Definition" toggle opens.
//
// It replaces the ADR-0132 §SD3 "Copy SQL" button, which exported the buffer
// and only the buffer. A sqlapplet's document carries more than that — the
// frontmatter that decides which panels open, the prose that says what the
// numbers mean, and the SQL — and it is the artifact the applet was minted
// from, so showing it is both the fuller export and the more honest one. The
// clipboard escape hatch survives inside it, per fenced block.
//
// Ordinary playgrounds have no definition: the buffer is the user's own and
// stands behind nothing. The drawer and its toggle are absent there.

import (
	"bytes"

	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// definitionDrawerWidth is the drawer's opening width in points: wide enough
// for a fenced SELECT to read without wrapping, narrow enough to leave the
// result panes the larger half of a 900pt applet window (the adhocdemo
// archetype — the narrowest window an applet realistically opens in). The
// panel is resizable from there, and the dock beside it reflows.
const definitionDrawerWidth float32 = 380

// definitionView is the parsed definition document plus the drawer's open
// state. The document is parsed once, at SetDefinitionMarkdown, and rendered
// many times — the retain-once/render-many shape markdown.Doc documents.
type definitionView struct {
	doc  *markdown.Doc
	open bool
}

// SetDefinitionMarkdown hands play the document this instance was defined by:
// a sqlapplet's markdown source (ADR-0132 §SD1) — frontmatter as the manifest,
// prose as the explanation, the first `sql` fence as this buffer. It turns on
// the top bar's "Definition" toggle and the drawer behind it. Call it between
// construction and mount, like the tab registry; empty source leaves the
// affordance off, which is what an ordinary playground wants.
//
// Parsing happens here rather than per frame. Images resolve through no vault
// — the seam carries the document's bytes and not the book's FS, so a doc that
// embeds one degrades to markdown's glyph-hyperlink fallback.
func (inst *PlayApp) SetDefinitionMarkdown(src []byte) {
	if len(bytes.TrimSpace(src)) == 0 {
		inst.definition = nil
		return
	}
	inst.definition = &definitionView{doc: markdown.Parse(src)}
}

// renderDefinitionPanel draws the drawer when it is open. Render calls it
// between the status bar and the central panel: egui lays side panels out in
// the order they are added, so one added after the central panel would be
// squeezed against an already-claimed remainder.
func (inst *PlayApp) renderDefinitionPanel() {
	d := inst.definition
	if d == nil || !d.open {
		return
	}
	ids := inst.ids
	for range c.PanelRightInside(ids.PrepareStr("definitionPanel")).
		DefaultSize(definitionDrawerWidth).
		Resizable(true).
		KeepIter() {
		for range c.Horizontal().KeepIter() {
			for rt := range c.RichTextLabel("Definition") {
				rt.Strong()
			}
			if c.Button(ids.PrepareStr("definitionClose"), c.Atoms().Text("×").Keep()).
				SendResp().HasPrimaryClicked() {
				d.open = false
			}
		}
		c.Separator().Send()
		for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
			// IdScope isolates the document's derived widget ids
			// (markdown.Doc.Render's documented invariant) so the drawer
			// cannot collide with a Docs or Snippets pane rendering another
			// document the same frame.
			for range c.IdScope(ids.PrepareStr("definitionBody")) {
				inst.renderDefinitionDoc(d.doc)
				// The frontmatter IS the manifest half of the definition —
				// title, icon, endpoint, the pinned `tabs:` list. Last,
				// where the helper is designed to sit, so the prose the
				// author wrote opens the drawer.
				d.doc.RenderFrontmatter()
			}
		}
	}
}

// renderDefinitionDoc emits the document body. With a bus wired, every SQL
// fence carries a Copy button — the ADR-0132 §SD3 clipboard escape hatch,
// attached now to the block it copies rather than to a toolbar button that
// could only ever mean the whole buffer. Without a bus the clipboard is
// unreachable, so the buttons are withheld rather than rendered dead (the
// sqlBlockActionable stance: an affordance that does nothing is worse than
// no affordance).
func (inst *PlayApp) renderDefinitionDoc(doc *markdown.Doc) {
	if inst.bus == nil {
		doc.Render(inst.ids)
		return
	}
	for act := range doc.RenderActions(inst.ids, "Copy",
		markdown.WithCodeActionFilter(sqlBlockActionable)) {
		// Off the frame goroutine — Request blocks until the broker acks
		// (the clipboard rule).
		text := act.Text
		go func() {
			_, _ = inst.bus.Request(clipboardbroker.SubjectWrite, []byte(text))
		}()
	}
}
