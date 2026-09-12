package launcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/keycodes"
)

// rowsFixture is a sectioned list: two headings with apps under each, the
// shape the no-query browse view builds.
func rowsFixture() (rows []rowT) {
	rows = []rowT{
		{heading: "Data"},
		{m: mkTopicManifest("d1", "D one", app.TopicData)},
		{m: mkTopicManifest("d2", "D two", app.TopicData)},
		{heading: "Runtime"},
		{m: mkTopicManifest("r1", "R one", app.TopicRuntime)},
	}
	return
}

// TestMoveCursor_SkipsHeadingsWithoutSpendingAStep is what makes ↓ from the
// last app of one section land on the first app of the next, rather than on
// the header between them.
func TestMoveCursor_SkipsHeadingsWithoutSpendingAStep(t *testing.T) {
	rows := rowsFixture()
	inst := newTestInst()
	inst.cursor = 2 // "D two", the last app of the first section
	inst.moveCursor(rows, 1)
	assert.Equal(t, 4, inst.cursor, "one step down crosses the heading to R one")
	assert.Equal(t, "", rows[inst.cursor].heading, "the cursor never rests on a heading")
}

// TestMoveCursor_StopsAtTheEndsRatherThanWrapping: wrapping in a ranked list
// means ↓ at the last hit jumps to the best one, which reads as a glitch.
func TestMoveCursor_StopsAtTheEndsRatherThanWrapping(t *testing.T) {
	rows := rowsFixture()
	inst := newTestInst()
	inst.cursor = 4
	inst.moveCursor(rows, 1)
	assert.Equal(t, 4, inst.cursor, "already at the last app")

	inst.cursor = 1
	inst.moveCursor(rows, -1)
	assert.Equal(t, 1, inst.cursor, "already at the first app; the heading above is not a stop")
}

// TestMoveCursor_PageStepClampsToTheLastApp covers a page jump longer than the
// list.
func TestMoveCursor_PageStepClampsToTheLastApp(t *testing.T) {
	rows := rowsFixture()
	inst := newTestInst()
	inst.cursor = 1
	inst.moveCursor(rows, keyPageStep)
	assert.Equal(t, 4, inst.cursor)
}

// TestMoveCursor_EmptyListIsANoop guards the degenerate case an empty query
// against a fully-filtered corpus produces.
func TestMoveCursor_EmptyListIsANoop(t *testing.T) {
	inst := newTestInst()
	inst.cursor = 3
	inst.moveCursor(nil, 1)
	assert.Equal(t, 3, inst.cursor)
}

// TestClampCursor_LandsOnAnAppRow: the row list changes shape under the cursor
// as the query changes, so the clamp runs every frame rather than trusting the
// stored value.
func TestClampCursor_LandsOnAnAppRow(t *testing.T) {
	rows := rowsFixture()
	inst := newTestInst()

	inst.cursor = 0 // a heading
	inst.clampCursor(rows)
	assert.Equal(t, 1, inst.cursor, "advances off the heading")

	inst.cursor = 99 // past the end, e.g. the query just narrowed
	inst.clampCursor(rows)
	assert.Equal(t, 4, inst.cursor)

	inst.cursor = -3
	inst.clampCursor(rows)
	assert.Equal(t, 1, inst.cursor)
}

// TestClampCursor_AllHeadingsFallsBackToFirstAppRow is the case where every
// row from the cursor on is a heading — reachable when a section's apps are
// filtered out from under a cursor sitting in it.
func TestClampCursor_AllHeadingsFallsBackToFirstAppRow(t *testing.T) {
	rows := []rowT{{m: mkTopicManifest("a", "A", app.TopicData)}, {heading: "Runtime"}}
	inst := newTestInst()
	inst.cursor = 1
	inst.clampCursor(rows)
	assert.Equal(t, 0, inst.cursor)
}

// TestFirstLastAppRow covers Home / End over a list that opens with a heading.
func TestFirstLastAppRow(t *testing.T) {
	rows := rowsFixture()
	assert.Equal(t, 1, firstAppRow(rows))
	assert.Equal(t, 4, lastAppRow(rows))
	assert.Equal(t, 0, firstAppRow(nil), "no app row is a defensible 0, not a panic")
	assert.Equal(t, 0, lastAppRow(nil))
}

// TestSyncCursorToFilter_ANarrowedListStartsAtTheFirstHit is the defect:
// applying a query moved the selection to the LAST hit.
//
// The cursor is an index, the query rebuilds the list under it, and an index
// past the end is clamped down to the last row — so a query matching two apps
// selected the second one. The first hit is the ranked answer to what was
// typed, and it is what Enter opens.
func TestSyncCursorToFilter_ANarrowedListStartsAtTheFirstHit(t *testing.T) {
	inst := newTestInst()
	browse := rowsFixture()

	inst.cursor = 4 // arrowed down to the last app of the browse view
	inst.syncCursorToFilter(browse)
	assert.Equal(t, 4, inst.cursor, "nothing about the filter changed")

	inst.searchText = "one"
	hits := []rowT{
		{m: mkTopicManifest("d1", "D one", app.TopicData)},
		{m: mkTopicManifest("r1", "R one", app.TopicRuntime)},
	}
	inst.syncCursorToFilter(hits)
	assert.Equal(t, 0, inst.cursor, "the best hit, not the last one")

	// And it stays there while the query does: the next frame must not fight
	// an arrow key.
	inst.cursor = 1
	inst.syncCursorToFilter(hits)
	assert.Equal(t, 1, inst.cursor)
}

// TestSyncCursorToFilter_ClearingTheQueryLandsOnAnAppRow: back in the browse
// view the list opens with a section heading, which is not a place the cursor
// can rest.
func TestSyncCursorToFilter_ClearingTheQueryLandsOnAnAppRow(t *testing.T) {
	inst := newTestInst()
	inst.searchText = "one"
	inst.syncCursorToFilter([]rowT{{m: mkTopicManifest("d1", "D one", app.TopicData)}})

	inst.searchText = ""
	inst.cursor = 3
	inst.syncCursorToFilter(rowsFixture())
	assert.Equal(t, 1, inst.cursor)
}

// TestSyncCursorToFilter_TheFacetsCountToo: the chips and the provenance
// toggles narrow the same list the query does, and each writes its filter at
// its own moment. Noticing the change in one place is what keeps them in
// step.
func TestSyncCursorToFilter_TheFacetsCountToo(t *testing.T) {
	inst := newTestInst()
	rows := rowsFixture()
	inst.cursor = 4
	inst.syncCursorToFilter(rows)
	require.Equal(t, 4, inst.cursor)

	inst.topicFilter = inst.topicFilter.toggledAt(0)
	inst.syncCursorToFilter(rows)
	assert.Equal(t, 1, inst.cursor, "a chip narrows the list, so the cursor restarts")

	inst.cursor = 4
	inst.ensureKindShown()
	inst.kindShown[0] = false
	inst.syncCursorToFilter(rows)
	assert.Equal(t, 1, inst.cursor, "so does hiding a kind")
}

// TestHandleKeys_SpaceOpensTheCursorRow covers the list site's one addition to
// the field's contract. Enter is the same branch; the point of the test is
// that Space reaches it at all, since the field's mask deliberately does not
// carry the key.
func TestHandleKeys_SpaceOpensTheCursorRow(t *testing.T) {
	rows := rowsFixture()
	inst := newTestInst()
	inst.cursor = 2

	assert.Equal(t, 2, inst.handleKeys(rows, []c.CapturedKey{{Code: keycodes.Space}}, 0))
	assert.Equal(t, 2, inst.handleKeys(rows, []c.CapturedKey{{Code: keycodes.Enter}}, 0))
	assert.Equal(t, -1, inst.handleKeys(rows, nil, 0), "no keys, nothing to open")

	// A cursor on a heading cannot be activated — it is not an app.
	inst.cursor = 0
	assert.Equal(t, -1, inst.handleKeys(rows, []c.CapturedKey{{Code: keycodes.Space}}, 0))
}

// TestHandleKeys_ArrowsThenSpaceOpenWhereTheCursorLanded: one batch of keys is
// a sequence, not a set — key repeat delivers several per frame.
func TestHandleKeys_ArrowsThenSpaceOpenWhereTheCursorLanded(t *testing.T) {
	rows := rowsFixture()
	inst := newTestInst()
	inst.cursor = 1
	got := inst.handleKeys(rows, []c.CapturedKey{
		{Code: keycodes.ArrowDown},
		{Code: keycodes.ArrowDown},
		{Code: keycodes.Space},
	}, 0)
	assert.Equal(t, 4, got)
	assert.Equal(t, 4, inst.cursor)
}

// TestHandleKeys_EscapeClearsTheQueryFirst pins the two-step:
// a person who typed a query and pressed Escape means "not that" far more
// often than "close the launcher".
func TestHandleKeys_EscapeClearsTheQueryFirst(t *testing.T) {
	inst := newTestInst()
	inst.searchText = "quant"
	assert.Equal(t, -1, inst.handleKeys(rowsFixture(), []c.CapturedKey{{Code: keycodes.Escape}}, 0))
	assert.Equal(t, "", inst.searchText, "the query goes first")
	// The empty-query press surrenders focus, which needs a live client, so
	// the second step is the scene's to assert rather than this test's.
}

// TestLauncherListKeyMask_IsTheFieldsPlusSpace pins the one key the two
// capture sites must disagree about: a space separates the battery's tokens,
// so the field cannot take it — and a focused list is where "activate" is the
// only thing Space could mean.
func TestLauncherListKeyMask_IsTheFieldsPlusSpace(t *testing.T) {
	has := func(m keycodes.Mask, code keycodes.Code) bool {
		return uint64(m)&(1<<uint64(code)) != 0
	}
	assert.True(t, has(launcherListKeyMask, keycodes.Space))
	assert.False(t, has(launcherKeyMask, keycodes.Space))
	assert.Equal(t, launcherKeyMask, launcherListKeyMask&launcherKeyMask,
		"the list keeps every key the field navigates with")
	assert.False(t, has(launcherListKeyMask, keycodes.Tab), "neither site is a focus trap")
}

// TestLauncherKeyMask_ShapeIsTheDecision pins the two deliberate differences
// from the tree's mask (ADR-0177 §SD9): Escape is ours because the launcher is
// what it should act on, and Tab is not, so the field is not a focus trap.
// Space is not either — in a text field it is a space.
func TestLauncherKeyMask_ShapeIsTheDecision(t *testing.T) {
	require.NotZero(t, launcherKeyMask)
	// Named constants rather than literal bit numbers: the codes are a wire
	// contract (ADR-0177 §SD4) and this test should not be a second place they
	// are written down.
	has := func(code keycodes.Code) bool {
		return uint64(launcherKeyMask)&(1<<uint64(code)) != 0
	}
	assert.True(t, has(keycodes.ArrowUp))
	assert.True(t, has(keycodes.ArrowDown))
	assert.True(t, has(keycodes.Enter))
	assert.True(t, has(keycodes.Escape))
	assert.False(t, has(keycodes.Tab), "capturing Tab makes the field a focus trap")
	assert.False(t, has(keycodes.Space), "in a text field, Space is a space")
}

// TestBuildRows_SameAppCanAppearTwice is the reason row ids are keyed by row
// index rather than by app id (rows.go, renderRowBadges).
//
// ADR-0158 §SD3 puts a manifest under every topic it declares, so the browse
// view legitimately renders one app id on two rows. Keying per-row widgets by
// the app id derives the same id for both, which egui resolves by sharing
// state between them — the hazard the pre-0214 launcher documented on its menu
// entries and which a screenshot run caught here as a duplicate-id warning.
func TestBuildRows_SameAppCanAppearTwice(t *testing.T) {
	inst := newTestInst()
	twoTopics := mkTopicManifest("both", "Both", app.TopicData, app.TopicRuntime)
	rows := inst.buildRows([]app.Manifest{twoTopics})

	seen := 0
	for _, r := range rows {
		if r.heading == "" && r.m.Id == "both" {
			seen++
		}
	}
	assert.Equal(t, 2, seen, "one manifest, two declared topics, two rows")

	// And the row indices that carry it are distinct, which is what makes the
	// index a usable id key.
	idxs := []int{}
	for i, r := range rows {
		if r.heading == "" {
			idxs = append(idxs, i)
		}
	}
	require.Len(t, idxs, 2)
	assert.NotEqual(t, idxs[0], idxs[1])
}
