package tree

// The rebuild edge from the KEYBOARD's side (ADR-0177 M3).
//
// That a cursor follows its key across a renumbering rebuild is already pinned
// by TestCursorFollowsItsKeyAndReportsNoneWhenGone, and selection by
// TestKeysSurviveARenumberingRebuild — both from ADR-0176's Keys work. What
// neither covers is the ORDERING those guarantees depend on, which is the one
// thing a keyboard host can get wrong: State is written by INDEX, so a write
// that lands before Bind files under whatever key the PREVIOUS build gave that
// index.
//
// This matters here and not elsewhere because the key pass is the only thing
// that writes a cursor every frame. Render calls Bind first and applyKeys after
// it; these tests are what makes that ordering a decision rather than an
// accident someone tidies away.

import "testing"

func TestWritingACursorBeforeBindFilesUnderTheOldBuildsKey(t *testing.T) {
	st := &State{}
	st.Bind(sections())

	// The host has rebuilt, but the caller writes before binding. Index 0
	// still means alpha to the State, though it means beta in the tree that
	// was just built — so the cursor files under a node the new build does
	// not contain, and reads back as no cursor at all.
	st.SetCursor(0)
	st.Bind(sectionsFiltered())

	if got := st.Cursor(); got != -1 {
		t.Errorf("Cursor = %d, want -1: the write filed under alpha, the old "+
			"index 0, which the filtered build drops", got)
	}
}

// The same write, in the order Render actually uses. Bind first, then move.
func TestWritingACursorAfterBindLandsOnTheNodeTheReaderSees(t *testing.T) {
	st := &State{}
	st.Bind(sections())
	st.SetCursor(2) // beta

	st.Bind(sectionsFiltered())
	st.SetCursor(2) // gamma, in the NEW numbering

	if got := st.Cursor(); got != 2 {
		t.Fatalf("Cursor = %d, want 2", got)
	}
	// And it is a key, not an index that happened to be right once: the next
	// rebuild moves gamma back to 4 and the cursor goes with it.
	st.Bind(sections())
	if got := st.Cursor(); got != 4 {
		t.Errorf("Cursor after rebinding the full tree = %d, want gamma's 4", got)
	}
}
