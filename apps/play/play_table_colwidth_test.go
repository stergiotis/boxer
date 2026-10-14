package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/recordtable"
)

// colWidthTestSchema is a two-column result whose second column is a List —
// the case the view discriminator exists for. The per-DB-row grid renders a
// List packed as `[len=N]`, the per-attribute grid explodes it to its inner
// scalar, so the two want different widths for the very same field.
func colWidthTestSchema() *arrow.Schema {
	return arrow.NewSchema([]arrow.Field{
		{Name: "id:id:u64:47::0:", Type: arrow.PrimitiveTypes.Uint64},
		{Name: "tv:symbol:value:val:s:124::I:0::data", Type: arrow.ListOf(arrow.BinaryTypes.String)},
	}, nil)
}

// masterKeys is the per-DB-row grid's width identity: what RenderGrid
// resolves under for the columns renderMasterTable lays out.
func masterKeys(schema *arrow.Schema, visCols []int) []colwidth.Column {
	return recordtable.WidthColumns(schema, masterGridColumns(visCols, nil), tableViewTagRow, true)
}

// The two Table granularities must never share a column-width identity: the
// column tier (ADR-0151 §SD1) matches on (name, type) alone and crosses
// tables, so without the view tag a width dragged in one grid would be applied
// to the other, which renders the same column's contents differently.
func TestColumnKeys_GridsDoNotShareIdentity(t *testing.T) {
	inst := &PlayApp{}
	schema := colWidthTestSchema()
	visCols := []int{0, 1}

	master := masterKeys(schema, visCols)
	attr := inst.attrColumnKeys(schema, visCols)

	require.Len(t, master, len(visCols)+1, "leading # column must take part so the positional read-back lines up")
	require.Len(t, attr, len(visCols)+1)

	for i := range master {
		assert.Equal(t, master[i].Name, attr[i].Name,
			"the two grids show the same field at slot %d; only the render type may differ", i)
		assert.NotEqual(t, master[i].Key(), attr[i].Key(),
			"slot %d (%q) must not share a column-tier key across grids", i, master[i].Name)
	}
}

// The discriminator is appended, not substituted, so a genuine Arrow type
// change still invalidates a stored width — the property §SD1 relies on to
// avoid needing a rule anyone has to remember.
func TestColumnKeys_TypeChangeStillInvalidates(t *testing.T) {
	inst := &PlayApp{}
	visCols := []int{0}

	asU64 := arrow.NewSchema([]arrow.Field{{Name: "col", Type: arrow.PrimitiveTypes.Uint64}}, nil)
	asStr := arrow.NewSchema([]arrow.Field{{Name: "col", Type: arrow.BinaryTypes.String}}, nil)

	for _, tc := range []struct {
		name string
		keys func(*arrow.Schema, []int) []colwidth.Column
	}{
		{"master", masterKeys},
		{"attr", inst.attrColumnKeys},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := tc.keys(asU64, visCols)
			after := tc.keys(asStr, visCols)
			// Slot 1 is the data column; slot 0 is the fixed "#" gutter.
			assert.NotEqual(t, before[1].Key(), after[1].Key(),
				"a column whose Arrow type changed must drop its stored width")
			assert.Equal(t, before[0].Key(), after[0].Key(),
				"the # gutter's identity does not depend on the result's types")
		})
	}
}

// The "#" gutter is fixed-width and never resizable, so it can never acquire
// an override — but it must still be present and distinct per grid, because
// the width read-back is positional and a missing leading entry would shift
// every later column onto the wrong identity.
func TestColumnKeys_RowNumGutterIsDistinctPerGrid(t *testing.T) {
	inst := &PlayApp{}
	schema := colWidthTestSchema()

	master := masterKeys(schema, []int{0, 1})
	attr := inst.attrColumnKeys(schema, []int{0, 1})

	assert.Equal(t, "#", master[0].Name)
	assert.Equal(t, "#", attr[0].Name)
	assert.NotEqual(t, master[0].Key(), attr[0].Key())
}

// The identity columns (ADR-0219) are computed, so their width identity is
// their caption and a format tag of their own — never an Arrow column's, and
// with the row view's tag like every other column in this grid.
func TestColumnKeys_IdentityColumns(t *testing.T) {
	schema := colWidthTestSchema()
	keys := recordtable.WidthColumns(schema, masterGridColumns([]int{0}, []int{2, 3}), tableViewTagRow, true)
	require.Len(t, keys, 4)
	assert.Equal(t, colwidth.Column{Name: "canonform", Type: "identity" + tableViewTagRow}, keys[2])
	assert.Equal(t, colwidth.Column{Name: "canonwire", Type: "identity" + tableViewTagRow}, keys[3])
}

// The two grids' instance-tier scopes must differ too, or one grid's drag
// would land on the other's entry even with the column tier kept apart.
func TestTableTags_DifferBetweenGrids(t *testing.T) {
	assert.NotEqual(t, masterTableTag, attrTableTag)
}
