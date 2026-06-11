//go:build llm_generated_opus47

package patch

import (
	"crypto/sha256"
	"testing"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

func ph(s string) t.PatchHash {
	return sha256.Sum256([]byte(s))
}

func nid(patchStr string, idx uint64) t.NodeID {
	return t.NodeID{Patch: ph(patchStr), Index: idx}
}

// mustLineDiff fails the test on a LineDiff validation error; the happy
// path of these tests never trips the size or length guards.
func mustLineDiff(tt *testing.T, oldIDs []t.NodeID, oldContents, newLines [][]byte) DiffResult {
	tt.Helper()
	result, err := LineDiff(oldIDs, oldContents, newLines)
	if err != nil {
		tt.Fatalf("LineDiff: %v", err)
	}
	return result
}
