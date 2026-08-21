package canonform

import (
	"hash"
	"io"

	"lukechampine.com/blake3"
)

// DigesterI supplies the two hashers of the form (ADR-0201 SD7): one per
// attribute item (the leaf) and one per entity item (the record). The leaf
// digests are embedded in the entity item, so Size is part of the form.
//
// The hash function is a parameter so the decision can be revisited without
// touching the form; the default is keyed BLAKE3. Two parties computing
// digests must agree on the digester as they must on the classifier and the
// plains selection.
type DigesterI interface {
	// NewLeaf returns a fresh hasher for one attribute item.
	NewLeaf() hash.Hash
	// NewRecord returns a fresh hasher for one entity item.
	NewRecord() hash.Hash
	// Size is the digest length in bytes, the same for both levels.
	Size() int
}

// The context strings the default digester derives its two keys from name the
// form, the level and the version: a later form version changes every digest
// without colliding with this one, and an attribute item can never be
// mistaken for an entity item. The derived key bytes are pinned by a golden
// in this package.
const (
	// ContextLeafV1 derives the attribute-level (leaf) key.
	ContextLeafV1 = "github.com/stergiotis/boxer leeway canonform v1 attribute"
	// ContextRecordV1 derives the entity-level (record) key.
	ContextRecordV1 = "github.com/stergiotis/boxer leeway canonform v1 record"
)

// Blake3DigestSize is the digest length of the default digester.
const Blake3DigestSize = 32

// Blake3Digester is the default digester: BLAKE3-256 in keyed mode, the keys
// derived once (BLAKE3 derive_key) from ContextLeafV1 / ContextRecordV1.
// Keyed mode is BLAKE3's own domain-separation mechanism.
type Blake3Digester struct {
	leafKey   [Blake3DigestSize]byte
	recordKey [Blake3DigestSize]byte
}

var _ DigesterI = (*Blake3Digester)(nil)

// NewBlake3Digester derives the two keys and returns the digester.
func NewBlake3Digester() (inst *Blake3Digester) {
	inst = &Blake3Digester{}
	blake3.DeriveKey(inst.leafKey[:], ContextLeafV1, nil)
	blake3.DeriveKey(inst.recordKey[:], ContextRecordV1, nil)
	return
}

// LeafKey returns the derived attribute-level key (for pinning).
func (inst *Blake3Digester) LeafKey() [Blake3DigestSize]byte { return inst.leafKey }

// RecordKey returns the derived entity-level key (for pinning).
func (inst *Blake3Digester) RecordKey() [Blake3DigestSize]byte { return inst.recordKey }

func (inst *Blake3Digester) NewLeaf() hash.Hash { return blake3.New(Blake3DigestSize, inst.leafKey[:]) }
func (inst *Blake3Digester) NewRecord() hash.Hash {
	return blake3.New(Blake3DigestSize, inst.recordKey[:])
}
func (inst *Blake3Digester) Size() int { return Blake3DigestSize }

// NewRecordingDigester wraps a digester so that every byte written to a leaf
// hasher is also copied to leaves and every byte written to a record hasher to
// records (either may be nil). The digests are unchanged. This is how tests
// and debugging obtain the canonical items, which otherwise exist only on the
// way into the hashers; production code does not materialize them.
func NewRecordingDigester(inner DigesterI, leaves io.Writer, records io.Writer) DigesterI {
	return &recordingDigester{inner: inner, leaves: leaves, records: records}
}

type recordingDigester struct {
	inner   DigesterI
	leaves  io.Writer
	records io.Writer
}

func (inst *recordingDigester) NewLeaf() hash.Hash {
	return teeHash{Hash: inst.inner.NewLeaf(), w: inst.leaves}
}

func (inst *recordingDigester) NewRecord() hash.Hash {
	return teeHash{Hash: inst.inner.NewRecord(), w: inst.records}
}

func (inst *recordingDigester) Size() int { return inst.inner.Size() }

// teeHash forwards every write to an extra writer; Sum / Size / Reset come
// from the embedded hasher unchanged.
type teeHash struct {
	hash.Hash
	w io.Writer
}

func (inst teeHash) Write(p []byte) (n int, err error) {
	if inst.w != nil {
		if _, err = inst.w.Write(p); err != nil {
			return
		}
	}
	return inst.Hash.Write(p)
}
