package schemaview

import (
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/canonicaltypesummary"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
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

const (
	navWidth = 340.0 // filter-box width hint inside the (now resizable) navigator pane

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
				// scroll so the filter stays usable while a long schema scrolls;
				// only the section list lives inside the ScrollArea.
				renderNavHeader(in.Ids, m, scope)
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					renderSections(in.Ids, m)
				}
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

// renderSections draws the navigator as a flat list of collapsible headers:
// plain item-types (◆), standalone tagged sections (◇), and co-grouped
// sections (◈, prefixed with the group key). Column rows inside each header
// are selectable and drive the detail pane. CollapsingHeader + SelectableLabel
// are used rather than egui_ltreeview: the flat-drain tree mis-renders this
// wider, multi-root shape (the shipped tree demos only feed it a single
// deeply-nested root).
func renderSections(ids *c.WidgetIdStack, m *Model) {
	t := m.Table

	// Plain item-types — one ◆ header per type present.
	for _, it := range common.AllPlainItemTypes {
		var idxs []int
		names := []string{it.String()}
		for i, pit := range t.PlainValuesItemTypes {
			if pit == it {
				idxs = append(idxs, i)
				names = append(names, t.PlainValuesNames[i].String())
			}
		}
		if len(idxs) == 0 || !m.matches(names...) {
			continue
		}
		for range c.CollapsingHeader(ids.PrepareStr("plain:"+it.String()), label("◆ "+it.String())).DefaultOpen(true).KeepIter() {
			for _, i := range idxs {
				colRow(ids, m,
					"plain:"+it.String()+":"+t.PlainValuesNames[i].String(),
					t.PlainValuesNames[i].String()+"  "+typeChip(t.PlainValuesTypes[i]),
					selection{kind: selPlainColumn, plainCol: i})
			}
		}
	}

	// Tagged sections, in declaration order. Co-grouped sections carry a
	// ◈ <key> · prefix; standalone sections carry ◇.
	for i := range t.TaggedValuesSections {
		sec := &t.TaggedValuesSections[i]
		if !m.matchesSection(sec) {
			continue
		}
		key := string(sec.CoSectionGroup)
		head, idp := "◇ ", "sec:"
		if key != "" {
			head, idp = "◈ "+key+" · ", "co:"+key+":"
		}
		head += sec.Name.String()
		if b := membershipBadge(sec.MembershipSpec); b != "" {
			head += " " + b
		}
		if len(sec.ValueColumnNames) == 0 {
			head += " ·∅"
		}
		base := idp + sec.Name.String()
		for range c.CollapsingHeader(ids.PrepareStr(base), label(head)).DefaultOpen(true).KeepIter() {
			colRow(ids, m, base+"#props", "· properties", selection{kind: selSection, section: i})
			for ci := range sec.ValueColumnNames {
				colRow(ids, m,
					base+":"+sec.ValueColumnNames[ci].String(),
					sec.ValueColumnNames[ci].String()+"  "+typeChip(sec.ValueColumnTypes[ci]),
					selection{kind: selSectionColumn, section: i, col: ci})
			}
		}
	}
}

// colRow renders one selectable navigator row; clicking it updates the
// selection that the detail pane reads.
func colRow(ids *c.WidgetIdStack, m *Model, id, text string, sel selection) {
	if c.SelectableLabel(ids.PrepareStr(id), m.isSel(sel), text).SendResp().HasPrimaryClicked() {
		m.sel = sel
	}
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

// label is the WidgetText holder the CollapsingHeader takes.
func label(s string) typed.RetainedFffiHolderTyped[c.WidgetTextS] {
	return c.WidgetText().Text(s).Keep()
}

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
