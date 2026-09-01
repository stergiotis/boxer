// Executable form of Apply's all-or-nothing claim. Two failure classes:
// patches the pre-validation (pass 0) rejects must leave the store
// byte-identical, and a store that fails a mutation AFTER pass 0 — here a
// decorator failing the Nth mutating call — must see the already-applied
// changes rolled back.
package patch

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/qc"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/store"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/types"
)

// storeState is the full observable state of a pushoutgraph: the GRG1
// snapshot (nodes, contents, tombstones, deleters, live/deleted edges)
// plus the derived pseudo-edges, which the snapshot omits.
type storeState struct {
	snapshot []byte
	pseudo   [][2]t.NodeID
}

func captureState(tt *testing.T, g *store.PushoutGraph) (st storeState) {
	tt.Helper()
	var err error
	st.snapshot, err = g.EncodeSnapshot()
	if err != nil {
		tt.Fatal(err)
	}
	for src := range g.ForwardEdgeSources() {
		for e := range g.ForwardEdges(src) {
			if e.Kind == t.EdgeKindPseudo {
				st.pseudo = append(st.pseudo, [2]t.NodeID{src, e.Dest})
			}
		}
	}
	slices.SortFunc(st.pseudo, func(a, b [2]t.NodeID) int {
		if c := t.CompareNodeID(a[0], b[0]); c != 0 {
			return c
		}
		return t.CompareNodeID(a[1], b[1])
	})
	return
}

func assertSameState(tt *testing.T, g *store.PushoutGraph, want storeState, context string) {
	tt.Helper()
	got := captureState(tt, g)
	if !bytes.Equal(got.snapshot, want.snapshot) {
		tt.Fatalf("%s: store snapshot changed across failed Apply\n%s", context, g.Debug())
	}
	if !slices.Equal(got.pseudo, want.pseudo) {
		tt.Fatalf("%s: pseudo-edges changed across failed Apply: %v -> %v", context, want.pseudo, got.pseudo)
	}
	if g.DirtyRepCount() != 0 {
		tt.Fatalf("%s: dirty components left behind: %d", context, g.DirtyRepCount())
	}
	if errs := qc.CheckInvariants(g); len(errs) > 0 {
		tt.Fatalf("%s: invariant violations after failed Apply: %v", context, errs)
	}
}

// chainBase builds root -> l0 -> l1 -> l2 and tombstones l1 under a
// separate patch, so the fixture carries a deleted component and a
// pseudo-edge (l0 -> l2) that a botched rollback would disturb.
func chainBase(tt *testing.T) (g *store.PushoutGraph, base *Patch, del *Patch, lines [3]t.NodeID) {
	tt.Helper()
	g = store.New()
	base = NewPatch("t", "base", nil, []Change{
		{Kind: ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("l0\n"), UpContext: []t.NodeID{t.RootNodeID}},
		{Kind: ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("l1\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
		{Kind: ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 2}, Content: []byte("l2\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 1}}},
	})
	if err := base.Apply(g); err != nil {
		tt.Fatal(err)
	}
	for i := range lines {
		lines[i] = t.NodeID{Patch: base.Hash, Index: uint64(i)}
	}
	del = NewPatch("t", "del l1", []t.PatchHash{base.Hash}, []Change{{Kind: ChangeKindDeleteNode, NodeID: lines[1]}})
	if err := del.Apply(g); err != nil {
		tt.Fatal(err)
	}
	return
}

// faultStore fails the failAt-th mutating call (AddNode, AddEdge,
// DeleteNode; 1-based) without forwarding it, then forwards everything
// after — including the rollback's inverse calls.
type faultStore struct {
	t.GraphStoreI
	failAt int
	calls  int
}

var errInjected = errors.New("injected store fault")

func (f *faultStore) tick() (err error) {
	f.calls++
	if f.calls == f.failAt {
		err = errInjected
	}
	return
}

func (f *faultStore) AddNode(id t.NodeID, content []byte, patch t.PatchHash, up, down []t.NodeID) (err error) {
	if err = f.tick(); err != nil {
		return
	}
	err = f.GraphStoreI.AddNode(id, content, patch, up, down)
	return
}

func (f *faultStore) AddEdge(src, dest t.NodeID, patch t.PatchHash) (err error) {
	if err = f.tick(); err != nil {
		return
	}
	err = f.GraphStoreI.AddEdge(src, dest, patch)
	return
}

func (f *faultStore) DeleteNode(id t.NodeID, deleter t.PatchHash) (err error) {
	if err = f.tick(); err != nil {
		return
	}
	err = f.GraphStoreI.DeleteNode(id, deleter)
	return
}

// mixedPatch: two inserts (the second chained on the first and anchored
// down on l2), an explicit edge, and a delete — four mutating calls, so
// every prefix of the application order can be interrupted.
func mixedPatch(base *Patch, lines [3]t.NodeID) *Patch {
	return NewPatch("t", "mixed", []t.PatchHash{base.Hash}, []Change{
		{Kind: ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("x\n"), UpContext: []t.NodeID{lines[0]}},
		{Kind: ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("y\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}, DownContext: []t.NodeID{lines[2]}},
		{Kind: ChangeKindNewEdge, Src: lines[0], Dest: lines[2]},
		{Kind: ChangeKindDeleteNode, NodeID: lines[2]},
	})
}

func TestPatchApply_RollsBackWhenStoreFailsMidApply(tt *testing.T) {
	g0, base, _, lines := chainBase(tt)
	p := mixedPatch(base, lines)
	const mutations = 4

	// Sanity: without a fault the patch applies and the fixture reaches
	// every mutation.
	probe := &faultStore{GraphStoreI: g0.Clone()}
	if err := p.Apply(probe); err != nil {
		tt.Fatal(err)
	}
	if probe.calls != mutations {
		tt.Fatalf("fixture drift: %d mutating calls, want %d", probe.calls, mutations)
	}

	want := captureState(tt, g0)
	for failAt := 1; failAt <= mutations; failAt++ {
		g := g0.Clone()
		fs := &faultStore{GraphStoreI: g, failAt: failAt}
		err := p.Apply(fs)
		if !errors.Is(err, errInjected) {
			tt.Fatalf("failAt=%d: expected the injected fault to surface, got %v", failAt, err)
		}
		assertSameState(tt, g, want, fmt.Sprintf("failAt=%d", failAt))
		// The rolled-back patch must apply cleanly once the fault is gone.
		if err := p.Apply(g); err != nil {
			tt.Fatalf("failAt=%d: re-apply after rollback: %v", failAt, err)
		}
		if errs := qc.CheckInvariants(g); len(errs) > 0 {
			tt.Fatalf("failAt=%d: invariants after re-apply: %v", failAt, errs)
		}
	}
}

// Every failure pass 0 can detect must leave the store byte-identical —
// including the dirty set and the derived pseudo-edges. The bad change is
// placed LAST so that, without pass 0, the good changes before it would
// already have landed.
func TestPatchApply_RejectedPatchLeavesStoreUntouched(tt *testing.T) {
	g0, base, _, lines := chainBase(tt)
	goodInsert := Change{Kind: ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("ok\n"), UpContext: []t.NodeID{lines[0]}}
	ghost := nid("ghost", 0)
	cases := []struct {
		name string
		bad  Change
		want string
	}{
		{"missing up-context", Change{Kind: ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("u\n"), UpContext: []t.NodeID{ghost}}, "does not exist"},
		{"missing down-context", Change{Kind: ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("d\n"), UpContext: []t.NodeID{t.RootNodeID}, DownContext: []t.NodeID{ghost}}, "does not exist"},
		{"duplicate node", Change{Kind: ChangeKindNewNode, NodeID: lines[0], Content: []byte("dup\n"), UpContext: []t.NodeID{t.RootNodeID}}, "already exists"},
		{"dangling delete", Change{Kind: ChangeKindDeleteNode, NodeID: ghost}, "does not exist"},
		{"root delete", Change{Kind: ChangeKindDeleteNode, NodeID: t.RootNodeID}, "root"},
		{"edge from missing", Change{Kind: ChangeKindNewEdge, Src: ghost, Dest: lines[0]}, "does not exist"},
		{"edge to missing", Change{Kind: ChangeKindNewEdge, Src: lines[0], Dest: ghost}, "does not exist"},
	}
	want := captureState(tt, g0)
	for _, c := range cases {
		tt.Run(c.name, func(tt *testing.T) {
			g := g0.Clone()
			p := NewPatch("t", c.name, []t.PatchHash{base.Hash}, []Change{goodInsert, c.bad})
			err := p.Apply(g)
			if err == nil {
				tt.Fatal("expected Apply to reject the patch")
			}
			if !bytes.Contains([]byte(err.Error()), []byte(c.want)) {
				tt.Fatalf("unexpected error: %v", err)
			}
			assertSameState(tt, g, want, c.name)
		})
	}
}

// A rejected AddNode must not half-land (node added, context check
// failed): the store-level atomicity Apply's rollback assumes.
func TestStoreAddNode_RejectedCallIsAtomic(tt *testing.T) {
	g0, _, _, lines := chainBase(tt)
	want := captureState(tt, g0)
	id := nid("fresh", 0)
	for _, c := range []struct {
		name     string
		up, down []t.NodeID
	}{
		{"missing up", []t.NodeID{nid("ghost", 0)}, nil},
		{"missing down", []t.NodeID{lines[0]}, []t.NodeID{nid("ghost", 0)}},
	} {
		g := g0.Clone()
		if err := g.AddNode(id, []byte("n\n"), ph("p"), c.up, c.down); err == nil {
			tt.Fatalf("%s: expected AddNode to fail", c.name)
		}
		if g.HasNode(id) {
			tt.Fatalf("%s: rejected AddNode left the node behind", c.name)
		}
		assertSameState(tt, g, want, c.name)
	}
}
