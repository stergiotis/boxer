package agentrun

import (
	"crypto/rand"
	"encoding/hex"

	"lukechampine.com/blake3"
)

// The commitments the chain holds in place of bodies (ADR-0320 §SD2): a
// per-entry random opening, one key per field derived from it under a
// field-scoped context, and keyed BLAKE3-256 of the field's bytes under
// that key. Without the opening a commitment is a pseudorandom value, so
// a vault row destroyed with its opening leaves nothing retained that
// reveals the field; with it, the body that opens the commitment is the
// body that was written.

// CommitScheme names the construction; an entry that carries a
// commitment records it in KeyId, so a verifier knows how to open.
const CommitScheme = "commit/blake3-keyed/v1"

// OpeningSize is the opening's length in bytes.
const OpeningSize = 32

// The fields a commitment is scoped to; the context string a key is
// derived under names the scheme and the field, so one opening yields
// unrelated keys per field.
const (
	FieldBody  = "body"
	FieldCalls = "calls"
)

const commitContextPrefix = "github.com/stergiotis/boxer agentrun " + CommitScheme + " "

// NewOpening draws an entry's opening; a test replaces it with a
// deterministic one, the way it replaces Now.
var NewOpening = func() (opening []byte) {
	opening = make([]byte, OpeningSize)
	if _, err := rand.Read(opening); err != nil {
		panic("agentrun: the system's random source failed: " + err.Error())
	}
	return
}

// Commit is the commitment to content under opening for field; empty
// content commits to the empty string, so an entry without a field has
// no commitment for it.
func Commit(opening []byte, field string, content string) (commitment string) {
	if content == "" {
		return ""
	}
	var key [OpeningSize]byte
	blake3.DeriveKey(key[:], commitContextPrefix+field, opening)
	h := blake3.New(32, key[:])
	_, _ = h.Write([]byte(content))
	return hex.EncodeToString(h.Sum(nil))
}

// Opens says content is what commitment commits to under opening for
// field: both empty, or the commitment recomputes.
func Opens(opening []byte, field string, content string, commitment string) bool {
	if content == "" {
		return commitment == ""
	}
	return len(opening) == OpeningSize && Commit(opening, field, content) == commitment
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
