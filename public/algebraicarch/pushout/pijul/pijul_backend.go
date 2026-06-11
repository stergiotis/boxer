//go:build llm_generated_opus47

package pijul

import (
	"context"
	"time"
)

// PatchID identifies a patch in a backend-defined opaque encoding.
// The text backend uses pijul's own hash string; the native backend uses
// the hex-encoded BLAKE3 patch hash. Comparison is by Hex string;
// equality is meaningful only within a single backend.
type PatchID struct {
	Hex string
}

// Empty reports whether the ID is the zero value.
func (id PatchID) Empty() (b bool) {
	b = id.Hex == ""
	return
}

// Short returns up to the first 8 characters of the hash for compact UI
// display.
func (id PatchID) Short() (s string) {
	s = id.Hex
	if len(s) > 8 {
		s = s[:8]
	}
	return
}

// PatchMetadata is the displayable description of a recorded patch:
// what the demo's history pane and provenance label render. Backends
// populate it from whatever underlying structure they use.
type PatchMetadata struct {
	ID           PatchID
	Authors      []string
	Timestamp    time.Time
	Message      string
	Dependencies []PatchID
}

// Author returns the first author or "System" as a fallback.
func (m PatchMetadata) Author() (a string) {
	if len(m.Authors) > 0 && m.Authors[0] != "" {
		a = m.Authors[0]
		return
	}
	a = "System"
	return
}

// PatchEnvelope is a transmittable patch payload for the demo's
// peer-to-peer "Email Patch" feature. Bytes are opaque to the demo;
// the producing and consuming repos must share a backend.
type PatchEnvelope struct {
	ID       PatchID
	Producer string
	Bytes    []byte
}

// RepoI is one actor's working copy. Methods are pure-domain: the
// interface deals in [KVLine] cells and [PatchEnvelope] blobs, never
// in raw text bytes or pijul-specific format details. The text
// backend serialises cells to pijul's flat-KV format internally; the
// native backend translates cells directly into pushout/graggle patch
// operations without ever materialising text.
//
// Every method returns a single audit string — a one-shot human
// readable line summarising what the backend did, suitable for
// appending to an actor's CLI log without further formatting.
type RepoI interface {
	// Path returns the user-facing on-disk location of the repo.
	// Backends are not required to expose a meaningful filesystem
	// path here; for in-memory backends this may be a synthetic ID.
	Path() string

	// Init prepares the repo on the underlying backend. Idempotent
	// against a freshly created [BackendI.NewRepo] handle; not
	// idempotent against an already-initialised path (the backend
	// may error or reset).
	Init(ctx context.Context) (audit string, err error)

	// State returns the current parsed cells together with the
	// patch history. Cells whose introducing patch is known carry
	// a non-nil [KVLine.Credit]; conflicted cells carry a non-nil
	// [KVLine.Conflict] and no Credit. The combined return shape
	// reflects how the demo always reads both together.
	State(ctx context.Context) (cells []KVLine, log []PatchMetadata, audit string, err error)

	// SetAndRecord replaces the working copy with the given cells
	// and creates a new patch authored by `author` with `message`.
	// Returns the new patch's ID. If the cells equal the
	// last-recorded state the backend may skip creating a patch
	// and return the empty PatchID.
	SetAndRecord(ctx context.Context, cells []KVLine, author string, message string) (id PatchID, audit string, err error)

	// Apply ingests an externally produced [PatchEnvelope]. The
	// envelope must originate from a repo of the same backend.
	Apply(ctx context.Context, env PatchEnvelope) (audit string, err error)

	// Push sends this repo's missing patches to dest. dest must be
	// of the same backend; implementations type-assert.
	Push(ctx context.Context, dest RepoI) (audit string, err error)

	// Pull pulls src's missing patches into this repo.
	// hadConflict==true means the pull applied with conflict
	// markers in the working copy; err is nil in that case.
	Pull(ctx context.Context, src RepoI) (audit string, hadConflict bool, err error)

	// ExportLatest returns the most recently recorded patch as a
	// portable envelope. Used by the demo's "Email Patch" feature.
	ExportLatest(ctx context.Context) (env PatchEnvelope, audit string, err error)
}

// BackendI is the factory for repos. The demo holds a single backend
// for the whole run; all per-actor repos come from it.
type BackendI interface {
	// Name is a stable diagnostic label, e.g. "pijul-text" or
	// "pushout-native". Surfaced in the operations log header.
	Name() string

	// NewRepo binds a logical actor name + on-disk path to an
	// uninitialised [RepoI]. Init must be called before any other
	// method.
	NewRepo(actor string, path string) (repo RepoI)

	// Clone creates a fresh repo at destPath/destActor that
	// mirrors src's history. src must originate from this backend.
	Clone(ctx context.Context, src RepoI, destPath string, destActor string) (dest RepoI, audit string, err error)
}
