package mdedit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/mdedit/transform"
)

// ---------------------------------------------------------------------------
// The splice and its staleness rule
// ---------------------------------------------------------------------------

func TestSpliceResult(t *testing.T) {
	assert.Equal(t, "aXc", spliceResult("abc", 1, 2, "X"))
	assert.Equal(t, "X", spliceResult("abc", 0, 3, "X"), "document scope replaces everything")
	assert.Equal(t, "Xabc", spliceResult("abc", 0, 0, "X"), "a collapsed span inserts")
	assert.Equal(t, "abc", spliceResult("abc", 1, 1, ""), "empty over empty is identity")
}

func TestApplyTransform_SplicesTheRequestedSpan(t *testing.T) {
	inst := &App{src: "keep THIS keep"}
	inst.xform.res = &transform.Result{Content: "THAT"}
	inst.xform.reqSrc, inst.xform.reqStart, inst.xform.reqStop = inst.src, 5, 9

	inst.applyTransform()

	assert.Equal(t, "keep THAT keep", inst.src)
	assert.True(t, inst.rebindSrc, "an apply is a rebind, not typing")
	assert.Nil(t, inst.xform.res, "the verdict closes the pane")
}

func TestApplyTransform_RefusesAMovedBuffer(t *testing.T) {
	inst := &App{src: "the reader typed meanwhile"}
	inst.xform.res = &transform.Result{Content: "THAT"}
	inst.xform.reqSrc, inst.xform.reqStart, inst.xform.reqStop = "what was sent", 0, 4

	inst.applyTransform()

	assert.Equal(t, "the reader typed meanwhile", inst.src, "a splice computed against one buffer must not land on another")
	assert.False(t, inst.rebindSrc)
	assert.NotNil(t, inst.xform.res, "the result stays for copy or discard")
	assert.Contains(t, inst.status, "changed")
}

// ---------------------------------------------------------------------------
// Scope resolution
// ---------------------------------------------------------------------------

func TestTransformSpan(t *testing.T) {
	// A selection exists (chars 4..7 of plain ASCII).
	inst := &App{src: "one two three"}
	inst.cursor = packCursorForTest(4, 7)

	start, stop := inst.transformSpan(transform.ScopeSelection)
	assert.Equal(t, [2]int{4, 7}, [2]int{start, stop}, "selection scope takes the selection")

	start, stop = inst.transformSpan(transform.ScopeDocument)
	assert.Equal(t, [2]int{0, len(inst.src)}, [2]int{start, stop}, "document scope ignores the selection")

	// A collapsed caret is no selection: fall back to the whole document.
	inst.cursor = packCursorForTest(4, 4)
	start, stop = inst.transformSpan(transform.ScopeSelection)
	assert.Equal(t, [2]int{0, len(inst.src)}, [2]int{start, stop})

	// A reversed selection (caret dragged leftwards) still spans forward.
	inst.cursor = packCursorForTest(7, 4)
	start, stop = inst.transformSpan(transform.ScopeSelection)
	assert.Equal(t, [2]int{4, 7}, [2]int{start, stop})

	// Read mode freezes the caret, so selection scope degrades to the
	// document rather than trusting a selection the reader cannot see.
	inst.viewMode = viewRead
	inst.cursor = packCursorForTest(4, 7)
	start, stop = inst.transformSpan(transform.ScopeSelection)
	assert.Equal(t, [2]int{0, len(inst.src)}, [2]int{start, stop})
}

// packCursorForTest mirrors c.PackCursorRange without pulling the bindings'
// send path into the test: low half start, high half end, char offsets.
func packCursorForTest(start, end int) (packed uint64) {
	packed = uint64(uint32(start)) | uint64(uint32(end))<<32
	return
}

// TestPackCursorForTestMatchesTheChannel guards the mirror above against the
// real unpack, so a wire-shape change fails here rather than making the span
// tests vacuously pass.
func TestPackCursorForTestMatchesTheChannel(t *testing.T) {
	inst := &App{src: "abcdefgh"}
	inst.cursor = packCursorForTest(2, 5)
	start, stop := inst.transformSpan(transform.ScopeSelection)
	require.Equal(t, [2]int{2, 5}, [2]int{start, stop})
}

// ---------------------------------------------------------------------------
// Pane state
// ---------------------------------------------------------------------------

func TestTransformPaneOpen(t *testing.T) {
	inst := &App{}
	assert.False(t, inst.transformPaneOpen())
	inst.xform.errText = "boom"
	assert.True(t, inst.transformPaneOpen(), "a failure opens the pane to be read")
	inst.xform.errText = ""
	inst.xform.res = &transform.Result{Content: "x"}
	assert.True(t, inst.transformPaneOpen())
	inst.discardTransform()
	assert.False(t, inst.transformPaneOpen(), "discard closes it")
}

func TestTransformPaneHeight_DerivesFromTheWindow(t *testing.T) {
	inst := &App{}
	assert.Equal(t, transformPaneFallbackPx, inst.transformPaneHeight(), "no probe yet, fallback")
	inst.winH = 1000
	assert.Equal(t, float32(320), inst.transformPaneHeight())
	inst.winH = 300
	assert.Equal(t, transformPaneMinPx, inst.transformPaneHeight(), "floored so the header row survives")
}
