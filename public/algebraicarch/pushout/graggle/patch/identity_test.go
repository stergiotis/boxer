// Regression tests for the patch-identity scope settled in the 2026-06-11
// review: the hash covers dependencies (canonicalized) plus changes;
// author and description stay provenance-only. Also pins NewPatch's
// no-caller-mutation contract.
package patch

import (
	"testing"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

func identityChanges() []Change {
	return []Change{{
		Kind:      ChangeKindNewNode,
		NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: 0},
		Content:   []byte("x\n"),
		UpContext: []t.NodeID{t.RootNodeID},
	}}
}

func TestPatchHash_CoversDependencies(tt *testing.T) {
	depA := t.PatchHash{1}
	depB := t.PatchHash{2}

	pNone := NewPatch("alice", "m", nil, identityChanges())
	pA := NewPatch("alice", "m", []t.PatchHash{depA}, identityChanges())
	pAB := NewPatch("alice", "m", []t.PatchHash{depA, depB}, identityChanges())

	if pNone.Hash == pA.Hash {
		tt.Fatal("adding a dependency must change the patch hash")
	}
	if pA.Hash == pAB.Hash {
		tt.Fatal("extending the dependency set must change the patch hash")
	}

	// Order and duplicates must NOT matter: dependencies are a set.
	pBA := NewPatch("alice", "m", []t.PatchHash{depB, depA}, identityChanges())
	pABA := NewPatch("alice", "m", []t.PatchHash{depA, depB, depA}, identityChanges())
	if pAB.Hash != pBA.Hash {
		tt.Fatal("dependency declaration order must not affect the hash")
	}
	if pAB.Hash != pABA.Hash {
		tt.Fatal("duplicate dependency entries must not affect the hash")
	}
}

func TestPatchHash_ExcludesAuthorAndDescription(tt *testing.T) {
	pA := NewPatch("alice", "msg A", nil, identityChanges())
	pB := NewPatch("bob", "completely different msg", nil, identityChanges())
	if pA.Hash != pB.Hash {
		tt.Fatal("author/description are provenance, not identity — same changes+deps must converge on the same hash")
	}
}

func TestPatchHash_IdempotentAcrossFixup(tt *testing.T) {
	p := NewPatch("alice", "m", []t.PatchHash{{7}}, identityChanges())
	if got := p.ComputeHash(); got != p.Hash {
		tt.Fatalf("ComputeHash after fixup diverges: stored %s computed %s", p.Hash, got)
	}
}

func TestNewPatch_DoesNotMutateCallerChanges(tt *testing.T) {
	ch := identityChanges()
	p1 := NewPatch("alice", "m", nil, ch)
	if !ch[0].NodeID.Patch.IsPlaceholder() {
		tt.Fatal("NewPatch rewrote the caller's changes slice in place")
	}
	// Reusing the same slice must produce the identical patch.
	p2 := NewPatch("alice", "m", nil, ch)
	if p1.Hash != p2.Hash {
		tt.Fatalf("reused changes slice produced a different hash: %s vs %s", p1.Hash, p2.Hash)
	}
}
