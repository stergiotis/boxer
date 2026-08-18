package play

// play_completion_panel.go is the Completion tool tab (ADR-0190): what may
// stand where the caret is, with the match state shown on the rows and, for an
// exact match, on the token itself.
//
// It is the Vocabulary tab's sibling, not its replacement. The tab is the
// browse-and-reconcile view — every name this build declares, sectioned by
// where it runs, marked against what the endpoint carries; the pane is the
// glance at the caret. They share a substrate: the same table widget, the same
// muted cell tones, the same provisioning marks, and — since ADR-0190 §SD4 —
// the same registry of declared functions.
//
// The engine runs every frame the editor renders, whether or not this tab is
// open, because the editor's tint reads the same result (§SD9). It is cheap by
// construction: the site walk is a lex-tier scan, every provider here is an
// in-process registry, and nothing on this path parses.

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/db/clickhouse/chtype"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlcomplete"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sqleditor"
)

// completionPaneProbeSalt namespaces this pane's size probe. NOT
// CaptureAvailableSize, which is one process-wide slot the frame's last capture
// wins.
const completionPaneProbeSalt uint64 = 0x704ab1e5a1700002

// completionState is the tab's cross-frame state plus this frame's answer.
type completionState struct {
	pane   sqleditor.Pane
	engine *sqlcomplete.Engine
	// result is recomputed every frame from the editor's published site, so
	// the tint and the pane never disagree about which caret they describe.
	result sqlcomplete.Result
	// typed and atEnd travel with result: the pane needs them to compute a
	// suffix, and they are properties of the caret rather than of the answer.
	typed string
	atEnd bool

	// catalog is the endpoint half of the providers, one probe per question.
	catalog *catalogProbe

	// findings is the off-caret validation of the buffer's literals (§SD9),
	// memoised by the buffer it describes. Recomputed only when the buffer
	// changes, because it walks every literal in the statement while the site
	// walk is per caret.
	findings    []sqlcomplete.Finding
	findingsFor string

	// result is memoised by the request it answers: the engine resolves its
	// providers on every Complete, and the editor asks once a frame whether
	// or not the caret moved. resultValid is separate from a zero key because
	// an empty buffer at caret 0 is a real request with a real (silent)
	// answer.
	resultKey   completionKey
	resultValid bool

	// exprItems is the composed expression-position answer — the source's
	// columns, this build's declared vocabulary, and every function the
	// endpoint carries — memoised by the table it is for. Composing it walks
	// and sorts a few thousand names, which is not a per-frame cost.
	exprItems []sqlcomplete.Item
	exprTable string
	exprGen   uint64
	exprValid bool

	paneW, paneH float32
}

// completionKey identifies the request a memoised answer belongs to.
//
// The site is not in here because it does not need to be: the lex-tier walk
// derives it from the buffer and the caret alone (ADR-0147 §SD2), so those two
// fields pin it. scope is compared by identity — a new sentinel parse
// publishes a new one. gen catches the remaining input, a probe answering.
type completionKey struct {
	buffer string
	caret  int
	scope  *sqlcomplete.Scope
	gen    uint64
}

// refreshCompletion recomputes this frame's answer from the editor's site.
//
// Called from renderSqlEditor right after Bind, for the reason every other
// caret-derived producer is: one caret per frame, or the overlays and the pane
// describe different buffers.
func (inst *PlayApp) refreshCompletion(res sqleditor.Result) {
	st := &inst.completion
	if st.engine == nil {
		st.catalog = newCatalogProbe(inst.client)
		st.engine = &sqlcomplete.Engine{
			Vocab:     sqlvocab.Default,
			Providers: inst.completionProviders(),
			// grammar1 takes `expr.name` on a named tuple since ADR-0190
			// §SD11, so the fields of a call receiver are a spelling that
			// parses, canonicalises and runs — which is the condition §SD7
			// gates that receiver on.
			NamedTupleAccess: true,
		}
	}
	st.typed = res.Site.PartialText
	st.atEnd = res.Site.CaretAtPartialEnd()
	// The off-caret validation walks every literal, so it is keyed on the
	// buffer rather than on the caret. The caret is passed anyway: the token
	// being typed is excluded, because a name half written is not a wrong one.
	if st.findingsFor != res.Buffer {
		st.findings = st.engine.Validate(res.Buffer, res.Scope, res.Caret)
		st.findingsFor = res.Buffer
	}
	key := completionKey{
		buffer: res.Buffer,
		caret:  res.Caret,
		scope:  res.Scope,
		gen:    inst.completionProbeGen(),
	}
	if st.resultValid && st.resultKey == key {
		return
	}
	st.result = st.engine.Complete(sqlcomplete.Request{
		Site:      res.Site,
		Scope:     res.Scope,
		Statement: res.Buffer,
		Caret:     res.Caret,
	})
	st.resultKey = key
	st.resultValid = true
}

// completionProbeGen counts what the completion's probes have banked.
//
// Every probe behind these providers is write-once per session: the catalog
// memoises one entry per question it has asked and never evicts, and the
// vocabulary probe fills its container once and short-circuits after. So this
// only ever increases, and an answer stamped with it is stale exactly when a
// probe has landed since — which is the one thing that can change a
// provider's reply for an unchanged buffer and caret.
func (inst *PlayApp) completionProbeGen() (gen uint64) {
	if c := inst.completion.catalog; c != nil {
		gen = uint64(len(c.memo)) + uint64(len(c.types))
	}
	if v := inst.vocab; v != nil && v.installed != nil {
		gen++
	}
	return
}

// completionProviders wires the in-process registries this build can answer
// from (ADR-0190 §SD12's A rows).
//
// A domain with no provider here is silent, and the pane says so — that is
// §SD1's posture, not an omission to fill in with a guess. What is missing
// today is what needs a source this build does not have in process: the leeway
// sections of the bound table (ADR-0147 §SD9's schema reader), the membership
// registry, the identity tag registry, and everything under `system.*`.
func (inst *PlayApp) completionProviders() (p sqlcomplete.Providers) {
	p.ComponentKinds = func() (items []sqlcomplete.Item, ready bool) {
		kinds := componentsql.Default.Kinds()
		items = make([]sqlcomplete.Item, 0, len(kinds))
		for _, k := range kinds {
			it := sqlcomplete.Item{Text: k, Kind: sqlcomplete.ItemComponentKind, Source: "components"}
			if b, ok := componentsql.Default.Lookup(k); ok {
				it.Doc = "published by " + b.Store + " over " + b.Table
			}
			items = append(items, it)
		}
		return items, true
	}
	p.ComponentType = func(kind string) (t chtype.Type, ok bool) {
		b, hit := componentsql.Default.Lookup(kind)
		if !hit {
			return
		}
		t, err := b.ProjectionType()
		return t, err == nil
	}
	p.IntrospectionTables = func() (items []sqlcomplete.Item, ready bool) {
		names := introspect.Default.Names()
		aliases := inst.datasetAliases()
		items = make([]sqlcomplete.Item, 0, len(names)+len(aliases))
		for _, n := range names {
			items = append(items, sqlcomplete.Item{Text: n, Kind: sqlcomplete.ItemTable, Source: "introspection"})
		}
		// Ad-hoc dataset aliases are tables no catalogue enumerates: they exist
		// only because this session bound them (ADR-0134 §SD4), which is why
		// the answer is per buffer rather than per build.
		for _, a := range aliases {
			items = append(items, sqlcomplete.Item{
				Text: a, Kind: sqlcomplete.ItemTable, Source: "bound dataset",
				Doc: "an ad-hoc dataset this session bound",
			})
		}
		return items, true
	}
	p.Channels = func() (items []sqlcomplete.Item, ready bool) {
		return membershipChannelItems(""), true
	}
	p.SupportRoles = func() (items []sqlcomplete.Item, ready bool) {
		items = make([]sqlcomplete.Item, 0, len(common.AllColumnRoles))
		for _, r := range common.AllColumnRoles {
			items = append(items, sqlcomplete.Item{
				Text: r.String(), Kind: sqlcomplete.ItemSupportRole, Source: "leeway roles",
			})
		}
		return items, true
	}
	p.Aspects = func() (items []sqlcomplete.Item, ready bool) {
		return aspectItems(), true
	}
	// The extraction family's trailing tokens are one position taking three
	// prefixes in any order, so the answer is their union. `col:` is absent
	// until ADR-0147 §SD9's schema reader can name a section's value columns;
	// deferred: add it there, keyed on the section this call names.
	p.ExtractionTokens = func(section string) (items []sqlcomplete.Item, ready bool) {
		return membershipChannelItems("chan:"), true
	}
	p.Glosses = func() (items []sqlcomplete.Item, ready bool) {
		for g := range gloss.Default().All() {
			items = append(items, sqlcomplete.Item{
				Text: g.MediaType(), Kind: sqlcomplete.ItemGloss, Source: "gloss catalog", Doc: g.Doc(),
			})
		}
		slices.SortFunc(items, func(a sqlcomplete.Item, b sqlcomplete.Item) int {
			return strings.Compare(a.Text, b.Text)
		})
		return items, true
	}
	// The endpoint half (§SD12's B rows). Every one is a probe: off the frame
	// thread, cached, and "not yet" until it answers.
	p.Catalog = sqlcomplete.Catalog{
		Databases: func() ([]sqlcomplete.Item, bool) { return inst.completion.catalog.databases() },
		Tables:    func(db string) ([]sqlcomplete.Item, bool) { return inst.completion.catalog.tables(db) },
		Columns:   func(table string) ([]sqlcomplete.Item, bool) { return inst.completion.catalog.columns(table) },
		ColumnType: func(table string, column string) (chtype.Type, bool) {
			return inst.completion.catalog.columnType(table, column)
		},
		Settings:  func() ([]sqlcomplete.Item, bool) { return inst.completion.catalog.settings() },
		TypeNames: func() ([]sqlcomplete.Item, bool) { return inst.completion.catalog.typeNames() },
		TimeZones: func() ([]sqlcomplete.Item, bool) { return inst.completion.catalog.timeZones() },
		Formats:   func() ([]sqlcomplete.Item, bool) { return inst.completion.catalog.formats() },
		Dictionaries: func() ([]sqlcomplete.Item, bool) {
			return inst.completion.catalog.dictionaries()
		},
	}
	// A free expression position is not one catalogue: a column of the
	// statement's source and a callable name are both valid there, and
	// offering only one of them would be exactly as wrong as offering neither.
	//
	// The vocabulary half is this build's own declared set, which needs no
	// probe; the endpoint's function list rides the Vocabulary tab's lane
	// (§SD12 B5) — one lane, two readers.
	//
	// Memoised on the table it is for, because it is the one provider here
	// whose population is large: the endpoint's function list runs to a few
	// thousand names, and composing plus sorting them is not something a
	// frame should repeat for an answer that cannot have changed. The
	// returned slice is shared with the memo — [sqlcomplete.Engine.Complete]
	// copies before it stamps kinds and insert texts, so no caller writes to
	// it.
	p.Expressions = func(table string) (items []sqlcomplete.Item, ready bool) {
		st := &inst.completion
		gen := inst.completionProbeGen()
		if st.exprValid && st.exprTable == table && st.exprGen == gen {
			return st.exprItems, true
		}
		if table != "" {
			cols, colsReady := inst.completion.catalog.columns(table)
			if !colsReady {
				// Not an answer, so nothing is memoised: the next frame asks
				// again and the probe landing moves gen anyway.
				return nil, false
			}
			items = append(items, cols...)
		}
		items = append(items, inst.completionVocabularyItems()...)
		installed, probeReady := inst.vocab.demandAll()
		if probeReady {
			// Sorted iteration, not a map range: the order a map hands these
			// back is randomised per range, so the sort below used to get a
			// freshly shuffled few-thousand-element input every time it ran
			// and could never recognise one it had already ordered.
			for name := range installed.IterateKeys() {
				items = append(items, sqlcomplete.Item{
					Text: name, Kind: sqlcomplete.ItemFunction, Source: "on this endpoint",
				})
			}
		}
		sortCompletionItems(items)
		st.exprItems = items
		st.exprTable = table
		st.exprGen = gen
		st.exprValid = true
		return items, true
	}
	p.GlossKeys = func(mediaType string) (items []sqlcomplete.Item, ready bool) {
		g, ok := gloss.Default().Lookup(mediaType)
		if !ok {
			return nil, true
		}
		specs := g.Params()
		items = make([]sqlcomplete.Item, 0, len(specs))
		for _, s := range specs {
			doc := s.Doc
			if len(s.Values) > 0 {
				doc = strings.TrimSpace(doc + " — one of " + strings.Join(s.Values, ", "))
			}
			items = append(items, sqlcomplete.Item{
				Text: s.Name, Kind: sqlcomplete.ItemGlossKey, Source: mediaType, Doc: doc,
			})
		}
		return items, true
	}
	return
}

// datasetAliases are the ad-hoc dataset aliases bound in this session.
func (inst *PlayApp) datasetAliases() (out []string) {
	if inst.client == nil {
		return
	}
	return inst.client.DatasetAliases()
}

// membershipChannelItems renders the authorable membership channels — the
// mixed ones excluded, since they carry two lanes and no per-column spelling
// mints two columns (ADR-0181 §SD8).
func membershipChannelItems(prefix string) (items []sqlcomplete.Item) {
	items = make([]sqlcomplete.Item, 0, len(common.AllMembershipSpecs))
	for _, m := range common.AllMembershipSpecs {
		if m.ContainsMixed() || m == common.MembershipSpecNone {
			continue
		}
		items = append(items, sqlcomplete.Item{
			Text: prefix + m.String(), Kind: sqlcomplete.ItemChannel, Source: "leeway channels",
		})
	}
	return
}

// aspectItems are the three aspect vocabularies under the prefixes the
// authoring surface spells them with.
func aspectItems() (items []sqlcomplete.Item) {
	items = make([]sqlcomplete.Item, 0,
		len(encodingaspects.AllAspects)+len(valueaspects.AllAspects)+len(useaspects.AllAspects))
	for _, a := range encodingaspects.AllAspects {
		items = append(items, sqlcomplete.Item{
			Text: "enc:" + a.String(), Kind: sqlcomplete.ItemAspect, Source: "encoding aspects",
		})
	}
	for _, a := range valueaspects.AllAspects {
		items = append(items, sqlcomplete.Item{
			Text: "sem:" + a.String(), Kind: sqlcomplete.ItemAspect, Source: "value aspects",
		})
	}
	for _, a := range useaspects.AllAspects {
		items = append(items, sqlcomplete.Item{
			Text: "use:" + a.String(), Kind: sqlcomplete.ItemAspect, Source: "use aspects",
		})
	}
	return
}

// renderCompletionTab draws the tab body.
//
// No ScrollArea around it, for the Vocabulary tab's reason: the pane's table is
// an etable, which brings its own scroll and culls the rows outside it.
func (inst *PlayApp) renderCompletionTab() {
	st := &inst.completion
	for rt := range c.RichTextLabel(sqleditor.PaneHeading(st.result)) {
		rt.Small().Weak()
	}
	c.Separator().Horizontal().Send()

	// Probed HERE: after the chrome and before the table, because the rect a
	// probe reports is the room left for the NEXT widget.
	if w, h, ok := c.CapturePaneSize(inst.ids.PrepareHighEntropy(completionPaneProbeSalt).Derive()); ok {
		st.paneW, st.paneH = w, h
	}

	st.pane.Render(sqleditor.PaneInput{
		Ids:               inst.ids,
		ScopeKey:          "completion",
		Result:            st.result,
		MaxHeight:         st.paneH,
		Width:             st.paneW,
		Typed:             st.typed,
		CaretAtPartialEnd: st.atEnd,
		OnAccept: func(_ sqlcomplete.Item, suffix string) {
			if suffix != "" {
				inst.InsertSqlAtCaret(suffix)
			}
		},
	})
}

// completionVocabularyItems are the names this build declares, with the marks
// the Vocabulary tab shows.
//
// The marks travel WITH the row rather than filtering it: a function this
// endpoint is missing is still a name the buffer may use — the expansion may
// be client-side, or the endpoint may be about to be provisioned — and hiding
// it would hide the provisioning fact the tab exists to report (§SD8).
func (inst *PlayApp) completionVocabularyItems() (items []sqlcomplete.Item) {
	entries := vocabDeclared(sqlvocab.Default)
	installed, ready := inst.vocab.demand()
	vocabMarkInstalled(entries, installed)
	items = make([]sqlcomplete.Item, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if _, dup := seen[e.Name]; dup {
			continue
		}
		seen[e.Name] = struct{}{}
		it := sqlcomplete.Item{
			Text: e.Name, Kind: sqlcomplete.ItemFunction, Doc: e.Doc, Source: e.Family,
		}
		if mark, _ := vocabRowMark(e, e.Where, ready); mark != "" {
			it.Marks = []string{mark}
		}
		items = append(items, it)
	}
	return
}

// sortCompletionItems orders a composed list by name, case-insensitively so a
// mixed-case corpus does not split into two runs.
//
// The comparison folds in place rather than lowering both operands. This runs
// over the endpoint's whole function list, and a comparator that allocated two
// strings per call made strings.ToLower the largest single allocation source
// in the process — enough garbage that collection, not rendering, was what
// tripped the slow-frame warning.
func sortCompletionItems(items []sqlcomplete.Item) {
	slices.SortFunc(items, func(a sqlcomplete.Item, b sqlcomplete.Item) int {
		return compareFoldThenExact(a.Text, b.Text)
	})
}

// compareFoldThenExact is the completion corpus's total order: case-insensitive
// first, so a mixed-case population stays in one run, then the exact spelling.
//
// The second tier is what keeps it a TOTAL order rather than merely a grouping,
// and that matters beyond sort stability: [vocabProbe] keys a container on this
// comparator, and names that folded together without a tiebreak would be one
// key. ClickHouse registers case-variant aliases as separate functions
// (`substring` and `SUBSTRING` are both in system.functions), so collapsing
// them would drop names the endpoint really carries.
func compareFoldThenExact(a string, b string) int {
	if c := compareFold(a, b); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// compareFold orders a against b the way a bytewise comparison of
// strings.ToLower(a) and strings.ToLower(b) would, without building either.
//
// The two agree because ToLower maps rune for rune — it is Map(unicode.ToLower)
// with no special-casing — and UTF-8 preserves code-point order, so comparing
// lowered runes pairwise reaches the same verdict as comparing the lowered
// encodings byte by byte.
func compareFold(a string, b string) int {
	// What this orders is SQL identifiers, so the ASCII path is the one that
	// normally runs to completion; the rune path picks up from the first byte
	// either side puts out of range, by which point the prefixes fold equal.
	for i := 0; i < len(a) && i < len(b); i++ {
		ca, cb := a[i], b[i]
		if ca >= utf8.RuneSelf || cb >= utf8.RuneSelf {
			return compareFoldRunes(a[i:], b[i:])
		}
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			if ca < cb {
				return -1
			}
			return 1
		}
	}
	return compareLen(a, b)
}

// compareFoldRunes is compareFold's non-ASCII tail.
func compareFoldRunes(a string, b string) int {
	for len(a) > 0 && len(b) > 0 {
		ra, na := utf8.DecodeRuneInString(a)
		rb, nb := utf8.DecodeRuneInString(b)
		ra, rb = unicode.ToLower(ra), unicode.ToLower(rb)
		if ra != rb {
			if ra < rb {
				return -1
			}
			return 1
		}
		a, b = a[na:], b[nb:]
	}
	return compareLen(a, b)
}

// compareLen breaks the tie when one string ran out first, which under a
// prefix-wise comparison is what makes the shorter one sort first.
func compareLen(a string, b string) int {
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

// completionWantsTab reports whether the editor should take Tab this frame
// (ADR-0190 §SD10).
//
// Three conditions, and each one is a case where a captured Tab would be a
// key eaten for nothing: there must be candidates the typed text extends, the
// caret must sit at the end of that text (a suffix insert is only valid
// there), and they must agree on something more than what is already written.
// Otherwise Tab stays a tab character.
func (inst *PlayApp) completionWantsTab() bool {
	st := &inst.completion
	if !st.atEnd || len(st.result.Prefix) == 0 {
		return false
	}
	_, ok := st.result.TabCompletion(st.typed)
	return ok
}

// applyTabCompletion splices what the captured Tab completes to.
func (inst *PlayApp) applyTabCompletion() {
	st := &inst.completion
	if !st.atEnd {
		return
	}
	if suffix, ok := st.result.TabCompletion(st.typed); ok {
		inst.InsertSqlAtCaret(suffix)
	}
}
