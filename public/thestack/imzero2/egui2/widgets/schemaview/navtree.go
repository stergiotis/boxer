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

// The navigator's category glyphs, documented in the help book, keyed by the
// legend window, and shared with the TopologySpark card that this vocabulary
// came from.
//
// Two things about them are load-bearing rather than decorative.
//
// They are drawn in the MONOSPACE face. None of the three is in Noto Sans, so
// each otherwise resolves through the client's fallback chain — and the CJK
// face that answers has the first two but not the third, which left the
// co-section glyph rendering as a tofu box. The mono font covers all three,
// which is why the legend window always showed it correctly: its chips are
// Monospace(). See the imzero2 skill, §12 "Oversized, Off-Centre Glyph".
//
// And the co-section glyph is ❖, not the ◈ it reads as in the spec. ◈ is
// "white diamond containing black small diamond", and the interior gap that
// distinguishes it from ◆ is SUB-PIXEL at the size a row draws it: rasterised
// from this face at 12 and 14px it is byte-identical to ◆, and only at 18px
// does any structure appear. A mark that cannot be told from its neighbour is
// not a mark. ❖ keeps the diamond family and carries a visible interior at
// 13px.
const (
	glyphPlainItemType  = "◆"
	glyphTaggedSection  = "◇"
	glyphCoSectionGroup = "❖"
)

// navNode is the per-node metadata the navigator keeps alongside the columnar
// [tree.Tree]. One entry per node, indexed the same way.
type navNode struct {
	// glyph is the section row's category mark (◆ / ◇ / ❖), or "" on a column
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
	keys := m.navKeys[:0]
	nodes := m.navNodes[:0]

	add := func(parent int32, key, glyph, label, typ string, sel selection) (node int32) {
		node = int32(len(labels))
		labels = append(labels, label)
		parents = append(parents, parent)
		keys = append(keys, key)
		nodes = append(nodes, navNode{glyph: glyph, typ: typ, sel: sel})
		return
	}

	t := m.Table
	if t == nil {
		m.navLabels, m.navParents, m.navKeys, m.navNodes = labels, parents, keys, nodes
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
	// ❖ <key> · prefix; standalone sections carry ◇.
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

	m.navLabels, m.navParents, m.navKeys, m.navNodes = labels, parents, keys, nodes
}

// navTree is the columnar view of the last [Model.buildNav], borrowed: valid
// until the next build. The key column is what carries a collapse across one.
func (m *Model) navTree() tree.Tree {
	return tree.Tree{Labels: m.navLabels, Parents: m.navParents, Keys: m.navKeys}
}

// syncNav sets up the widget's state for the frame: the expansion default, and
// the selection projected from the Model's own [selection].
//
// Expansion is not projected. The hierarchy carries a key column, so the
// widget files a collapse under [navNode.key] and it survives the rebuild
// every filter keystroke triggers — which is what the Model used to keep a
// parallel map for. What it does need saying is the polarity: sections start
// OPEN, as the CollapsingHeader navigator's DefaultOpen(true) did, so only
// what the reader closed is stored.
//
// The selection stays a projection because [selection] is the richer thing —
// it names a plain column, a section, or a column within one, which is what
// the detail pane reads, and the host sets it from elsewhere (a fixture swap
// resets it). Overwriting the widget's selection here also settles what a
// click on a row with no selection does: nothing is drawn from it, because
// Render applies its own selection after the row loop and this runs before the
// next one.
func (m *Model) syncNav() {
	// Bound to THIS frame's hierarchy before anything is written, or the
	// selection below is filed under whatever key the previous build gave that
	// index — and on the first frame, under no key at all.
	m.navState.Bind(m.navTree())
	m.navState.SetDefaultExpanded(true)
	sel := int32(-1)
	for i := range m.navNodes {
		if m.navNodes[i].sel.kind != selNone && m.navNodes[i].sel == m.sel {
			sel = int32(i)
			break
		}
	}
	if sel < 0 {
		m.navState.ClearSelection()
		return
	}
	m.navState.SelectOnly(sel)
}

// applyNav turns a frame's tree interaction into a Model change. Expansion
// needs nothing: the widget has already applied it to the state that owns it.
func (m *Model) applyNav(res tree.Result) {
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
		m.navState.ToggleExpanded(n)
	}
}
