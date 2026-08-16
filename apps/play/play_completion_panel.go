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
	"sort"
	"strings"

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

	paneW, paneH float32
}

// refreshCompletion recomputes this frame's answer from the editor's site.
//
// Called from renderSqlEditor right after Bind, for the reason every other
// caret-derived producer is: one caret per frame, or the overlays and the pane
// describe different buffers.
func (inst *PlayApp) refreshCompletion(res sqleditor.Result) {
	st := &inst.completion
	if st.engine == nil {
		st.engine = &sqlcomplete.Engine{
			Vocab:     sqlvocab.Default,
			Providers: inst.completionProviders(),
		}
	}
	st.typed = res.Site.PartialText
	st.atEnd = res.Site.CaretAtPartialEnd()
	st.result = st.engine.Complete(sqlcomplete.Request{
		Site:      res.Site,
		Scope:     res.Scope,
		Statement: res.Buffer,
		Caret:     res.Caret,
	})
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
		sort.Slice(items, func(i, j int) bool { return items[i].Text < items[j].Text })
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
