//go:build llm_generated_opus47

// Package envelope provides a JSON codec for transmittable patch records.
//
// An envelope wraps a *patch.Patch with metadata (Producer, Timestamp) that
// shouldn't live on the patch itself: the patch is a value type whose hash
// identifies it, so author / send time must travel alongside, not within.
//
// The envelope is the on-wire / on-disk form. Decode validates the embedded
// patch's stored hash against a freshly recomputed hash and rejects any
// mismatch — that catches both tampered envelopes and lossy round-trips
// through a non-canonical encoder.
package envelope

import (
	"encoding/json"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
)

// EnvelopeV1 is the v1 transmissible form of a patch. Producer and
// Timestamp are envelope-level (not patch-level) so they don't enter the
// patch's content hash.
type EnvelopeV1 struct {
	Patch     *patch.Patch `json:"patch"`
	Producer  string       `json:"producer"`
	Timestamp time.Time    `json:"timestamp"`
}

// Encode serialises an envelope to canonical JSON. The output is byte-stable
// for byte-stable inputs (the encoder writes struct fields in declaration
// order, slices preserve order, and PatchHash uses MarshalText to emit hex).
func Encode(env EnvelopeV1) (data []byte, err error) {
	if env.Patch == nil {
		err = eh.Errorf("envelope: cannot encode envelope with nil patch")
		return
	}
	data, err = json.MarshalIndent(env, "", "  ")
	if err != nil {
		err = eh.Errorf("envelope: marshal: %w", err)
		return
	}
	return
}

// Decode parses an envelope and verifies that the stored patch hash matches
// a freshly computed hash. Returns an error if the patch is missing, the
// JSON is malformed, or the hash check fails.
func Decode(data []byte) (env EnvelopeV1, err error) {
	if err = json.Unmarshal(data, &env); err != nil {
		err = eh.Errorf("envelope: unmarshal: %w", err)
		return
	}
	if env.Patch == nil {
		err = eh.Errorf("envelope: missing patch")
		return
	}
	computed := env.Patch.ComputeHash()
	if env.Patch.Hash != computed {
		err = eh.Errorf("envelope: hash mismatch (stored %s, computed %s) — envelope was tampered or truncated", env.Patch.Hash, computed)
		return
	}
	return
}
