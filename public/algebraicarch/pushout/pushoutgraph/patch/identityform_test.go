// Pins the patch identity form itself — the CBOR item shape and the
// properties it was chosen for. identity_test.go asserts the *relational*
// facts (what moves a hash and what must not); this file asserts the
// bytes those facts are computed from.
package patch

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"lukechampine.com/blake3"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/types"
)

// diag renders the identity item in RFC 8949 §8 diagnostic notation, so
// a failure shows the shape rather than a hex wall.
func diag(tt *testing.T, p *Patch) string {
	tt.Helper()
	item, err := p.IdentityPreimage()
	if err != nil {
		tt.Fatal(err)
	}
	d, err := cbor.Diagnose(item)
	if err != nil {
		tt.Fatalf("identity item is not well-formed CBOR (%s): %v", hex.EncodeToString(item), err)
	}
	return d
}

// The grammar in identityform.go's package comment, asserted against a
// patch that exercises all three kinds, both contexts, a self-reference,
// an absent context and empty content.
func TestIdentityForm_Shape(tt *testing.T) {
	dep := t.PatchHash{0xAA}
	foreign := t.NodeID{Patch: dep, Index: 7}
	p := NewPatch("alice", "shape", []t.PatchHash{dep}, []Change{
		{
			Kind:      ChangeKindNewNode,
			NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: 0},
			Content:   []byte("hi\n"),
			UpContext: []t.NodeID{t.RootNodeID},
		},
		{
			Kind:   ChangeKindNewNode,
			NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 1},
			// no content, no contexts at all
		},
		{Kind: ChangeKindDeleteNode, NodeID: foreign},
		{Kind: ChangeKindNewEdge, Src: t.NodeID{Patch: t.PlaceholderHash, Index: 0}, Dest: foreign},
	})

	const want = `[258([h'aa00000000000000000000000000000000000000000000000000000000000000']), ` +
		`[[0, [null, 0], h'68690a', [[h'0000000000000000000000000000000000000000000000000000000000000000', 0]], []], ` +
		`[0, [null, 1], h'', [], []], ` +
		`[1, [h'aa00000000000000000000000000000000000000000000000000000000000000', 7]], ` +
		`[2, [null, 0], [h'aa00000000000000000000000000000000000000000000000000000000000000', 7]]]]`
	if got := diag(tt, p); got != want {
		tt.Fatalf("identity item shape changed — this moves every patch hash.\n got: %s\nwant: %s", got, want)
	}
}

// The item must be identical before and after NewPatch's placeholder
// fixup: the self-reference is a sentinel on both sides, so the bytes
// never see the patch's own hash. This is what makes ComputeHash
// idempotent, and it is asserted on the bytes rather than the digest so
// a failure says where it broke.
func TestIdentityForm_StableAcrossFixup(tt *testing.T) {
	changes := []Change{{
		Kind:        ChangeKindNewNode,
		NodeID:      t.NodeID{Patch: t.PlaceholderHash, Index: 0},
		Content:     []byte("x\n"),
		UpContext:   []t.NodeID{t.RootNodeID},
		DownContext: []t.NodeID{{Patch: t.PlaceholderHash, Index: 1}},
	}}

	pre := &Patch{Author: "alice", Changes: changes} // zero Hash: pre-fixup
	preItem, err := pre.IdentityPreimage()
	if err != nil {
		tt.Fatal(err)
	}
	post := NewPatch("alice", "", nil, changes) // fixed up: NodeIDs carry the hash
	postItem, err := post.IdentityPreimage()
	if err != nil {
		tt.Fatal(err)
	}
	if !bytes.Equal(preItem, postItem) {
		tt.Fatalf("identity item moved across fixup:\n pre: %x\npost: %x", preItem, postItem)
	}
	if !bytes.Contains(postItem, []byte{0xF6}) {
		tt.Fatal("expected the self-reference to be written as CBOR null (0xf6)")
	}
	if bytes.Contains(postItem, post.Hash[:]) {
		tt.Fatal("the item must never contain the patch's own hash")
	}
}

// The 0xFF… placeholder is a construction device, not a reserved hash
// value: the item writes self-references as null, so the placeholder no
// longer occupies a point in the hash space.
func TestIdentityForm_PlaceholderBytesAreNotInTheItem(tt *testing.T) {
	p := NewPatch("alice", "m", nil, []Change{{
		Kind:      ChangeKindNewNode,
		NodeID:    t.NodeID{Patch: t.PlaceholderHash, Index: 0},
		Content:   []byte("x\n"),
		UpContext: []t.NodeID{t.RootNodeID},
	}})
	item, err := p.IdentityPreimage()
	if err != nil {
		tt.Fatal(err)
	}
	if bytes.Contains(item, t.PlaceholderHash[:]) {
		tt.Fatalf("placeholder bytes leaked into the identity item: %x", item)
	}
}

// nil and empty are the same content and the same context. The JSON form
// distinguished them (null vs "" vs []), so two semantically identical
// patches could carry different identities.
func TestIdentityForm_FoldsNilAndEmpty(tt *testing.T) {
	nilled := NewPatch("alice", "m", nil, []Change{{
		Kind: ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0},
		Content: nil, UpContext: nil, DownContext: nil,
	}})
	emptied := NewPatch("alice", "m", []t.PatchHash{}, []Change{{
		Kind: ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0},
		Content: []byte{}, UpContext: []t.NodeID{}, DownContext: []t.NodeID{},
	}})
	if nilled.Hash != emptied.Hash {
		tt.Fatalf("nil and empty must hash alike: %s vs %s", nilled.Hash, emptied.Hash)
	}
}

// Change order is identity-bearing (Apply and validateAgainst depend on
// it), while dependency order is not — the pair that tag 258 and the
// plain change array encode.
func TestIdentityForm_ChangeOrderIsIdentity(tt *testing.T) {
	depA, depB := t.PatchHash{1}, t.PatchHash{2}
	nodeA := t.NodeID{Patch: depA, Index: 0}
	nodeB := t.NodeID{Patch: depB, Index: 0}

	ab := NewPatch("alice", "m", []t.PatchHash{depA, depB}, []Change{
		{Kind: ChangeKindDeleteNode, NodeID: nodeA},
		{Kind: ChangeKindDeleteNode, NodeID: nodeB},
	})
	ba := NewPatch("alice", "m", []t.PatchHash{depB, depA}, []Change{
		{Kind: ChangeKindDeleteNode, NodeID: nodeB},
		{Kind: ChangeKindDeleteNode, NodeID: nodeA},
	})
	if ab.Hash == ba.Hash {
		tt.Fatal("reordering changes must change the patch hash")
	}
}

// A kind the form does not know must fail loudly rather than fall
// through to a shape that could collide with a known kind's.
func TestIdentityForm_RejectsUnknownChangeKind(tt *testing.T) {
	p := &Patch{Changes: []Change{{Kind: ChangeKindE(200)}}}
	if _, err := p.IdentityPreimage(); err == nil {
		tt.Fatal("expected an error for an unencodable change kind")
	}
	defer func() {
		if recover() == nil {
			tt.Fatal("expected ComputeHash to panic on an unencodable change kind")
		}
	}()
	_ = p.ComputeHash()
}

// The digest is keyed, not a bare hash of the item: hashing the same
// preimage unkeyed must not reproduce a PatchHash. That is the domain
// separation a form-version bump relies on.
func TestIdentityForm_DigestIsKeyed(tt *testing.T) {
	p := NewPatch("alice", "m", nil, []Change{{
		Kind: ChangeKindNewNode, NodeID: t.NodeID{Patch: t.PlaceholderHash, Index: 0},
		Content: []byte("x\n"), UpContext: []t.NodeID{t.RootNodeID},
	}})
	item, err := p.IdentityPreimage()
	if err != nil {
		tt.Fatal(err)
	}
	if p.Hash == unkeyedDigest(item) {
		tt.Fatal("patch identity must be a keyed digest, not blake3.Sum256 over the item")
	}
}

func unkeyedDigest(item []byte) (h t.PatchHash) {
	h = blake3.Sum256(item)
	return
}
