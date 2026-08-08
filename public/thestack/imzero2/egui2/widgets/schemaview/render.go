package schemaview

import (
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/canonicaltypesummary"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
)

// Input is the per-frame render request. The widget is pure: it renders the
// Model's TableDesc and mutates only the Model's selection / filter.
type Input struct {
	// Ids is the widget id stack supplied by the host (the tour / window
	// scopes each instance).
	Ids *c.WidgetIdStack
	// ScopeKey is retained for callers that embed two instances under one
	// unscoped parent and must disambiguate; the default host path already
	// scopes per instance, so Render does not open its own scope (a nested
	// scope broke egui_ltreeview node-state keying, before this widget moved
	// to CollapsingHeader-based navigation).
	ScopeKey string
	// Model is the inspector state, mutated in place by the navigator.
	Model *Model
	// FillHost tells Render its host already gives it a bounded height, so
	// it must fill that rect rather than floor to dockMinHeight. The floor
	// is a scroll-host device (see dockMinHeight): the standalone gallery is
	// a vertically-unbounded ScrollArea, so without a floor the dock would
	// collapse. Inside a host that is ALREADY bounded and often shorter than
	// the floor — a dock-tab leaf (the play Schema tab) — forcing 620 px
	// overflows the leaf, and the nested dock's tab-bars / separators paint
	// across the neighbouring panes (severe disarray once the section list
	// is scrolled). Bounded hosts set this true; the gallery leaves it false.
	FillHost bool
}

// navTypeFg / navTypeBg tone the terse canonical type trailing a column's name
// in the navigator, sharing the secondary-text role with every other muted
// annotation in the design system (ADR-0031).
var (
	navTypeFg = color.Hex(styletokens.NeutralTextSecondary.AsHex())
	navTypeBg = color.Transparent
)

const (
	navWidth = 340.0 // filter-box width hint inside the (now resizable) navigator pane
	// navOutlineWidth is the outline column's floor, used until the pane probe
	// answers and whenever the pane is narrower than this. Wide enough for a
	// section name and its membership badge.
	navOutlineWidth = 300.0
	// navScrollbarGutter is held back from the measured pane width so the
	// outline column plus egui_table's vertical scrollbar fit without pushing a
	// horizontal one in beneath them. egui_table budgets 16px for it.
	navScrollbarGutter = 18.0

	// Dock tab ids — reserved high so they never collide with anything the
	// host might add. The navigator leaf splits the detail leaf off to its
	// right at navFrac of the width.
	navTabID    uint64 = 1 << 62
	detailTabID uint64 = 1<<62 | 1
	navFrac            = 0.40

	// dockMinHeight floors the dock so it has a bounded rect inside the
	// gallery's scroll host — mappingplanview's idiom. A bounded leaf is also
	// what lets each pane's ScrollArea actually scroll (see renderNavigator).
	dockMinHeight = 620
)

// Render lays the inspector out as a two-leaf dock area: the section navigator
// ("structure") on the left and the decoded detail pane ("detail") on the
// right. Both leaves are draggable / resizable (egui_dock persists the layout)
// and each scrolls independently. The tethered glyph-legend window is rendered
// outside the dock — see renderLegendWindow.
func Render(in Input) {
	m := in.Model
	if m == nil || m.Table == nil {
		return
	}
	scope := legendScope(in.ScopeKey)
	for range c.IdScope(in.Ids.PrepareStr(in.ScopeKey)) {
		// Floor the dock's height only in an unbounded scroll host; a bounded
		// host (FillHost) lets the dock fill its leaf instead of overflowing it.
		if !in.FillHost {
			c.UiSetMinHeight(dockMinHeight)
		}
		for dock := range c.DockArea(in.Ids.PrepareStr("svdock")) {
			root := dock.InitRoot(navTabID)
			dock.Split(root, c.DockRight, navFrac, detailTabID)

			for range dock.Tab(navTabID, "structure") {
				// Header (title + legend toggle + filter) is pinned above the
				// outline so the filter stays usable while a long schema
				// scrolls. There is no ScrollArea around the outline: the tree
				// renders through an etable, which brings its own scroll and
				// culls to it. Wrapping it in one would give the pane two
				// scrollbars and hand the table an unbounded parent, which is
				// the case its 400px auto-fit cap exists for.
				//
				// The probe sits after the header so it measures what is left
				// for the outline, and answers one frame late — see
				// renderSections on the first frame's fallback.
				renderNavHeader(in.Ids, m, scope)
				availW, availH, _ := c.CapturePaneSize(c.ProbeSeq(scope, "nav-pane"))
				renderSections(in.Ids, m, availW, availH)
			}
			for range dock.Tab(detailTabID, "detail") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					renderDetail(in.Ids, m)
				}
			}
		}
		renderLegendWindow(in.Ids, m, scope)
	}
}

// renderNavHeader draws the pinned navigator header — table title + glyph-legend
// toggle, the optional comment, and the filter box. The dock-tab call site
// renders it above the section ScrollArea, so the filter stays in view while a
// long schema scrolls. The section list itself (renderSections) lives inside
// that ScrollArea: a dock leaf hands its content a bounded child rect, so the
// ScrollArea fills and clips it (a ScrollArea inside the former width-pinned
// Vertical-in-Horizontal collapsed to its first child — see the package history).
func renderNavHeader(ids *c.WidgetIdStack, m *Model, scope string) {
	density := styletokens.DensityFromEnv()
	t := m.Table
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel(t.DictionaryEntry.Name.String()) {
			rt.Strong().Size(15)
		}
		c.AddSpace(styletokens.GapInline(density))
		renderLegendToggle(m, scope)
	}
	if cmt := t.DictionaryEntry.Comment; cmt != "" {
		for rt := range c.RichTextLabel(cmt) {
			rt.Weak().Small()
		}
	}
	c.TextEdit(ids.PrepareStr("filter"), m.filter, false).
		HintText("filter sections / columns…").
		DesiredWidth(navWidth - 16).
		SendRespVal(&m.filter)
	c.AddSpace(styletokens.PaddingInner(density))
}

// renderSections draws the navigator as an outline: plain item-types (◆),
// standalone tagged sections (◇), and co-grouped sections (◈, prefixed with
// the group key) at the top level, each over its value columns. Selecting a
// row drives the detail pane.
//
// The hierarchy is the native tree widget's (ADR-0176 M3), which replaced a
// hand-rolled CollapsingHeader + SelectableLabel navigator. What that buys
// here is virtualisation — only the rows on screen build widgets, where before
// every column of every open section did — a selection highlight that spans
// the row rather than just its label, and expansion state Go owns and can
// persist. The earlier egui_ltreeview binding was not an option for this
// shape: it mis-rendered a wide, multi-root forest.
//
// availW / availH are the navigator pane's measured size, and both matter.
// Without a height the table falls back to an auto-fit capped at
// ETABLE_AUTOFIT_CAP_PX, which a schema of any size overruns. Without a width
// the single outline column would be a fixed slab with the pane's remaining
// width dead beside it, and a row's selection outline — which spans the
// table's columns, not the pane — would stop short of the pane edge. The probe
// answers one frame late and not at all on the first, where both fall back.
//
// That is also why the column is not resizable: there is nothing to its right
// to trade width with, and egui_table only leaves a non-resizable column's
// declared width alone, so this is what lets it track the pane as the reader
// drags the dock splitter.
func renderSections(ids *c.WidgetIdStack, m *Model, availW, availH float32) {
	outlineW := float32(navOutlineWidth)
	if w := availW - navScrollbarGutter; w > outlineW {
		outlineW = w
	}
	m.buildNav()
	m.syncNav()
	res := tree.Render(tree.Input{
		Ids:      ids,
		ScopeKey: "nav",
		Tree:     m.navTree(),
		State:    &m.navState,
		Outline: tree.Column{
			Width: outlineW,
			Cell:  m.navCell,
		},
		MaxHeight: availH,
	})
	m.applyNav(res)
}

// navCell draws one navigator row: the node's label, then a column's terse
// canonical type after it — weak and small, so the name stays what the eye
// lands on and the type is there to confirm rather than to scan. Before the
// port the two were concatenated into one label with two spaces between them.
//
// Both labels are Selectable(false). That is load-bearing rather than tidy: a
// selectable label senses click-and-drag and is registered after the row's own
// sense region, so it would sit over it and swallow every click on its rect
// (ADR-0176 SD7).
func (m *Model) navCell(node int32) {
	c.Label(m.navLabels[node]).Selectable(false).Truncate().Send()
	typ := m.navNodes[node].typ
	if typ == "" {
		return
	}
	c.AddSpace(styletokens.GapInline(styletokens.DensityFromEnv()))
	c.LabelAtoms(c.Atoms().
		BeginRichTextColored(navTypeFg, navTypeBg, typ).Small().End().
		Keep()).Selectable(false).Truncate().Send()
}

// renderDetail draws the property pane for the current selection: a
// category-accented name header (the navigator glyph in its tone + a kind
// chip), the canonical-type inspector (for columns), and a two-column grid of
// the remaining facts — scalars as monospace values, aspect sets as toned
// chips.
func renderDetail(ids *c.WidgetIdStack, m *Model) {
	t := m.Table
	switch m.sel.kind {
	case selPlainColumn:
		i := m.sel.plainCol
		if i < 0 || i >= len(t.PlainValuesNames) {
			detailEmpty()
			return
		}
		it := t.PlainValuesItemTypes[i]
		detailHeaderCat(ids, t.PlainValuesNames[i].String(), "◆", styletokens.InfoDefault, "value column", badge.ToneInfo)
		renderTypeBlock(ids, t.PlainValuesTypes[i])
		for range c.Grid(ids.PrepareStr("detail")).NumColumns(2).KeepIter() {
			gridRow("scope", plainScope(it))
			gridRow("item type", it.String())
			chipRow(ids, "enc", "enc hints", encHintList(t.PlainValuesEncodingHints[i]), badge.ToneInfo)
			chipRow(ids, "sem", "semantics", valSemList(t.PlainValuesValueSemantics[i]), badge.TonePrimary)
		}

	case selSectionColumn:
		si, ci := m.sel.section, m.sel.col
		if si < 0 || si >= len(t.TaggedValuesSections) {
			detailEmpty()
			return
		}
		sec := &t.TaggedValuesSections[si]
		if ci < 0 || ci >= len(sec.ValueColumnNames) {
			detailEmpty()
			return
		}
		glyph, gtone := sectionGlyph(sec)
		detailHeaderCat(ids, sec.ValueColumnNames[ci].String(), glyph, gtone, "value column", badge.TonePrimary)
		renderTypeBlock(ids, sec.ValueColumnTypes[ci])
		for range c.Grid(ids.PrepareStr("detail")).NumColumns(2).KeepIter() {
			gridRow("scope", "tagged")
			gridRow("section", sec.Name.String())
			chipRow(ids, "enc", "enc hints", encHintList(sec.ValueEncodingHints[ci]), badge.ToneInfo)
			chipRow(ids, "sem", "semantics", valSemList(sec.ValueSemantics[ci]), badge.TonePrimary)
			chipRow(ids, "memb", "membership", membershipSpecList(sec.MembershipSpec), badge.ToneNeutral)
			chipRow(ids, "cog", "co-group", oneOrNone(string(sec.CoSectionGroup)), badge.ToneNeutral)
			chipRow(ids, "str", "streaming", oneOrNone(string(sec.StreamingGroup)), badge.ToneNeutral)
		}

	case selSection:
		si := m.sel.section
		if si < 0 || si >= len(t.TaggedValuesSections) {
			detailEmpty()
			return
		}
		sec := &t.TaggedValuesSections[si]
		glyph, gtone := sectionGlyph(sec)
		detailHeaderCat(ids, sec.Name.String(), glyph, gtone, "tagged section", badge.TonePrimary)
		for range c.Grid(ids.PrepareStr("detail")).NumColumns(2).KeepIter() {
			chipRow(ids, "memb", "membership", membershipSpecList(sec.MembershipSpec), badge.ToneNeutral)
			chipRow(ids, "use", "use aspects", useAspList(sec.UseAspects), badge.ToneNeutral)
			chipRow(ids, "cog", "co-group", oneOrNone(string(sec.CoSectionGroup)), badge.ToneNeutral)
			chipRow(ids, "str", "streaming", oneOrNone(string(sec.StreamingGroup)), badge.ToneNeutral)
			gridRow("value cols", strconv.Itoa(len(sec.ValueColumnNames)))
		}

	default:
		detailEmpty()
	}
}

// detailHeaderCat draws the selection's name preceded by its navigator glyph
// (in the category tone) and trailed by a small kind chip, so the detail header
// echoes the tree at a glance.
func detailHeaderCat(ids *c.WidgetIdStack, name, glyph string, glyphTone styletokens.RGBA8, kind string, kindTone badge.ToneE) {
	density := styletokens.DensityFromEnv()
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabelColored(color.Hex(glyphTone.AsHex()).Keep(), color.Transparent.Keep(), glyph) {
			rt.Strong().Size(15)
		}
		c.AddSpace(styletokens.PaddingInner(density))
		for rt := range c.RichTextLabel(name) {
			rt.Strong().Size(15)
		}
		c.AddSpace(styletokens.GapItems(density))
		badge.New(ids.PrepareStr("detail-kind"), kind).Tone(kindTone).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
	}
	c.AddSpace(styletokens.PaddingHair(density))
}

// chipRow is a grid row whose value cell is a run of toned chips, one per
// aspect — replacing the old comma-joined gray string. An empty set renders a
// weak "—" so the row still reads.
func chipRow(ids *c.WidgetIdStack, key, label string, items []string, tone badge.ToneE) {
	for rt := range c.RichTextLabel(label) {
		rt.Weak()
	}
	if len(items) == 0 {
		for rt := range c.RichTextLabel("—") {
			rt.Weak()
		}
		c.EndRow()
		return
	}
	for range c.Horizontal().KeepIter() {
		for i, it := range items {
			badge.New(ids.PrepareStr(key+"/"+strconv.Itoa(i)), it).
				Tone(tone).
				Variant(badge.VariantSoft).
				Size(badge.SizeSm).
				Send()
		}
	}
	c.EndRow()
}

// sectionGlyph picks the navigator glyph + tone for a tagged section: ◈ when it
// belongs to a co-section group, ◇ otherwise. Both carry the accent tone (the
// tagged category colour).
func sectionGlyph(sec *common.TaggedValuesSection) (glyph string, tone styletokens.RGBA8) {
	if string(sec.CoSectionGroup) != "" {
		return "◈", styletokens.AccentDefault
	}
	return "◇", styletokens.AccentDefault
}

// membershipSpecList decomposes a MembershipSpec set into one label per
// cardinality channel (the same channels membershipBadge condenses to ˡ/ʰ/ᵐ),
// for rendering as chips. Empty for MembershipSpecNone.
func membershipSpecList(spec common.MembershipSpecE) (out []string) {
	if spec == common.MembershipSpecNone {
		return nil
	}
	for s := range spec.Iterate() {
		out = append(out, s.String())
	}
	return
}

// oneOrNone wraps a possibly-empty identifier as a 0- or 1-element slice, so a
// single-valued field (co-group, streaming group) flows through chipRow.
func oneOrNone(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

func detailEmpty() {
	for rt := range c.RichTextLabel("select a node") {
		rt.Weak()
	}
}

// renderTypeBlock shows the column's canonical type via the
// canonicaltypesummary inspector (ADR-0067): a compact level-1 line —
// canonical string · validity dot · footprint trailer — that tethers into a
// Layout / Members / Go-codec popup, replacing a hand-rolled decomposition.
// One persistent instance (stable idPrefix + idGen) tracks whichever column
// is selected.
func renderTypeBlock(ids *c.WidgetIdStack, ct canonicaltypes.PrimitiveAstNodeI) {
	for rt := range c.RichTextLabel("canonical type") {
		rt.Weak().Small()
	}
	for range c.Horizontal().KeepIter() {
		canonicaltypesummary.New("schemaview-coltype").Render(ids.PrepareStr("cts-col"), canonicalOf(ct))
	}
	c.AddSpace(styletokens.PaddingInner(styletokens.DensityFromEnv()))
}

// --- formatting helpers ---

func gridRow(lbl, value string) {
	for rt := range c.RichTextLabel(lbl) {
		rt.Weak()
	}
	if value == "" {
		value = "—"
	}
	for rt := range c.RichTextLabel(value) {
		rt.Monospace()
	}
	c.EndRow()
}

// typeChip is the terse canonical-type form shown on a navigator row.
func typeChip(ct canonicaltypes.PrimitiveAstNodeI) string {
	if ct == nil {
		return "—"
	}
	return ct.String()
}

// canonicalOf is the terse canonical string handed to the type inspector;
// "" for a nil type, which canonicaltypesummary renders as an empty-type
// placeholder.
func canonicalOf(ct canonicaltypes.PrimitiveAstNodeI) string {
	if ct == nil {
		return ""
	}
	return ct.String()
}

// membershipBadge renders the section's MembershipSpec cardinality classes
// as compact glyphs: ˡ low-card, ʰ high-card, ᵐ mixed.
func membershipBadge(spec common.MembershipSpecE) string {
	if spec == common.MembershipSpecNone {
		return ""
	}
	var low, high, mixed bool
	for s := range spec.Iterate() {
		switch s {
		case common.MembershipSpecMixedLowCardRefHighCardParameters,
			common.MembershipSpecMixedLowCardVerbatimHighCardParameters:
			mixed = true
		case common.MembershipSpecHighCardRef,
			common.MembershipSpecHighCardVerbatim,
			common.MembershipSpecHighCardRefParametrized:
			high = true
		case common.MembershipSpecLowCardRef,
			common.MembershipSpecLowCardVerbatim,
			common.MembershipSpecLowCardRefParametrized:
			low = true
		}
	}
	b := ""
	if low {
		b += "ˡ"
	}
	if high {
		b += "ʰ"
	}
	if mixed {
		b += "ᵐ"
	}
	return b
}

func encHintList(s encodingaspects.AspectSet) (out []string) {
	for _, a := range s.IterateAspects() {
		out = append(out, a.String())
	}
	return
}

func valSemList(s valueaspects.AspectSet) (out []string) {
	for _, a := range s.IterateAspects() {
		out = append(out, a.String())
	}
	return
}

func useAspList(s useaspects.AspectSet) (out []string) {
	for _, a := range s.IterateAspects() {
		out = append(out, a.String())
	}
	return
}

func plainScope(it common.PlainItemTypeE) string {
	switch it {
	case common.PlainItemTypeEntityId,
		common.PlainItemTypeEntityTimestamp,
		common.PlainItemTypeEntityRouting,
		common.PlainItemTypeEntityLifecycle:
		return "entity"
	case common.PlainItemTypeTransaction:
		return "transaction"
	case common.PlainItemTypeOpaque:
		return "opaque"
	}
	return "—"
}
