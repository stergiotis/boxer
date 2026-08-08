package schemaview

// navtree.go builds the navigator's hierarchy (ADR-0176 M3). It is the pure
// half of the port from CollapsingHeader + SelectableLabel to the native tree
// widget: it turns the bound TableDesc and the active filter into the columnar
// shape tree.Render takes, and records per node the two things the widget has
// no way to know — the stable key its collapse state is filed under, and the
// [selection] clicking it produces.
//
// No bindings import, so the whole hierarchy is testable without a renderer,
// the way the tree widget's own flatten is.

import (
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
)

// The navigator's category glyphs, documented in the help book and keyed by the
// legend window. They are drawn in the MONOSPACE face, which is not decoration:
// none of the three is in Noto Sans, so each resolves through the client's
// fallback chain — and the CJK face that answers has ◆ and ◇ but NOT ◈, which
// left the co-section glyph rendering as a tofu box. The mono font the client
// loads covers all three, which is exactly why the legend window has always
// shown ◈ correctly: its chips are Monospace().
//
// Keeping the vocabulary and changing the face fixes the tofu and takes ◆ and ◇
// off the fallback chain at the same time — they render today only because a
// CJK font happened to be loaded and happened to have them. See the imzero2
// skill, §12 "Oversized, Off-Centre Glyph", for the general rule.
const (
	glyphPlainItemType  = "◆"
	glyphTaggedSection  = "◇"
	glyphCoSectionGroup = "◈"
)

// navNode is the per-node metadata the navigator keeps alongside the columnar
// [tree.Tree]. One entry per node, indexed the same way.
type navNode struct {
	// key identifies the node across frames. The tree widget keys its state on
	// node indices, which is the only identity a columnar input has — but this
	// navigator rebuilds its hierarchy on every filter keystroke, so an index
	// means a different node one frame to the next. The key is what the
	// Model's own collapse state is filed under; see [Model.syncNav].
	key string
	// glyph is the section row's category mark (◆ / ◇ / ◈), or "" on a column
	// row. It rides beside the label rather than inside it because it has to
	// be drawn in a DIFFERENT FACE — see [glyphPlainItemType].
	glyph string
	// typ is the terse canonical type shown after a column's name, or "" on a
	// section row. It rides here rather than in the label so the row can draw
	// it in its own weight without the name having to be re-split.
	typ string
	// sel is what clicking this row selects. A zero value (selNone) marks a
	// row that names a grouping rather than a schema object — a plain
	// item-type header — and clicking it changes nothing.
	sel selection
}

// buildNav rebuilds the navigator hierarchy from the bound schema under the
// active filter, into the Model's scratch slices.
//
// The order is the one the CollapsingHeader navigator used, because it is the
// order the schema is authored in and the detail pane's indices point into:
// plain item-types in [common.AllPlainItemTypes] order, then tagged sections
// in declaration order. Filtering drops whole sections rather than individual
// columns, which is [Model.matchesSection]'s rule, not a new one.
//
// A tagged section's header row IS the section — clicking it selects the
// section itself, which is what the "· properties" child row did before. The
// tree gives a section row a click of its own, so the child was left saying
// what its parent already says.
func (m *Model) buildNav() {
	labels := m.navLabels[:0]
	parents := m.navParents[:0]
	nodes := m.navNodes[:0]

	add := func(parent int32, key, glyph, label, typ string, sel selection) (node int32) {
		node = int32(len(labels))
		labels = append(labels, label)
		parents = append(parents, parent)
		nodes = append(nodes, navNode{key: key, glyph: glyph, typ: typ, sel: sel})
		return
	}

	t := m.Table
	if t == nil {
		m.navLabels, m.navParents, m.navNodes = labels, parents, nodes
		return
	}

	// Plain item-types — one ◆ header per type present, its backbone columns
	// beneath it.
	var idxs []int
	var names []string
	for _, it := range common.AllPlainItemTypes {
		idxs, names = idxs[:0], append(names[:0], it.String())
		for i, pit := range t.PlainValuesItemTypes {
			if pit == it {
				idxs = append(idxs, i)
				names = append(names, t.PlainValuesNames[i].String())
			}
		}
		if len(idxs) == 0 || !m.matches(names...) {
			continue
		}
		base := "plain:" + it.String()
		head := add(-1, base, glyphPlainItemType, it.String(), "", selection{})
		for _, i := range idxs {
			name := t.PlainValuesNames[i].String()
			add(head, base+":"+name, "", name, typeChip(t.PlainValuesTypes[i]),
				selection{kind: selPlainColumn, plainCol: i})
		}
	}

	// Tagged sections, in declaration order. Co-grouped sections carry a
	// ◈ <key> · prefix; standalone sections carry ◇.
	for i := range t.TaggedValuesSections {
		sec := &t.TaggedValuesSections[i]
		if !m.matchesSection(sec) {
			continue
		}
		group := string(sec.CoSectionGroup)
		glyph, idp, label := glyphTaggedSection, "sec:", ""
		if group != "" {
			glyph, idp, label = glyphCoSectionGroup, "co:"+group+":", group+" · "
		}
		label += sec.Name.String()
		if b := membershipBadge(sec.MembershipSpec); b != "" {
			label += " " + b
		}
		if len(sec.ValueColumnNames) == 0 {
			label += " ·∅"
		}
		base := idp + sec.Name.String()
		head := add(-1, base, glyph, label, "", selection{kind: selSection, section: i})
		for ci := range sec.ValueColumnNames {
			name := sec.ValueColumnNames[ci].String()
			add(head, base+":"+name, "", name, typeChip(sec.ValueColumnTypes[ci]),
				selection{kind: selSectionColumn, section: i, col: ci})
		}
	}

	m.navLabels, m.navParents, m.navNodes = labels, parents, nodes
}

// navTree is the columnar view of the last [Model.buildNav], borrowed: valid
// until the next build.
func (m *Model) navTree() tree.Tree {
	return tree.Tree{Labels: m.navLabels, Parents: m.navParents}
}

// syncNav projects the Model's own state onto the tree widget's, which is what
// makes the Model the single authority and the widget's [tree.State] a
// per-frame scratch.
//
// It has to be a projection rather than a hand-over because the two key on
// different things. The widget keys on node indices; the Model keys expansion
// on [navNode.key] and selection on a [selection], both of which survive the
// rebuild that every filter keystroke triggers. Handing the widget's state
// authority instead would mean a section collapsed before typing "id" reopens
// as whichever section inherited its index after.
//
// It also settles what a click on a row with no [selection] does: nothing.
// Render applies its own selection after the row loop, so the widget's stray
// selection is overwritten here before it is ever drawn.
func (m *Model) syncNav() {
	sel := int32(-1)
	for i := range m.navNodes {
		n := int32(i)
		m.navState.SetExpanded(n, !m.collapsed[m.navNodes[i].key])
		if m.navNodes[i].sel.kind != selNone && m.navNodes[i].sel == m.sel {
			sel = n
		}
	}
	if sel < 0 {
		m.navState.ClearSelection()
		return
	}
	m.navState.SelectOnly(sel)
}

// applyNav writes a frame's tree interaction back onto the Model's own state.
// The widget has already applied both to its [tree.State]; this is what makes
// them outlive the next rebuild.
func (m *Model) applyNav(res tree.Result) {
	if n := res.Toggled; n >= 0 && int(n) < len(m.navNodes) {
		m.setCollapsed(m.navNodes[n].key, !m.navState.IsExpanded(n))
	}
	n := res.Clicked
	if n < 0 || int(n) >= len(m.navNodes) {
		return
	}
	if s := m.navNodes[n].sel; s.kind != selNone {
		m.sel = s
		return
	}
	// A plain item-type row names a grouping, not a schema object, so it has
	// no detail to show and its click does the other thing a tree row can do —
	// the way clicking a CollapsingHeader's title did before. Suppressed when
	// the widget already toggled this row on the same frame, so a double-click
	// nets two toggles rather than three.
	if res.Toggled < 0 {
		m.setCollapsed(m.navNodes[n].key, m.navState.IsExpanded(n))
	}
}

// setCollapsed records a section's closed state. Open is the default, so an
// open section is an absent entry rather than a false one — which keeps the
// map bounded by what the reader actually closed, and means a section that
// disappears under a filter and comes back is still open.
func (m *Model) setCollapsed(key string, closed bool) {
	if !closed {
		delete(m.collapsed, key)
		return
	}
	if m.collapsed == nil {
		m.collapsed = make(map[string]bool, 8)
	}
	m.collapsed[key] = true
}
