//go:build llm_generated_opus47

package store

import (
	"crypto/sha256"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/qc"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

func ph(s string) t.PatchHash {
	return sha256.Sum256([]byte(s))
}

func nid(patchStr string, idx uint64) t.NodeID {
	return t.NodeID{Patch: ph(patchStr), Index: idx}
}

func assertNoInvariantViolations(tt *testing.T, g *Graggle) {
	tt.Helper()
	errs := qc.CheckInvariants(g)
	if len(errs) > 0 {
		for _, e := range errs {
			tt.Errorf("invariant violation: %v", e)
		}
		tt.FailNow()
	}
}

func makeBaseGraggle(n int, seed string) (*Graggle, *patch.Patch) {
	g := New()
	changes := make([]patch.Change, n)
	for i := 0; i < n; i++ {
		up := t.RootNodeID
		if i > 0 {
			up = t.NodeID{Patch: t.PlaceholderHash, Index: uint64(i - 1)}
		}
		changes[i] = patch.Change{
			Kind:      patch.ChangeKindNewNode,
			NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: uint64(i)},
			Content:   []byte(fmt.Sprintf("%s_line_%d\n", seed, i)),
			UpContext: []t.NodeID{up},
		}
	}
	p := patch.NewPatch("test", seed, nil, changes)
	p.Apply(g)
	return g, p
}

func randomInsertPatch(base *patch.Patch, rng *rand.Rand, label string, lineCount int) *patch.Patch {
	// Pick a random insertion point among the base's lines.
	pos := rng.Intn(lineCount)
	upCtx := t.RootNodeID
	if pos > 0 {
		upCtx = t.NodeID{Patch: base.Hash, Index: uint64(pos - 1)}
	}
	var downCtx []t.NodeID
	if pos < lineCount {
		downCtx = []t.NodeID{{Patch: base.Hash, Index: uint64(pos)}}
	}
	return patch.NewPatch("test", label, []t.PatchHash{base.Hash}, []patch.Change{
		{
			Kind:        patch.ChangeKindNewNode,
			NodeID:      t.NodeID{Patch: t.PlaceholderHash, Index: 0},
			Content:     []byte(fmt.Sprintf("%s\n", label)),
			UpContext:   []t.NodeID{upCtx},
			DownContext: downCtx,
		},
	})
}

// testDeleter is the deleter hash used by white-box tests that tombstone
// nodes directly rather than through a patch. Tests that undelete must
// pass the same hash back — UndeleteNode rejects unknown undeleters.
var testDeleter = ph("test_deleter")
