package store

import (
	"fmt"
	"testing"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/algo"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/patch"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/types"
)

func benchmarkBase(b *testing.B, n int) (*PushoutGraph, *patch.Patch) {
	changes := make([]patch.Change, n)
	for i := range n {
		up := []t.NodeID{t.RootNodeID}
		if i > 0 {
			up = []t.NodeID{{Patch: t.PlaceholderHash, Index: uint64(i - 1)}}
		}
		changes[i] = patch.Change{
			Kind:      patch.ChangeKindNewNode,
			NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: uint64(i)},
			Content:   fmt.Appendf(nil, "line %d\n", i),
			UpContext: up,
		}
	}
	g := New()
	p := patch.NewPatch("bench", "base", nil, changes)
	p.Apply(g)
	return g, p
}

func BenchmarkPatchApply_Insert100(b *testing.B) {
	for i := 0; i < b.N; i++ {
		g := New()
		changes := make([]patch.Change, 100)
		for j := range 100 {
			up := []t.NodeID{t.RootNodeID}
			if j > 0 {
				up = []t.NodeID{{Patch: t.PlaceholderHash, Index: uint64(j - 1)}}
			}
			changes[j] = patch.Change{
				Kind:      patch.ChangeKindNewNode,
				NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: uint64(j)},
				Content:   fmt.Appendf(nil, "line %d\n", j),
				UpContext: up,
			}
		}
		p := patch.NewPatch("bench", "100 lines", nil, changes)
		p.Apply(g)
	}
}

func BenchmarkPatchApply_Insert1000(b *testing.B) {
	for i := 0; i < b.N; i++ {
		g := New()
		changes := make([]patch.Change, 1000)
		for j := range 1000 {
			up := []t.NodeID{t.RootNodeID}
			if j > 0 {
				up = []t.NodeID{{Patch: t.PlaceholderHash, Index: uint64(j - 1)}}
			}
			changes[j] = patch.Change{
				Kind:      patch.ChangeKindNewNode,
				NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: uint64(j)},
				Content:   fmt.Appendf(nil, "line %d\n", j),
				UpContext: up,
			}
		}
		p := patch.NewPatch("bench", "1000 lines", nil, changes)
		p.Apply(g)
	}
}

func BenchmarkResolvePseudoEdges_100Deleted(b *testing.B) {
	// Create 102 nodes, delete the middle 100.
	g, base := benchmarkBase(b, 102)
	for i := 1; i <= 100; i++ {
		g.DeleteNode(t.NodeID{Patch: base.Hash, Index: uint64(i)}, ph("bench_del"))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.dirtyReps[g.deletedPartition.Find(t.NodeID{Patch: base.Hash, Index: 1})] = struct{}{}
		g.ResolvePseudoEdges()
	}
}

func BenchmarkTarjan_100Nodes(b *testing.B) {
	g, _ := benchmarkBase(b, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		algo.Tarjan(g)
	}
}

func BenchmarkTarjan_1000Nodes(b *testing.B) {
	g, _ := benchmarkBase(b, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		algo.Tarjan(g)
	}
}

func BenchmarkClone_100Nodes(b *testing.B) {
	g, _ := benchmarkBase(b, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Clone()
	}
}

func BenchmarkClone_1000Nodes(b *testing.B) {
	g, _ := benchmarkBase(b, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Clone()
	}
}

func BenchmarkRender_100Nodes(b *testing.B) {
	g, _ := benchmarkBase(b, 100)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Render()
	}
}

func BenchmarkRender_1000Nodes(b *testing.B) {
	g, _ := benchmarkBase(b, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Render()
	}
}

func BenchmarkLineDiff_100Lines(b *testing.B) {
	n := 100
	oldIDs := make([]t.NodeID, n)
	oldContents := make([][]byte, n)
	newLines := make([][]byte, n)
	for i := range n {
		oldIDs[i] = nid("bench_diff", uint64(i))
		oldContents[i] = fmt.Appendf(nil, "line %d\n", i)
		if i == n/2 {
			newLines[i] = []byte("CHANGED\n")
		} else {
			newLines[i] = fmt.Appendf(nil, "line %d\n", i)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = patch.LineDiff(oldIDs, oldContents, newLines)
	}
}

func BenchmarkLineDiff_1000Lines(b *testing.B) {
	n := 1000
	oldIDs := make([]t.NodeID, n)
	oldContents := make([][]byte, n)
	newLines := make([][]byte, n)
	for i := range n {
		oldIDs[i] = nid("bench_diff", uint64(i))
		oldContents[i] = fmt.Appendf(nil, "line %d\n", i)
		if i == n/2 {
			newLines[i] = []byte("CHANGED\n")
		} else {
			newLines[i] = fmt.Appendf(nil, "line %d\n", i)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = patch.LineDiff(oldIDs, oldContents, newLines)
	}
}

// --- Scale benchmarks (10k / 100k nodes) ---
//
// Unlike the Insert100/Insert1000 benchmarks above, these build the patch
// once outside the timed loop: at these sizes NewPatch's identity hashing
// is itself a measurable cost and would hide the engine's numbers.

func chainPatch(n int) *patch.Patch {
	changes := make([]patch.Change, n)
	for i := range n {
		up := []t.NodeID{t.RootNodeID}
		if i > 0 {
			up = []t.NodeID{{Patch: t.PlaceholderHash, Index: uint64(i - 1)}}
		}
		changes[i] = patch.Change{
			Kind:      patch.ChangeKindNewNode,
			NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: uint64(i)},
			Content:   fmt.Appendf(nil, "line %d\n", i),
			UpContext: up,
		}
	}
	return patch.NewPatch("bench", "chain", nil, changes)
}

func benchmarkApplyN(b *testing.B, n int) {
	p := chainPatch(n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g := New()
		if err := p.Apply(g); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkRenderN(b *testing.B, n int) {
	g := New()
	if err := chainPatch(n).Apply(g); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Render()
	}
}

func benchmarkCloneN(b *testing.B, n int) {
	g := New()
	if err := chainPatch(n).Apply(g); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Clone()
	}
}

func benchmarkEncodeSnapshotN(b *testing.B, n int) {
	g := New()
	if err := chainPatch(n).Apply(g); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := g.EncodeSnapshot(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPatchApply_Insert10k(b *testing.B)  { benchmarkApplyN(b, 10_000) }
func BenchmarkPatchApply_Insert100k(b *testing.B) { benchmarkApplyN(b, 100_000) }
func BenchmarkPatchApply_Insert1M(b *testing.B)   { benchmarkApplyN(b, 1_000_000) }

func BenchmarkRender_10kNodes(b *testing.B)  { benchmarkRenderN(b, 10_000) }
func BenchmarkRender_100kNodes(b *testing.B) { benchmarkRenderN(b, 100_000) }
func BenchmarkRender_1MNodes(b *testing.B)   { benchmarkRenderN(b, 1_000_000) }

func BenchmarkClone_10kNodes(b *testing.B)  { benchmarkCloneN(b, 10_000) }
func BenchmarkClone_100kNodes(b *testing.B) { benchmarkCloneN(b, 100_000) }
func BenchmarkClone_1MNodes(b *testing.B)   { benchmarkCloneN(b, 1_000_000) }

func BenchmarkEncodeSnapshot_1kNodes(b *testing.B)   { benchmarkEncodeSnapshotN(b, 1_000) }
func BenchmarkEncodeSnapshot_10kNodes(b *testing.B)  { benchmarkEncodeSnapshotN(b, 10_000) }
func BenchmarkEncodeSnapshot_100kNodes(b *testing.B) { benchmarkEncodeSnapshotN(b, 100_000) }
func BenchmarkEncodeSnapshot_1MNodes(b *testing.B)   { benchmarkEncodeSnapshotN(b, 1_000_000) }
