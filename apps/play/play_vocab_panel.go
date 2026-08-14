package play

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/help/search"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/regexedit"
)

// play_vocab_panel.go draws the Vocabulary tool tab (ADR-0174): what a buffer
// in this build may call, grouped by where each name runs, with the server
// section marked against what the endpoint actually carries.
//
// It is a sibling of Snippets in TabZoneTools and shares its two seams — the
// regex battery for filtering (ADR-0164 §SD4) and InsertSqlAtCaret for
// delivery — so a user filtering the vocabulary and filtering the snippet
// library types in the same language, and an Insert lands the same way.
//
// The panel reports; it never provisions. Installing is a process-level
// reconcile at startup (installSQLSurface), and a per-user "install it" button
// would be a decision about who may change server state that ADR-0174 does
// not make.

// vocabTabState is the Vocabulary tab's render-thread state.
type vocabTabState struct {
	filter   string
	query    string // trimmed filter the accepted set was computed for
	accepted map[string]bool
	literal  bool // some token was not a valid regexp and matched literally
	altHint  string
}

// renderVocabularyTab draws the tab body (inside the dock's scroll host).
func (inst *PlayApp) renderVocabularyTab() {
	declared := vocabDeclared()
	installed, ready := inst.vocab.demand()
	vocabMarkInstalled(declared, installed)

	inst.renderVocabFilterRow(declared)
	inst.renderVocabStatus(installed, ready)

	st := &inst.vocabTab
	for _, where := range []vocabWhereE{vocabServer, vocabClient, vocabPlay} {
		rows := make([]vocabEntry, 0, len(declared))
		for _, e := range declared {
			if e.Where == where && vocabAccepted(st, e) {
				rows = append(rows, e)
			}
		}
		if where == vocabServer {
			for _, e := range vocabExtras(installed, declared) {
				if vocabAccepted(st, e) {
					rows = append(rows, e)
				}
			}
		}
		inst.renderVocabSection(where, rows, ready)
	}
}

// vocabAccepted applies the active filter to one entry. With no filter every
// entry passes. The name, the doc line and the family are all searchable —
// someone looking for "the ragged thing that counts" should find
// LW_RAGGED_COUNT by either half.
func vocabAccepted(st *vocabTabState, e vocabEntry) bool {
	if st.query == "" {
		return true
	}
	return st.accepted[strings.ToLower(e.Name)]
}

// renderVocabFilterRow draws the filter box and recomputes the accepted set
// when the trimmed query changed. The corpus is a few dozen in-memory
// strings, so the sweep is synchronous and per keystroke — the same shape as
// the snippets filter, three orders of magnitude smaller.
func (inst *PlayApp) renderVocabFilterRow(declared []vocabEntry) {
	st := &inst.vocabTab
	for range c.Horizontal().KeepIter() {
		inst.vocabHl.Prepare(inst.ids.PrepareStr("vocabFilter"), st.filter, false, regexedit.ModeTokens).
			HintText("Filter (regex, space = AND)").
			SendRespVal(&st.filter)
		if st.filter != "" {
			if c.Button(inst.ids.PrepareStr("vocabFilterClear"), c.Atoms().Text("×").Keep()).
				SendResp().HasPrimaryClicked() {
				st.filter = ""
			}
		}
	}
	if st.literal {
		for rt := range c.RichTextLabel("some tokens are not valid regexps and match literally") {
			rt.Small().Weak()
		}
	}
	if st.altHint != "" {
		for rt := range c.RichTextLabel("also matching: " + st.altHint) {
			rt.Small().Weak()
		}
	}

	q := strings.TrimSpace(st.filter)
	if q == st.query {
		return
	}
	st.query = q
	st.accepted = nil
	st.literal = false
	st.altHint = ""
	if q == "" {
		return
	}
	battery := search.ParseQueryWith(q, snippetThesaurus())
	st.altHint = battery.AlternatesHint()
	for i := range battery.Patterns {
		if battery.Patterns[i].Literal {
			st.literal = true
			break
		}
	}
	// Every pattern must match somewhere in the entry (space = AND). The
	// haystack is one string rather than per-field, so `ragged count` matches
	// a name and a doc line jointly — a user narrowing a list is describing
	// the thing, not saying which field carries which word.
	st.accepted = make(map[string]bool, len(declared))
	for _, e := range declared {
		hay := e.Name + " " + e.Doc + " " + e.Family
		all := true
		for i := range battery.Patterns {
			if !battery.Patterns[i].Matches(hay) {
				all = false
				break
			}
		}
		if all {
			st.accepted[strings.ToLower(e.Name)] = true
		}
	}
}

// renderVocabStatus is the line under the filter saying what is known about
// the endpoint. It distinguishes three states that a naive rendering would
// collapse: no client at all, a probe still in flight, and an answer.
func (inst *PlayApp) renderVocabStatus(installed map[string]string, ready bool) {
	switch {
	case inst.client == nil:
		for rt := range c.RichTextLabel("No endpoint — the server section cannot be checked. Client and play functions work regardless.") {
			rt.Small().Weak()
		}
	case !ready:
		for rt := range c.RichTextLabel("asking " + inst.client.URL() + " which functions it carries…") {
			rt.Small().Weak()
		}
	default:
		line := strconv.Itoa(len(installed)) + " user-defined function(s) on " + inst.client.URL()
		if skew, ok := vocabSurfaceSkew(inst.vocab.surfaceVersion, inst.vocab.preSurfaceVersion); ok {
			line += " · " + skew
		}
		for rt := range c.RichTextLabel(line) {
			rt.Small().Weak()
		}
	}
	c.Separator().Horizontal().Send()
}

// renderVocabSection draws one population: heading, what membership means,
// then a row per function.
func (inst *PlayApp) renderVocabSection(where vocabWhereE, rows []vocabEntry, ready bool) {
	for rt := range c.RichTextLabel(where.title()) {
		rt.Strong()
	}
	for rt := range c.RichTextLabel(where.blurb()) {
		rt.Small().Weak()
	}
	if len(rows) == 0 {
		for rt := range c.RichTextLabel("(nothing matches the filter)") {
			rt.Small().Weak()
		}
		return
	}
	// IdScope per section: the row ids derive from the function name, and two
	// sections legitimately hold the same name (LW_ID_* is both server and
	// client), which would otherwise collide into one widget id.
	for range c.IdScope(inst.ids.PrepareStr("vocab-" + where.String())) {
		for i := range rows {
			inst.renderVocabRow(rows[i], where, ready)
		}
	}
	c.Separator().Horizontal().Send()
}

// renderVocabRow draws one function: the mark, the call signature, an Insert
// button, and the doc line under it.
func (inst *PlayApp) renderVocabRow(e vocabEntry, where vocabWhereE, ready bool) {
	for range c.Horizontal().KeepIter() {
		if mark, tone := vocabRowMark(e, where, ready); mark != "" {
			for rt := range c.RichTextLabel(mark) {
				rt.Small()
				if tone {
					rt.Weak()
				}
			}
		}
		for rt := range c.RichTextLabel(e.call()) {
			rt.Monospace()
		}
		if c.Button(inst.ids.PrepareStr("vocabIns-"+e.Name), c.Atoms().Text("Insert").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.InsertSqlAtCaret(e.call())
		}
	}
	if e.Doc != "" {
		for rt := range c.RichTextLabel(e.Doc) {
			rt.Small().Weak()
		}
	}
	// The dependency line only appears when something is actually missing.
	// Listing what an expansion needs on every endpoint that has it would
	// be noise on every row; naming it on the endpoint where the call will
	// fail is the whole point (ADR-0174 §SD6).
	if ready && len(e.MissingDeps) > 0 {
		for rt := range c.RichTextLabel("expands into " + strings.Join(e.MissingDeps, ", ") + " — MISSING on this endpoint") {
			rt.Small()
		}
	}
}

// vocabRowMark is the leading status glyph and whether it renders recessed.
//
// Only the server section can be missing, and only once the probe has
// answered: before that every server row is unmarked rather than marked
// present, because "we have not asked yet" and "it is there" are different
// facts and only one of them is known.
func vocabRowMark(e vocabEntry, where vocabWhereE, ready bool) (mark string, weak bool) {
	switch {
	case where != vocabServer && !e.Available:
		return "reserved ·", true
	case where != vocabServer:
		return "", false
	case !ready:
		return "? ·", true
	case !e.Declared:
		return "extra ·", true
	case e.Installed:
		return "✓ ·", true
	}
	return "MISSING ·", false
}
