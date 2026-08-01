package play

// The Docs pane's render half: the installed DocsSourceI's documentation for
// whatever the caret is pointing at, as a markdown view.
//
// It is a TOOL pane, not a result panel — it registers with a nil PanelI and
// reads nothing from the query result. Its input is the editor's published
// caret entity (ADR-0147 §SD2), which is why it needs no analysis of its own:
// the editor already knows what the caret is on, and a second derivation here
// would eventually disagree with the first about which frame it belonged to.

import (
	"fmt"
	"strings"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// docsPaneState is the pane's own view state — what it is showing and how it
// got there. Kept separate from docsDriver, which owns the data.
type docsPaneState struct {
	// follow makes the pane track the caret. Turning it off pins whatever is
	// on screen, which is what a reader wants while typing a call out from the
	// signature they are reading.
	follow bool
	// manual is the lookup box's buffer, which overrides the caret while
	// non-empty — the way to reach a name that is not in the buffer yet.
	manual string
	// shown is the name currently rendered, and shownKind which of its kinds.
	// They are held rather than re-derived so the pane stays put when the
	// caret crosses something undocumented, instead of blanking on every
	// keyword between two names.
	shown     string
	shownKind string
	// lastMiss is the most recent name that resolved to nothing, so the pane
	// can say so without discarding what it is showing.
	lastMiss string

	// nav is a followed link's ranked candidate names, best first, pending
	// resolution; navURL is the link's original target, kept so a link into
	// something this server does not document still has somewhere to go.
	// Both empty when no navigation is in flight.
	nav    []string
	navURL string
	// back is the names visited before the current one, most recent last.
	back []string

	// scrolled is the name whose body the ScrollArea is currently positioned
	// in. A page reached by following a link would otherwise inherit the
	// previous page's scroll offset and open part-way down — worst on the
	// short entries the corpus is full of, where the offset can be past the
	// whole body and the pane looks empty.
	scrolled string
}

// docsFollowDefault is true: a pane that has to be switched on before it does
// anything reads as broken.
func newDocsPaneState() *docsPaneState { return &docsPaneState{follow: true} }

// renderDocsTab is the Docs dock tab body.
func (inst *PlayApp) renderDocsTab() {
	ids := inst.ids
	s := inst.docsPane

	// Header: the follow toggle and the manual lookup box, always drawn so the
	// pane never reflows around them.
	for range c.Horizontal().KeepIter() {
		if len(s.back) > 0 {
			if c.Button(ids.PrepareStr("docsBack"), c.Atoms().Text("←").Keep()).
				SendResp().HasPrimaryClicked() {
				s.shown = s.back[len(s.back)-1]
				s.back = s.back[:len(s.back)-1]
				s.shownKind, s.nav, s.navURL, s.lastMiss = "", nil, "", ""
			}
		}
		c.Checkbox(ids.PrepareStr("docsFollow"), s.follow, "Follow caret").
			SendRespVal(&s.follow)
		c.Label("look up").Send()
		c.TextEdit(ids.PrepareStr("docsManual"), s.manual, false).
			HintText("name").
			SendRespVal(&s.manual)
		if s.manual != "" {
			if c.Button(ids.PrepareStr("docsManualClear"), c.Atoms().Text("×").Keep()).
				SendResp().HasPrimaryClicked() {
				s.manual = ""
			}
		}
	}

	if inst.docs == nil || inst.docs.source == nil {
		for rt := range c.RichTextLabel("No client in this session — documentation lookup is unavailable.") {
			rt.Small().Weak()
		}
		return
	}

	// The editor already knows what the caret is on (ADR-0147 §SD2); the pane
	// reads it rather than deriving a second answer from the same buffer.
	er := inst.editor.Result()
	res := inst.resolveDocs(docsCandidates(er.Entity, er.EntityOk))

	// The body.
	switch {
	case s.shown == "":
		for rt := range c.RichTextLabel(inst.docs.source.EmptyHint()) {
			rt.Small().Weak()
		}
		return
	case res == nil:
		for rt := range c.RichTextLabel("Looking up " + s.shown + "…") {
			rt.Small().Weak()
		}
		return
	case res.err != nil:
		inst.renderDocsError(res.err)
		return
	case len(res.entries) == 0:
		for rt := range c.RichTextLabel(fmt.Sprintf("No documentation for %q on this endpoint.", s.shown)) {
			rt.Small().Weak()
		}
		// A followed link into something this server does not document is the
		// one case where leaving for a browser is the right answer, so the
		// target it named is still reachable.
		if s.navURL != "" {
			c.HyperlinkTo("open "+s.navURL, inst.docs.source.AbsoluteURL(s.navURL)).OpenInNewTab(true).Send()
		}
		return
	}

	entry := inst.pickDocsKind(res.entries)
	if entry == nil {
		return
	}
	if s.lastMiss != "" {
		for rt := range c.RichTextLabel(fmt.Sprintf("(nothing for %q — still showing %s)", s.lastMiss, entry.Name)) {
			rt.Small().Weak()
		}
	}
	c.Separator().Send()

	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		// Emitted before the body, so the op targets the cursor at the top of
		// the document: a page change scrolls back to the start exactly once.
		if s.scrolled != entry.Name {
			c.ScrollToCursor(0)
			s.scrolled = entry.Name
		}
		// IdScope isolates the document's derived widget ids (markdown.Doc's
		// documented invariant) so the pane cannot collide with the Snippets
		// tab or the Help center rendering another document the same frame.
		for range c.IdScope(ids.PrepareStr("docsBody")) {
			// Links the source itself documents are followed in place; the
			// rest stay browser hyperlinks (DocsSourceI.LinkClaimed). The
			// action filter is what keeps a ```response block of query
			// output from advertising an Insert (sqlBlockActionable).
			for act := range entry.rendered().RenderActionsN(ids, snippetActionLabels,
				markdown.WithLinkRouter(inst.docs.source.LinkClaimed, inst.followDocsLink),
				markdown.WithCodeActionFilter(sqlBlockActionable)) {
				switch act.Button {
				case snippetButtonInsert:
					inst.InsertSqlAtCaret(act.Text)
				case snippetButtonReplace:
					inst.ReplaceSql(act.Text)
				}
			}
		}
		if entry.Source != "" {
			c.Separator().Send()
			for rt := range c.RichTextLabel("defined in " + entry.Source) {
				rt.Small().Weak().Monospace()
			}
		}
	}
}

// resolveDocs decides what the pane shows this frame and returns its result,
// nil meaning "not answered yet".
//
// Exactly ONE docsDriver.lookup call per frame, whatever path is taken: the
// debounce is a single slot, so a second call naming a different entity would
// restart the first's timer and neither would ever ship. Candidates already in
// the cache are screened with docsDriver.cached, which arms nothing.
//
// The manual box wins whenever it has content — it is an explicit act, and it
// must not be fought by the caret moving underneath it. Otherwise the caret's
// candidates are tried in order (the name under it, then the calls enclosing
// it) and the first one with documentation wins. That walk is what makes the
// pane useful mid-expression: with the caret inside `toHour(|)` the name under
// it is nothing, but the call enclosing it is what the reader is asking about.
//
// `cands` is that ranked list — see docsCandidates. Taking it as an argument
// rather than reading the editor here is what makes the walk table-testable
// without a frame.
func (inst *PlayApp) resolveDocs(cands []string) (res *docsResult) {
	s := inst.docsPane
	if manual := strings.TrimSpace(s.manual); manual != "" {
		if manual != s.shown {
			s.shown, s.shownKind, s.lastMiss = manual, "", ""
		}
		res, _ = inst.docs.lookup(manual)
		return
	}
	// A followed link, still being resolved. Its candidates are walked the
	// same way the caret's are — one lookup per frame — because the reason is
	// the same: a name may simply not be documented, and the next candidate is
	// the better guess rather than a failure.
	if len(s.nav) > 0 {
		for _, cand := range s.nav {
			hit := inst.docs.cached(cand)
			if hit == nil {
				res, _ = inst.docs.lookup(cand)
				if res == nil {
					s.shown = cand // so the "Looking up …" line names it
					return
				}
				hit = res
			}
			if len(hit.entries) > 0 || hit.err != nil {
				s.shown, s.shownKind, s.lastMiss = cand, "", ""
				// navURL survives a successful resolve only long enough for an
				// errored lookup to still offer the original page.
				s.nav = nil
				if hit.err == nil {
					s.navURL = ""
				}
				return hit
			}
		}
		// Nothing the link could have meant is documented here. Keep the URL
		// so the pane can still offer the page it pointed at.
		s.shown, s.shownKind = s.nav[0], ""
		s.nav = nil
		return inst.docs.cached(s.shown)
	}

	if !s.follow {
		// Pinned: keep serving whatever was resolved, and finish a lookup that
		// was still in flight when the toggle went off.
		if hit := inst.docs.cached(s.shown); hit != nil {
			return hit
		}
		res, _ = inst.docs.lookup(s.shown)
		return
	}

	for _, cand := range cands {
		hit := inst.docs.cached(cand)
		if hit == nil {
			// The first candidate we know nothing about is the one to pursue.
			// Naming it as the target only when the pane has nothing better to
			// show keeps a resolved page up while the fetch runs.
			if s.shown == "" {
				s.shown, s.shownKind = cand, ""
			}
			res, _ = inst.docs.lookup(cand)
			if res != nil && (res.err != nil || len(res.entries) > 0) {
				s.shown, s.shownKind, s.lastMiss = cand, "", ""
				return
			}
			if s.shown == cand {
				return
			}
			return inst.docs.cached(s.shown)
		}
		if hit.err != nil || len(hit.entries) > 0 {
			if cand != s.shown {
				s.shown, s.shownKind = cand, ""
			}
			s.lastMiss = ""
			return hit
		}
		// A cached miss: this name has no documentation, so try what encloses it.
	}
	if len(cands) == 0 {
		// Nothing under the caret — keep what is up rather than blanking on
		// every literal and operator the caret crosses.
		return inst.docs.cached(s.shown)
	}
	// Every candidate resolved to nothing.
	if s.shown == "" {
		s.shown, s.shownKind, s.lastMiss = cands[0], "", ""
		return inst.docs.cached(s.shown)
	}
	s.lastMiss = cands[0]
	return inst.docs.cached(s.shown)
}

// pickDocsKind draws the kind selector when a name carries more than one, and
// returns the entry to render.
//
// ~70 names do: `Array` is a data type and an aggregate-function combinator,
// `JSON` a data type and a format, `Dictionary` a table engine and a database
// engine. Which one the reader meant is not knowable from the buffer — the
// lexer sees one identifier either way — so it is offered rather than guessed.
func (inst *PlayApp) pickDocsKind(entries []docEntry) (entry *docEntry) {
	s := inst.docsPane
	if len(entries) == 1 {
		return &entries[0]
	}
	for range c.Horizontal().KeepIter() {
		for i := range entries {
			selected := entries[i].Kind == s.shownKind ||
				(s.shownKind == "" && i == 0)
			if c.Button(inst.ids.PrepareStr("docsKind-"+entries[i].Kind),
				c.Atoms().Text(entries[i].Kind).Keep()).
				Selected(selected).
				SendResp().HasPrimaryClicked() {
				s.shownKind = entries[i].Kind
			}
		}
	}
	for i := range entries {
		if entries[i].Kind == s.shownKind {
			return &entries[i]
		}
	}
	// The stored kind is not among this name's — the caret moved to a
	// different name. First row wins, which the ORDER BY made the exact-case
	// match.
	return &entries[0]
}

// renderDocsError explains a failed lookup, in the installed source's own
// words (DocsSourceI.ExplainError) — a source knows which of its own
// failures a reader can act on, and which are just "the lookup failed".
func (inst *PlayApp) renderDocsError(err error) {
	for rt := range c.RichTextLabel(inst.docs.source.ExplainError(err)) {
		rt.Small().Weak()
	}
}

// followDocsLink is the markdown widget's link-click callback: it records the
// intent and lets the next resolution act on it, rather than re-entering the
// render from inside a callback fired mid-document.
//
// Following a link pins the pane (Follow goes off). A link is a deliberate act
// and the caret is almost certainly still sitting where it was, so leaving
// Follow on would snap the pane back on the very next frame — the click would
// look like it did nothing.
func (inst *PlayApp) followDocsLink(label string, url string) {
	cands := inst.docs.source.LinkCandidates(label, url)
	if len(cands) == 0 {
		return
	}
	s := inst.docsPane
	if s.shown != "" {
		s.back = append(s.back, s.shown)
	}
	s.follow = false
	// The lookup box outranks everything, so a stale entry in it would drag
	// the pane straight back to the name the reader just navigated away from —
	// the click would land, resolve, and be undone on the same frame.
	s.manual = ""
	s.nav, s.navURL = cands, url
	s.lastMiss = ""
}
