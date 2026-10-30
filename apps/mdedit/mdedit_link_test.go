package mdedit

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/mdedit/launchcfg"
	"github.com/stergiotis/boxer/apps/mdedit/mdeditlink"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

const linkDoc = "intro ^p1\n\n# First\n\nbody\n\n## Second Heading\n\nmore ^p2\n"

// handWith opens a window on a launch config and runs the frame steps a
// location needs: the parse, then the location.
func handWith(t *testing.T, l launchcfg.MdeditLaunch) (inst *App) {
	t.Helper()
	inst = newApp()
	wire, err := buscodec.Encode(l)
	require.NoError(t, err)
	require.True(t, inst.handOver(wire))
	inst.refreshDerived()
	inst.applyPendingLocation()
	// The same frame's caret tracking, which reads the caret the editor
	// reported — stale until the editor reports the requested one.
	inst.trackCaretSection()
	return
}

// The mode is the launch's (ADR-0347 §SD3); a block id scrolls the preview
// to its block, and in Read mode no caret is asked for, since no pane shows
// one. An unknown mode opens in Split and says so.
func TestLaunchModeAndBlockLocation(t *testing.T) {
	inst := handWith(t, launchcfg.MdeditLaunch{Text: linkDoc, Mode: "read", Location: "^p2"})
	assert.Equal(t, viewRead, inst.viewMode)
	assert.Equal(t, "^p2", inst.pendingScroll)
	assert.False(t, inst.pendingCaretOk, "read mode shows no caret")
	assert.False(t, inst.pendingLocationOk, "consumed")

	inst = handWith(t, launchcfg.MdeditLaunch{Text: linkDoc, Mode: "sideways"})
	assert.Equal(t, viewSplit, inst.viewMode)
	assert.Contains(t, inst.status, "unknown view mode")
}

// A line scrolls to the block holding it and, in a mode with a caret, moves
// the caret there with the baseline section and the reported caret set, so
// neither this frame's tracking nor the caret's arrival next frame is read
// as a change that drags the preview back to the heading.
func TestLineLocationMovesTheCaretWithItsBaseline(t *testing.T) {
	inst := handWith(t, launchcfg.MdeditLaunch{Text: linkDoc, Location: "L9"})
	off, ok := lineOffset(linkDoc, 9)
	require.True(t, ok)
	assert.Equal(t, "more ^p2\n", linkDoc[off:])
	assert.True(t, inst.pendingScrollOffOk)
	assert.Equal(t, off, inst.pendingScrollOff)
	require.True(t, inst.pendingCaretOk)
	assert.Equal(t, off, inst.pendingCaret.Start)
	assert.False(t, inst.pendingCaret.Focus, "a launch does not focus the source pane")
	assert.Equal(t, "second-heading", inst.caretSlug)
	caret, _ := c.UnpackCursorRange(inst.cursor)
	assert.Equal(t, len([]rune(linkDoc[:off])), caret, "the reported caret is the target already")

	got, ok := inst.takeScrollOffset()
	assert.True(t, ok)
	assert.Equal(t, off, got)
	_, ok = inst.takeScrollOffset()
	assert.False(t, ok, "one-shot")

	// A block target in Split mode survives the frame's caret tracking and
	// the caret's arrival in the block's section next frame.
	inst = handWith(t, launchcfg.MdeditLaunch{Text: linkDoc, Location: "^p2"})
	assert.Equal(t, "^p2", inst.pendingScroll)
	inst.trackCaretSection()
	assert.Equal(t, "^p2", inst.pendingScroll, "the caret landing where it was sent is no change")
}

// A heading slug, in any case the author spelled it, scrolls to the heading
// and puts the caret on its line.
func TestHeadingLocation(t *testing.T) {
	inst := handWith(t, launchcfg.MdeditLaunch{Text: linkDoc, Location: "Second-Heading"})
	assert.Equal(t, "second-heading", inst.pendingScroll)
	require.True(t, inst.pendingCaretOk)
	assert.Equal(t, "## Second Heading", linkDoc[inst.pendingCaret.Start:inst.pendingCaret.Start+17])
}

// A location the document does not have opens at the top and says so; one
// that is not in the grammar says that instead. Neither refuses the open.
func TestMissingLocationSaysSo(t *testing.T) {
	inst := handWith(t, launchcfg.MdeditLaunch{Text: linkDoc, Location: "^nope"})
	assert.Contains(t, inst.status, "not in this document")
	assert.Empty(t, inst.pendingScroll)
	assert.False(t, inst.pendingCaretOk)

	inst = handWith(t, launchcfg.MdeditLaunch{Text: linkDoc, Location: "L99"})
	assert.Contains(t, inst.status, "not in this document")

	inst = handWith(t, launchcfg.MdeditLaunch{Text: linkDoc, Location: "L0"})
	assert.Contains(t, inst.status, "is not a heading")
	assert.Equal(t, linkDoc, inst.src, "the document is open regardless")
}

// A snapshot handover's location refers to the snapshot's bytes: once its
// text lands and replaces the handed text, the location is queued again.
func TestLocationIsAppliedAgainWhenTheSnapshotLands(t *testing.T) {
	inst := newApp()
	wire, err := buscodec.Encode(launchcfg.MdeditLaunch{Text: "", Name: "a.md", Mount: testRef.mount.Value(), SnapNanos: testRef.snap.UnixNano(), Path: "a.md", Mode: "read", Location: "L2"})
	require.NoError(t, err)
	require.True(t, inst.handOver(wire))
	inst.refreshDerived()
	inst.applyPendingLocation()
	assert.True(t, inst.pendingLocationOk, "waits for the snapshot's text rather than judging the handed one")
	assert.NotContains(t, inst.status, "not in this document")
	_, ok := inst.linkHere()
	assert.False(t, ok, "no link until the snapshot's text is in the buffer")

	inst.startHandedLoad(fstest.MapFS{"a.md": {Data: []byte("one\ntwo\n")}}, inst.handedSnap)
	inst.handedSnap = nil
	drainLoadUntil(t, inst)
	assert.Equal(t, "one\ntwo\n", inst.src)
	require.True(t, inst.pendingLocationOk, "queued again over the snapshot's text")
	inst.refreshDerived()
	inst.applyPendingLocation()
	assert.True(t, inst.pendingScrollOffOk)
	assert.Equal(t, 4, inst.pendingScrollOff)
	require.NotNil(t, inst.snapRef)
	assert.Equal(t, "a.md", inst.snapRef.path, "the landed document knows its path, for its own link")
}

// The window's own link (ADR-0347 §SD6): the snapshot triple, the caret's
// section and the mode; none for a document not from a snapshot.
func TestLinkHere(t *testing.T) {
	inst := newApp()
	inst.src = linkDoc
	inst.refreshDerived()
	_, ok := inst.linkHere()
	assert.False(t, ok, "no snapshot, no identity")

	inst.snapRef = &snapshotRef{mount: testRef.mount, snap: testRef.snap, path: "notes/a.md"}
	_, ok = inst.linkHere()
	assert.False(t, ok, "a snapshot named but not yet read is not a document to link")
	inst.readFromSnapshot = true
	inst.viewMode = viewRead
	ch := len([]rune(linkDoc[:len(linkDoc)-3]))
	inst.cursor = c.PackCursorRange(ch, ch)
	link, ok := inst.linkHere()
	require.True(t, ok)
	r, err := mdeditlink.Parse(link)
	require.NoError(t, err)
	assert.Equal(t, testRef.mount, r.Mount)
	assert.Equal(t, testRef.snap.UTC(), r.Snap)
	assert.Equal(t, "notes/a.md", r.Path)
	assert.Equal(t, mdeditlink.ModeRead, r.Mode)
	assert.Equal(t, "second-heading", r.Fragment)

	inst.cursor = 0
	link, _ = inst.linkHere()
	r, err = mdeditlink.Parse(link)
	require.NoError(t, err)
	assert.Equal(t, "", r.Fragment, "before the first heading there is no section")
}

func TestLineOffset(t *testing.T) {
	src := "a\nbb\n\nd"
	for n, want := range map[int]int{1: 0, 2: 2, 3: 5, 4: 6} {
		off, ok := lineOffset(src, n)
		require.True(t, ok, n)
		assert.Equal(t, want, off, n)
	}
	for _, n := range []int{0, 5} {
		_, ok := lineOffset(src, n)
		assert.False(t, ok, n)
	}
	_, ok := lineOffset("one\n", 2)
	assert.False(t, ok, "a trailing newline opens no line")
}
