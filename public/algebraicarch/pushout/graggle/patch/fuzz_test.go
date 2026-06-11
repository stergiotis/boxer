//go:build llm_generated_opus47

package patch

import (
	"bytes"
	"testing"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/store"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

func FuzzLineDiff(f *testing.F) {
	// Seed corpus.
	f.Add([]byte("a\nb\nc\n"), []byte("a\nx\nc\n"))
	f.Add([]byte(""), []byte("new\n"))
	f.Add([]byte("only\n"), []byte(""))
	f.Add([]byte("same\n"), []byte("same\n"))

	f.Fuzz(func(tt *testing.T, oldData, newData []byte) {
		oldLines := splitFuzzLines(oldData)
		newLines := splitFuzzLines(newData)

		oldIDs := make([]t.NodeID, len(oldLines))
		for i := range oldLines {
			oldIDs[i] = t.NodeID{Patch: ph("fuzz"), Index: uint64(i)}
		}

		// Should not panic.
		result := LineDiff(oldIDs, oldLines, newLines)

		// Basic invariant: deletions reference existing old IDs.
		oldIDSet := make(map[t.NodeID]struct{})
		for _, id := range oldIDs {
			oldIDSet[id] = struct{}{}
		}
		for _, c := range result.Changes {
			if c.Kind == ChangeKindDeleteNode {
				if _, ok := oldIDSet[c.NodeID]; !ok {
					tt.Fatalf("deletion of non-existent old ID: %v", c.NodeID)
				}
			}
		}
	})
}

// splitFuzzLines splits data into lines for fuzzing. Each line includes its newline.
func splitFuzzLines(data []byte) [][]byte {
	if len(data) == 0 {
		return nil
	}
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i+1])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

func FuzzPatchApplyUnapply(f *testing.F) {
	f.Add(uint64(0), []byte("inserted line\n"))
	f.Add(uint64(1), []byte("x\n"))

	f.Fuzz(func(tt *testing.T, insertAfter uint64, content []byte) {
		if len(content) == 0 || len(content) > 1024 {
			return
		}

		// Build a small base graggle.
		g := store.New()
		base := NewPatch("fuzz", "base", nil, []Change{
			{Kind: ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Content: []byte("a\n"), UpContext: []t.NodeID{t.RootNodeID}},
			{Kind: ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1}, Content: []byte("b\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 0}}},
			{Kind: ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 2}, Content: []byte("c\n"), UpContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 1}}},
		})
		base.Apply(g)

		origRender := g.Render()

		// Insert at a valid position.
		idx := insertAfter % 3
		upCtx := t.NodeID{Patch: base.Hash, Index: idx}
		var downCtx []t.NodeID
		if idx+1 < 3 {
			downCtx = []t.NodeID{{Patch: base.Hash, Index: idx + 1}}
		}

		p := NewPatch("fuzz", "insert", []t.PatchHash{base.Hash}, []Change{
			{
				Kind:        ChangeKindNewNode,
				NodeID:      t.NodeID{Patch: t.PlaceholderHash, Index: 0},
				Content:     content,
				UpContext:   []t.NodeID{upCtx},
				DownContext: downCtx,
			},
		})

		if err := p.Apply(g); err != nil {
			return // some inputs may be invalid
		}

		if err := p.Unapply(g); err != nil {
			tt.Fatalf("unapply failed: %v", err)
		}

		restored := g.Render()
		if !bytes.Equal(origRender, restored) {
			tt.Fatalf("roundtrip failed: orig=%q restored=%q", origRender, restored)
		}
	})
}
