package tree

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// applySelection is the one piece of render.go that is pure enough to test
// without a renderer, and the one piece the live probe could not reach: the
// egui-mcp driver carries a modifier on the click event but not into the
// frame's modifier register, so ctrl- and shift-click cannot be synthesized.
// clickMode is the thin part that reads that register; everything the mode
// then means is here.

// selTree is a two-root forest, both roots expanded:
//
//	a        (0)      row 0
//	  a1     (1)      row 1
//	  a2     (2)      row 2
//	b        (3)      row 3
//	  b1     (4)      row 4
func selTree(t *testing.T) (Tree, []Row, *State) {
	t.Helper()
	tr := Tree{
		Labels:  []string{"a", "a1", "a2", "b", "b1"},
		Parents: []int32{-1, 0, 0, -1, 3},
	}
	st := &State{}
	st.SetExpanded(0, true)
	st.SetExpanded(3, true)
	rows, err := Flatten(tr, st, nil)
	require.NoError(t, err)
	require.Len(t, rows, 5)
	return tr, rows, st
}

func selected(st *State) []int32 {
	out := st.Selection(nil)
	// Map order is arbitrary; insertion-sort the handful of entries so the
	// assertions can compare slices.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func TestApplySelectionReplace(t *testing.T) {
	_, rows, st := selTree(t)

	applySelection(st, rows, 1, selectReplace)
	assert.Equal(t, []int32{1}, selected(st))
	assert.Equal(t, int32(1), st.Cursor())

	// A second plain click replaces rather than accumulates.
	applySelection(st, rows, 4, selectReplace)
	assert.Equal(t, []int32{4}, selected(st))
	assert.Equal(t, int32(4), st.Cursor())
}

func TestApplySelectionToggle(t *testing.T) {
	_, rows, st := selTree(t)

	applySelection(st, rows, 1, selectReplace)
	applySelection(st, rows, 4, selectToggle)
	assert.Equal(t, []int32{1, 4}, selected(st), "ctrl-click adds to the selection")
	assert.Equal(t, int32(4), st.Cursor())

	applySelection(st, rows, 4, selectToggle)
	assert.Equal(t, []int32{1}, selected(st), "ctrl-clicking a selected row removes it")
	assert.Equal(t, int32(4), st.Cursor(), "the cursor stays on the row clicked, selected or not")
}

func TestApplySelectionExtend(t *testing.T) {
	_, rows, st := selTree(t)

	applySelection(st, rows, 1, selectReplace)
	applySelection(st, rows, 3, selectExtend)
	assert.Equal(t, []int32{1, 2, 3}, selected(st), "shift-click covers the rows between")
	assert.Equal(t, int32(1), st.Cursor(),
		"the cursor stays on the anchor so the next shift-click re-extends from it")

	// Re-extending from the same anchor, this time upward, replaces the range
	// rather than adding to it.
	applySelection(st, rows, 0, selectExtend)
	assert.Equal(t, []int32{0, 1}, selected(st))
	assert.Equal(t, int32(1), st.Cursor())
}

func TestApplySelectionExtendWithoutAnchorIsAPlainClick(t *testing.T) {
	_, rows, st := selTree(t)

	// No cursor at all — nothing has been clicked yet.
	applySelection(st, rows, 2, selectExtend)
	assert.Equal(t, []int32{2}, selected(st))
	assert.Equal(t, int32(2), st.Cursor())
}

func TestApplySelectionExtendFromACollapsedAwayCursor(t *testing.T) {
	tr, _, st := selTree(t)

	// Put the cursor on a1, then collapse its parent so the cursor's row is
	// gone. The extend has no anchor to reach and degrades to a plain click.
	st.SetCursor(1)
	st.SetExpanded(0, false)
	rows, err := Flatten(tr, st, nil)
	require.NoError(t, err)
	require.Equal(t, -1, RowOf(rows, st.Cursor()), "the cursor's row really is gone")

	applySelection(st, rows, 1, selectExtend) // row 1 is now b
	assert.Equal(t, []int32{3}, selected(st))
	assert.Equal(t, int32(3), st.Cursor())
}
