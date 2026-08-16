package play

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/help/search"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/regexedit"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
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
//
// # Why a tree and not a run of labels
//
// The first version drew each row as a Horizontal — a variable-width mark, the
// call, an Insert button — with the doc line under it. Two things about that
// cost more than they looked like they would, both of them about SCANNING a
// list rather than reading one row:
//
//   - The mark led the row and its width varied with its own text ("✓" against
//     "MISSING", in the proportional face), so no two rows started their name
//     at the same x. A reader going down the list for a name has no left edge
//     to follow.
//   - Nothing grouped a section's rows, and Family — which names the declaring
//     roster and its ADR — was computed, searched, and never drawn.
//
// Columns fix the first by construction and the tree fixes the second
// (ADR-0176, the widget three other hand-rolled outlines were ported onto at
// its M3). Two more things came free and matter here: the extras population is
// unbounded — the endpoint's undeclared functions, in the hundreds on a real
// one — so a family that collapses and an etable that culls its off-screen
// rows are worth more here than in the outlines already ported.
//
// The cost is ADR-0176 §SD12's, paid by every adopter: a tree row is a fixed
// height, so the doc line that used to sit UNDER each row is now a column
// beside it, truncated, with the full text on hover. That is the same trade
// fieldview's value and configview's description made.

const (
	// vocabPaneProbeSalt namespaces the pane probe's r21 slot; threading it
	// through the instance's id stack keeps two play windows apart. NOT
	// CaptureAvailableSize, which is one process-wide slot the frame's last
	// capture wins (see play_dist_panel.go for the case that taught this).
	vocabPaneProbeSalt uint64 = 0x704ab1e5a1700001
	// vocabInsertIDBase namespaces the per-row Insert buttons. Keyed on the
	// NODE, not the row: responses come back a frame late and an expand
	// between those two frames renumbers every row below it (ADR-0176).
	vocabInsertIDBase uint64 = 0x0174_0001_0000_0000
)

// Column widths, in points. The call column is wide enough for the longest
// declared prototype without truncating it, since the name is what the reader
// scans; everything else is sized to its content and the doc column takes what
// is left.
const (
	vocabCallColWidth float32 = 330
	// One glyph and its inset, and nothing else: the column answers "is this
	// name on the endpoint", which is a mark, and every WORD it used to carry
	// — MISSING, extra, reserved, the dependency note — now prefixes the doc
	// line instead (vocabDocNote).
	//
	// It was 96, sized for MISSING. That is the exception paying for the rule:
	// on a provisioned endpoint the column is a run of identical ✓, so 96
	// points bought roughly zero bits per row and spent them out of the doc
	// column, which was truncating mid-sentence at the time. The prose reads
	// better beside the sentence it qualifies than alone in a box too narrow
	// for it — `needs LW_…` was already truncating to a hover at 96.
	//
	// A header does not fit in 24 and a clipped header reads as a rendering
	// fault, so the column has none; the glyphs carry a hover saying what they
	// mean, which is what the header was doing.
	vocabMarkColWidth   float32 = 24
	vocabInsertColWidth float32 = 62
	vocabDocColMinWidth float32 = 180
	// vocabScrollbarGutter keeps the doc column clear of the table's own
	// vertical scrollbar, which is inside the width the pane probe reports.
	vocabScrollbarGutter float32 = 18
	// vocabFallbackHeight is the table's height on the frames the pane probe
	// has not answered for — the first, and the one a hidden tab comes back
	// on. Left at 0 the table falls back to endETable's auto-fit cap, which a
	// list this long overruns.
	vocabFallbackHeight float32 = 420
)

// Mark colours. The mark is where the scan actually lands once the names are
// aligned — on a provisioned endpoint the column is a run of identical ✓ and
// the handful of rows that are not are the whole point — so it carries the
// colour, rather than the prototype, whose tokens are the same on every row.
var (
	vocabFgMuted   = color.Hex(styletokens.NeutralTextSecondary.AsHex())
	vocabFgMissing = color.Hex(styletokens.ErrorDefault.AsHex())
	vocabFgExtra   = color.Hex(styletokens.WarningDefault.AsHex())
	vocabBgNone    = color.Transparent
)

// vocabTabState is the Vocabulary tab's render-thread state.
type vocabTabState struct {
	filter   string
	query    string // trimmed filter the accepted set was computed for
	corpus   int    // entry count the accepted set was computed over
	accepted map[string]bool
	literal  bool // some token was not a valid regexp and matched literally
	altHint  string

	// nav is the outline's expansion, selection and cursor. Keyed, so it
	// survives the per-frame rebuild and the per-keystroke renumber
	// (ADR-0176 §SD2 and its 2026-08-09 Keys update).
	nav tree.State
	// outline is this frame's hierarchy, held so the cell callbacks — which
	// run inside tree.Render — can read a node's entry back.
	outline vocabOutline
	// seeded records that the default expansion has been applied, and
	// seededExtras that the extras families have been closed. They are two
	// flags because they become possible on different frames: the roster is
	// there on the first, the extras only once the probe answers.
	seeded, seededExtras bool
	// ready is this frame's probe state, held for the cell callbacks, which
	// run inside tree.Render and take only a row.
	ready bool
	// paneW / paneH are the last size the pane probe reported. It answers one
	// frame late and not at all on the first.
	paneW, paneH float32
}

// renderVocabularyTab draws the tab body.
//
// There is deliberately no ScrollArea around this: the tree renders through an
// etable, which brings its own scroll and culls the rows outside it. Wrapping
// it in one gives the tab two scrollbars and hands the table an unbounded
// parent — the case its auto-fit cap exists for. The tab is registered without
// scrollTab for that reason (play_tabs.go).
func (inst *PlayApp) renderVocabularyTab() {
	declared := vocabDeclared(sqlvocab.Default)
	installed, ready := inst.vocab.demand()
	vocabMarkInstalled(declared, installed)
	// Extras join the corpus BEFORE the filter is computed, so a filtered
	// vocabulary still lists what the endpoint carries and no roster claims.
	// They used to be filtered against an accepted set built without them,
	// which silently emptied the population the moment anything was typed.
	extras := vocabExtras(installed, declared)
	entries := append(declared, extras...)

	inst.renderVocabFilterRow(entries)
	inst.renderVocabStatus(installed, ready)

	st := &inst.vocabTab
	st.outline = buildVocabOutline(entries, func(e vocabEntry) bool { return vocabAccepted(st, e) })
	inst.seedVocabExpansion()

	// Probed HERE: after the chrome above and before the table, because the
	// rect a probe reports is the room left for the NEXT widget. Placed after
	// the table it would size the table against its own output.
	if w, h, ok := c.CapturePaneSize(inst.ids.PrepareHighEntropy(vocabPaneProbeSalt).Derive()); ok {
		st.paneW, st.paneH = w, h
	}
	inst.renderVocabTree(ready)
}

// seedVocabExpansion opens the outline: everything expanded, except the
// families holding what no roster declares.
//
// Those are the one population this build does not bound — a live endpoint has
// reported hundreds — and a reader arriving at the tab is looking for what this
// build offers, not for what somebody once installed by hand.
//
// Each half is applied ONCE, so a reader who opens a family keeps it open, and
// they are tracked separately because they become possible on different
// frames: the declared roster is there on the first, and the extras families
// exist only once the probe has answered. Closing them on the first frame and
// never looking again would have closed nothing at all.
//
// A collapsed family stays collapsed under a filter, deliberately: its row
// still shows how many of its members matched, so nothing a filter finds is
// invisible, and expanding everything on a keystroke would throw away a
// collapse the reader chose.
func (inst *PlayApp) seedVocabExpansion() {
	st := &inst.vocabTab
	if st.outline.Len() == 0 || (st.seeded && st.seededExtras) {
		return
	}
	// Bind before writing by node index: until the State is bound to THIS
	// build, an index means whatever the previous one called it (ADR-0176's
	// 2026-08-09 update — the sharp edge of keyed state).
	st.nav.Bind(st.outline.Tree)
	if !st.seeded {
		st.seeded = true
		st.nav.SetDefaultExpanded(true)
	}
	if st.seededExtras {
		return
	}
	closed := vocabExtraFamilies()
	for node, n := range st.outline.Nodes {
		if n.Kind != vocabNodeFamily {
			continue
		}
		for _, fam := range closed {
			if st.outline.Tree.Labels[node] == fam {
				st.nav.SetExpanded(int32(node), false)
				st.seededExtras = true
			}
		}
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
//
// It also recomputes when the corpus SIZE changed, which is how a filter typed
// before the probe answered comes to include the extras that answer brought:
// the accepted set is a function of the entries as well as of the query, and
// the probe delivers its half a frame or more later.
func (inst *PlayApp) renderVocabFilterRow(entries []vocabEntry) {
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
	if q == st.query && len(entries) == st.corpus {
		return
	}
	st.query, st.corpus = q, len(entries)
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
	st.accepted = make(map[string]bool, len(entries))
	for _, e := range entries {
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

// renderVocabTree draws the outline: population, family, function, with the
// endpoint mark, the Insert action and the doc each in their own column.
func (inst *PlayApp) renderVocabTree(ready bool) {
	st := &inst.vocabTab
	st.ready = ready

	h := st.paneH
	if h <= 0 {
		h = vocabFallbackHeight
	}
	docW := vocabDocColMinWidth
	if w := st.paneW - vocabCallColWidth - vocabMarkColWidth - vocabInsertColWidth - vocabScrollbarGutter; w > docW {
		docW = w
	}

	tree.Render(tree.Input{
		Ids:      inst.ids,
		ScopeKey: "vocab",
		Tree:     st.outline.Tree,
		State:    &st.nav,
		Outline: tree.Column{
			Header:    "call",
			Width:     vocabCallColWidth,
			Resizable: true,
			Cell:      inst.renderVocabCallCell,
		},
		Columns: []tree.Column{
			// Untitled: the column is one glyph wide and a header wider than
			// its column is clipped, which reads as a rendering fault rather
			// than as a narrow column. See vocabMarkColWidth.
			{Header: "", Width: vocabMarkColWidth, Cell: inst.renderVocabMarkCell},
			{Header: "", Width: vocabInsertColWidth, Cell: inst.renderVocabInsertCell},
			{Header: "what it does", Width: docW, Cell: inst.renderVocabDocCell},
		},
		MaxHeight: h,
	})
}

// renderVocabCallCell draws the outline column. A section or family row draws
// its own label and its count; a function row draws its call prototype in the
// monospace face, which is what the aligned left edge is for.
//
// Every label is emitted Selectable(false): a selectable label senses
// click-and-drag and is registered after the row's own sense region, so it
// would sit over it and swallow clicks on its own rect (ADR-0176 §SD7).
func (inst *PlayApp) renderVocabCallCell(r tree.Row) {
	n := inst.vocabNode(r.Node)
	if n == nil {
		return
	}
	label := inst.vocabTab.outline.Tree.Labels[r.Node]
	switch n.Kind {
	case vocabNodeFunc:
		// Hovered as well as truncated: the widest declared prototype is
		// wider than any column this pane can spare, and the parameter list
		// is the half that gets cut. The name — what a reader scans — is the
		// half that survives, being the prefix.
		for range c.HoverText(label).KeepIter() {
			c.LabelAtoms(c.Atoms().BeginRichText(label).Monospace().End().Keep()).
				Selectable(false).Truncate().Send()
		}
	default:
		atoms := c.Atoms().BeginRichText(label)
		if n.Kind == vocabNodeSection {
			atoms = atoms.Strong()
		}
		a := atoms.End()
		// The count rides in the same atom run rather than in a second label,
		// so the truncating label above it cannot push it out of the cell.
		a = a.BeginRichTextColored(vocabFgMuted, vocabBgNone, "  "+strconv.Itoa(n.Count)).Small().End()
		// Hovered for the same reason a function row is: a section title is
		// the longest thing in this column and the indent leaves it least
		// room.
		for range c.HoverText(label).KeepIter() {
			c.LabelAtoms(a.Keep()).Selectable(false).Truncate().Send()
		}
	}
}

// renderVocabMarkCell draws one glyph: whether this endpoint carries the name.
//
// Only that. Everything the endpoint has to say in WORDS — the missing
// verdict, the extra and reserved markers, §SD6's expansion-dependency note —
// prefixes the doc line instead (vocabDocNote), which is where there is room
// for a sentence.
//
// The split also un-hides a fact the single column could not hold. The
// dependency note used to REPLACE the mark, being strictly more specific, and
// that cost the row's own installed state on exactly the rows where it is most
// worth knowing. The two now sit in different columns and neither displaces
// the other.
func (inst *PlayApp) renderVocabMarkCell(r tree.Row) {
	n := inst.vocabNode(r.Node)
	if n == nil || n.Kind != vocabNodeFunc {
		return
	}
	mark, weak := vocabRowMark(n.Entry, n.Where, inst.vocabTab.ready)
	glyph, hover := vocabMarkGlyph(mark)
	if glyph == "" {
		return
	}
	fg := vocabFgMissing
	switch {
	case weak:
		fg = vocabFgMuted
	case mark == vocabMarkExtra:
		fg = vocabFgExtra
	}
	// Hovered because the column has no header to say what the glyph means —
	// 24 points cannot hold one. This is the header, per row.
	for range c.HoverText(hover).KeepIter() {
		c.LabelAtoms(c.Atoms().BeginRichTextColored(fg, vocabBgNone, glyph).Small().End().Keep()).
			Selectable(false).Truncate().Send()
	}
}

// renderVocabInsertCell draws the per-row Insert action, the seam Snippets
// uses. A button legitimately takes the pointer over its own rect, which
// costs that rect's row selection — 62 points of a row (ADR-0176 §SD7).
//
// Small() because a tree row is 22 points and a default button is about 25:
// the overflow does not centre, it hangs off the bottom of the row and through
// the selection outline (ADR-0176 §SD12's height budget, on [tree.Column.Cell]).
func (inst *PlayApp) renderVocabInsertCell(r tree.Row) {
	n := inst.vocabNode(r.Node)
	if n == nil || n.Kind != vocabNodeFunc {
		return
	}
	if c.Button(inst.ids.PrepareSeq(vocabInsertIDBase+uint64(r.Node)),
		c.Atoms().Text("Insert").Keep()).Small().SendResp().HasPrimaryClicked() {
		inst.InsertSqlAtCaret(n.Entry.call())
	}
}

// renderVocabDocCell draws the one-line doc the declared roster carries, or a
// section's blurb — what membership in that population means for a user — with
// this endpoint's verdict on the row as a coloured prefix ahead of it.
//
// Truncated with the full text on hover, which is what a fixed-height tree row
// costs (ADR-0176 §SD12). HoverText must wrap exactly ONE widget; wrapping a
// multi-child layout renders nothing at all, silently — which is why the note
// and the doc are two runs of ONE label rather than two labels in a Horizontal.
//
// The runs carry their own separator: atoms are laid out with no gap between
// them, so a space between two runs has to be inside one of them (the same
// reason the call cell's count is prefixed with two).
func (inst *PlayApp) renderVocabDocCell(r tree.Row) {
	n := inst.vocabNode(r.Node)
	if n == nil {
		return
	}
	var text string
	switch n.Kind {
	case vocabNodeSection:
		text = vocabWhereBlurb(n.Where)
	case vocabNodeFunc:
		text = n.Entry.Doc
	}
	note, full, fg := "", "", vocabFgMuted
	if n.Kind == vocabNodeFunc {
		mark, _ := vocabRowMark(n.Entry, n.Where, inst.vocabTab.ready)
		note, full, fg = vocabDocNote(n.Entry, mark, inst.vocabTab.ready)
	}
	// A doc line that already opens with the note says it once, not twice.
	// `tsMotifs` reads "reserved for the motif-set ADR", and prefixing that
	// with `reserved` produced "reserved — reserved for the motif-set ADR".
	// Dropping the prefix costs its colour, which is the same muted grey as
	// the doc in the one case this fires on today — and where it would not be,
	// the glyph column is still carrying that colour.
	if note != "" && strings.HasPrefix(strings.ToLower(text), strings.ToLower(note)) {
		note = ""
	}
	if text == "" && note == "" {
		return
	}
	atoms := c.Atoms()
	if note != "" {
		atoms = atoms.BeginRichTextColored(fg, vocabBgNone, note).Small().End()
		if text != "" {
			atoms = atoms.BeginRichTextColored(vocabFgMuted, vocabBgNone, " — ").Small().End()
		}
	}
	if text != "" {
		atoms = atoms.BeginRichTextColored(vocabFgMuted, vocabBgNone, text).Small().End()
	}
	// The hover expands the note into the sentence it abbreviates, since the
	// note is the half most likely to be truncated away — it leads the line.
	hover := text
	switch {
	case full != "" && text != "":
		hover = full + " — " + text
	case full != "":
		hover = full
	}
	for range c.HoverText(hover).KeepIter() {
		c.LabelAtoms(atoms.Keep()).Selectable(false).Truncate().Send()
	}
}

// vocabNode resolves a row's node against this frame's outline. The widget
// hands a cell whatever it flattened; a nil here means the outline moved under
// it, which is a host bug rather than something to draw.
func (inst *PlayApp) vocabNode(node int32) *vocabNode {
	nodes := inst.vocabTab.outline.Nodes
	if node < 0 || int(node) >= len(nodes) {
		return nil
	}
	return &nodes[node]
}

// The mark vocabulary. These are the WORDS for a row's endpoint verdict, and
// they are still drawn as words by the completion panel, whose list has room
// for them. The tree's own column draws vocabMarkGlyph instead.
const (
	vocabMarkUnknown  = "?"
	vocabMarkPresent  = "✓"
	vocabMarkMissing  = "MISSING"
	vocabMarkExtra    = "extra"
	vocabMarkReserved = "reserved"
)

// vocabMarkGlyph renders a [vocabRowMark] verdict for the one-glyph endpoint
// column, with the sentence its hover says. "" for a verdict the column has
// nothing to say about, which the caller draws as an empty cell.
//
// Phosphor rather than the text marks, and Phosphor for ALL of them rather
// than keeping ✓ and reaching for the font only where a glyph is missing. Two
// reasons, and the second is why the column is not half text:
//
//   - ✗ (U+2717) is in scripts/ci/glyph-baseline.txt as accepted tofu: no font
//     this client loads can draw it. ✓ (U+2713) is absent from that file and
//     does render, so the obvious pairing has one half that works and one half
//     that is an empty box.
//   - a mark drawn from the text face and its neighbour drawn from the icon
//     face are two different ems on one column of glyphs. That is the defect
//     the tree widget's own disclosure glyphs were moved to Phosphor for.
//
// The reserved marker earns no glyph: it says a name this build does not
// implement refuses rather than travelling, which is a fact about the BUILD.
// This column is about the endpoint, and a client row's cell is empty by
// design. It keeps its words, in the doc column.
func vocabMarkGlyph(mark string) (glyph string, hover string) {
	switch mark {
	case vocabMarkUnknown:
		return icons.PhQuestion, "not asked yet — the endpoint has not said which functions it carries"
	case vocabMarkPresent:
		return icons.PhCheck, "installed on this endpoint"
	case vocabMarkExtra:
		// A check, not a warning: an extra is on the endpoint — that is how
		// the probe found it. What is unusual about it is its provenance, and
		// the colour plus the doc-column note carry that.
		return icons.PhCheck, "installed on this endpoint, and declared by no roster in this build"
	case vocabMarkMissing:
		return icons.PhX, "not installed on this endpoint"
	}
	return "", ""
}

// vocabDocNote is the endpoint's verdict on a row in words, as the coloured
// prefix the doc line carries — everything the mark column used to spend 96
// points saying, beside the sentence it qualifies.
//
// short is drawn and truncates with the rest of the line; full is what the
// hover expands it into. A row with nothing to report returns "", which is the
// common case on a provisioned endpoint and draws the doc line alone.
//
// The dependency note still wins over the row's own verdict (ADR-0174 §SD6:
// it is strictly more specific). That precedence used to cost the row's own
// installed state, because one column held both; the glyph column reports it
// independently now, so nothing is displaced. It also names every missing
// dependency rather than the first — the reason it named one was the width.
func vocabDocNote(e vocabEntry, mark string, ready bool) (short string, full string, fg color.Color) {
	// Only when something is actually missing. Listing what an expansion needs
	// on every endpoint that has it would be noise on every row; naming it on
	// the endpoint where the call will fail is the whole point (§SD6).
	if ready && len(e.MissingDeps) > 0 {
		deps := strings.Join(e.MissingDeps, ", ")
		return "needs " + deps, "expands into " + deps + " — MISSING on this endpoint", vocabFgMissing
	}
	switch mark {
	case vocabMarkMissing:
		return vocabMarkMissing,
			"not installed on this endpoint — provisioning it is the fix, not a different query",
			vocabFgMissing
	case vocabMarkExtra:
		return vocabMarkExtra,
			"on this endpoint and declared by no roster in this build",
			vocabFgExtra
	case vocabMarkReserved:
		return vocabMarkReserved,
			"reserved by the vocabulary and not implemented — the call refuses rather than travelling",
			vocabFgMuted
	}
	return "", "", vocabFgMuted
}

// vocabRowMark is the row's endpoint verdict and whether it renders recessed.
//
// Only the server section can be missing, and only once the probe has
// answered: before that every server row is unmarked rather than marked
// present, because "we have not asked yet" and "it is there" are different
// facts and only one of them is known.
func vocabRowMark(e vocabEntry, where sqlvocab.WhereE, ready bool) (mark string, weak bool) {
	switch {
	case where != vocabServer && !e.Available:
		return vocabMarkReserved, true
	case where != vocabServer:
		return "", false
	case !ready:
		return vocabMarkUnknown, true
	case !e.Declared:
		return vocabMarkExtra, false
	case e.Installed:
		return vocabMarkPresent, true
	}
	return vocabMarkMissing, false
}
