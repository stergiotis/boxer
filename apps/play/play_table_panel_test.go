package play

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Accept claims any non-nil result; the claim is the selection row read from the
// signal env.
func TestTablePanelAcceptClaimsSelectionRow(t *testing.T) {
	p := tablePanel{}
	claim, reason := p.AcceptForChannel(chMain, schemaWith(strField("c")), sigWith(4))
	require.Empty(t, reason)
	row, ok := claim.(int64)
	require.True(t, ok)
	require.Equal(t, int64(4), row)
}

// Unlike Detail, the Table still renders with no selection (-1 ⇒ nothing
// highlighted), so Accept claims rather than rejecting.
func TestTablePanelClaimsWithoutSelection(t *testing.T) {
	p := tablePanel{}
	claim, reason := p.AcceptForChannel(chMain, schemaWith(strField("c")), sigWith(-1))
	require.Empty(t, reason)
	row, _ := claim.(int64)
	require.Equal(t, int64(-1), row)
}

func TestTablePanelRejectsNilSchema(t *testing.T) {
	p := tablePanel{}
	claim, reason := p.AcceptForChannel(chMain, nil, sigWith(0))
	require.Nil(t, claim)
	require.NotEmpty(t, reason)
}

func TestTablePanelDeclaresMainChannel(t *testing.T) {
	var p PanelI = tablePanel{}
	require.Equal(t, PanelID("table"), p.ID())
	require.Equal(t, []ChannelSpec{{ID: chMain, Required: true, Label: "rows"}}, p.Channels())
}

// The re-fit trigger has to fire exactly once per change: never firing
// leaves a new result wearing the previous one's column widths, and firing
// every frame re-measures the columns while the user is reading them.
func TestTableColsChangedFiresOncePerChange(t *testing.T) {
	inst := &PlayApp{}
	a := schemaWith(strField("x"), strField("y"))
	b := schemaWith(strField("x"), strField("y"))

	require.True(t, inst.tableColsChanged(a, []int{0, 1}), "the first result is a change")
	require.False(t, inst.tableColsChanged(a, []int{0, 1}), "a steady frame is not")

	// A new query: same column count and names, but a different result and
	// so a different *arrow.Schema.
	require.True(t, inst.tableColsChanged(b, []int{0, 1}), "a new result re-fits")
	require.False(t, inst.tableColsChanged(b, []int{0, 1}))

	// The options bar reveals a column without touching the schema. This is
	// the case that keying on the schema alone would miss, and it is the one
	// that puts a column into a slot sized for its predecessor.
	require.True(t, inst.tableColsChanged(b, []int{0, 1, 2}), "a revealed column re-fits")
	require.False(t, inst.tableColsChanged(b, []int{0, 1, 2}))

	// Hiding one again is equally a change, and a reordering with the same
	// length must not slip through a length-only comparison.
	require.True(t, inst.tableColsChanged(b, []int{0, 1}))
	require.True(t, inst.tableColsChanged(b, []int{1, 0}), "a reorder changes what each slot holds")
	require.False(t, inst.tableColsChanged(b, []int{1, 0}))
}

// The recorded column set must be a copy: visibleTableCols hands back a
// slice the next frame is free to overwrite, and retaining it would make
// every later comparison compare a slice with itself.
func TestTableColsChangedCopiesTheColumnSet(t *testing.T) {
	inst := &PlayApp{}
	s := schemaWith(strField("x"))
	cols := []int{0, 1}
	require.True(t, inst.tableColsChanged(s, cols))

	cols[1] = 7 // the caller's buffer, reused
	require.True(t, inst.tableColsChanged(s, cols), "a mutated column set is a change")
}
