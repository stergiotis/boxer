package tree

import "testing"

// model_test.go covers what a [State] remembers and what it forgets: the two
// identities it can file under, and the default a node with no record follows.
//
// The fixture is a filter, because that is the shape every adopter hit — the
// host rebuilds its columns on each keystroke and every index below the first
// dropped node means a different thing afterwards.

// sections is the unfiltered hierarchy: three sections, one child each.
//
//	alpha   0     child 1
//	beta    2     child 3
//	gamma   4     child 5
func sections() Tree {
	return Tree{
		Labels:  []string{"alpha", "a1", "beta", "b1", "gamma", "g1"},
		Parents: []int32{-1, 0, -1, 2, -1, 4},
		Keys:    []string{"alpha", "alpha/1", "beta", "beta/1", "gamma", "gamma/1"},
	}
}

// sectionsFiltered is the same hierarchy with alpha filtered out, which slides
// beta from index 2 to index 0 and gamma from 4 to 2.
func sectionsFiltered() Tree {
	return Tree{
		Labels:  []string{"beta", "b1", "gamma", "g1"},
		Parents: []int32{-1, 0, -1, 2},
		Keys:    []string{"beta", "beta/1", "gamma", "gamma/1"},
	}
}

// unkeyed drops the Keys column, leaving the same shape filed by index.
func unkeyed(t Tree) Tree {
	t.Keys = nil
	return t
}

func TestKeysSurviveARenumberingRebuild(t *testing.T) {
	full, filtered := sections(), sectionsFiltered()
	st := &State{}
	st.Bind(full)
	st.SetExpanded(2, true) // beta
	st.SelectOnly(3)        // beta's child

	if got := labels(filtered, mustFlatten(t, filtered, st)); got != "beta\n  b1\ngamma\n" {
		t.Errorf("after the filter renumbered every node:\n%s", got)
	}
	// beta is index 0 now, and its child index 1.
	if !st.IsExpanded(0) {
		t.Error("beta lost its expansion when its index changed")
	}
	if !st.IsSelected(1) {
		t.Error("the selection did not follow beta's child to its new index")
	}
	if st.IsExpanded(2) {
		t.Error("gamma inherited beta's old index AND its expansion")
	}
}

// TestWithoutKeysARebuildMisfilesEverything is the contrast the Keys column
// exists to remove. It is not a defect being pinned but the documented cost of
// filing by index, and it is asserted so that the two behaviours cannot drift
// into each other silently.
func TestWithoutKeysARebuildMisfilesEverything(t *testing.T) {
	full, filtered := unkeyed(sections()), unkeyed(sectionsFiltered())
	st := &State{}
	st.Bind(full)
	st.SetExpanded(2, true) // beta, at index 2

	// Index 2 is gamma after the filter, so gamma opens and beta does not.
	if got := labels(filtered, mustFlatten(t, filtered, st)); got != "beta\ngamma\n  g1\n" {
		t.Errorf("unkeyed state after a renumbering rebuild:\n%s", got)
	}
}

func TestDefaultExpandedDrawsUntouchedNodes(t *testing.T) {
	tr := sections()
	st := &State{}
	st.SetDefaultExpanded(true)

	if got := labels(tr, mustFlatten(t, tr, st)); got != "alpha\n  a1\nbeta\n  b1\ngamma\n  g1\n" {
		t.Errorf("default-open outline:\n%s", got)
	}

	// One explicit closure, and it is the only thing stored.
	st.SetExpanded(2, false)
	if got := labels(tr, mustFlatten(t, tr, st)); got != "alpha\n  a1\nbeta\ngamma\n  g1\n" {
		t.Errorf("after closing beta:\n%s", got)
	}
	if n := len(st.expandedKeys); n != 1 {
		t.Errorf("stored %d records, want only the one closure", n)
	}

	// Flipping the default moves everything untouched. A record says what it
	// says, so beta is unaffected either way — closed here because it agrees
	// with the new default, and still closed when the default swings back.
	st.SetDefaultExpanded(false)
	if got := labels(tr, mustFlatten(t, tr, st)); got != "alpha\nbeta\ngamma\n" {
		t.Errorf("after the default went closed:\n%s", got)
	}
	st.SetDefaultExpanded(true)
	if st.IsExpanded(2) {
		t.Error("beta was closed by the reader and should stay closed across a default round-trip")
	}
}

func TestSetExpandedToTheDefaultDropsTheRecord(t *testing.T) {
	tr := sections()
	st := &State{}
	st.Bind(tr)
	st.SetDefaultExpanded(true)

	st.SetExpanded(0, false)
	st.SetExpanded(0, true)
	if n := len(st.expandedKeys); n != 0 {
		t.Errorf("closing then reopening left %d records, want none", n)
	}
	// And it is genuinely untouched again: the default still moves it.
	st.SetDefaultExpanded(false)
	if st.IsExpanded(0) {
		t.Error("a node whose record was dropped should follow the new default")
	}
}

// TestExpandAllCoversNodesThatArriveLater is the difference between setting a
// default and looping over the nodes that happen to exist.
func TestExpandAllCoversNodesThatArriveLater(t *testing.T) {
	st := &State{}
	st.Bind(sectionsFiltered())
	st.SetExpanded(0, false)
	st.ExpandAll()

	full := sections()
	if got := labels(full, mustFlatten(t, full, st)); got != "alpha\n  a1\nbeta\n  b1\ngamma\n  g1\n" {
		t.Errorf("alpha arrived after ExpandAll and should be open:\n%s", got)
	}

	st.CollapseAll()
	if got := labels(full, mustFlatten(t, full, st)); got != "alpha\nbeta\ngamma\n" {
		t.Errorf("after CollapseAll:\n%s", got)
	}
}

func TestSelectionIsAscendingAndSkipsAbsentNodes(t *testing.T) {
	full, filtered := sections(), sectionsFiltered()
	st := &State{}
	st.Bind(full)
	st.SetSelected(4, true) // gamma
	st.SetSelected(0, true) // alpha
	st.SetSelected(2, true) // beta

	if got := st.Selection(nil); len(got) != 3 || got[0] != 0 || got[1] != 2 || got[2] != 4 {
		t.Errorf("Selection on the full tree = %v, want ascending 0 2 4", got)
	}

	// alpha is gone; its selection is still stored, and is simply not
	// reportable as an index.
	st.Bind(filtered)
	if got := st.Selection(nil); len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Errorf("Selection on the filtered tree = %v, want beta and gamma", got)
	}
	if n := st.SelectionLen(); n != 3 {
		t.Errorf("SelectionLen = %d, want the 3 stored including the filtered-out one", n)
	}
	// And it comes back when its row does.
	st.Bind(full)
	if !st.IsSelected(0) {
		t.Error("alpha should still be selected once the filter widens again")
	}
}

func TestUnkeyedSelectionIsAscendingToo(t *testing.T) {
	st := &State{}
	st.Bind(unkeyed(sections()))
	for _, n := range []int32{4, 0, 2} {
		st.SetSelected(n, true)
	}
	got := st.Selection(nil)
	if len(got) != 3 || got[0] != 0 || got[1] != 2 || got[2] != 4 {
		t.Errorf("Selection = %v, want ascending 0 2 4", got)
	}
}

func TestCursorFollowsItsKeyAndReportsNoneWhenGone(t *testing.T) {
	full, filtered := sections(), sectionsFiltered()
	st := &State{}
	st.Bind(full)

	st.SetCursor(4) // gamma
	st.Bind(filtered)
	if got := st.Cursor(); got != 2 {
		t.Errorf("Cursor = %d, want gamma's new index 2", got)
	}

	st.Bind(full)
	st.SetCursor(0) // alpha, which the filter drops
	st.Bind(filtered)
	if got := st.Cursor(); got != -1 {
		t.Errorf("Cursor = %d, want -1 for a node the tree no longer has", got)
	}
}

func TestPendingRevealIsObservableAndConsumedOnce(t *testing.T) {
	tr := sections()
	st := &State{}
	st.Bind(tr)

	if got := st.PendingReveal(); got != -1 {
		t.Errorf("PendingReveal on a fresh State = %d, want -1", got)
	}
	st.Reveal(3)
	if got := st.PendingReveal(); got != 3 {
		t.Errorf("PendingReveal = %d, want 3", got)
	}
	if got := st.takeReveal(); got != 3 {
		t.Errorf("takeReveal = %d, want 3", got)
	}
	if got := st.PendingReveal(); got != -1 {
		t.Errorf("PendingReveal after the render consumed it = %d, want -1", got)
	}

	// A reveal outlives a rebuild too, which is what makes "reveal the caret's
	// section" survive the reparse that moved it.
	st.Reveal(4)
	st.Bind(sectionsFiltered())
	if got := st.takeReveal(); got != 2 {
		t.Errorf("takeReveal after the rebuild = %d, want gamma's new index 2", got)
	}

	st.Reveal(-1)
	if got := st.PendingReveal(); got != -1 {
		t.Errorf("a negative Reveal should cancel, got %d", got)
	}
}

// TestFirstBindAdoptsEntriesFiledByIndex covers the seam a host hits exactly
// once: writing to a fresh State before the first render, when there is no
// binding yet to file under.
func TestFirstBindAdoptsEntriesFiledByIndex(t *testing.T) {
	full, filtered := sections(), sectionsFiltered()
	st := &State{}
	st.SetExpanded(2, true) // beta, before anything has bound
	st.SelectOnly(3)
	st.SetCursor(3)
	st.Reveal(2)

	// The first render binds, and the seed has to survive that.
	if got := labels(full, mustFlatten(t, full, st)); got != "alpha\nbeta\n  b1\ngamma\n" {
		t.Errorf("seeded before the first bind:\n%s", got)
	}
	if !st.IsSelected(3) || st.Cursor() != 3 || st.PendingReveal() != 2 {
		t.Errorf("selection/cursor/reveal lost at the first bind: sel=%v cursor=%d reveal=%d",
			st.IsSelected(3), st.Cursor(), st.PendingReveal())
	}

	// And having been adopted, it is keyed like anything else.
	st.Bind(filtered)
	if !st.IsExpanded(0) || !st.IsSelected(1) || st.Cursor() != 1 {
		t.Error("the adopted entries did not follow beta to its new index")
	}
}

func TestValidateRejectsAMisalignedKeyColumn(t *testing.T) {
	tr := sections()
	tr.Keys = tr.Keys[:3]
	if err := tr.Validate(); err == nil {
		t.Fatal("a Keys column shorter than the tree should not validate")
	}
	// A nil column stays valid — it is what "file by index" looks like.
	tr.Keys = nil
	if err := tr.Validate(); err != nil {
		t.Errorf("nil Keys should validate: %v", err)
	}
}

// TestEmptyKeyColumnFallsBackToIndices pins the boundary a filter crosses when
// it narrows the tree to nothing: an empty Keys column is not a binding, so
// the State does not start filing under the empty string.
func TestEmptyKeyColumnFallsBackToIndices(t *testing.T) {
	st := &State{}
	st.Bind(sections())
	st.SetExpanded(2, true)

	st.Bind(Tree{Labels: []string{}, Parents: []int32{}, Keys: []string{}})
	if st.keyed() {
		t.Error("an empty Keys column should not read as a binding")
	}
	// And the keyed records are untouched by the excursion.
	st.Bind(sections())
	if !st.IsExpanded(2) {
		t.Error("beta lost its expansion while the tree was empty")
	}
}
