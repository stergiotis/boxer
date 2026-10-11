package mdedit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// ---------------------------------------------------------------------------
// The catalog
// ---------------------------------------------------------------------------

func TestTheCatalogRegisters(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))
	for _, name := range []string{opGetDocument, opProposeEdit, opGetProposal} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
	}
}

func TestCheckProposal(t *testing.T) {
	src := "héllo world"
	rev := documentRevision(src)
	ok := ProposeEditArgs{Title: "t", Revision: rev, Start: 0, Stop: len(src), Replacement: "x"}
	require.NoError(t, checkProposal(src, ok))

	insert := ok
	insert.Start, insert.Stop = 3, 3
	assert.NoError(t, checkProposal(src, insert), "a collapsed span inserts")

	stale := ok
	stale.Revision = documentRevision("something else")
	assert.ErrorContains(t, checkProposal(src, stale), "changed")

	untitled := ok
	untitled.Title = ""
	assert.Error(t, checkProposal(src, untitled))

	for _, span := range [][2]int{{-1, 2}, {3, 2}, {0, len(src) + 1}} {
		bad := ok
		bad.Start, bad.Stop = span[0], span[1]
		assert.ErrorContains(t, checkProposal(src, bad), "outside", "%v", span)
	}

	// 'é' is two bytes, at 1..3: byte 2 is inside it.
	cut := ok
	cut.Start, cut.Stop = 2, 5
	assert.ErrorContains(t, checkProposal(src, cut), "cuts a character")
}

func TestDocumentRevision(t *testing.T) {
	assert.Equal(t, documentRevision("a"), documentRevision("a"))
	assert.NotEqual(t, documentRevision("a"), documentRevision("b"))
}

// ---------------------------------------------------------------------------
// The splice and its staleness rule
// ---------------------------------------------------------------------------

func TestSpliceResult(t *testing.T) {
	assert.Equal(t, "aXc", spliceResult("abc", 1, 2, "X"))
	assert.Equal(t, "X", spliceResult("abc", 0, 3, "X"), "document scope replaces everything")
	assert.Equal(t, "Xabc", spliceResult("abc", 0, 0, "X"), "a collapsed span inserts")
	assert.Equal(t, "abc", spliceResult("abc", 1, 1, ""), "empty over empty is identity")
}

func TestApplyProposal_SplicesTheProposedSpan(t *testing.T) {
	inst := &App{src: "keep THIS keep"}
	inst.offerProposal(proposal{title: "swap", content: "THAT", writer: "task:1"}, 5, 9)
	require.True(t, inst.proposalPaneOpen())

	inst.applyProposal()

	assert.Equal(t, "keep THAT keep", inst.src)
	assert.True(t, inst.rebindSrc, "an apply is a rebind, not typing")
	assert.False(t, inst.proposalPaneOpen(), "the verdict closes the pane")
	assert.Equal(t, outcomeApplied, inst.prop.outcome)
}

func TestApplyProposal_RefusesAMovedBuffer(t *testing.T) {
	inst := &App{src: "what was proposed against"}
	inst.offerProposal(proposal{title: "swap", content: "THAT"}, 0, 4)
	inst.src = "the reader typed meanwhile"

	inst.applyProposal()

	assert.Equal(t, "the reader typed meanwhile", inst.src, "a splice computed against one buffer must not land on another")
	assert.False(t, inst.rebindSrc)
	assert.True(t, inst.proposalPaneOpen(), "the proposal stays for copy or discard")
	assert.Contains(t, inst.status, "changed")
	assert.Empty(t, inst.prop.outcome, "no verdict yet")
}

func TestProposalOutcomes(t *testing.T) {
	inst := &App{src: "abc"}
	inst.offerProposal(proposal{title: "one", content: "x"}, 0, 1)
	inst.offerProposal(proposal{title: "two", content: "y"}, 0, 1)
	assert.Equal(t, outcomeSuperseded, inst.prop.outcome, "a second proposal supersedes the first")
	assert.Equal(t, "two", inst.prop.pending.title)

	inst.discardProposal()
	assert.False(t, inst.proposalPaneOpen())
	assert.Equal(t, outcomeDiscarded, inst.prop.outcome)
	assert.Equal(t, "abc", inst.src, "discard leaves the buffer alone")
}

// ---------------------------------------------------------------------------
// The selection
// ---------------------------------------------------------------------------

func TestSelectionSpan(t *testing.T) {
	inst := &App{src: "one two three"}
	inst.cursor = packCursorForTest(4, 7)
	start, stop, ok := inst.selectionSpan()
	assert.True(t, ok)
	assert.Equal(t, [2]int{4, 7}, [2]int{start, stop})

	// A collapsed caret is no selection.
	inst.cursor = packCursorForTest(4, 4)
	_, _, ok = inst.selectionSpan()
	assert.False(t, ok)

	// A reversed selection (caret dragged leftwards) still spans forward.
	inst.cursor = packCursorForTest(7, 4)
	start, stop, ok = inst.selectionSpan()
	assert.True(t, ok)
	assert.Equal(t, [2]int{4, 7}, [2]int{start, stop})

	// Char offsets become byte offsets.
	inst = &App{src: "héllo world"}
	inst.cursor = packCursorForTest(1, 5)
	start, stop, ok = inst.selectionSpan()
	assert.True(t, ok)
	assert.Equal(t, "éllo", inst.src[start:stop])

	// Read mode freezes the caret, so there is no selection to trust.
	inst.viewMode = viewRead
	_, _, ok = inst.selectionSpan()
	assert.False(t, ok)
}

// packCursorForTest mirrors c.PackCursorRange without pulling the bindings'
// send path into the test: low half start, high half end, char offsets.
func packCursorForTest(start, end int) (packed uint64) {
	packed = uint64(uint32(start)) | uint64(uint32(end))<<32
	return
}

// ---------------------------------------------------------------------------
// Pane state
// ---------------------------------------------------------------------------

func TestProposalPaneHeight_DerivesFromTheWindow(t *testing.T) {
	inst := &App{}
	assert.Equal(t, proposalPaneFallbackPx, inst.proposalPaneHeight(), "no probe yet, fallback")
	inst.winH = 1000
	assert.Equal(t, float32(320), inst.proposalPaneHeight())
	inst.winH = 300
	assert.Equal(t, proposalPaneMinPx, inst.proposalPaneHeight(), "floored so the header row survives")
}
