package play

// play_definition.go carries the two surfaces an embedded instance derives
// from the document it was defined by: the **Definition drawer** (the whole
// document, on demand) and the **preamble** (a short explanatory passage the
// author chose to keep over the numbers, always visible). It also carries the
// **dataset notice**, which rides in the same strip but comes from the host
// rather than the document — see [PlayApp.SetDatasetNotice].
//
// The Definition drawer is the markdown document an embedded instance was
// defined BY, rendered as a right-hand drawer the top bar's "Definition"
// toggle opens.
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

// SetPreambleMarkdown hands play the explanatory passage an applet author
// wants standing over the results — a sqlapplet's `md preamble` aux fence.
// It renders above every result pane, inside the central panel, so it reads
// as a header on the data rather than as another piece of toolbar. Call it
// between construction and mount; empty source renders nothing.
//
// Separate from [PlayApp.SetDefinitionMarkdown] on purpose: the definition is
// the whole document a reader opens deliberately, the preamble is the couple
// of sentences they should not have to open anything to see. An applet may
// have either, both, or neither.
func (inst *PlayApp) SetPreambleMarkdown(src []byte) {
	if len(bytes.TrimSpace(src)) == 0 {
		inst.preamble = nil
		return
	}
	inst.preamble = markdown.Parse(src)
}

// SetDatasetNotice hands play a runtime notice about this instance's data
// preconditions — in practice, that a declared ad-hoc dataset alias has no
// dataset behind it yet (ADR-0134 §SD4). It renders above the preamble,
// inside the central panel, so the reader meets the reason the panes are
// empty before the prose explaining what the panes would mean.
//
// Unlike [PlayApp.SetPreambleMarkdown] this is expected to be re-set while
// the instance is live, as the condition it reports appears and clears;
// empty source clears it. It reparses on every call, so a caller that
// re-derives the same text each frame should compare before setting.
//
// It is the host's surface, not the document's: play has no idea what
// produces a given alias, and the embedder that declared the alias does.
func (inst *PlayApp) SetDatasetNotice(src []byte) {
	if len(bytes.TrimSpace(src)) == 0 {
		inst.datasetNotice = nil
		return
	}
	inst.datasetNotice = markdown.Parse(src)
}

// renderDatasetNotice draws the notice strip, immediately above the preamble
// and under the same panel discipline: non-resizable, sized to its content.
func (inst *PlayApp) renderDatasetNotice() {
	if inst.datasetNotice == nil {
		return
	}
	ids := inst.ids
	for range c.PanelTopInside(ids.PrepareStr("datasetNoticePanel")).Resizable(false).KeepIter() {
		// IdScope isolates the document's derived widget ids, for the reason
		// the preamble does it: more than one markdown doc renders per frame.
		for range c.IdScope(ids.PrepareStr("datasetNoticeBody")) {
			inst.datasetNotice.Render(ids)
		}
		c.Separator().Send()
	}
}

// renderPreamble draws the preamble strip. Render calls it inside the central
// panel, above the DockArea, so it spans every result tab and leaves the top
// bar to the controls. Non-resizable, so it fits its content the way the top
// bar fits its buttons — a two-line preamble reserves two lines. A runaway
// one squeezes the panes, which is the author's signal to move the prose into
// the document proper, where the Definition drawer already shows it.
func (inst *PlayApp) renderPreamble() {
	if inst.preamble == nil {
		return
	}
	ids := inst.ids
	for range c.PanelTopInside(ids.PrepareStr("preamblePanel")).Resizable(false).KeepIter() {
		// IdScope isolates the document's derived widget ids — the same
		// invariant the Definition drawer observes, and it matters more here:
		// both documents can be on screen in the same frame.
		for range c.IdScope(ids.PrepareStr("preambleBody")) {
			inst.preamble.Render(ids)
		}
		c.Separator().Send()
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
