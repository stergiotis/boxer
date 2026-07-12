package play

import (
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// playAppId must match the manifest Id in app_register.go. The snippet
// library is play's own help book: the "snippets" doc, whose fenced SQL
// blocks are surfaced as Insert buttons.
const playAppId app.AppIdT = "github.com/stergiotis/boxer/apps/play"

// snippetActionLabels are the per-block buttons in the Snippets tab, in the
// order RenderActionsN reports them via CodeBlockAction.Button: Insert
// splices the snippet at the editor caret (TextEditFluid.InsertAtCursor);
// Replace swaps the whole editor buffer.
var snippetActionLabels = []string{"Insert", "Replace"}

const (
	snippetButtonInsert  = 0
	snippetButtonReplace = 1
)

// snippetDoc memoizes the parsed "snippets" help doc for the whole package.
// The corpus is embedded and immutable, so one parse serves every PlayApp
// instance. The book is built straight from the embedded FS (helpFS) rather
// than via help.DefaultLibrary, so it does not depend on registry-sync
// timing — but it is the same help.Book + markdown machinery the Help
// center uses. snippetDocCached stays nil when the doc is absent or fails to
// parse, which renderSnippetsTab degrades to a short notice.
var (
	snippetDocOnce   sync.Once
	snippetDocCached *markdown.Doc
)

func loadSnippetDoc() *markdown.Doc {
	snippetDocOnce.Do(func() {
		book, err := help.NewBook(playAppId, help.MustSub(helpFS, "help"))
		if err != nil {
			return
		}
		if doc, _, ok := book.Doc("snippets"); ok {
			snippetDocCached = doc
		}
	})
	return snippetDocCached
}

// renderSnippetsTab draws the snippet library in the Snippets dock tab: the
// "snippets" help doc rendered with Insert and Replace buttons above every
// fenced code block. This reuses markdown.Doc.RenderActionsN — the same
// mechanism HelpHost wires to "Copy" — but routes a click into the editor
// instead of the clipboard: Insert stashes the snippet on
// inst.pendingSnippetInsert (the Rust side splices it at the caret,
// TextEditFluid.InsertAtCursor, ADR-0063); Replace stashes it on
// inst.pendingSnippetReplace (a whole-buffer swap, no FFI). renderSqlEditor
// consumes whichever is pending on the next frame.
//
// Keeping the editor visible (Snippets is a sibling of the bottom body
// tabs, not of the editor) is what lets the insert land at the caret: the
// splice reads the editor's persisted cursor, which only exists while the
// editor is shown. The Insert button is gated to SQL (or untyped) blocks so
// a stray prose block in the corpus never lands in the SQL buffer.
func (inst *PlayApp) renderSnippetsTab() {
	doc := loadSnippetDoc()
	if doc == nil {
		for rt := range c.RichTextLabel("No snippets available.") {
			rt.Small().Weak()
		}
		return
	}
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		// IdScope isolates the doc's derived widget ids (markdown.Doc.Render's
		// documented invariant), so the Snippets tab can't collide ids with
		// the Help center rendering the same doc.
		for range c.IdScope(inst.ids.PrepareStr("snippets-doc")) {
			for act := range doc.RenderActionsN(inst.ids, snippetActionLabels) {
				// Never drop a prose block (e.g. a ```bash sample) into the
				// SQL editor; only SQL or untyped blocks are actionable.
				if act.Lang != "sql" && act.Lang != "" {
					continue
				}
				switch act.Button {
				case snippetButtonInsert:
					inst.pendingSnippetInsert = act.Text
				case snippetButtonReplace:
					inst.pendingSnippetReplace = act.Text
				}
				// Focus the Editor tab: the splice op rides the editor's
				// TextEdit, and a hidden tab's body buffer is discarded
				// uninterpreted — the insert would be silently lost (and
				// invisible even if it landed). The activation applies this
				// frame; the pending is consumed by renderSqlEditor on the
				// next frame, when the editor is already showing.
				inst.pendingDockActivate = dockTabEditor
			}
		}
	}
}
