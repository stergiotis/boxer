package repo

import "errors"

// Sentinel errors of the engine's public API, matched via errors.Is.
// Where a deeper layer classified the condition first (patch, store,
// envelope), instances wrap BOTH sentinels via errors.Join, so callers
// may match at either level.
var (
	// ErrClosed: the repo has been closed.
	ErrClosed = errors.New("repo is closed")
	// ErrNoChanges: Record was called with an empty change list.
	ErrNoChanges = errors.New("no changes to record")
	// ErrMissingDependency: the envelope depends on a patch that is not
	// currently APPLIED (merely having seen its envelope is not enough).
	ErrMissingDependency = errors.New("missing dependency; apply prerequisite patches first")
	// ErrDependentExists: Unrecord refused because an applied patch
	// declares the target among its dependencies.
	ErrDependentExists = errors.New("patch is a dependency of an applied patch; unrecord dependents first")
	// ErrNotApplied: the patch is not in the applied set.
	ErrNotApplied = errors.New("patch not currently applied")
	// ErrRetentionBlocked: Unrecord would resurrect content destroyed by
	// a retention sweep; the patch is permanent on this repo.
	ErrRetentionBlocked = errors.New("retention sweep made this patch permanent")
	// ErrIdentityExhausted: the identity-collision disambiguation could
	// not find a fresh patch identity (pathological; indicates a bug or
	// adversarial history).
	ErrIdentityExhausted = errors.New("could not disambiguate patch identity")
	// ErrEnvelopeNotFound: the storage holds no envelope for the hash.
	// StorageI implementations wrap this sentinel.
	ErrEnvelopeNotFound = errors.New("envelope not found")
	// ErrCorruptStore: recovery found storage violating engine
	// guarantees (missing envelope for an applied hash, dependency
	// applied out of order, undecodable persisted envelope). Refuse to
	// open rather than guess.
	ErrCorruptStore = errors.New("storage is corrupt")
)
