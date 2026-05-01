//go:build llm_generated_opus47

package store

import (
	"fmt"
	"testing"

	"github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/algo"
	"github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/patch"
	t "github.com/stergiotis/pebble2impl/src/go/public/algebraicarch/pushout/graggle/types"
)

func benchmarkBase(b *testing.B, n int) (*Graggle, *patch.Patch) {
	changes := make([]patch.Change, n)
	for i := 0; i < n; i++ {
		up := []t.NodeID{t.RootNodeID}
		if i > 0 {
			up = []t.NodeID{{Patch: t.PlaceholderHash, Index: uint64(i - 1)}}
		}
		changes[i] = patch.Change{
			Kind:      patch.ChangeNewNode,
			NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: uint64(i)},
			Content:   []byte(fmt.Sprintf("line %d\n", i)),
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
		for j := 0; j < 100; j++ {
			up := []t.NodeID{t.RootNodeID}
			if j > 0 {
				up = []t.NodeID{{Patch: t.PlaceholderHash, Index: uint64(j - 1)}}
			}
			changes[j] = patch.Change{
				Kind:      patch.ChangeNewNode,
				NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: uint64(j)},
				Content:   []byte(fmt.Sprintf("line %d\n", j)),
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
		for j := 0; j < 1000; j++ {
			up := []t.NodeID{t.RootNodeID}
			if j > 0 {
				up = []t.NodeID{{Patch: t.PlaceholderHash, Index: uint64(j - 1)}}
			}
			changes[j] = patch.Change{
				Kind:      patch.ChangeNewNode,
				NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: uint64(j)},
				Content:   []byte(fmt.Sprintf("line %d\n", j)),
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
		g.DeleteNode(t.NodeID{Patch: base.Hash, Index: uint64(i)})
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
	for i := 0; i < n; i++ {
		oldIDs[i] = nid("bench_diff", uint64(i))
		oldContents[i] = []byte(fmt.Sprintf("line %d\n", i))
		if i == n/2 {
			newLines[i] = []byte("CHANGED\n")
		} else {
			newLines[i] = []byte(fmt.Sprintf("line %d\n", i))
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		patch.LineDiff(oldIDs, oldContents, newLines)
	}
}

func BenchmarkLineDiff_1000Lines(b *testing.B) {
	n := 1000
	oldIDs := make([]t.NodeID, n)
	oldContents := make([][]byte, n)
	newLines := make([][]byte, n)
	for i := 0; i < n; i++ {
		oldIDs[i] = nid("bench_diff", uint64(i))
		oldContents[i] = []byte(fmt.Sprintf("line %d\n", i))
		if i == n/2 {
			newLines[i] = []byte("CHANGED\n")
		} else {
			newLines[i] = []byte(fmt.Sprintf("line %d\n", i))
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		patch.LineDiff(oldIDs, oldContents, newLines)
	}
}