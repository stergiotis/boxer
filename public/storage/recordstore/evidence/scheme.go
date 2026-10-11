package evidence

import (
	"crypto/rand"
	"encoding/hex"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonform"
)

// OpeningSize is an opening's length in bytes.
const OpeningSize = 32

// CommitConstruction names the commitment construction every scheme uses:
// a key derived from the opening under the scheme's context, and keyed
// BLAKE3-256 under that key of the item. Without the opening the
// commitment is a pseudorandom value, so a vault row destroyed with its
// opening leaves nothing retained that reveals the content; with it, the
// item that opens the commitment is the item that was committed to.
const CommitConstruction = "commit/blake3-keyed/v2"

// Scheme is the hash constructions a store's digests outlive the build
// under: the canonical record form's keyed digester for chain and content
// digests, and a commitment context of the scheme's own. Its name is
// recorded beside every digest (on the row's key id and on the anchor), so
// a verifier dispatches on it; two schemes never share a commitment
// context, so a commitment under one does not open under another.
type Scheme struct {
	name          string
	digester      canonform.DigesterI
	commitContext string
}

// NewScheme is the scheme name names, over the canonical record form's
// default digester. name is a store's own, versioned by the store when
// its canonical item changes: `agentrun-chain/v4`.
func NewScheme(name string) (inst Scheme) {
	return Scheme{name: name, digester: canonform.NewBlake3Digester(),
		commitContext: "github.com/stergiotis/boxer evidence " + CommitConstruction + " " + name}
}

// Name is the digester's name and the scheme's, what a row and an anchor
// record.
func (inst Scheme) Name() string { return inst.digester.Name() + "/" + inst.name }

// IsZero says the scheme was not made by NewScheme.
func (inst Scheme) IsZero() bool { return inst.digester == nil }

// ChainDigest is the digest over the predecessor's digest and a row's
// canonical item (law L31).
func (inst Scheme) ChainDigest(prev string, item []byte) string {
	h := inst.digester.NewRecord()
	_, _ = h.Write([]byte(prev))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(item)
	return hex.EncodeToString(h.Sum(nil))
}

// ContentDigest is the digest of content alone, as an endorsement or a
// copy record names it.
func (inst Scheme) ContentDigest(content []byte) string {
	h := inst.digester.NewLeaf()
	_, _ = h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// Commit is the commitment to item under opening.
func (inst Scheme) Commit(opening []byte, item []byte) (commitment string) {
	var key [OpeningSize]byte
	blake3.DeriveKey(key[:], inst.commitContext, opening)
	h := blake3.New(32, key[:])
	_, _ = h.Write(item)
	return hex.EncodeToString(h.Sum(nil))
}

// Opens says item is what commitment commits to under opening.
func (inst Scheme) Opens(opening []byte, item []byte, commitment string) bool {
	return len(opening) == OpeningSize && commitment != "" && inst.Commit(opening, item) == commitment
}

// NewOpening draws an opening; a test replaces it with a deterministic
// one.
var NewOpening = func() (opening []byte) {
	opening = make([]byte, OpeningSize)
	if _, err := rand.Read(opening); err != nil {
		panic("evidence: the system's random source failed: " + err.Error())
	}
	return
}

// OpeningText is the opening as a wire and a vault row carry it, hex;
// empty for none.
func OpeningText(opening []byte) string { return hex.EncodeToString(opening) }

// OpeningOf is the inverse of OpeningText; a malformed text is no opening.
func OpeningOf(text string) (opening []byte) {
	if text == "" {
		return nil
	}
	opening, err := hex.DecodeString(text)
	if err != nil || len(opening) != OpeningSize {
		return nil
	}
	return
}
