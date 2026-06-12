package store

import "errors"

// Sentinel errors for programmatic matching via errors.Is. Instances are
// wrapped with eh.Errorf("...: %w", Err...) so call sites carry detail
// (node ids, deleter sets) while callers branch on the sentinel. The
// sentinel texts double as the stable message tail.
var (
	// ErrNodeExists: AddNode target already present (live or tombstoned).
	ErrNodeExists = errors.New("node already exists")
	// ErrNodeMissing: a referenced node (target, context, endpoint) is
	// neither live nor tombstoned.
	ErrNodeMissing = errors.New("node does not exist")
	// ErrRootImmutable: the sentinel root node cannot be deleted.
	ErrRootImmutable = errors.New("cannot delete root node")
	// ErrNotDeleted: UndeleteNode on a node that is not tombstoned.
	ErrNotDeleted = errors.New("node is not deleted")
	// ErrWrongUndeleter: the undeleting patch is not among the node's
	// recorded deleters.
	ErrWrongUndeleter = errors.New("patch did not delete this node")
	// ErrContentPurged: the operation would need content destroyed by
	// SweepTombstones; past the retention horizon the deleting patch is
	// effectively permanent.
	ErrContentPurged = errors.New("content purged past retention horizon")
	// ErrBadSnapshot: snapshot bytes are malformed or carry an
	// unsupported version.
	ErrBadSnapshot = errors.New("malformed graggle snapshot")
)
