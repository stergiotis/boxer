package mdoutline

// Tests for the state that has to survive a rebuild: expansion filed under
// keys, selection derived from the current section, and the reveal that is
// issued only on a change.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// TestCollapseSurvivesAHeadingInsert is the reason the hierarchy carries a
// key column. A node index is the only identity a columnar input has on its
// own, and a host renumbers every node whenever the source changes;
// inserting a section above a collapsed one would otherwise hand the
// collapse to whichever heading slid into the vacated slot.
func TestCollapseSurvivesAHeadingInsert(t *testing.T) {
	var v View
	v.Model.Build([]markdown.HeadingInfo{heading(1, "intro"), heading(1, "body"), heading(2, "detail")})
	v.Select("")
	// The reader folds "body" away.
	v.State.SetExpanded(1, false)
	require.False(t, v.State.IsExpanded(1), "body is closed")

	// A new section is typed in above it, so every index below shifts.
	v.Model.Build([]markdown.HeadingInfo{
		heading(1, "intro"), heading(1, "preamble"), heading(1, "body"), heading(2, "detail"),
	})
	v.Select("")
	assert.True(t, v.State.IsExpanded(1), "the new section is not the collapsed one")
	assert.False(t, v.State.IsExpanded(2), "body is still closed, at its new index")
}

// TestDefaultsToExpanded pins the choice: the zero value is a fully open
// outline, and collapsing is added without taking that away.
func TestDefaultsToExpanded(t *testing.T) {
	var v View
	v.Model.Build([]markdown.HeadingInfo{heading(1, "a"), heading(2, "b"), heading(3, "c")})
	v.Select("")
	for i := 0; i < v.Model.Len(); i++ {
		assert.True(t, v.State.IsExpanded(int32(i)), "node %d starts open", i)
	}
}

func TestCollapseAllSkipsLeaves(t *testing.T) {
	var v View
	v.Model.Build([]markdown.HeadingInfo{heading(1, "a"), heading(2, "b"), heading(1, "c")})
	v.Select("")
	v.CollapseAll()
	assert.False(t, v.State.IsExpanded(0), "a has a child to hide")
	assert.True(t, v.State.IsExpanded(1), "b is a leaf and is left alone")
	assert.True(t, v.State.IsExpanded(2), "c is a leaf and is left alone")

	v.ExpandAll()
	assert.True(t, v.State.IsExpanded(0))
}

// TestCollapseAllLeavesLaterHeadingsOpen is why collapse-all is a loop over
// the nodes rather than tree.State.CollapseAll, which would also flip the
// default: a section written after the fold has never been closed by anyone
// and has to arrive showing.
func TestCollapseAllLeavesLaterHeadingsOpen(t *testing.T) {
	var v View
	v.Model.Build([]markdown.HeadingInfo{heading(1, "a"), heading(2, "b")})
	v.Select("")
	v.CollapseAll()

	v.Model.Build([]markdown.HeadingInfo{
		heading(1, "a"), heading(2, "b"), heading(1, "c"), heading(2, "d"),
	})
	v.Select("")
	assert.False(t, v.State.IsExpanded(0), "a stays folded")
	assert.True(t, v.State.IsExpanded(2), "c was written afterwards and is open")
}

// TestSelectionFollowsTheCurrentSection pins that the highlight is derived
// from where the reader is rather than remembered by the tree.
func TestSelectionFollowsTheCurrentSection(t *testing.T) {
	var v View
	v.Model.Build([]markdown.HeadingInfo{heading(1, "a"), heading(2, "b")})

	v.Select("b")
	assert.True(t, v.State.IsSelected(1))
	assert.Equal(t, 1, v.State.SelectionLen())

	// Above the first heading there is no section, and nothing is
	// highlighted.
	v.Select("")
	assert.Equal(t, 0, v.State.SelectionLen())
}

// TestRevealAsksForTheCurrentSection covers the view's half of the reveal:
// naming the node. Opening its ancestors is the widget's half.
func TestRevealAsksForTheCurrentSection(t *testing.T) {
	var v View
	v.Model.Build([]markdown.HeadingInfo{heading(1, "a"), heading(2, "b"), heading(3, "c")})
	v.Select("")
	v.CollapseAll()

	// The reader lands in the innermost section, two closed levels down.
	require.True(t, v.Reveal("c"))
	assert.Equal(t, int32(2), v.State.PendingReveal(),
		"the request names c; the widget opens a and b on its way to it")
}

// TestRevealOnlyOnAChange is what keeps the outline from scrolling itself
// under the reader every frame, and what lets a deliberate collapse stand.
func TestRevealOnlyOnAChange(t *testing.T) {
	var v View
	v.Model.Build([]markdown.HeadingInfo{heading(1, "a"), heading(2, "b")})
	v.Select("")

	require.True(t, v.Reveal("b"))
	require.Equal(t, int32(1), v.State.PendingReveal())
	assert.False(t, v.Reveal("b"), "a reader that has stayed put re-issues nothing")

	// Moving away and back does reveal it again.
	assert.True(t, v.Reveal("a"))
	assert.True(t, v.Reveal("b"))
}

// TestMarkRevealedSuppressesTheNextReveal: a host marks the section it just
// navigated to, so the reader arriving there one frame later does not
// scroll the outline under a pointer that was already on target.
func TestMarkRevealedSuppressesTheNextReveal(t *testing.T) {
	var v View
	v.Model.Build([]markdown.HeadingInfo{heading(1, "a"), heading(1, "b")})
	v.MarkRevealed("b")
	assert.False(t, v.Reveal("b"), "no reveal is issued for a row the reader clicked")
	assert.True(t, v.Reveal("a"), "a section the reader did not click still reveals")
}

// TestSyncRebuildsOnANewParse: the gate is the Doc pointer, so the same
// parse rebuilds nothing and a new one rebuilds everything.
func TestSyncRebuildsOnANewParse(t *testing.T) {
	var v View
	d1 := markdown.Parse([]byte("# One\n\n## Two\n"))
	v.Sync(d1)
	require.Equal(t, 2, v.Model.Len())
	v.State.SetExpanded(0, false)

	v.Sync(d1)
	assert.False(t, v.State.IsExpanded(0), "the same parse keeps the fold")

	d2 := markdown.Parse([]byte("# Zero\n\n# One\n\n## Two\n"))
	v.Sync(d2)
	require.Equal(t, 3, v.Model.Len())
	assert.False(t, v.State.IsExpanded(1), "the fold followed its key to the new index")

	v.Sync(nil)
	assert.Equal(t, 0, v.Model.Len())
}

func TestColWidthTracksThePaneAndHasAFloor(t *testing.T) {
	var v View
	v.SetPaneWidth(200)
	assert.Equal(t, 200-ScrollbarPx-CountWidthPx, v.ColWidth())

	// Before the probe answers, and in a pane too narrow to divide, the
	// floor keeps the column from collapsing to nothing.
	v.SetPaneWidth(0)
	assert.Equal(t, MinColWidthPx, v.ColWidth())
	v.SetPaneWidth(60)
	assert.Equal(t, MinColWidthPx, v.ColWidth())
}
