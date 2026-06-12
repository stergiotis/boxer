//go:build llm_generated_opus47

package store

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

// fakeClock returns a function that, when called, yields the times
// from values in order. Used to feed deterministic timestamps into
// DeleteNode via Graggle.SetClock.
func fakeClock(values ...time.Time) func() time.Time {
	i := 0
	return func() (out time.Time) {
		out = values[i]
		if i+1 < len(values) {
			i++
		}
		return
	}
}

func TestSweepTombstones_NothingToPurge(tt *testing.T) {
	g := New()
	purgedCount, purgedIDs := g.SweepTombstones(time.Unix(1_000_000, 0), time.Hour)
	if purgedCount != 0 || len(purgedIDs) != 0 {
		tt.Fatalf("empty graggle should produce zero purges, got count=%d ids=%v", purgedCount, purgedIDs)
	}
}

func TestSweepTombstones_PurgePastHorizon(tt *testing.T) {
	g := New()
	g.SetClock(fakeClock(time.Unix(1000, 0)))
	id := nid("p1", 0)
	if err := g.AddNode(id, []byte("hello\n"), ph("p1"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.DeleteNode(id, testDeleter); err != nil {
		tt.Fatal(err)
	}
	// Sweep at t=2000 with horizon=500s; tombstone is at 1000, cutoff is 1500 → purged.
	purgedCount, purgedIDs := g.SweepTombstones(time.Unix(2000, 0), 500*time.Second)
	if purgedCount != 1 || len(purgedIDs) != 1 || purgedIDs[0] != id {
		tt.Fatalf("expected single purge of %v, got count=%d ids=%v", id, purgedCount, purgedIDs)
	}
	if g.NodeContent(id) != nil {
		tt.Fatal("content should have been freed after purge")
	}
	if g.NodeContentStatus(id) != t.NodeContentStatusPurged {
		tt.Fatalf("expected NodeContentStatusPurged, got %v", g.NodeContentStatus(id))
	}
	if !g.IsDeleted(id) {
		tt.Fatal("purged node should still be tombstoned")
	}
}

func TestSweepTombstones_PreserveWithinHorizon(tt *testing.T) {
	g := New()
	g.SetClock(fakeClock(time.Unix(1000, 0)))
	id := nid("p1", 0)
	if err := g.AddNode(id, []byte("hello\n"), ph("p1"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.DeleteNode(id, testDeleter); err != nil {
		tt.Fatal(err)
	}
	// Sweep at t=1200 with horizon=500s; cutoff is 700, tombstone at 1000 is newer → preserved.
	purgedCount, _ := g.SweepTombstones(time.Unix(1200, 0), 500*time.Second)
	if purgedCount != 0 {
		tt.Fatalf("tombstone within horizon should not be purged, got count=%d", purgedCount)
	}
	if string(g.NodeContent(id)) != "hello\n" {
		tt.Fatalf("content should be preserved within horizon, got %q", string(g.NodeContent(id)))
	}
	if g.NodeContentStatus(id) != t.NodeContentStatusPresent {
		tt.Fatalf("expected NodeContentStatusPresent, got %v", g.NodeContentStatus(id))
	}
}

func TestSweepTombstones_Idempotent(tt *testing.T) {
	g := New()
	g.SetClock(fakeClock(time.Unix(1000, 0)))
	id := nid("p1", 0)
	if err := g.AddNode(id, []byte("x"), ph("p1"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.DeleteNode(id, testDeleter); err != nil {
		tt.Fatal(err)
	}
	first, _ := g.SweepTombstones(time.Unix(2000, 0), 500*time.Second)
	if first != 1 {
		tt.Fatalf("first sweep should purge 1, got %d", first)
	}
	second, _ := g.SweepTombstones(time.Unix(3000, 0), 500*time.Second)
	if second != 0 {
		tt.Fatalf("second sweep over the same state should purge 0, got %d", second)
	}
}

func TestSweepTombstones_DoesNotTouchLiveNodes(tt *testing.T) {
	g := New()
	g.SetClock(fakeClock(time.Unix(1000, 0), time.Unix(1010, 0)))
	live := nid("p1", 0)
	deleted := nid("p1", 1)
	if err := g.AddNode(live, []byte("live\n"), ph("p1"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.AddNode(deleted, []byte("dead\n"), ph("p1"), []t.NodeID{live}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.DeleteNode(deleted, testDeleter); err != nil {
		tt.Fatal(err)
	}
	g.SweepTombstones(time.Unix(9999, 0), time.Second)
	if string(g.NodeContent(live)) != "live\n" {
		tt.Fatalf("live node content disturbed: %q", string(g.NodeContent(live)))
	}
	if g.NodeContentStatus(live) != t.NodeContentStatusPresent {
		tt.Fatalf("live node should remain Present, got %v", g.NodeContentStatus(live))
	}
}

func TestSweepTombstones_ReturnsSortedIDs(tt *testing.T) {
	g := New()
	g.SetClock(fakeClock(time.Unix(1000, 0), time.Unix(1010, 0), time.Unix(1020, 0)))
	a := nid("p1", 2)
	b := nid("p1", 0)
	c := nid("p1", 1)
	for _, id := range []t.NodeID{a, b, c} {
		if err := g.AddNode(id, []byte("x"), ph("p1"), []t.NodeID{t.RootNodeID}, nil); err != nil {
			tt.Fatal(err)
		}
	}
	for _, id := range []t.NodeID{a, b, c} {
		if err := g.DeleteNode(id, testDeleter); err != nil {
			tt.Fatal(err)
		}
	}
	_, ids := g.SweepTombstones(time.Unix(9999, 0), time.Second)
	if !slices.IsSortedFunc(ids, t.CompareNodeID) {
		tt.Fatalf("returned ids not sorted: %v", ids)
	}
}

func TestUndeleteNode_RefusesWhenPurged(tt *testing.T) {
	g := New()
	g.SetClock(fakeClock(time.Unix(1000, 0)))
	id := nid("p1", 0)
	if err := g.AddNode(id, []byte("x"), ph("p1"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.DeleteNode(id, testDeleter); err != nil {
		tt.Fatal(err)
	}
	g.SweepTombstones(time.Unix(2000, 0), time.Second)
	err := g.UndeleteNode(id, testDeleter)
	if err == nil || !strings.Contains(err.Error(), "purged past retention horizon") {
		tt.Fatalf("expected purged-content error from UndeleteNode, got %v", err)
	}
}

func TestUndeleteNode_ClearsTombstoneAtAndContent(tt *testing.T) {
	g := New()
	g.SetClock(fakeClock(time.Unix(1000, 0)))
	id := nid("p1", 0)
	if err := g.AddNode(id, []byte("x"), ph("p1"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.DeleteNode(id, testDeleter); err != nil {
		tt.Fatal(err)
	}
	if _, ok := g.tombstoneAt[id]; !ok {
		tt.Fatal("DeleteNode should record tombstoneAt")
	}
	if err := g.UndeleteNode(id, testDeleter); err != nil {
		tt.Fatal(err)
	}
	if _, ok := g.tombstoneAt[id]; ok {
		tt.Fatal("UndeleteNode should clear tombstoneAt")
	}
	if string(g.NodeContent(id)) != "x" {
		tt.Fatal("content should survive undelete-within-horizon")
	}
}

func TestNodeContentStatus_AllThreeStates(tt *testing.T) {
	g := New()
	g.SetClock(fakeClock(time.Unix(1000, 0)))
	present := nid("p1", 0)
	if err := g.AddNode(present, []byte("v"), ph("p1"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	if got := g.NodeContentStatus(present); got != t.NodeContentStatusPresent {
		tt.Fatalf("present node: expected Present, got %v", got)
	}
	missing := nid("p9", 9)
	if got := g.NodeContentStatus(missing); got != t.NodeContentStatusMissing {
		tt.Fatalf("missing node: expected Missing, got %v", got)
	}
	purged := nid("p1", 1)
	if err := g.AddNode(purged, []byte("p"), ph("p1"), []t.NodeID{present}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.DeleteNode(purged, testDeleter); err != nil {
		tt.Fatal(err)
	}
	g.SweepTombstones(time.Unix(9999, 0), time.Second)
	if got := g.NodeContentStatus(purged); got != t.NodeContentStatusPurged {
		tt.Fatalf("purged node: expected Purged, got %v", got)
	}
}

func TestClone_PreservesTombstoneBookkeeping(tt *testing.T) {
	g := New()
	g.SetClock(fakeClock(time.Unix(1000, 0)))
	id := nid("p1", 0)
	if err := g.AddNode(id, []byte("x"), ph("p1"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.DeleteNode(id, testDeleter); err != nil {
		tt.Fatal(err)
	}
	g.SweepTombstones(time.Unix(2000, 0), time.Second)

	clone := g.Clone()
	if clone.NodeContentStatus(id) != t.NodeContentStatusPurged {
		tt.Fatal("clone should carry the contentPurged flag forward")
	}
	// Re-sweep on the clone should be a no-op — already purged.
	count, _ := clone.SweepTombstones(time.Unix(3000, 0), time.Second)
	if count != 0 {
		tt.Fatalf("cloned graggle re-sweep should purge nothing, got %d", count)
	}
}

func TestPatchUnapply_FailsWhenTombstonedContentPurged(tt *testing.T) {
	// Build a tiny scenario: patch P1 adds node N; patch P2 deletes N.
	// Apply both. Sweep past the horizon. Try to unapply P2 — the
	// undelete-target's content has been purged, so unapply must fail.
	g := New()
	g.SetClock(fakeClock(time.Unix(1000, 0)))

	addChanges := []patch.Change{{
		Kind:        patch.ChangeKindNewNode,
		NodeID:      t.NodeID{Patch: t.PlaceholderHash, Index: 0},
		Content:     []byte("doomed\n"),
		UpContext:   []t.NodeID{t.RootNodeID},
		DownContext: nil,
	}}
	pAdd := patch.NewPatch("alice", "add", nil, addChanges)
	if err := pAdd.Apply(g); err != nil {
		tt.Fatalf("apply add: %v", err)
	}
	target := t.NodeID{Patch: pAdd.Hash, Index: 0}

	delChanges := []patch.Change{{Kind: patch.ChangeKindDeleteNode, NodeID: target}}
	pDel := patch.NewPatch("alice", "delete", []t.PatchHash{pAdd.Hash}, delChanges)
	if err := pDel.Apply(g); err != nil {
		tt.Fatalf("apply delete: %v", err)
	}
	if !g.IsDeleted(target) {
		tt.Fatal("target should be tombstoned after pDel.Apply")
	}

	// Within horizon, Unapply works.
	if err := pDel.Unapply(g); err != nil {
		tt.Fatalf("within-horizon unapply unexpectedly failed: %v", err)
	}
	if g.IsDeleted(target) {
		tt.Fatal("unapply should resurrect tombstoned node")
	}
	// Redelete (clock advances to record fresh tombstone), then sweep past the horizon.
	g.SetClock(fakeClock(time.Unix(1500, 0)))
	if err := pDel.Apply(g); err != nil {
		tt.Fatalf("re-apply delete: %v", err)
	}
	g.SweepTombstones(time.Unix(9999, 0), time.Second)
	if g.NodeContentStatus(target) != t.NodeContentStatusPurged {
		tt.Fatalf("target should be purged after sweep, got %v", g.NodeContentStatus(target))
	}

	err := pDel.Unapply(g)
	if err == nil {
		tt.Fatal("expected unapply to fail when target content has been purged")
	}
	if !strings.Contains(err.Error(), "purged past retention horizon") {
		tt.Fatalf("error should mention purged retention, got %v", err)
	}
}

func TestSweepTombstones_NoTimeAdvanceMeansNoPurges(tt *testing.T) {
	g := New()
	g.SetClock(fakeClock(time.Unix(1000, 0)))
	id := nid("p1", 0)
	if err := g.AddNode(id, []byte("x"), ph("p1"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.DeleteNode(id, testDeleter); err != nil {
		tt.Fatal(err)
	}
	// Sweep with now == tombstone time, horizon 0: tombstone is at cutoff,
	// not strictly before, so it is preserved (Before is exclusive).
	count, _ := g.SweepTombstones(time.Unix(1000, 0), 0)
	if count != 0 {
		tt.Fatalf("tombstone at cutoff should be preserved (Before is strict), got %d", count)
	}
}

// Multi-deleter retention semantics: when TWO patches hold a node
// tombstoned and a sweep purges its content, unapplying a non-final
// deleter must still succeed (it only shrinks the deleter set — no
// resurrection, so the purge is irrelevant), while unapplying the LAST
// deleter must fail with the retention error. Pins the deleterCount
// branch in Patch.Unapply's purge pre-flight and UndeleteNode's
// no-resurrection path, which no deterministic test exercised before.
func TestPatchUnapply_PurgedMultiDeleterSemantics(tt *testing.T) {
	g := New()
	g.SetClock(fakeClock(time.Unix(1000, 0)))

	pAdd := patch.NewPatch("alice", "add", nil, []patch.Change{{
		Kind:      patch.ChangeKindNewNode,
		NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: 0},
		Content:   []byte("doomed\n"),
		UpContext: []t.NodeID{t.RootNodeID},
	}})
	if err := pAdd.Apply(g); err != nil {
		tt.Fatal(err)
	}
	target := t.NodeID{Patch: pAdd.Hash, Index: 0}

	// Two convergent edits delete the same node (each with its own
	// replacement so the patches are distinct).
	mkDel := func(author, val string) *patch.Patch {
		return patch.NewPatch(author, "edit", []t.PatchHash{pAdd.Hash}, []patch.Change{
			{Kind: patch.ChangeKindDeleteNode, NodeID: target},
			{Kind: patch.ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0},
				Content: []byte(val + "\n"), UpContext: []t.NodeID{t.RootNodeID}},
		})
	}
	p1 := mkDel("alice", "alice")
	p2 := mkDel("bob", "bob")
	if err := p1.Apply(g); err != nil {
		tt.Fatal(err)
	}
	if err := p2.Apply(g); err != nil {
		tt.Fatal(err)
	}
	if got := g.NodeDeleterCount(target); got != 2 {
		tt.Fatalf("expected 2 deleters, got %d", got)
	}

	if n, _ := g.SweepTombstones(time.Unix(2000, 0), 0); n != 1 {
		tt.Fatalf("expected exactly the tombstone purged, got %d", n)
	}
	if g.NodeContentStatus(target) != t.NodeContentStatusPurged {
		tt.Fatal("setup: target not purged")
	}

	// Non-final deleter: must succeed without resurrecting.
	if err := p2.Unapply(g); err != nil {
		tt.Fatalf("unapply of non-final deleter must succeed despite purge: %v", err)
	}
	if !g.IsDeleted(target) {
		tt.Fatal("target resurrected by non-final unapply")
	}
	if g.NodeContentStatus(target) != t.NodeContentStatusPurged {
		tt.Fatal("purge marker lost on non-final unapply")
	}
	if got := g.NodeDeleterCount(target); got != 1 {
		tt.Fatalf("expected 1 remaining deleter, got %d", got)
	}
	assertNoInvariantViolations(tt, g)

	// Final deleter: resurrection would need the purged bytes — refuse,
	// and leave the state untouched.
	err := p1.Unapply(g)
	if err == nil || !strings.Contains(err.Error(), "permanent past retention") {
		tt.Fatalf("expected retention rejection for final deleter, got: %v", err)
	}
	if !g.IsDeleted(target) || g.NodeDeleterCount(target) != 1 {
		tt.Fatal("rejected unapply mutated tombstone state")
	}
	assertNoInvariantViolations(tt, g)
}
