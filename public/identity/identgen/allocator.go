package identgen

import (
	"context"

	"github.com/stergiotis/boxer/public/identity/identifier"
)

// AllocatorI durably and exclusively reserves contiguous blocks of body
// (untagged) id space for a tag. It is the technology seam under the leased
// generators (identgen/leased): a local implementation advances a persisted
// per-tag counter, a remote one forwards the reservation to a network
// authority. A generator hands out ids from a reserved block in memory and
// calls AllocateBlock again only when the block is spent, so the backend is
// touched once per block, not once per id.
type AllocatorI interface {
	// AllocateBlock reserves a contiguous range of fresh body values for
	// tagValue and returns it half-open as [lo, hi). It reserves at least
	// minSize values (returning ErrIdSpaceExhausted when it cannot) and at most
	// maxSize; hi-lo may be less than maxSize when a backend caps a reservation,
	// but is always >= minSize on success. Across successful calls for one
	// tagValue, lo is strictly increasing and ranges never overlap — even across
	// a crash or a lost reply. A reserved block is spent whether or not the
	// caller uses it: reservations are monotonic and never recycled, so an
	// implementation in doubt must over-reserve (burn a block), never re-hand
	// one. minSize must be >= 1 and <= maxSize.
	AllocateBlock(ctx context.Context, tagValue identifier.TagValue, minSize uint64, maxSize uint64) (lo identifier.UntaggedId, hi identifier.UntaggedId, err error)
	// Close releases resources the allocator holds (a store handle, a network
	// connection). Generators built on it must not be used afterwards.
	Close() (err error)
}
