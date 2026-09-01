package patch

import "errors"

// Sentinel errors for programmatic matching via errors.Is; wrapped with
// eh.Errorf("...: %w", Err...) at the call sites.
var (
	// ErrNodeExists: Apply's pre-validation found a NewNode whose id is
	// already in the store — typically the same patch applied twice. The
	// store is left untouched. Same text as store.ErrNodeExists (no
	// patch→store import).
	ErrNodeExists = errors.New("node already exists")
	// ErrHasDependents: Unapply refused because another still-applied
	// patch references this patch's nodes (incident foreign edges or a
	// foreign tombstone). Unapply dependents first.
	ErrHasDependents = errors.New("dependent patches still applied; unapply dependents first")
	// ErrRetentionPermanent: Unapply would resurrect a node whose
	// content was purged by SweepTombstones; past the retention horizon
	// the patch is permanent. The pushoutgraph layer reports the same
	// condition as store.ErrContentPurged; this sentinel is the
	// patch-level classification (no patch→store import).
	ErrRetentionPermanent = errors.New("content purged past retention horizon; patch is permanent past retention")
)
