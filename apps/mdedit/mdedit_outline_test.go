package mdedit

// Tests for what an outline click means in this app. The hierarchy, the
// fold and the reveal are the mdoutline widget's and are tested there.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/mdoutline"
)

// TestOutlineClickNavigatesWithoutMovingTheCaretBaseline: a click must queue
// the preview scroll and ask for the caret, and must NOT update caretSlug
// itself — that is the baseline change detection compares against, and
// moving it here would make the next frame read the caret's real section as
// changed and drag the preview straight back off the heading just clicked.
func TestOutlineClickNavigatesWithoutMovingTheCaretBaseline(t *testing.T) {
	inst := &App{caretSlug: "first", src: "# First\n\ntext\n\n## Second\n\nmore\n"}
	inst.outline.Model.Build([]markdown.HeadingInfo{
		{Text: "First", Slug: "first", Level: 1, ByteOffset: 0},
		{Text: "Second", Slug: "second", Level: 2, ByteOffset: 16},
	})

	inst.applyOutline(mdoutline.Result{Clicked: 1, Node: inst.outline.Model.Node(1)})

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
	inst.outline.Model.Build([]markdown.HeadingInfo{
		{Text: "a", Slug: "a", Level: 1, ByteOffset: 0},
		{Text: "b", Slug: "b", Level: 1, ByteOffset: 10},
	})
	inst.outline.MarkRevealed("a")

	inst.applyOutline(mdoutline.Result{Clicked: 1, Node: inst.outline.Model.Node(1)})

	// The editor reports back next frame and the caret's section catches up.
	assert.False(t, inst.outline.Reveal("b"), "no reveal is issued for a row the reader clicked")
	// A section the reader did NOT click still reveals normally.
	assert.True(t, inst.outline.Reveal("a"))
}

// TestOutlineApplyIgnoresNoClick: a frame with no click changes nothing.
func TestOutlineApplyIgnoresNoClick(t *testing.T) {
	inst := &App{}
	inst.applyOutline(mdoutline.Result{Clicked: -1})
	assert.Empty(t, inst.pendingScroll)
	assert.False(t, inst.pendingCaretOk)
}

func TestOutlineSummaryCountsSluggedHeadings(t *testing.T) {
	assert.Equal(t, "Outline (0)", outlineSummary(nil))
	assert.Equal(t, "Outline (2)", outlineSummary([]markdown.HeadingInfo{
		{Slug: "a", Level: 1}, {Slug: "", Level: 2}, {Slug: "b", Level: 2},
	}))
}
