package play

import (
	"strconv"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/help/search"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/regexedit"
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

// sqlBlockActionable gates which fenced blocks carry the Insert/Replace row
// (markdown.WithCodeActionFilter). Only SQL — or untyped, which the corpora
// use for plain snippets — may reach the editor.
//
// It withholds the BUTTONS, not just the click. Both surfaces used to render
// the row on every block and ignore the unwanted clicks, which left a
// ```response block of ClickHouse's box-drawing output advertising an Insert
// that did nothing. An affordance that does nothing is worse than no
// affordance: the reader has to click it to find out.
//
// Shared by the Snippets tab and the Docs pane so the two cannot drift about
// what is insertable, and it is the single gate — neither call site re-checks
// the language after the fact.
func sqlBlockActionable(_ string, lang string) bool {
	return lang == "sql" || lang == ""
}

// snippetDoc memoizes the parsed "snippets" help doc — plus its section
// list and a search index over the book — for the whole package. The
// corpus is embedded and immutable, so one parse serves every PlayApp
// instance. The book is built straight from the embedded FS (helpFS)
// rather than via help.DefaultLibrary, so it does not depend on
// registry-sync timing — but it is the same help.Book + markdown
// machinery the Help center uses. snippetDocCached stays nil when the
// doc is absent or fails to parse, which renderSnippetsTab degrades to
// a short notice.
var (
	snippetDocOnce   sync.Once
	snippetDocCached *markdown.Doc
	snippetSections  []help.SectionInfo
	snippetIndex     *search.Index
)

func loadSnippetDoc() *markdown.Doc {
	snippetDocOnce.Do(func() {
		book, err := help.NewBook(playAppId, help.MustSub(helpFS, "help"))
		if err != nil {
			return
		}
		if doc, info, ok := book.Doc("snippets"); ok {
			snippetDocCached = doc
			snippetSections = info.Sections
			snippetIndex = search.NewIndexBooks(book)
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
//
// The filter box narrows the doc to matching sections (ADR-0164 §SD4):
// hits from the shared pattern-battery search, expanded to descendant
// subsections, drive markdown.WithSectionFilter. The filtered render
// lives under an IdScope keyed by the query — skipping sections shifts
// the doc's seq-derived widget ids, and abandoning egui state (an open
// callout, a dragged column) on filter change is the accepted cost.
// The UNfiltered render keeps its historical "snippets-doc" scope, so
// state there survives exactly as before the filter existed.
func (inst *PlayApp) renderSnippetsTab() {
	doc := loadSnippetDoc()
	if doc == nil {
		for rt := range c.RichTextLabel("No snippets available.") {
			rt.Small().Weak()
		}
		return
	}
	inst.renderSnippetsFilterRow()
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		switch {
		case inst.snippetsQuery == "":
			// IdScope isolates the doc's derived widget ids (markdown.Doc.Render's
			// documented invariant), so the Snippets tab can't collide ids with
			// the Help center rendering the same doc.
			for range c.IdScope(inst.ids.PrepareStr("snippets-doc")) {
				inst.renderSnippetsDoc(doc, nil)
			}
		case len(inst.snippetsAccepted) == 0:
			for rt := range c.RichTextLabel("(no matching snippets)") {
				rt.Small().Weak()
			}
		default:
			accepted := inst.snippetsAccepted
			for range c.IdScope(inst.ids.PrepareStr("snippets-f-" + inst.snippetsQuery)) {
				inst.renderSnippetsDoc(doc, markdown.WithSectionFilter(func(slug string) bool {
					return accepted[slug]
				}))
			}
		}
	}
}

// renderSnippetsDoc is the shared render body: RenderActionsN with the
// SQL gate, plus whatever extra options the caller adds (the section
// filter), delivering clicks through the public seam (play_delivery.go).
// Both ops focus the Editor tab, so the splice lands where the buffer is
// live (a hidden editor discards its body buffer uninterpreted, losing
// the insert). Snippets is the in-tree consumer of the same ops an
// embedder's snippet-class pane uses (ADR-0097 slice-6 D5 Update).
func (inst *PlayApp) renderSnippetsDoc(doc *markdown.Doc, extra markdown.RenderOpt) {
	opts := make([]markdown.RenderOpt, 0, 2)
	opts = append(opts, markdown.WithCodeActionFilter(sqlBlockActionable))
	if extra != nil {
		opts = append(opts, extra)
	}
	for act := range doc.RenderActionsN(inst.ids, snippetActionLabels, opts...) {
		switch act.Button {
		case snippetButtonInsert:
			inst.InsertSqlAtCaret(act.Text)
		case snippetButtonReplace:
			inst.ReplaceSql(act.Text)
		}
	}
}

// snippetsCoverageBarWidth is explicit because a ProgressBar without
// DesiredWidth takes everything before it in a Horizontal — the
// documented trap.
const snippetsCoverageBarWidth = 120.0

// renderSnippetsFilterRow draws the filter box and recomputes the
// accepted-section set when the trimmed query changed — an RE2 sweep of
// one embedded book, so per keystroke and synchronous (ADR-0164 §SD3).
// Only real sections are accepted (the doc-level "" region is hidden
// while a filter is active — the intro is chrome, not a snippet), and a
// matched section keeps its descendant subsections.
//
// While a filter is active, a selectivity meter says how much of the
// snippets doc survives it: byte-share bar, numbers in the adjacent
// label (never on the bar — ProgressBar's own text is illegible at low
// fractions).
func (inst *PlayApp) renderSnippetsFilterRow() {
	for range c.Horizontal().KeepIter() {
		// regexedit paints the battery shape: one independent pattern
		// per whitespace-separated token, monospace (ADR-0164 §SD4).
		inst.snippetsHl.Prepare(inst.ids.PrepareStr("snippetsFilter"), inst.snippetsFilter, false, regexedit.ModeTokens).
			HintText("Filter (regex, space = AND)").
			SendRespVal(&inst.snippetsFilter)
		if inst.snippetsFilter != "" {
			if c.Button(inst.ids.PrepareStr("snippetsFilterClear"), c.Atoms().Text("×").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.snippetsFilter = ""
			}
		}
	}
	if inst.snippetsLiteral {
		for rt := range c.RichTextLabel("some tokens are not valid regexps and match literally") {
			rt.Small().Weak()
		}
	}
	q := strings.TrimSpace(inst.snippetsFilter)
	if q != inst.snippetsQuery {
		inst.snippetsQuery = q
		inst.snippetsAccepted = nil
		inst.snippetsLiteral = false
		inst.snippetsCoverage = search.Coverage{}
		if q != "" && snippetIndex != nil {
			battery := search.ParseQuery(q)
			for i := range battery.Patterns {
				if battery.Patterns[i].Literal {
					inst.snippetsLiteral = true
					break
				}
			}
			accepted := make(map[string]bool, 8)
			for _, h := range snippetIndex.Search(battery, 0) {
				// The index spans the whole play book; the tab shows one doc.
				if h.Ref.Doc == "snippets" && h.Ref.Section != "" {
					accepted[h.Ref.Section] = true
				}
			}
			inst.snippetsAccepted = search.ExpandDescendants(snippetSections, accepted)
			inst.snippetsCoverage = snippetIndex.DocCoverage(playAppId, "snippets", inst.snippetsAccepted)
		}
	}
	if inst.snippetsQuery == "" {
		return
	}
	cov := inst.snippetsCoverage
	for range c.Horizontal().KeepIter() {
		c.ProgressBar(cov.Frac()).DesiredWidth(snippetsCoverageBarWidth).Send()
		c.Label(strconv.Itoa(cov.SelSections) + "/" + strconv.Itoa(cov.TotalSections) +
			" sections · " + strconv.Itoa(int(cov.Frac()*100+0.5)) + "% of the library").Send()
	}
}
