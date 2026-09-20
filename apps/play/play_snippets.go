package play

import (
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/keelson/runtime/help/search"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
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

// snippetSource is one snippet library, parsed: a doc of a help book, its
// section list, and a search index over the book. It is immutable and shared
// by every PlayApp instance; what differs per window — the filter — is
// [snippetPane]. The book is built straight from an fs.FS rather than via
// help.DefaultLibrary, so it does not depend on registry-sync timing — but it
// is the same help.Book + markdown machinery the Help center uses. doc stays
// nil when the doc is absent or fails to parse, which the pane degrades to a
// short notice.
type snippetSource struct {
	appId    app.AppIdT
	docName  string
	doc      *markdown.Doc
	sections []help.SectionInfo
	index    *search.Index
}

func newSnippetSource(appId app.AppIdT, fsys fs.FS, docName string) (src *snippetSource) {
	src = &snippetSource{appId: appId, docName: docName}
	book, err := help.NewBook(appId, fsys)
	if err != nil {
		return
	}
	if doc, info, ok := book.Doc(docName); ok {
		src.doc = doc
		src.sections = info.Sections
		src.index = search.NewIndexBooks(book)
	}
	return
}

// builtinSnippetSource memoizes play's own library for the whole package:
// the corpus is embedded and immutable, so one parse serves every PlayApp.
var builtinSnippetSource = sync.OnceValue(func() *snippetSource {
	return newSnippetSource(playAppId, help.MustSub(helpFS, "help"), "snippets")
})

var (
	// snippetThOnce/snippetTh memoise the thesaurus (ADR-0164 §SD7)
	// separately from the doc parse: the app registry must be complete
	// when it is built, and the first filter keystroke — well past
	// init — is a safely late moment.
	snippetThOnce sync.Once
	snippetTh     search.Thesaurus
)

func snippetThesaurus() search.Thesaurus {
	snippetThOnce.Do(func() { snippetTh = search.DefaultThesaurus() })
	return snippetTh
}

// builtinSnippetsKey is the built-in pane's key. Widget ids are derived from
// the key, and the built-in's spell exactly what they did before panes could
// be contributed, so its egui state survives.
const builtinSnippetsKey = "snippets"

// snippetPane is one Snippets-class dock tab of one window: a source plus the
// filter state (ADR-0164 §SD4). filter backs the box; query is the trimmed
// query accepted (matching section slugs, descendant-expanded) was computed
// for — refreshed on change, not per frame. literal flags a token that
// degraded to a literal match so the tab can say so; coverage is how much of
// the doc the filter keeps (the selectivity meter); hl is the filter box's
// regexedit highlighter, per pane because the widget owns a highlight cache.
type snippetPane struct {
	key      string
	src      *snippetSource
	filter   string
	query    string
	accepted map[string]bool
	literal  bool
	altHint  string
	coverage search.Coverage
	hl       regexedit.Edit
}

// SnippetLibrary is a snippet pane contributed to every play window: a doc of
// a help book, shown in a dock tab of its own beside the built-in Snippets tab
// with the same filter and the same Insert and Replace buttons.
//
// It is the seam for a repository that mounts play as it is and wants its own
// table names and recipes one click from the editor, without those names
// entering this repository's corpus. An app that embeds a PlayApp and drives
// its tab registry itself needs none of this.
type SnippetLibrary struct {
	// TabID is the tab's slug, unique among a window's tabs.
	TabID string
	// DockID is the tab's frozen dock identity. The persisted dock layout
	// keys on it, so it never changes once shipped; built-ins hold 1..63 and
	// a contributed library takes 64 or above.
	DockID uint64
	// Title is what the tab says.
	Title string
	// AppId names the contributor. It keys the help book the library is read
	// through and need not be a registered app.
	AppId app.AppIdT
	// Help is the root of a help book: a directory of markdown documents.
	Help fs.FS
	// Doc is the document within it, its path less the `.md`, whose fenced
	// SQL blocks are the snippets.
	Doc string
}

var (
	snippetLibrariesMu sync.Mutex
	snippetLibraries   []registeredSnippetLibrary
)

type registeredSnippetLibrary struct {
	lib SnippetLibrary
	src func() *snippetSource
}

// RegisterSnippetLibraryE contributes a snippet pane to every play window
// opened afterwards. Call it at init, the way a book or a pass is registered;
// a window already open keeps the tab set it was built with.
func RegisterSnippetLibraryE(lib SnippetLibrary) (err error) {
	if lib.TabID == "" || lib.Title == "" || lib.Doc == "" || lib.Help == nil || lib.DockID < 64 {
		err = eb.Build().Str("tabId", lib.TabID).Uint64("dockId", lib.DockID).Errorf("snippet library needs a tab id, a title, a help book, a doc and a dock id of 64 or above")
		return
	}
	snippetLibrariesMu.Lock()
	defer snippetLibrariesMu.Unlock()
	for _, r := range snippetLibraries {
		if r.lib.TabID == lib.TabID || r.lib.DockID == lib.DockID {
			err = eb.Build().Str("tabId", lib.TabID).Uint64("dockId", lib.DockID).Errorf("snippet library collides with one already registered")
			return
		}
	}
	snippetLibraries = append(snippetLibraries, registeredSnippetLibrary{
		lib: lib,
		// Parsed at first use, not here: init runs before anyone has asked
		// for a window.
		src: sync.OnceValue(func() *snippetSource { return newSnippetSource(lib.AppId, lib.Help, lib.Doc) }),
	})
	return
}

func registeredSnippetLibraries() (libs []registeredSnippetLibrary) {
	snippetLibrariesMu.Lock()
	defer snippetLibrariesMu.Unlock()
	libs = slices.Clone(snippetLibraries)
	return
}

// addSnippetLibraryTabs gives a new window one tab per contributed library,
// in the tools zone beside the built-in Snippets tab. A library whose tab
// cannot be added — its dock id taken by an embedder's own tab — is skipped:
// a missing reference pane is not a reason to refuse a window.
//
// Each tab carries TabSpec.Contributed, which is how an embedder that strips
// the editor recognises a pane whose id it cannot know: a snippet inserts at
// the caret, so the pane is chrome wherever there is no editor to insert into.
func addSnippetLibraryTabs(inst *PlayApp, reg *TabRegistry) {
	for _, r := range registeredSnippetLibraries() {
		pane := &snippetPane{key: r.lib.TabID}
		src := r.src
		_ = reg.Add(TabSpec{
			ID: r.lib.TabID, DockID: r.lib.DockID, Title: r.lib.Title, Zone: TabZoneTools, Lazy: true,
			Contributed: true,
			Render: func(f *TabFrame) {
				pane.src = src()
				pane.render(inst)
			},
		})
	}
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
	if inst.snippets.src == nil {
		inst.snippets.key = builtinSnippetsKey
		inst.snippets.src = builtinSnippetSource()
	}
	inst.snippets.render(inst)
}

// render is the body of a Snippets-class tab.
func (pane *snippetPane) render(inst *PlayApp) {
	doc := pane.src.doc
	if doc == nil {
		for rt := range c.RichTextLabel("No snippets available.") {
			rt.Small().Weak()
		}
		return
	}
	pane.renderFilterRow(inst)
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		switch {
		case pane.query == "":
			// IdScope isolates the doc's derived widget ids (markdown.Doc.Render's
			// documented invariant), so the Snippets tab can't collide ids with
			// the Help center rendering the same doc.
			for range c.IdScope(inst.ids.PrepareStr(pane.key + "-doc")) {
				inst.renderSnippetsDoc(doc, nil)
			}
		case len(pane.accepted) == 0:
			for rt := range c.RichTextLabel("(no matching snippets)") {
				rt.Small().Weak()
			}
		default:
			accepted := pane.accepted
			for range c.IdScope(inst.ids.PrepareStr(pane.key + "-f-" + pane.query)) {
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
func (pane *snippetPane) renderFilterRow(inst *PlayApp) {
	for range c.Horizontal().KeepIter() {
		// regexedit paints the battery shape: one independent pattern
		// per whitespace-separated token, monospace (ADR-0164 §SD4).
		pane.hl.Prepare(inst.ids.PrepareStr(pane.key+"Filter"), pane.filter, false, regexedit.ModeTokens).
			HintText("Filter (regex, space = AND)").
			SendRespVal(&pane.filter)
		if pane.filter != "" {
			if c.Button(inst.ids.PrepareStr(pane.key+"FilterClear"), c.Atoms().Text("×").Keep()).
				SendResp().HasPrimaryClicked() {
				pane.filter = ""
			}
		}
	}
	if pane.literal {
		for rt := range c.RichTextLabel("some tokens are not valid regexps and match literally") {
			rt.Small().Weak()
		}
	}
	if pane.altHint != "" {
		for rt := range c.RichTextLabel("also matching: " + pane.altHint) {
			rt.Small().Weak()
		}
	}
	q := strings.TrimSpace(pane.filter)
	if q != pane.query {
		pane.query = q
		pane.accepted = nil
		pane.literal = false
		pane.altHint = ""
		pane.coverage = search.Coverage{}
		if q != "" && pane.src.index != nil {
			battery := search.ParseQueryWith(q, snippetThesaurus())
			pane.altHint = battery.AlternatesHint()
			for i := range battery.Patterns {
				if battery.Patterns[i].Literal {
					pane.literal = true
					break
				}
			}
			accepted := make(map[string]bool, 8)
			for _, h := range pane.src.index.Search(battery, 0) {
				// The index spans the whole book; the tab shows one doc.
				if h.Ref.Doc == pane.src.docName && h.Ref.Section != "" {
					accepted[h.Ref.Section] = true
				}
			}
			pane.accepted = search.ExpandDescendants(pane.src.sections, accepted)
			pane.coverage = pane.src.index.DocCoverage(pane.src.appId, pane.src.docName, pane.accepted)
		}
	}
	if pane.query == "" {
		return
	}
	cov := pane.coverage
	for range c.Horizontal().KeepIter() {
		c.ProgressBar(cov.Frac()).DesiredWidth(snippetsCoverageBarWidth).Send()
		c.Label(strconv.Itoa(cov.SelSections) + "/" + strconv.Itoa(cov.TotalSections) +
			" sections · " + strconv.Itoa(int(cov.Frac()*100+0.5)) + "% of the library").Send()
	}
}
