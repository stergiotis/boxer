package schemaview

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
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
}

const (
	navWidth    = 340.0
	detailWidth = 420.0
)

// Render lays the inspector out as two pinned columns: the section navigator
// on the left, the decoded detail pane on the right.
func Render(in Input) {
	m := in.Model
	if m == nil || m.Table == nil {
		return
	}
	for range c.Horizontal().KeepIter() {
		for range c.Vertical().KeepIter() {
			c.UiSetMinWidth(navWidth)
			c.UiSetMaxWidth(navWidth)
			renderNavigator(in.Ids, m)
		}
		c.AddSpace(8)
		for range c.Vertical().KeepIter() {
			c.UiSetMinWidth(detailWidth)
			c.UiSetMaxWidth(detailWidth)
			renderDetail(in.Ids, m)
		}
	}
}

// renderNavigator draws the table header, the filter box, and the scrollable
// section list.
func renderNavigator(ids *c.WidgetIdStack, m *Model) {
	t := m.Table
	for rt := range c.RichTextLabel(t.DictionaryEntry.Name.String()) {
		rt.Strong().Size(15)
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
	c.AddSpace(4)

	// Rendered directly in the pinned column (no ScrollArea). A ScrollArea
	// here — inside a width-pinned Vertical inside a Horizontal — rendered
	// only its first child; mappingplanview's editor stacks rows the same
	// way and is the proven pattern. Long schemas rely on the host window's
	// own scroll; an in-widget scroll can return once that combination is
	// understood.
	renderSections(ids, m)
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

// renderDetail draws the property pane for the current selection as a
// two-column grid (weak label · monospace value).
func renderDetail(ids *c.WidgetIdStack, m *Model) {
	t := m.Table
	for range c.Grid(ids.PrepareStr("detail")).NumColumns(2).KeepIter() {
		switch m.sel.kind {
		case selPlainColumn:
			i := m.sel.plainCol
			if i < 0 || i >= len(t.PlainValuesNames) {
				gridRow("selection", "—")
				break
			}
			it := t.PlainValuesItemTypes[i]
			ct := t.PlainValuesTypes[i]
			gridRow("name", t.PlainValuesNames[i].String())
			gridRow("scope", plainScope(it))
			gridRow("item type", it.String())
			gridRow("kind", "value column")
			gridRow("type", typeChip(ct))
			gridRow("decoded", typeDecompose(ct))
			gridRow("shape", scalarShape(ct))
			gridRow("enc hints", joinAspects(encHintList(t.PlainValuesEncodingHints[i])))
			gridRow("semantics", joinAspects(valSemList(t.PlainValuesValueSemantics[i])))

		case selSectionColumn:
			si, ci := m.sel.section, m.sel.col
			if si < 0 || si >= len(t.TaggedValuesSections) {
				gridRow("selection", "—")
				break
			}
			sec := &t.TaggedValuesSections[si]
			if ci < 0 || ci >= len(sec.ValueColumnNames) {
				gridRow("selection", "—")
				break
			}
			ct := sec.ValueColumnTypes[ci]
			gridRow("name", sec.ValueColumnNames[ci].String())
			gridRow("scope", "tagged")
			gridRow("kind", "value column")
			gridRow("type", typeChip(ct))
			gridRow("decoded", typeDecompose(ct))
			gridRow("shape", scalarShape(ct))
			gridRow("enc hints", joinAspects(encHintList(sec.ValueEncodingHints[ci])))
			gridRow("semantics", joinAspects(valSemList(sec.ValueSemantics[ci])))
			gridRow("— section —", sec.Name.String())
			gridRow("membership", sec.MembershipSpec.String())
			gridRow("co-group", strOrDash(string(sec.CoSectionGroup)))
			gridRow("streaming", strOrDash(string(sec.StreamingGroup)))

		case selSection:
			si := m.sel.section
			if si < 0 || si >= len(t.TaggedValuesSections) {
				gridRow("selection", "—")
				break
			}
			sec := &t.TaggedValuesSections[si]
			gridRow("section", sec.Name.String())
			gridRow("membership", sec.MembershipSpec.String())
			gridRow("use aspects", joinAspects(useAspList(sec.UseAspects)))
			gridRow("co-group", strOrDash(string(sec.CoSectionGroup)))
			gridRow("streaming", strOrDash(string(sec.StreamingGroup)))
			gridRow("value cols", strconv.Itoa(len(sec.ValueColumnNames)))

		default:
			gridRow("selection", "select a node")
		}
	}
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

// typeChip is the terse canonical-type form shown on a column row.
func typeChip(ct canonicaltypes.PrimitiveAstNodeI) string {
	if ct == nil {
		return "—"
	}
	return ct.String()
}

// typeFamily names the canonical-type family via the interface predicates.
func typeFamily(ct canonicaltypes.PrimitiveAstNodeI) string {
	switch {
	case ct == nil:
		return ""
	case ct.IsMachineNumericNode():
		return "machine-numeric"
	case ct.IsStringNode():
		return "string"
	case ct.IsTemporalNode():
		return "temporal"
	case ct.IsNetworkNode():
		return "network"
	}
	return "unknown"
}

// typeDecompose breaks a canonical type into its components, family-specific.
// Falls back to the family name when the concrete node type is unrecognised.
func typeDecompose(ct canonicaltypes.PrimitiveAstNodeI) string {
	if ct == nil {
		return ""
	}
	var parts []string
	switch n := ct.(type) {
	case canonicaltypes.MachineNumericTypeAstNode:
		parts = nonEmpty(n.BaseType.String(), n.Width.String(), n.ByteOrderModifier.String(), n.ScalarModifier.String())
	case canonicaltypes.StringAstNode:
		parts = nonEmpty(n.BaseType.String(), n.WidthModifier.String(), n.Width.String(), n.ScalarModifier.String())
	case canonicaltypes.TemporalTypeAstNode:
		parts = nonEmpty(n.BaseType.String(), n.Width.String(), n.ScalarModifier.String())
	case canonicaltypes.NetworkTypeAstNode:
		parts = nonEmpty(n.BaseType.String(), n.CIDRModifier.String(), n.ScalarModifier.String())
	}
	if len(parts) == 0 {
		return typeFamily(ct)
	}
	return strings.Join(parts, " · ")
}

// scalarShape reports scalar / homogenous array / set, derived from the
// canonical type's trailing scalar-modifier in its terse form.
func scalarShape(ct canonicaltypes.PrimitiveAstNodeI) string {
	if ct == nil {
		return "—"
	}
	if ct.IsScalar() {
		return "scalar"
	}
	s := ct.String()
	if len(s) > 0 {
		switch s[len(s)-1] {
		case 'h':
			return "homogenous array"
		case 'm':
			return "set"
		}
	}
	return "non-scalar"
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

func joinAspects(l []string) string {
	if len(l) == 0 {
		return "—"
	}
	return strings.Join(l, ", ")
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

func strOrDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// nonEmpty drops empty / none-marker components ("", "-", "0", "<none>") so a
// decomposed type line reads cleanly.
func nonEmpty(parts ...string) (out []string) {
	for _, p := range parts {
		switch p {
		case "", "-", "0", "<none>":
			continue
		}
		out = append(out, p)
	}
	return
}
