// Package envelope defines the transmissible form of a patch and its
// wire-format seam.
//
// An [EnvelopeV1] wraps a *patch.Patch with provenance (Producer,
// Timestamp) that must not enter the patch's content hash: the patch is
// a value whose identity is the BLAKE3 hash of its canonicalized
// dependencies plus changes, so author/send-time travel alongside, not
// within. Provenance is NOT tamper-evident; identity and dependencies
// are (they feed the hash).
//
// Wire bytes are self-describing frames — "PXE1", a codec name, then a
// codec payload — so heterogeneous fleets interoperate as long as both
// registries know the named codec (see [CodecI], [Registry], [JSONV1]).
// [Validate] holds the codec-independent semantic checks and runs on
// every Registry encode/decode.
package envelope

import (
	"errors"
	"slices"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"

	"github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/patch"
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

// EnvelopeV1 is the v1 logical envelope. Producer and Timestamp are
// envelope-level (not patch-level) so they don't enter the patch's
// content hash.
type EnvelopeV1 struct {
	Patch     *patch.Patch `json:"patch"`
	Producer  string       `json:"producer"`
	Timestamp time.Time    `json:"timestamp"`
}

// Sentinel errors for semantic envelope validation.
var (
	// ErrMissingPatch: the envelope carries no patch.
	ErrMissingPatch = errors.New("envelope carries no patch")
	// ErrTampered: the stored hash does not match the recomputed hash —
	// the changes or dependency set were altered, or the bytes were
	// produced against a different identity scheme.
	ErrTampered = errors.New("patch hash mismatch")
	// ErrUndeclaredDependency: the changes reference a patch the
	// dependency list does not declare (authored broken; hashes
	// consistently).
	ErrUndeclaredDependency = errors.New("change references an undeclared dependency")
	// ErrPlaceholderNodeID: the patch carries pre-fixup placeholder
	// NodeIDs and can never apply meaningfully.
	ErrPlaceholderNodeID = errors.New("patch carries a placeholder NodeID")
)

// Validate performs the codec-independent semantic checks on a decoded
// (or about-to-be-encoded) envelope:
//
//   - the stored patch hash equals the freshly recomputed hash; the hash
//     covers the canonicalized dependency set plus the changes, so
//     dependency tampering fails here too
//   - every patch referenced by the changes is declared as a dependency
//     (post-fixup self-references excluded)
//   - no NodeID carries the pre-fixup placeholder hash
//
// Producer, Timestamp, Author, and Description are provenance and remain
// outside these checks.
func Validate(env EnvelopeV1) (err error) {
	if env.Patch == nil {
		err = eh.Errorf("%w", ErrMissingPatch)
		return
	}
	computed := env.Patch.ComputeHash()
	if env.Patch.Hash != computed {
		err = eh.Errorf("stored %s, computed %s: %w", env.Patch.Hash, computed, ErrTampered)
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
			err = eh.Errorf("patch %s references %s: %w", env.Patch.Hash, d, ErrUndeclaredDependency)
			return
		}
	}
	if slices.ContainsFunc(env.Patch.Changes, changeHasPlaceholder) {
		err = eh.Errorf("patch %s: %w", env.Patch.Hash, ErrPlaceholderNodeID)
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
