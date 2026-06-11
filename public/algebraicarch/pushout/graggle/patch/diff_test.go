//go:build llm_generated_opus47

package patch

import (
	"testing"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

func TestLineDiff_BothEmpty(tt *testing.T) {
	result := mustLineDiff(tt, nil, nil, nil)
	if len(result.Changes) != 0 {
		tt.Fatalf("expected no changes, got %d", len(result.Changes))
	}
}

func TestLineDiff_OldOnly(tt *testing.T) {
	oldIDs := []t.NodeID{nid("diff1", 0), nid("diff1", 1)}
	oldContents := [][]byte{[]byte("a\n"), []byte("b\n")}
	result := mustLineDiff(tt, oldIDs, oldContents, nil)

	deletes := 0
	for _, c := range result.Changes {
		if c.Kind == ChangeKindDeleteNode {
			deletes++
		}
	}
	if deletes != 2 {
		tt.Fatalf("expected 2 deletions, got %d", deletes)
	}
}

func TestLineDiff_NewOnly(tt *testing.T) {
	newLines := [][]byte{[]byte("a\n"), []byte("b\n")}
	result := mustLineDiff(tt, nil, nil, newLines)

	inserts := 0
	for _, c := range result.Changes {
		if c.Kind == ChangeKindNewNode {
			inserts++
		}
	}
	if inserts != 2 {
		tt.Fatalf("expected 2 insertions, got %d", inserts)
	}
}

func TestLineDiff_Identical(tt *testing.T) {
	oldIDs := []t.NodeID{nid("diff2", 0), nid("diff2", 1)}
	oldContents := [][]byte{[]byte("a\n"), []byte("b\n")}
	newLines := [][]byte{[]byte("a\n"), []byte("b\n")}

	result := mustLineDiff(tt, oldIDs, oldContents, newLines)
	if len(result.Changes) != 0 {
		tt.Fatalf("expected no changes for identical content, got %d", len(result.Changes))
	}
}

func TestLineDiff_SingleLineInsert(tt *testing.T) {
	result := mustLineDiff(tt, nil, nil, [][]byte{[]byte("new\n")})
	if len(result.Changes) != 1 || result.Changes[0].Kind != ChangeKindNewNode {
		tt.Fatalf("expected 1 insertion, got %v", result.Changes)
	}
	if string(result.Changes[0].Content) != "new\n" {
		tt.Fatalf("wrong content: %q", result.Changes[0].Content)
	}
}

func TestLineDiff_SingleLineDelete(tt *testing.T) {
	oldIDs := []t.NodeID{nid("diff3", 0)}
	oldContents := [][]byte{[]byte("old\n")}
	result := mustLineDiff(tt, oldIDs, oldContents, nil)
	if len(result.Changes) != 1 || result.Changes[0].Kind != ChangeKindDeleteNode {
		tt.Fatalf("expected 1 deletion, got %v", result.Changes)
	}
}

func TestLineDiff_RepeatedLines(tt *testing.T) {
	// Lines with duplicate content. LCS should still handle this correctly.
	oldIDs := []t.NodeID{nid("diff4", 0), nid("diff4", 1), nid("diff4", 2)}
	oldContents := [][]byte{[]byte("x\n"), []byte("x\n"), []byte("x\n")}
	newLines := [][]byte{[]byte("x\n"), []byte("x\n")}

	result := mustLineDiff(tt, oldIDs, oldContents, newLines)
	deletes := 0
	for _, c := range result.Changes {
		if c.Kind == ChangeKindDeleteNode {
			deletes++
		}
	}
	if deletes != 1 {
		tt.Fatalf("expected 1 deletion for removing one of three identical lines, got %d", deletes)
	}
}

func TestLineDiff_LargeReplacement(tt *testing.T) {
	// Replace all old lines with new ones.
	oldIDs := []t.NodeID{nid("diff5", 0), nid("diff5", 1)}
	oldContents := [][]byte{[]byte("a\n"), []byte("b\n")}
	newLines := [][]byte{[]byte("x\n"), []byte("y\n")}

	result := mustLineDiff(tt, oldIDs, oldContents, newLines)
	deletes, inserts := 0, 0
	for _, c := range result.Changes {
		switch c.Kind {
		case ChangeKindDeleteNode:
			deletes++
		case ChangeKindNewNode:
			inserts++
		}
	}
	if deletes != 2 || inserts != 2 {
		tt.Fatalf("expected 2 deletes + 2 inserts, got %d deletes + %d inserts", deletes, inserts)
	}
}

// Multiple new lines inserted between the same anchors must form a chain
// (a -> b), not a fork (root -> a, root -> b). Without chaining, applying
// the resulting patch produces an order conflict for trivial inputs.
func TestLineDiff_ConsecutiveInsertsAreChained(tt *testing.T) {
	// Insert ["a", "b"] into an empty file.
	res := mustLineDiff(tt, nil, nil, [][]byte{[]byte("a\n"), []byte("b\n")})
	if len(res.Changes) != 2 {
		tt.Fatalf("expected 2 changes, got %d", len(res.Changes))
	}
	first := res.Changes[0]
	second := res.Changes[1]
	if len(second.UpContext) != 1 {
		tt.Fatalf("second insert should have 1 upContext, got %d", len(second.UpContext))
	}
	if second.UpContext[0] != first.NodeID {
		tt.Fatalf("second insert should chain to first new node, got upContext %v", second.UpContext[0])
	}
}

// Inserting multiple new lines between two existing kept lines must form a
// chain: kept0 -> new0 -> new1 -> kept1.
func TestLineDiff_ChainedInsertsBetweenKeptLines(tt *testing.T) {
	oldIDs := []t.NodeID{nid("diff_chain", 0), nid("diff_chain", 1)}
	oldContents := [][]byte{[]byte("a\n"), []byte("d\n")}
	newLines := [][]byte{[]byte("a\n"), []byte("b\n"), []byte("c\n"), []byte("d\n")}

	res := mustLineDiff(tt, oldIDs, oldContents, newLines)

	var inserts []Change
	for _, c := range res.Changes {
		if c.Kind == ChangeKindNewNode {
			inserts = append(inserts, c)
		}
	}
	if len(inserts) != 2 {
		tt.Fatalf("expected 2 inserts, got %d", len(inserts))
	}
	// First insert ("b") chains from kept "a"; second insert ("c") chains
	// from the first insert.
	if inserts[0].UpContext[0] != oldIDs[0] {
		tt.Fatalf("first insert should anchor to a, got %v", inserts[0].UpContext)
	}
	if inserts[1].UpContext[0] != inserts[0].NodeID {
		tt.Fatalf("second insert should chain to first insert, got %v", inserts[1].UpContext)
	}
	// Both should land before kept "d".
	if len(inserts[0].DownContext) != 1 || inserts[0].DownContext[0] != oldIDs[1] {
		tt.Fatalf("first insert downContext mismatch: %v", inserts[0].DownContext)
	}
	if len(inserts[1].DownContext) != 1 || inserts[1].DownContext[0] != oldIDs[1] {
		tt.Fatalf("second insert downContext mismatch: %v", inserts[1].DownContext)
	}
}

func TestLineDiff_ContextCorrectness(tt *testing.T) {
	// Verify that inserted nodes have correct up/down context.
	oldIDs := []t.NodeID{nid("diff6", 0), nid("diff6", 1)}
	oldContents := [][]byte{[]byte("a\n"), []byte("c\n")}
	newLines := [][]byte{[]byte("a\n"), []byte("b\n"), []byte("c\n")}

	result := mustLineDiff(tt, oldIDs, oldContents, newLines)
	for _, c := range result.Changes {
		if c.Kind == ChangeKindNewNode {
			if string(c.Content) != "b\n" {
				tt.Fatalf("unexpected inserted content: %q", c.Content)
			}
			// Up context should be nid("diff6", 0) (line "a").
			if len(c.UpContext) != 1 || c.UpContext[0] != nid("diff6", 0) {
				tt.Fatalf("wrong up context: %v", c.UpContext)
			}
			// Down context should be nid("diff6", 1) (line "c").
			if len(c.DownContext) != 1 || c.DownContext[0] != nid("diff6", 1) {
				tt.Fatalf("wrong down context: %v", c.DownContext)
			}
		}
	}
}
