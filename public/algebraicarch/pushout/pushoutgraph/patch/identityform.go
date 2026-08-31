package patch

import (
	"bytes"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/cbor"

	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/pushoutgraph/types"
)

// The patch identity form v1 — the CBOR item a PatchHash is the digest
// of. Written under RFC 8949 §4.2 core deterministic encoding (shortest
// argument in every head, definite lengths throughout), the rule set
// ADR-0201 pins for the leeway canonical record form; this item is
// canonform-*compatible*, not canonform-*shaped* (ADR-0209).
//
// The grammar, in CBOR diagnostic notation:
//
//	patch     = [ dependencies, changes ]
//	dependencies = 258([ bytes .size 32, * ])   ; sorted, deduplicated
//	changes      = [ * change ]                 ; position order is identity
//
//	change    = newnode / deletenode / newedge
//	newnode   = [ 0, nodeid, bytes, context, context ]   ; content, up, down
//	deletenode= [ 1, nodeid ]
//	newedge   = [ 2, nodeid, nodeid ]                    ; src, dest
//
//	context   = [ * nodeid ]
//	nodeid    = [ patchref, uint ]
//	patchref  = bytes .size 32 / null           ; null == "this patch"
//
// Four properties this shape is chosen for:
//
//   - **Per-kind arrays.** A change carries only the fields its kind
//     uses. Adding a field to one kind moves only that kind's hashes,
//     where the previous encoding (a JSON object with every Change field
//     zero-filled) moved every hash in the system.
//   - **Dependencies are a set.** Tag 258 (IANA "mathematical finite
//     set") states the intent the sort-and-deduplicate in canonicalDeps
//     already enforces, and canonform uses the same tag for its own sets.
//   - **Self-reference is a sentinel, not a magic value.** A patch cannot
//     contain its own hash, so pre-fixup NodeIDs name "this patch". The
//     item writes that as `null`, which no real hash can collide with —
//     the 0xFF… PlaceholderHash stays an in-memory construction device
//     and is no longer load-bearing on the wire. The zero (root/genesis)
//     hash is a real 32-byte string and stays distinct.
//   - **nil and empty are the same thing.** An absent context and an
//     empty one both write as `80`; absent content writes as `40`. The
//     JSON form distinguished them (null vs [] vs ""), which gave two
//     patches with identical semantics different identities.
//
// Author, Description, Producer and Timestamp are provenance and stay
// outside the item, as they were outside the JSON payload.

// identityContextV1 is the BLAKE3 derive_key context for the v1 form.
// Keyed mode is BLAKE3's own domain separation: the digest of this form
// can never collide with the digest of another form or another version,
// and it cannot be reproduced by hashing the same bytes unkeyed. The
// same pattern as canonform's digester (ADR-0201 SD7).
//
// Wire-frozen: changing this string changes every PatchHash.
const identityContextV1 = "github.com/stergiotis/boxer pushout patch identity v1"

// identityKeyV1 is derived once; blake3.DeriveKey is not free and the
// context is a constant.
var identityKeyV1 = func() (k [32]byte) {
	blake3.DeriveKey(k[:], identityContextV1, nil)
	return
}()

// CBOR tag numbers used by the form (IANA CBOR Tags registry).
const tagMathematicalFiniteSet = cbor.TagMathematicalFiniteSet // 258

// IdentityPreimage returns the CBOR identity item — the exact bytes
// [Patch.ComputeHash] digests. Production code never needs it; it exists
// so the form can be pinned by a golden, printed with
// `boxer.sh cbor diagnostics`, and compared across implementations.
func (inst *Patch) IdentityPreimage() (item []byte, err error) {
	var buf bytes.Buffer
	err = inst.writeIdentityItem(&buf)
	if err != nil {
		return
	}
	item = buf.Bytes()
	return
}

// ComputeHash computes the patch hash from its dependencies and changes.
//
// The digest is keyed BLAKE3-256 over the CBOR identity item defined at
// the top of this file: {dependency set, changes}. The dependency set is
// part of patch identity, so an envelope whose dependency list was
// stripped or extended no longer validates against the stored hash. The
// list is canonicalized (sorted, deduplicated) before it is written —
// dependencies are semantically a set, and identity must not depend on
// declaration order. Author and description stay OUTSIDE the hash: they
// are provenance, carried at the envelope level, and two actors
// independently recording the same edit against the same state still
// converge on the same patch.
//
// Idempotence: NewPatch first hashes the changes with PlaceholderHash
// self-references, then rewrites those placeholders to the resulting
// patch hash. ComputeHash undoes that rewrite (changesForHash) before
// writing the item, so repeated calls return the same value regardless
// of fixup state.
//
// Writing the item cannot fail in practice — the sink is a bytes.Buffer
// and every field is a type the form covers — so an error indicates a
// programmer error in extending Change. Panic rather than produce a
// silently bogus hash that breaks patch identity downstream.
func (inst *Patch) ComputeHash() (h t.PatchHash) {
	item, err := inst.IdentityPreimage()
	if err != nil {
		panic(eh.Errorf("write identity item: %w", err))
	}
	hasher := blake3.New(len(h), identityKeyV1[:])
	// blake3's Write never returns an error.
	_, _ = hasher.Write(item)
	h = t.PatchHash(hasher.Sum(make([]byte, 0, len(h))))
	return
}

// writeIdentityItem writes the patch item into w. Kept separate from
// ComputeHash so the bytes can be captured without recomputing a digest.
func (inst *Patch) writeIdentityItem(w *bytes.Buffer) (err error) {
	iw := identityWriter{enc: cbor.NewEncoder(w, nil)}

	iw.arrayHead(2)
	iw.writeDependencies(canonicalDeps(inst.Dependencies))
	changes := inst.changesForHash()
	iw.arrayHead(len(changes))
	for i := range changes {
		iw.writeChange(&changes[i])
	}
	err = iw.err
	return
}

// identityWriter is a thin, error-latching wrapper over the CBOR
// encoder: the form is a fixed sequence of writes with nothing to
// recover from, so the first error is kept and every later call is a
// no-op, and the caller checks once.
type identityWriter struct {
	enc *cbor.Encoder
	err error
}

func (inst *identityWriter) do(_ int, err error) {
	if inst.err == nil && err != nil {
		inst.err = err
	}
}

func (inst *identityWriter) arrayHead(n int) {
	if inst.err != nil {
		return
	}
	inst.do(inst.enc.EncodeArrayDefinite(uint64(n)))
}

func (inst *identityWriter) writeUint(v uint64) {
	if inst.err != nil {
		return
	}
	inst.do(inst.enc.EncodeUint(v))
}

// writeBytes writes a byte string, folding nil to empty: the form does
// not distinguish "no content" from "zero-length content".
func (inst *identityWriter) writeBytes(b []byte) {
	if inst.err != nil {
		return
	}
	if b == nil {
		b = []byte{}
	}
	inst.do(inst.enc.EncodeByteSlice(b))
}

// writeDependencies writes the dependency set. deps arrives sorted and
// deduplicated from canonicalDeps; tag 258 records that it is a set
// rather than a sequence whose order happens not to matter.
func (inst *identityWriter) writeDependencies(deps []t.PatchHash) {
	if inst.err != nil {
		return
	}
	inst.do(inst.enc.EncodeTag16(tagMathematicalFiniteSet))
	inst.arrayHead(len(deps))
	for i := range deps {
		inst.writeBytes(deps[i][:])
	}
}

// writeNodeID writes [patchref, index], with the self-reference as null.
// changesForHash has already rewritten a post-fixup patch's own hash back
// to PlaceholderHash, so "this patch" is always the placeholder here,
// whichever side of NewPatch's fixup the caller is on.
func (inst *identityWriter) writeNodeID(id t.NodeID) {
	if inst.err != nil {
		return
	}
	inst.arrayHead(2)
	if id.Patch.IsPlaceholder() {
		inst.do(inst.enc.EncodeNil())
	} else {
		inst.writeBytes(id.Patch[:])
	}
	inst.writeUint(id.Index)
}

func (inst *identityWriter) writeContext(ids []t.NodeID) {
	if inst.err != nil {
		return
	}
	inst.arrayHead(len(ids))
	for _, id := range ids {
		inst.writeNodeID(id)
	}
}

// writeChange writes one change as [kind, …only that kind's fields].
//
// An unknown kind is a programmer error — a new ChangeKindE that nobody
// taught the form about — and must not fall through to a shape that
// silently collides with an existing kind's.
func (inst *identityWriter) writeChange(c *Change) {
	if inst.err != nil {
		return
	}
	switch c.Kind {
	case ChangeKindNewNode:
		inst.arrayHead(5)
		inst.writeUint(uint64(c.Kind))
		inst.writeNodeID(c.NodeID)
		inst.writeBytes(c.Content)
		inst.writeContext(c.UpContext)
		inst.writeContext(c.DownContext)
	case ChangeKindDeleteNode:
		inst.arrayHead(2)
		inst.writeUint(uint64(c.Kind))
		inst.writeNodeID(c.NodeID)
	case ChangeKindNewEdge:
		inst.arrayHead(3)
		inst.writeUint(uint64(c.Kind))
		inst.writeNodeID(c.Src)
		inst.writeNodeID(c.Dest)
	default:
		inst.err = eb.Build().Uint8("kind", uint8(c.Kind)).Errorf("change kind has no identity-form encoding")
	}
}
