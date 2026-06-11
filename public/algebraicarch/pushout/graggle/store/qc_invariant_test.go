//go:build llm_generated_opus47

// Negative tests for the qc invariant detectors. These live in package
// store (white-box) so they can deliberately corrupt unexported Graggle
// state and confirm each detector reports the violation.
package store

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/qc"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

// hasErrorContaining asserts that errs contains at least one error whose
// Error() string contains substr.
func hasErrorContaining(tt *testing.T, errs []error, substr string) {
	tt.Helper()
	for _, e := range errs {
		if strings.Contains(e.Error(), substr) {
			return
		}
	}
	tt.Fatalf("expected an error containing %q; got %d errors:\n%v", substr, len(errs), errs)
}

// A clean graggle must produce zero invariant violations.
func TestCheckInvariants_Clean(tt *testing.T) {
	g := New()
	if errs := qc.CheckInvariants(g); len(errs) != 0 {
		tt.Fatalf("expected no errors on clean graggle, got: %v", errs)
	}
}

// 1. Root node must be live.
func TestInvariant_RootMissing(tt *testing.T) {
	g := New()
	g.nodes.Remove(t.RootNodeID)
	hasErrorContaining(tt, qc.CheckInvariants(g), "root node not in live set")
}

func TestInvariant_RootDeleted(tt *testing.T) {
	g := New()
	g.deletedNodes.Add(t.RootNodeID)
	hasErrorContaining(tt, qc.CheckInvariants(g), "root node in deleted set")
}

// 2. No node may be in both live and deleted sets; edge endpoints must exist.
func TestInvariant_NodeInBothSets(tt *testing.T) {
	g := New()
	id := nid("p", 0)
	if err := g.AddNode(id, []byte("x"), ph("p"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	g.deletedNodes.Add(id)
	hasErrorContaining(tt, qc.CheckInvariants(g), "in both live and deleted sets")
}

func TestInvariant_EdgeEndpointMissing(tt *testing.T) {
	g := New()
	missing := nid("missing", 7)
	g.edges.Add(t.RootNodeID, t.Edge{Dest: missing, Kind: t.EdgeKindLive, IntroducedBy: ph("p")})
	g.backEdges.Add(missing, t.Edge{Dest: t.RootNodeID, Kind: t.EdgeKindLive, IntroducedBy: ph("p")})
	hasErrorContaining(tt, qc.CheckInvariants(g), "not in any node set")
}

// 3. Edge symmetry: every forward edge must have a matching back-edge.
func TestInvariant_ForwardWithoutBackEdge(tt *testing.T) {
	g := New()
	id := nid("p", 0)
	if err := g.AddNode(id, []byte("x"), ph("p"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	g.edges.Add(t.RootNodeID, t.Edge{Dest: id, Kind: t.EdgeKindPseudo, IntroducedBy: ph("rogue")})
	hasErrorContaining(tt, qc.CheckInvariants(g), "no matching back-edge")
}

// 4. Edge kind consistency: a live edge to a deleted endpoint is invalid.
func TestInvariant_LiveEdgeToDeletedNode(tt *testing.T) {
	g := New()
	a := nid("p", 0)
	b := nid("p", 1)
	if err := g.AddNode(a, []byte("a"), ph("p"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.AddNode(b, []byte("b"), ph("p"), []t.NodeID{a}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.DeleteNode(b, testDeleter); err != nil {
		tt.Fatal(err)
	}
	g.ResolvePseudoEdges()
	g.edges.Add(a, t.Edge{Dest: b, Kind: t.EdgeKindLive, IntroducedBy: ph("rogue")})
	g.backEdges.Add(b, t.Edge{Dest: a, Kind: t.EdgeKindLive, IntroducedBy: ph("rogue")})
	hasErrorContaining(tt, qc.CheckInvariants(g), "live edge")
}

// 5. Deleted partition coverage.
func TestInvariant_DeletedNodeMissingFromPartition(tt *testing.T) {
	g := New()
	a := nid("p", 0)
	if err := g.AddNode(a, []byte("a"), ph("p"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.DeleteNode(a, testDeleter); err != nil {
		tt.Fatal(err)
	}
	g.deletedPartition.Remove(a)
	hasErrorContaining(tt, qc.CheckInvariants(g), "not in DeletedPartition")
}

func TestInvariant_LiveNodeInPartition(tt *testing.T) {
	g := New()
	a := nid("p", 0)
	if err := g.AddNode(a, []byte("a"), ph("p"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	g.deletedPartition.Add(a)
	hasErrorContaining(tt, qc.CheckInvariants(g), "found in DeletedPartition")
}

// 6. No dirty reps must remain after Resolve.
func TestInvariant_DirtyRepRemains(tt *testing.T) {
	g := New()
	a := nid("p", 0)
	if err := g.AddNode(a, []byte("a"), ph("p"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.DeleteNode(a, testDeleter); err != nil {
		tt.Fatal(err)
	}
	// Skip ResolvePseudoEdges so dirty reps remain.
	hasErrorContaining(tt, qc.CheckInvariants(g), "dirty reps remain")
}

// 7. Pseudo-edge minimality: no pseudo-edge may duplicate a live edge.
func TestInvariant_PseudoEdgeDuplicatesLive(tt *testing.T) {
	g := New()
	a := nid("p", 0)
	if err := g.AddNode(a, []byte("a"), ph("p"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	g.ResolvePseudoEdges()
	g.edges.Add(t.RootNodeID, t.Edge{Dest: a, Kind: t.EdgeKindPseudo, IntroducedBy: t.PatchHash{}})
	g.backEdges.Add(a, t.Edge{Dest: t.RootNodeID, Kind: t.EdgeKindPseudo, IntroducedBy: t.PatchHash{}})
	hasErrorContaining(tt, qc.CheckInvariants(g), "duplicates a live edge")
}

// 8. Pseudo-edge reachability: a pseudo-edge with no path through deleted.
func TestInvariant_PseudoEdgeUnreachable(tt *testing.T) {
	g := New()
	a := nid("p", 0)
	b := nid("p", 1)
	if err := g.AddNode(a, []byte("a"), ph("p"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.AddNode(b, []byte("b"), ph("p"), []t.NodeID{a}, nil); err != nil {
		tt.Fatal(err)
	}
	g.ResolvePseudoEdges()
	g.edges.Add(a, t.Edge{Dest: b, Kind: t.EdgeKindPseudo, IntroducedBy: t.PatchHash{}})
	g.backEdges.Add(b, t.Edge{Dest: a, Kind: t.EdgeKindPseudo, IntroducedBy: t.PatchHash{}})
	hasErrorContaining(tt, qc.CheckInvariants(g), "not justified by path through deleted nodes")
}

// 9. Pseudo-edge completeness.
func TestInvariant_PseudoEdgeMissing(tt *testing.T) {
	g := New()
	a := nid("p", 0)
	b := nid("p", 1)
	c := nid("p", 2)
	if err := g.AddNode(a, []byte("a"), ph("p"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.AddNode(b, []byte("b"), ph("p"), []t.NodeID{a}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.AddNode(c, []byte("c"), ph("p"), []t.NodeID{b}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.DeleteNode(b, testDeleter); err != nil {
		tt.Fatal(err)
	}
	g.ResolvePseudoEdges()
	g.edges.Remove(a, t.Edge{Dest: c, Kind: t.EdgeKindPseudo, IntroducedBy: t.PatchHash{}})
	g.backEdges.Remove(c, t.Edge{Dest: a, Kind: t.EdgeKindPseudo, IntroducedBy: t.PatchHash{}})
	hasErrorContaining(tt, qc.CheckInvariants(g), "missing pseudo-edge")
}

// 11. Reason tracking consistency: tracked pseudo-edge with no graph edge.
func TestInvariant_PseudoEdgeTrackedButMissing(tt *testing.T) {
	g := New()
	a := nid("p", 0)
	b := nid("p", 1)
	c := nid("p", 2)
	if err := g.AddNode(a, []byte("a"), ph("p"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.AddNode(b, []byte("b"), ph("p"), []t.NodeID{a}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.AddNode(c, []byte("c"), ph("p"), []t.NodeID{b}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.DeleteNode(b, testDeleter); err != nil {
		tt.Fatal(err)
	}
	g.ResolvePseudoEdges()
	g.edges.Remove(a, t.Edge{Dest: c, Kind: t.EdgeKindPseudo, IntroducedBy: t.PatchHash{}})
	g.backEdges.Remove(c, t.Edge{Dest: a, Kind: t.EdgeKindPseudo, IntroducedBy: t.PatchHash{}})
	hasErrorContaining(tt, qc.CheckInvariants(g), "no pseudo-edge in graph")
}

// 12. Connectivity: a live node unreachable from root.
func TestInvariant_LiveNodeUnreachable(tt *testing.T) {
	g := New()
	stranded := nid("stranded", 0)
	g.nodes.Add(stranded)
	g.contents[stranded] = []byte("x")
	hasErrorContaining(tt, qc.CheckInvariants(g), "unreachable from root")
}

// 13. LinearOrder vs DetectConflicts cross-check: a stranded live node
// breaks the linear order AND is reported by the detector (as an
// "orphan" conflict), so invariant 13 stays silent while connectivity
// (invariant 12) still flags the node. Before the orphan kind existed,
// this fixture made HasConflicts()==true with an empty conflict list.
func TestInvariant_ConflictDetectorCoversOrphans(tt *testing.T) {
	g := New()
	stranded := nid("stranded", 0)
	g.nodes.Add(stranded)
	g.contents[stranded] = []byte("x")
	errs := qc.CheckInvariants(g)
	for _, e := range errs {
		if strings.Contains(e.Error(), "conflict detector is incomplete") {
			tt.Fatalf("invariant 13 fired although the orphan kind covers this graph: %v", e)
		}
	}
	hasErrorContaining(tt, errs, "unreachable from root")
}

// CheckInvariants must not mutate the graggle it inspects. The idempotence
// check used to call ResolvePseudoEdges in place, which silently resolved
// dirty components during "read-only" checking.
func TestCheckInvariants_DoesNotMutate(tt *testing.T) {
	g := New()
	a := nid("p", 0)
	b := nid("p", 1)
	c := nid("p", 2)
	for _, n := range []struct {
		id t.NodeID
		up t.NodeID
	}{{a, t.RootNodeID}, {b, a}, {c, b}} {
		if err := g.AddNode(n.id, []byte("x"), ph("p"), []t.NodeID{n.up}, nil); err != nil {
			tt.Fatal(err)
		}
	}
	if err := g.DeleteNode(b, testDeleter); err != nil {
		tt.Fatal(err)
	}
	// Deliberately dirty: no ResolvePseudoEdges yet.
	if g.DirtyRepCount() == 0 {
		tt.Fatal("setup: expected dirty reps before resolution")
	}
	errs := qc.CheckInvariants(g)
	hasErrorContaining(tt, errs, "dirty reps remain")
	if g.DirtyRepCount() == 0 {
		tt.Fatal("CheckInvariants resolved dirty reps — checker mutated the graggle")
	}

	// And on a clean graggle the full state must be untouched bit for bit.
	g.ResolvePseudoEdges()
	want := g.Debug()
	wantDirty := g.DirtyRepCount()
	wantReasons := len(g.reasonPseudoEdges)
	wantTracked := len(g.pseudoEdgeReasons)
	if errs := qc.CheckInvariants(g); len(errs) != 0 {
		tt.Fatalf("expected clean graggle, got: %v", errs)
	}
	if got := g.Debug(); got != want {
		tt.Fatalf("CheckInvariants mutated graph state:\nbefore:\n%s\nafter:\n%s", want, got)
	}
	if g.DirtyRepCount() != wantDirty || len(g.reasonPseudoEdges) != wantReasons || len(g.pseudoEdgeReasons) != wantTracked {
		tt.Fatal("CheckInvariants mutated pseudo-edge bookkeeping")
	}
}

// 14. Tombstone deleter tracking: a deleted node without a recorded
// deleter, and a live node carrying one, must both be reported.
func TestInvariant_TombstoneWithoutDeleter(tt *testing.T) {
	g := New()
	a := nid("p", 0)
	if err := g.AddNode(a, []byte("a"), ph("p"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	if err := g.DeleteNode(a, testDeleter); err != nil {
		tt.Fatal(err)
	}
	g.ResolvePseudoEdges()
	delete(g.deleters, a)
	hasErrorContaining(tt, qc.CheckInvariants(g), "no recorded deleter")
}

func TestInvariant_LiveNodeWithDeleter(tt *testing.T) {
	g := New()
	a := nid("p", 0)
	if err := g.AddNode(a, []byte("a"), ph("p"), []t.NodeID{t.RootNodeID}, nil); err != nil {
		tt.Fatal(err)
	}
	g.addDeleter(a, testDeleter)
	hasErrorContaining(tt, qc.CheckInvariants(g), "recorded deleters")
}
