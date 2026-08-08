package schemaview

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The navigator hierarchy is pure, so it is tested without a renderer — the
// same split the tree widget's own flatten uses (ADR-0176 SD3). What is
// asserted here is what the port had to get right: the row order and parentage
// the CollapsingHeader navigator produced by nesting, the stable keys the
// collapse state is filed under, and the selection each row carries.

func nm(s string) naming.StylableName { return naming.MustBeValidStylableName(s) }

// fixture is one plain item-type carrying two columns, a standalone tagged
// section with one column, and a co-grouped section with none — one of every
// row kind the navigator emits.
//
// Column types are left nil, which typeChip renders as "—". Nothing here is
// testing canonical types, and a nil type is a shape the widget must survive
// anyway.
func fixture() *common.TableDesc {
	return &common.TableDesc{
		PlainValuesNames:     []naming.StylableName{nm("shard"), nm("tenant")},
		PlainValuesTypes:     make([]canonicaltypes.PrimitiveAstNodeI, 2),
		PlainValuesItemTypes: []common.PlainItemTypeE{common.PlainItemTypeEntityId, common.PlainItemTypeEntityId},
		TaggedValuesSections: []common.TaggedValuesSection{
			{
				Name:             nm("readings"),
				ValueColumnNames: []naming.StylableName{nm("celsius")},
				ValueColumnTypes: make([]canonicaltypes.PrimitiveAstNodeI, 1),
			},
			{
				Name:           nm("audit"),
				CoSectionGroup: naming.Key("prov"),
			},
		},
	}
}

func keysOf(m *Model) (out []string) {
	for i := range m.navNodes {
		out = append(out, m.navNodes[i].key)
	}
	return
}

func TestBuildNavShape(t *testing.T) {
	m := NewModel(fixture())
	m.buildNav()

	// The category glyph is NOT in the label: it rides in navNode.glyph so the
	// renderer can draw it in the monospace face, which is the only loaded one
	// covering all three (see glyphPlainItemType).
	assert.Equal(t, []string{
		"entity-id", "shard", "tenant",
		"readings", "celsius",
		"prov · audit ·∅",
	}, m.navLabels)
	assert.Equal(t, []string{"◆", "", "", "◇", "", "❖"},
		[]string{m.navNodes[0].glyph, m.navNodes[1].glyph, m.navNodes[2].glyph,
			m.navNodes[3].glyph, m.navNodes[4].glyph, m.navNodes[5].glyph},
		"section rows carry a category glyph; column rows do not")
	assert.Equal(t, []int32{-1, 0, 0, -1, 3, -1}, m.navParents,
		"section headers are roots; their columns are children")
	require.NoError(t, m.navTree().Validate())

	assert.Equal(t, []string{
		"plain:entity-id", "plain:entity-id:shard", "plain:entity-id:tenant",
		"sec:readings", "sec:readings:celsius",
		"co:prov:audit",
	}, keysOf(m), "keys distinguish a co-grouped section from a standalone one")

	assert.Equal(t, "—", m.navNodes[1].typ, "a column carries its terse type")
	assert.Equal(t, "", m.navNodes[0].typ, "a section header does not")
}

func TestBuildNavSelections(t *testing.T) {
	m := NewModel(fixture())
	m.buildNav()

	assert.Equal(t, selection{}, m.navNodes[0].sel,
		"a plain item-type names a grouping, not a schema object, so it selects nothing")
	assert.Equal(t, selection{kind: selPlainColumn, plainCol: 1}, m.navNodes[2].sel)
	assert.Equal(t, selection{kind: selSection, section: 0}, m.navNodes[3].sel,
		"a tagged section's own row selects the section — what the '· properties' child did before")
	assert.Equal(t, selection{kind: selSectionColumn, section: 0, col: 0}, m.navNodes[4].sel)
	assert.Equal(t, selection{kind: selSection, section: 1}, m.navNodes[5].sel,
		"a value-less section is reachable the same way as any other")
}

func TestBuildNavFilterDropsWholeSections(t *testing.T) {
	m := NewModel(fixture())
	m.filter = "celsius"
	m.buildNav()

	// Filtering is per-section: a section matched by one of its column names
	// shows all of them, and a section that matches nothing disappears with
	// its columns.
	assert.Equal(t, []string{"readings", "celsius"}, m.navLabels)
	assert.Equal(t, []int32{-1, 0}, m.navParents)
}

func TestSyncNavProjectsTheModelsOwnState(t *testing.T) {
	m := NewModel(fixture())
	m.buildNav()
	m.syncNav()

	// Open by default, which is what the CollapsingHeader navigator's
	// DefaultOpen(true) meant.
	assert.True(t, m.navState.IsExpanded(0))
	assert.True(t, m.navState.IsExpanded(3))
	// NewModel's default selection is the first plain column, at node 1.
	assert.True(t, m.navState.IsSelected(1))
	assert.Equal(t, 1, m.navState.SelectionLen())

	m.setCollapsed("sec:readings", true)
	m.syncNav()
	assert.True(t, m.navState.IsExpanded(0))
	assert.False(t, m.navState.IsExpanded(3), "a closed section stays closed")
}

func TestSyncNavSurvivesAFilterRenumberingTheNodes(t *testing.T) {
	m := NewModel(fixture())
	m.buildNav()

	// Close the tagged section and select its column, then narrow the filter
	// so the plain item-type disappears and every node index shifts.
	m.setCollapsed("sec:readings", true)
	m.sel = selection{kind: selSectionColumn, section: 0, col: 0}

	m.filter = "readings"
	m.buildNav()
	m.syncNav()

	require.Equal(t, []string{"readings", "celsius"}, m.navLabels,
		"the section is now node 0, where the plain item-type used to be")
	assert.False(t, m.navState.IsExpanded(0), "the collapse followed the section, not the index")
	assert.True(t, m.navState.IsSelected(1), "and so did the selection")
}

func TestApplyNavWritesBackToTheModel(t *testing.T) {
	m := NewModel(fixture())
	m.buildNav()
	m.syncNav()

	// A toggle: the widget has already flipped its own State, so applyNav
	// reads it back rather than recomputing it.
	m.navState.ToggleExpanded(3)
	m.applyNav(tree.Result{Clicked: -1, Activated: -1, Toggled: 3})
	assert.True(t, m.collapsed["sec:readings"])

	m.navState.ToggleExpanded(3)
	m.applyNav(tree.Result{Clicked: -1, Activated: -1, Toggled: 3})
	assert.NotContains(t, m.collapsed, "sec:readings", "reopening drops the entry rather than storing false")

	// A click on a column selects it.
	m.applyNav(tree.Result{Clicked: 4, Activated: -1, Toggled: -1})
	assert.Equal(t, selection{kind: selSectionColumn, section: 0, col: 0}, m.sel)

	// A click on a plain item-type header leaves the selection where it was
	// and closes the header instead, since it has no detail to show.
	m.applyNav(tree.Result{Clicked: 0, Activated: -1, Toggled: -1})
	assert.Equal(t, selection{kind: selSectionColumn, section: 0, col: 0}, m.sel)
	assert.True(t, m.collapsed["plain:entity-id"])

	// The same click while the widget also toggled that row is the second
	// half of a double-click. Only the widget's toggle counts, or the two
	// would cancel and a double-click would do nothing at all.
	m.syncNav()
	m.navState.ToggleExpanded(0)
	m.applyNav(tree.Result{Clicked: 0, Activated: -1, Toggled: 0})
	assert.NotContains(t, m.collapsed, "plain:entity-id")
}
