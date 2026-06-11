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
	"slices"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
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
// a freshly computed hash. The hash covers the patch's canonicalized
// dependency set plus its changes, so dependency tampering fails the check
// too. Two further structural guards reject patches that hash consistently
// but were authored broken: changes referencing a patch the dependency
// list does not declare, and placeholder NodeIDs (which only exist in the
// pre-fixup form and can never apply meaningfully). Author, description,
// Producer, and Timestamp remain outside the hash — they are provenance,
// not identity, and are NOT tamper-evident.
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
	declared := make(map[t.PatchHash]struct{}, len(env.Patch.Dependencies))
	for _, d := range env.Patch.Dependencies {
		declared[d] = struct{}{}
	}
	for _, d := range patch.ComputeDependencies(env.Patch.Changes) {
		if d == env.Patch.Hash {
			// Post-fixup self-reference (a node anchored on a sibling
			// from the same patch), not a dependency.
			continue
		}
		if _, ok := declared[d]; !ok {
			err = eh.Errorf("envelope: patch %s references %s but does not declare it as a dependency", env.Patch.Hash, d)
			return
		}
	}
	if slices.ContainsFunc(env.Patch.Changes, changeHasPlaceholder) {
		err = eh.Errorf("envelope: patch %s carries a placeholder NodeID — pre-fixup form cannot be applied", env.Patch.Hash)
		return
	}
	return
}

// changeHasPlaceholder reports whether any NodeID field of the change
// still carries the pre-fixup placeholder hash.
func changeHasPlaceholder(c patch.Change) bool {
	isPlc := func(id t.NodeID) bool { return id.Patch.IsPlaceholder() }
	return isPlc(c.NodeID) || isPlc(c.Src) || isPlc(c.Dest) ||
		slices.ContainsFunc(c.UpContext, isPlc) ||
		slices.ContainsFunc(c.DownContext, isPlc)
}
