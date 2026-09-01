// Full-state commutativity of causally independent patches that MIX
// NewNode, DeleteNode and NewEdge. The older property tests compare only
// live-node sets of insert-only patches; this one compares everything the
// pushoutgraph holds after each permutation: live and tombstone sets,
// contents, deleter sets, retention bookkeeping, live/deleted edges with
// their introducers (all of that through the deterministic GRG1
// snapshot), the derived pseudo-edge set (excluded from snapshots), the
// dirty-component count, and the Render output.
package store

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/patch"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/qc"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/types"
)

// fixedClock pins tombstoneAt so two graphs that tombstone the same node
// in different orders still encode to identical snapshot bytes.
var fixedClock = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }

// graphFingerprint is the complete observable state of a pushoutgraph.
type graphFingerprint struct {
	render   []byte
	snapshot []byte
	pseudo   []string
	dirty    int
}

// fingerprintGraph captures g's full state. Render is taken first: it
// resolves pseudo-edges, which the snapshot and pseudo-edge listing must
// observe at rest.
func fingerprintGraph(tb interface{ Fatalf(string, ...any) }, g *PushoutGraph) (fp graphFingerprint) {
	fp.render = g.Render()
	var err error
	fp.snapshot, err = g.EncodeSnapshot()
	if err != nil {
		tb.Fatalf("EncodeSnapshot: %v", err)
	}
	fp.pseudo = pseudoEdgeList(g)
	fp.dirty = g.DirtyRepCount()
	return
}

// pseudoEdgeList lists every pseudo-kind edge in g, sorted, as strings.
func pseudoEdgeList(g *PushoutGraph) (out []string) {
	for src := range g.ForwardEdgeSources() {
		for e := range g.ForwardEdges(src) {
			if e.Kind == t.EdgeKindPseudo {
				out = append(out, src.String()+"->"+e.Dest.String())
			}
		}
	}
	slices.Sort(out)
	return
}

// diffFingerprints names the components in which a and b differ.
func diffFingerprints(a, b graphFingerprint) (diff []string) {
	if !bytes.Equal(a.snapshot, b.snapshot) {
		diff = append(diff, "snapshot (nodes/contents/tombstones/deleters/edges)")
	}
	if !slices.Equal(a.pseudo, b.pseudo) {
		diff = append(diff, fmt.Sprintf("pseudo-edges %v vs %v", a.pseudo, b.pseudo))
	}
	if a.dirty != b.dirty {
		diff = append(diff, fmt.Sprintf("dirty reps %d vs %d", a.dirty, b.dirty))
	}
	if !bytes.Equal(a.render, b.render) {
		diff = append(diff, fmt.Sprintf("render %q vs %q", a.render, b.render))
	}
	return
}

// drawIndependentPatch generates a patch whose contexts, delete targets
// and edge endpoints reference only the base patch's nodes (plus the root
// and nodes the patch itself introduces earlier in its change list), so
// any two patches drawn this way are causally independent of each other
// and depend only on base.
func drawIndependentPatch(rt *rapid.T, base *patch.Patch, lineCount int, label string) *patch.Patch {
	baseNode := func(i int) t.NodeID {
		if i == 0 {
			return t.RootNodeID
		}
		return t.NodeID{Patch: base.Hash, Index: uint64(i - 1)}
	}
	var own []t.NodeID
	// anyNode picks among root, base lines and this patch's own nodes.
	anyNode := func(name string) t.NodeID {
		i := rapid.IntRange(0, lineCount+len(own)).Draw(rt, name)
		if i <= lineCount {
			return baseNode(i)
		}
		return own[i-lineCount-1]
	}
	kinds := []patch.ChangeKindE{patch.ChangeKindNewNode, patch.ChangeKindDeleteNode, patch.ChangeKindNewEdge}
	n := rapid.IntRange(1, 3).Draw(rt, "nChanges")
	changes := make([]patch.Change, 0, n)
	for i := range n {
		switch rapid.SampledFrom(kinds).Draw(rt, fmt.Sprintf("kind%d", i)) {
		case patch.ChangeKindNewNode:
			id := t.NodeID{Patch: t.PlaceholderHash, Index: uint64(len(own))}
			c := patch.Change{
				Kind:      patch.ChangeKindNewNode,
				NodeID:    id,
				Content:   fmt.Appendf(nil, "%s_%d\n", label, id.Index),
				UpContext: []t.NodeID{anyNode(fmt.Sprintf("up%d", i))},
			}
			if rapid.Bool().Draw(rt, fmt.Sprintf("hasDown%d", i)) {
				c.DownContext = []t.NodeID{baseNode(rapid.IntRange(1, lineCount).Draw(rt, fmt.Sprintf("down%d", i)))}
			}
			own = append(own, id)
			changes = append(changes, c)
		case patch.ChangeKindDeleteNode:
			changes = append(changes, patch.Change{
				Kind:   patch.ChangeKindDeleteNode,
				NodeID: baseNode(rapid.IntRange(1, lineCount).Draw(rt, fmt.Sprintf("del%d", i))),
			})
		case patch.ChangeKindNewEdge:
			src := anyNode(fmt.Sprintf("src%d", i))
			dest := anyNode(fmt.Sprintf("dest%d", i))
			if src == dest {
				continue
			}
			changes = append(changes, patch.Change{Kind: patch.ChangeKindNewEdge, Src: src, Dest: dest})
		}
	}
	if len(changes) == 0 {
		rt.Skip("no applicable change drawn")
	}
	return patch.NewPatch("prop", label, []t.PatchHash{base.Hash}, changes)
}

func describePatches(patches []*patch.Patch, order []int) string {
	var sb strings.Builder
	for _, idx := range order {
		p := patches[idx]
		fmt.Fprintf(&sb, "  %s (%s):\n", p.Description, p.Hash)
		for _, c := range p.Changes {
			switch c.Kind {
			case patch.ChangeKindNewNode:
				fmt.Fprintf(&sb, "    new %s up=%v down=%v\n", c.NodeID, c.UpContext, c.DownContext)
			case patch.ChangeKindDeleteNode:
				fmt.Fprintf(&sb, "    del %s\n", c.NodeID)
			case patch.ChangeKindNewEdge:
				fmt.Fprintf(&sb, "    edge %s -> %s\n", c.Src, c.Dest)
			}
		}
	}
	return sb.String()
}

func TestProperty_MixedPatchesCommuteOnFullState(tt *testing.T) {
	rapid.Check(tt, func(rt *rapid.T) {
		lineCount := rapid.IntRange(2, 5).Draw(rt, "lineCount")
		baseSeed := "mixed_full_state"
		nPatches := rapid.IntRange(2, 4).Draw(rt, "nPatches")

		_, base := makeBasePushoutGraph(lineCount, baseSeed)
		var patches []*patch.Patch
		seen := make(map[t.PatchHash]struct{})
		for i := range nPatches {
			p := drawIndependentPatch(rt, base, lineCount, fmt.Sprintf("p%d", i))
			// Two patches with identical changes are the SAME patch (the
			// hash is the identity); applying it twice is not a
			// commutativity question, so keep one copy only.
			if _, dup := seen[p.Hash]; dup {
				continue
			}
			seen[p.Hash] = struct{}{}
			patches = append(patches, p)
		}
		if len(patches) < 2 {
			rt.Skip("fewer than two distinct patches")
		}

		idx := make([]int, len(patches))
		for i := range idx {
			idx[i] = i
		}
		nPerms := 3
		if len(patches) == 2 {
			nPerms = 2
		}
		orders := make([][]int, 0, nPerms)
		orders = append(orders, idx)
		for k := 1; k < nPerms; k++ {
			perm := rapid.Permutation(idx).Draw(rt, fmt.Sprintf("perm%d", k))
			orders = append(orders, perm)
		}

		var want graphFingerprint
		for k, order := range orders {
			g, _ := makeBasePushoutGraph(lineCount, baseSeed)
			g.SetClock(fixedClock)
			for _, i := range order {
				if err := patches[i].Apply(g); err != nil {
					rt.Fatalf("order %v: apply %s: %v\n%s", order, patches[i].Description, err, describePatches(patches, order))
				}
			}
			if errs := qc.CheckInvariants(g); len(errs) > 0 {
				rt.Fatalf("order %v: invariant violations: %v\n%s", order, errs, describePatches(patches, order))
			}
			got := fingerprintGraph(rt, g)
			if k == 0 {
				want = got
				continue
			}
			if diff := diffFingerprints(want, got); len(diff) > 0 {
				rt.Fatalf("order %v differs from order %v in: %s\npatches:\n%s", order, orders[0], strings.Join(diff, "; "), describePatches(patches, idx))
			}
		}
	})
}
