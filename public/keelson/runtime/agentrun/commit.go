package agentrun

import (
	"crypto/rand"
	"encoding/hex"

	"lukechampine.com/blake3"
)

// The commitment the chain holds in place of content (ADR-0325 §SD3,
// amending ADR-0320 §SD2): a per-entry random opening, a key derived from
// it under the scheme's context, and keyed BLAKE3-256 under that key of
// the canonical wire item of the entry's vault row without the opening.
// Without the opening the commitment is a pseudorandom value, so a vault
// row destroyed with its opening leaves nothing retained that reveals the
// content; with it, the row that opens the commitment is the row that was
// written.

// CommitScheme names the construction; an entry that carries a
// commitment records it in KeyId, so a verifier knows how to open.
const CommitScheme = "commit/blake3-keyed/v2"

// OpeningSize is the opening's length in bytes.
const OpeningSize = 32

const commitContext = "github.com/stergiotis/boxer agentrun " + CommitScheme + " vault"

// NewOpening draws an entry's opening; a test replaces it with a
// deterministic one, the way it replaces Now.
var NewOpening = func() (opening []byte) {
	opening = make([]byte, OpeningSize)
	if _, err := rand.Read(opening); err != nil {
		panic("agentrun: the system's random source failed: " + err.Error())
	}
	return
}

// CommitVault is the commitment to a vault row's canonical item under
// opening.
func CommitVault(opening []byte, item []byte) (commitment string) {
	var key [OpeningSize]byte
	blake3.DeriveKey(key[:], commitContext, opening)
	h := blake3.New(32, key[:])
	_, _ = h.Write(item)
	return hex.EncodeToString(h.Sum(nil))
}

// OpensVault says item is what commitment commits to under opening.
func OpensVault(opening []byte, item []byte, commitment string) bool {
	return len(opening) == OpeningSize && commitment != "" && CommitVault(opening, item) == commitment
}

// OpeningText is the opening as the wire and the vault carry it, hex;
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
