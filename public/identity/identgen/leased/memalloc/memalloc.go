// Package memalloc is an in-memory identgen.AllocatorI: a per-tag monotonic
// counter handing out body-value blocks. It is the dependency-free reference
// backend for the leased generators — used in tests, and as the model of the
// server-side state a network id authority keeps per tag. It is safe for
// concurrent use, so several leased generators may share one allocator and draw
// disjoint, globally-unique blocks. It keeps no durable state; a durable or
// remote allocator implements the same seam.
package memalloc

import (
	"context"
	"sync"

	"github.com/stergiotis/boxer/public/identity/identgen"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

const maxUint64 = ^uint64(0)

var _ identgen.AllocatorI = (*Allocator)(nil)

// Allocator hands out monotonically increasing body-value blocks per tag,
// entirely in memory. Bodies start at 1 so the zero id stays reserved as
// invalid/NULL.
type Allocator struct {
	mu   sync.Mutex
	next map[identifier.TagValue]uint64 // next unreserved body value; absent => 1
}

// NewAllocator returns an empty in-memory allocator.
func NewAllocator() (inst *Allocator) {
	return &Allocator{next: make(map[identifier.TagValue]uint64)}
}

func (inst *Allocator) AllocateBlock(ctx context.Context, tagValue identifier.TagValue, minSize uint64, maxSize uint64) (lo identifier.UntaggedId, hi identifier.UntaggedId, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	if !tagValue.IsValid() {
		err = eb.Build().Uint64("tagValue", uint64(tagValue)).Errorf("invalid tag value (zero is reserved)")
		return
	}
	if minSize == 0 || minSize > maxSize {
		err = eb.Build().Uint64("minSize", minSize).Uint64("maxSize", maxSize).Errorf("invalid block size request (need 1 <= minSize <= maxSize)")
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	n, ok := inst.next[tagValue]
	if !ok {
		n = 1 // body 0 reserved as invalid/NULL
	}
	// Reserve up to maxSize, but never wrap the counter. If we cannot reserve
	// even minSize, the (in-memory-modelled) space is exhausted.
	room := maxUint64 - n
	if room < minSize {
		err = eb.Build().Uint64("tagValue", uint64(tagValue)).Errorf("cannot reserve id block: %w", identgen.ErrIdSpaceExhausted)
		return
	}
	sz := min(maxSize, room)
	lo = identifier.UntaggedId(n)
	hi = identifier.UntaggedId(n + sz)
	inst.next[tagValue] = n + sz
	return
}

func (inst *Allocator) Close() (err error) {
	return
}
