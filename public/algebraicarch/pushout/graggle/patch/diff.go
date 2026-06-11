//go:build llm_generated_opus47

package patch

import (
	"bytes"

	"github.com/stergiotis/boxer/public/observability/eh"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

// DiffResult holds the changes needed to transform one file state to another.
type DiffResult struct {
	Changes []Change
}

// maxLineDiffCells bounds the LCS dynamic-programming table at
// (m+1)*(n+1) int cells (≈32 MiB of table). Quadratic LCS is fine for
// the demo's file sizes; beyond the cap the caller gets an error instead
// of an unbounded allocation.
const maxLineDiffCells = 4 << 20

// LineDiff computes changes between an old set of lines (with known NodeIDs)
// and a new set of lines. The old lines are assumed to be in linear order
// in the graggle.
//
// oldIDs: NodeIDs of the current lines in order (after root).
// oldContents: content of each old line.
// newLines: content of the desired new lines.
//
// Errors on mismatched oldIDs/oldContents lengths and on inputs whose
// LCS table would exceed maxLineDiffCells.
func LineDiff(oldIDs []t.NodeID, oldContents [][]byte, newLines [][]byte) (result DiffResult, err error) {
	if len(oldIDs) != len(oldContents) {
		err = eh.Errorf("LineDiff: %d oldIDs but %d oldContents", len(oldIDs), len(oldContents))
		return
	}
	// Compute LCS using standard DP.
	m := len(oldContents)
	n := len(newLines)
	if (m+1)*(n+1) > maxLineDiffCells {
		err = eh.Errorf("LineDiff: input too large for the quadratic LCS table (%d x %d lines, cap %d cells)", m, n, maxLineDiffCells)
		return
	}

	// dp[i][j] = length of LCS of oldContents[:i] and newLines[:j]
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if bytes.Equal(oldContents[i-1], newLines[j-1]) {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrack to find which old lines are kept.
	kept := make([]bool, m)
	i, j := m, n
	for i > 0 && j > 0 {
		if bytes.Equal(oldContents[i-1], newLines[j-1]) {
			kept[i-1] = true
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			i--
		} else {
			j--
		}
	}

	var changes []Change

	// Delete old lines not in LCS.
	for idx := 0; idx < m; idx++ {
		if !kept[idx] {
			changes = append(changes, Change{
				Kind:   ChangeKindDeleteNode,
				NodeID: oldIDs[idx],
			})
		}
	}

	// Build the sequence of kept old IDs (in order) plus a NodeID -> content
	// map so we don't do an O(m) linear scan per kept line.
	var keptIDs []t.NodeID
	idToContent := make(map[t.NodeID][]byte, m)
	for idx := 0; idx < m; idx++ {
		idToContent[oldIDs[idx]] = oldContents[idx]
		if kept[idx] {
			keptIDs = append(keptIDs, oldIDs[idx])
		}
	}

	// Walk through newLines, inserting new nodes where needed. Track:
	//  - position in keptIDs: the current anchor for up/down context
	//  - lastInserted: the most recent inserted NodeID, used as upContext for
	//    the next insertion when several new lines fall between the same two
	//    anchors. Without this chaining, consecutive inserts share an anchor
	//    pair and produce a fork (order conflict) instead of a sequence.
	var newNodeIndex uint64
	keptPos := 0 // index into keptIDs
	newIdx := 0  // index into newLines
	var lastInserted *t.NodeID

	for newIdx < n {
		if keptPos < len(keptIDs) {
			keptNodeID := keptIDs[keptPos]
			if bytes.Equal(idToContent[keptNodeID], newLines[newIdx]) {
				keptPos++
				newIdx++
				lastInserted = nil
				continue
			}
		}

		// This newLine is an insertion. Chain to the previous insertion if
		// there was one in this same anchor gap.
		var upCtx t.NodeID
		if lastInserted != nil {
			upCtx = *lastInserted
		} else if keptPos > 0 {
			upCtx = keptIDs[keptPos-1]
		} else {
			upCtx = t.RootNodeID
		}
		var downCtx []t.NodeID
		if keptPos < len(keptIDs) {
			downCtx = []t.NodeID{keptIDs[keptPos]}
		}

		newID := t.NodeID{Patch: t.PlaceholderHash, Index: newNodeIndex}
		changes = append(changes, Change{
			Kind:        ChangeKindNewNode,
			NodeID:      newID,
			Content:     newLines[newIdx],
			UpContext:   []t.NodeID{upCtx},
			DownContext: downCtx,
		})
		idCopy := newID
		lastInserted = &idCopy
		newNodeIndex++
		newIdx++
	}

	result = DiffResult{Changes: changes}
	return
}
