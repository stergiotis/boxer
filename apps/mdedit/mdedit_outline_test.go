package mdedit

// Tests for the outline's hierarchy and the state that has to survive a
// rebuild. Everything here is the pure half — no binding is imported, the way
// the tree widget's own flatten is tested.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/tree"
)

// heading is the terse constructor these tables are written with. The byte
// offset is only read by the click path, so it is a distinct number rather
// than a real one except where a test says otherwise.
func heading(level uint8, slug string) markdown.HeadingInfo {
	return markdown.HeadingInfo{Text: slug, Slug: slug, Level: level, ByteOffset: int(level) * 100}
}

// ---------------------------------------------------------------------------
// Nesting
// ---------------------------------------------------------------------------

// TestOutlineNestsByHeadingLevel covers the rule and the three shapes a
// hand-written document takes that a strict level-equals-depth reading gets
// wrong.
func TestOutlineNestsByHeadingLevel(t *testing.T) {
	cases := []struct {
		name     string
		headings []markdown.HeadingInfo
		parents  []int32
	}{
		{
			name:     "plain nesting",
			headings: []markdown.HeadingInfo{heading(1, "a"), heading(2, "b"), heading(3, "c")},
			parents:  []int32{-1, 0, 1},
		},
		{
			name:     "siblings share a parent",
			headings: []markdown.HeadingInfo{heading(1, "a"), heading(2, "b"), heading(2, "c")},
			parents:  []int32{-1, 0, 0},
		},
		{
			name: "a skipped level nests under the nearest smaller one",
			// `#` straight to `###`, with no `##` between them.
			headings: []markdown.HeadingInfo{heading(1, "a"), heading(3, "b")},
			parents:  []int32{-1, 0},
		},
		{
			name: "no level 1 at all leaves a forest of level 2s",
			headings: []markdown.HeadingInfo{
				heading(2, "a"), heading(3, "b"), heading(2, "c"),
			},
			parents: []int32{-1, 0, -1},
		},
		{
			name: "coming back up re-parents to the right ancestor",
			headings: []markdown.HeadingInfo{
				heading(1, "a"), heading(2, "b"), heading(3, "c"), heading(2, "d"), heading(1, "e"),
			},
			parents: []int32{-1, 0, 1, 0, -1},
		},
		{
			name: "a deeper heading before any shallower one is a root",
			// A fragment that opens at `###`; there is nothing above it to
			// belong to, and inventing a document root would state a
			// containment the markdown does not.
			headings: []markdown.HeadingInfo{heading(3, "a"), heading(1, "b"), heading(2, "c")},
			parents:  []int32{-1, -1, 1},
		},
		{
			name: "a slugless heading is dropped, and does not break the nesting",
			headings: []markdown.HeadingInfo{
				heading(1, "a"), {Text: "", Slug: "", Level: 2}, heading(2, "b"),
			},
			parents: []int32{-1, 0},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m outlineModel
			m.build(tc.headings)
			assert.Equal(t, tc.parents, m.parents)
			// Whatever the shape, the widget must accept it: a tree that fails
			// validation draws an error message instead of an outline.
			require.NoError(t, m.tree().Validate())
		})
	}
}

func TestOutlineDescendantCounts(t *testing.T) {
	var m outlineModel
	m.build([]markdown.HeadingInfo{
		heading(1, "a"), heading(2, "b"), heading(3, "c"), heading(3, "d"), heading(2, "e"),
		heading(1, "f"),
	})
	// a holds everything under it at any depth, not just its own children.
	assert.Equal(t, 4, m.nodes[0].descendants, "a")
	assert.Equal(t, 2, m.nodes[1].descendants, "b")
	assert.Equal(t, 0, m.nodes[2].descendants, "c is a leaf")
	assert.Equal(t, 0, m.nodes[4].descendants, "e is a leaf")
	assert.Equal(t, 0, m.nodes[5].descendants, "f is a leaf")
}

func TestOutlineKeysDisambiguateDuplicateSlugs(t *testing.T) {
	var m outlineModel
	m.build([]markdown.HeadingInfo{
		heading(1, "notes"), heading(1, "notes"), heading(1, "other"),
	})
	assert.Equal(t, "notes#0", m.nodes[0].key)
	assert.Equal(t, "notes#1", m.nodes[1].key,
		"two sections with the same title must not share collapse state")
	assert.Equal(t, "other#0", m.nodes[2].key)
}

// TestOutlineBuildReusesItsSlices is the allocation property the retained
// model exists for: a rebuild refills in place rather than reallocating.
func TestOutlineBuildReusesItsSlices(t *testing.T) {
	var m outlineModel
	m.build([]markdown.HeadingInfo{heading(1, "a"), heading(2, "b"), heading(2, "c")})
	before := m.labels
	m.build([]markdown.HeadingInfo{heading(1, "a"), heading(2, "b")})
	require.Len(t, m.labels, 2)
	assert.Equal(t, cap(before), cap(m.labels), "the backing array is reused")
}

// ---------------------------------------------------------------------------
// State across a rebuild
// ---------------------------------------------------------------------------

// TestOutlineCollapseSurvivesAHeadingInsert is the reason the collapse map is
// keyed by slug and not by node index. The tree widget's State keys on the
// index, which is the only identity a columnar input has — and this app
// renumbers every node whenever the buffer changes. Inserting a section above
// a collapsed one would otherwise hand the collapse to whichever heading slid
// into the vacated slot.
func TestOutlineCollapseSurvivesAHeadingInsert(t *testing.T) {
	inst := &App{}
	inst.outline.build([]markdown.HeadingInfo{
		heading(1, "intro"), heading(1, "body"), heading(2, "detail"),
	})
	// The reader folds "body" away.
	inst.outlineSetCollapsed(inst.outline.nodes[1].key, true)
	inst.syncOutline()
	require.False(t, inst.outlineState.IsExpanded(1), "body is closed")

	// A new section is typed in above it, so every index below shifts by one.
	inst.outline.build([]markdown.HeadingInfo{
		heading(1, "intro"), heading(1, "preamble"), heading(1, "body"), heading(2, "detail"),
	})
	inst.syncOutline()
	assert.True(t, inst.outlineState.IsExpanded(1), "the new section is not the collapsed one")
	assert.False(t, inst.outlineState.IsExpanded(2), "body is still closed, at its new index")
}

// TestOutlineDefaultsToExpanded pins the choice the port made deliberately:
// the zero value is a fully open outline, which is exactly what the flat list
// M2 shipped showed. Collapsing is added without taking the old view away.
func TestOutlineDefaultsToExpanded(t *testing.T) {
	inst := &App{}
	inst.outline.build([]markdown.HeadingInfo{
		heading(1, "a"), heading(2, "b"), heading(3, "c"),
	})
	inst.syncOutline()
	for i := range inst.outline.nodes {
		assert.True(t, inst.outlineState.IsExpanded(int32(i)), "node %d starts open", i)
	}
	assert.Nil(t, inst.outlineCollapsed, "an all-open outline carries no map at all")
}

func TestOutlineCollapseAllSkipsLeaves(t *testing.T) {
	inst := &App{}
	inst.outline.build([]markdown.HeadingInfo{
		heading(1, "a"), heading(2, "b"), heading(1, "c"),
	})
	inst.outlineCollapseAll()
	assert.True(t, inst.outlineIsCollapsed("a#0"), "a has a child to hide")
	assert.False(t, inst.outlineIsCollapsed("b#0"), "b is a leaf")
	assert.False(t, inst.outlineIsCollapsed("c#0"), "c is a leaf")
	assert.Len(t, inst.outlineCollapsed, 1, "leaves leave no entries to walk")

	// Expand-all is the plain inverse and clears the map rather than filling
	// it with falses.
	clear(inst.outlineCollapsed)
	inst.syncOutline()
	assert.True(t, inst.outlineState.IsExpanded(0))
}

// TestOutlineSelectionFollowsTheCaret pins that the highlight is derived from
// where the caret is rather than remembered by the tree, so the outline can
// never disagree with the editor about which section is current.
func TestOutlineSelectionFollowsTheCaret(t *testing.T) {
	inst := &App{}
	inst.outline.build([]markdown.HeadingInfo{heading(1, "a"), heading(2, "b")})

	inst.caretSlug = "b"
	inst.syncOutline()
	assert.True(t, inst.outlineState.IsSelected(1))
	assert.Equal(t, 1, inst.outlineState.SelectionLen())

	// Above the first heading there is no section, and nothing is highlighted.
	inst.caretSlug = ""
	inst.syncOutline()
	assert.Equal(t, 0, inst.outlineState.SelectionLen())
}

// ---------------------------------------------------------------------------
// Reveal and click
// ---------------------------------------------------------------------------

// TestOutlineRevealOpensAncestorsInTheHostMap covers the half of the reveal
// the widget cannot do for itself. tree.State.Reveal opens the ancestors in
// the widget's own State — which syncOutline overwrites from the host map on
// the very next frame — so the host has to open them where they will last.
func TestOutlineRevealOpensAncestorsInTheHostMap(t *testing.T) {
	inst := &App{}
	inst.outline.build([]markdown.HeadingInfo{
		heading(1, "a"), heading(2, "b"), heading(3, "c"),
	})
	inst.outlineCollapseAll()
	require.True(t, inst.outlineIsCollapsed("a#0"))
	require.True(t, inst.outlineIsCollapsed("b#0"))

	// The caret lands in the innermost section, two closed levels down.
	inst.caretSlug = "c"
	require.True(t, inst.outlineReveal())
	assert.False(t, inst.outlineIsCollapsed("a#0"), "an ancestor is opened")
	assert.False(t, inst.outlineIsCollapsed("b#0"), "and so is the one between")

	// c itself is not touched: revealing a section says nothing about whether
	// its own children should be showing.
	inst.outline.build([]markdown.HeadingInfo{
		heading(1, "a"), heading(2, "b"), heading(3, "c"), heading(4, "d"),
	})
	inst.outlineSetCollapsed("c#0", true)
	inst.caretSlug = "c"
	inst.outlineRevealed = ""
	inst.outlineReveal()
	assert.True(t, inst.outlineIsCollapsed("c#0"), "the revealed section keeps its own state")
}

// TestOutlineRevealOnlyOnAChange is what keeps the outline from scrolling
// itself under the reader every frame — the same guard the preview's own
// scroll target has.
func TestOutlineRevealOnlyOnAChange(t *testing.T) {
	inst := &App{}
	inst.outline.build([]markdown.HeadingInfo{heading(1, "a"), heading(2, "b")})
	inst.outlineSetCollapsed("a#0", true)

	inst.caretSlug = "b"
	inst.outlineReveal()
	require.False(t, inst.outlineIsCollapsed("a#0"))

	// The reader deliberately folds the section they are working in. Nothing
	// has moved since, so the next frame leaves it folded.
	inst.outlineSetCollapsed("a#0", true)
	inst.outlineReveal()
	assert.True(t, inst.outlineIsCollapsed("a#0"), "a deliberate collapse is not fought")

	// Moving away and back does reveal it again.
	inst.caretSlug = "a"
	inst.outlineReveal()
	inst.caretSlug = "b"
	inst.outlineReveal()
	assert.False(t, inst.outlineIsCollapsed("a#0"))
}

// TestOutlineClickNavigatesWithoutMovingTheCaretBaseline is the M2
// reconciliation, carried over to the tree: a click must queue the preview
// scroll and ask for the caret, and must NOT update caretSlug itself — that is
// the baseline change detection compares against, and moving it here would
// make the next frame read the caret's real section as changed and drag the
// preview straight back off the heading just clicked.
func TestOutlineClickNavigatesWithoutMovingTheCaretBaseline(t *testing.T) {
	inst := &App{caretSlug: "first", src: "# First\n\ntext\n\n## Second\n\nmore\n"}
	inst.outline.build([]markdown.HeadingInfo{
		{Text: "First", Slug: "first", Level: 1, ByteOffset: 0},
		{Text: "Second", Slug: "second", Level: 2, ByteOffset: 16},
	})

	inst.applyOutline(tree.Result{Clicked: 1, Activated: -1, Toggled: -1})

	assert.Equal(t, "second", inst.pendingScroll, "the preview is asked to move")
	assert.Equal(t, "first", inst.caretSlug, "a click must not move the caret's baseline")
	require.True(t, inst.pendingCaretOk, "the writer is taken there too")
	assert.True(t, inst.pendingCaret.Focus, "\"take me there\" means focus")
	// The caret lands at the LINE start, before the `#`.
	assert.Equal(t, lineStart(inst.src, 16), inst.pendingCaret.Start)
	assert.Equal(t, inst.pendingCaret.Start, inst.pendingCaret.Stop)
}

// TestOutlineClickSuppressesItsOwnReveal: the caret arrives in the clicked
// section one frame later, which would otherwise read as a change and scroll
// the outline to centre a row the reader had already put their pointer on.
func TestOutlineClickSuppressesItsOwnReveal(t *testing.T) {
	inst := &App{caretSlug: "a"}
	inst.outline.build([]markdown.HeadingInfo{heading(1, "a"), heading(1, "b")})
	inst.outlineRevealed = "a"

	inst.applyOutline(tree.Result{Clicked: 1, Activated: -1, Toggled: -1})
	require.Equal(t, "b", inst.outlineRevealed)

	// The editor reports back next frame and the caret's section catches up.
	inst.caretSlug = "b"
	assert.False(t, inst.outlineReveal(),
		"no reveal is issued for a row the reader clicked")

	// A section the reader did NOT click still reveals normally.
	inst.caretSlug = "a"
	assert.True(t, inst.outlineReveal())
}

// TestOutlineToggleIsRecordedByKey covers the write-back that makes a
// disclosure click outlive the next reparse.
func TestOutlineToggleIsRecordedByKey(t *testing.T) {
	inst := &App{}
	inst.outline.build([]markdown.HeadingInfo{heading(1, "a"), heading(2, "b")})
	inst.syncOutline()

	// The widget has already applied the toggle to its own State by the time
	// the result comes back; this is the host catching up.
	inst.outlineState.SetExpanded(0, false)
	inst.applyOutline(tree.Result{Clicked: -1, Activated: -1, Toggled: 0})
	assert.True(t, inst.outlineIsCollapsed("a#0"))

	inst.outlineState.SetExpanded(0, true)
	inst.applyOutline(tree.Result{Clicked: -1, Activated: -1, Toggled: 0})
	assert.False(t, inst.outlineIsCollapsed("a#0"))
}

// TestOutlineApplyIgnoresStaleNodeIndices: a result describes the PREVIOUS
// frame's geometry, and a reparse between the two can have shortened the
// outline. An out-of-range node must be dropped rather than panic.
func TestOutlineApplyIgnoresStaleNodeIndices(t *testing.T) {
	inst := &App{}
	inst.outline.build([]markdown.HeadingInfo{heading(1, "a")})
	assert.NotPanics(t, func() {
		inst.applyOutline(tree.Result{Clicked: 7, Activated: -1, Toggled: 9})
	})
	assert.Empty(t, inst.pendingScroll)
	assert.False(t, inst.pendingCaretOk)
}

// ---------------------------------------------------------------------------
// Geometry
// ---------------------------------------------------------------------------

func TestOutlineColWidthTracksThePaneAndHasAFloor(t *testing.T) {
	inst := &App{}
	inst.outlinePaneW = 200
	assert.Equal(t, 200-outlineScrollbarPx-outlineCountWidthPx, inst.outlineColWidth())

	// Before the probe answers, and in a pane too narrow to divide, the floor
	// is what keeps the column from collapsing to nothing.
	inst.outlinePaneW = 0
	assert.Equal(t, outlineMinColWidthPx, inst.outlineColWidth())
	inst.outlinePaneW = 60
	assert.Equal(t, outlineMinColWidthPx, inst.outlineColWidth())
}
