package runtime

import (
	"lukechampine.com/blake3"
)

// FingerprintContextV1 is the context string the fingerprint key is derived
// from (BLAKE3 derive_key). It names the form, its version and the level, so a
// fingerprint of a canonical-wire entity item can never coincide with a
// canonform digest of the same record, and a later wire version changes every
// fingerprint without colliding with this one. TestFingerprintContextNamesVersion
// pins it to Version; the derived key bytes are pinned by a golden.
const FingerprintContextV1 = "github.com/stergiotis/boxer leeway canonwire v1 entity"

// FingerprintSize is the fingerprint length in bytes.
const FingerprintSize = 32

// FingerprintKey returns the key Fingerprint hashes under: BLAKE3 derive_key
// over FingerprintContextV1, computed on every call (it is a few hundred
// nanoseconds; callers that fingerprint in bulk hold a Fingerprinter).
func FingerprintKey() (key [FingerprintSize]byte) {
	blake3.DeriveKey(key[:], FingerprintContextV1, nil)
	return
}

// Fingerprint is the identity of one canonical-wire entity item's bytes
// (ADR-0219 SD4): BLAKE3-256 in keyed mode under FingerprintKey, over the
// item exactly as an encoder flushed it.
//
// It is a representation identity, not a content identity: two entities have
// equal fingerprints iff they have the same lossless wire form — the same
// values at the same widths, the same memberships on the same channels, the
// same set multiplicities — up to collision. The content identity is
// canonform's digest (ADR-0201), which erases what this keeps. The wire is
// not a hash preimage in the sense of ADR-0210 — nothing stores these as
// identities — but two fingerprints side by side say whether two records are
// the same record, which is what a comparison view needs.
//
// A form bump (Version) changes the context string and so every
// fingerprint, which is correct: the bytes changed.
func Fingerprint(item []byte) (fp [FingerprintSize]byte) {
	f := NewFingerprinter()
	return f.Sum(item)
}

// Fingerprinter holds the derived key so a caller fingerprinting many items
// pays the derivation once. Not goroutine-safe.
type Fingerprinter struct {
	key [FingerprintSize]byte
}

// NewFingerprinter derives the key and returns the fingerprinter.
func NewFingerprinter() (inst *Fingerprinter) {
	inst = &Fingerprinter{key: FingerprintKey()}
	return
}

// Sum fingerprints one entity item.
func (inst *Fingerprinter) Sum(item []byte) (fp [FingerprintSize]byte) {
	h := blake3.New(FingerprintSize, inst.key[:])
	_, _ = h.Write(item)
	h.Sum(fp[:0])
	return
}
