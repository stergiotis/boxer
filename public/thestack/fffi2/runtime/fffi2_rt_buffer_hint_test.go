package runtime

import (
	"bytes"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFoldSizeHintRatchetsAndDecays pins both directions of the update rule:
// a bigger observation takes effect immediately, a smaller one only bleeds the
// hint down by 1/32 of the gap, so a single quiet frame cannot collapse a hint
// that a busy one earned.
func TestFoldSizeHintRatchetsAndDecays(t *testing.T) {
	var h atomic.Uint64

	FoldSizeHint(&h, 1000)
	require.EqualValues(t, 1000, h.Load(), "a cold hint takes the first observation")

	FoldSizeHint(&h, 4000)
	require.EqualValues(t, 4000, h.Load(), "a larger observation ratchets up at once")

	FoldSizeHint(&h, 0)
	require.EqualValues(t, 4000-(4000>>deferredHintDecayShift), h.Load(),
		"a smaller observation decays by (gap >> shift), not to the observation")

	// Repeated small observations converge downward rather than oscillating.
	// They do not reach zero: the decay is integer, so once the gap is below
	// 1<<shift the shift yields 0 and the hint stops moving. The residue is
	// under 32 bytes, which no consumer can see — BufferHint floors a cold
	// buffer at detachedBufferFloor and PooledBufferCeiling at
	// PooledCeilingMin, both orders of magnitude above it.
	for range 1000 {
		FoldSizeHint(&h, 0)
	}
	require.Less(t, h.Load(), uint64(1<<deferredHintDecayShift),
		"sustained small observations decay to the shift's residue")
}

// TestPooledBufferCeilingClamps pins the adaptive ceiling's two hard limits
// and the slack between them. The min is what keeps a pool that has only seen
// small buffers from dropping its first big one; the max is the
// golang/go#23199 insurance.
func TestPooledBufferCeilingClamps(t *testing.T) {
	require.EqualValues(t, PooledCeilingMin, PooledBufferCeiling(0),
		"a cold hint must not yield a ceiling below the minimum")
	require.EqualValues(t, PooledCeilingMin, PooledBufferCeiling(PooledCeilingMin/4),
		"a small hint stays clamped at the minimum")

	// Above min/slack the ceiling tracks the hint.
	const tracking = PooledCeilingMin // slack 2 puts this above the min
	require.EqualValues(t, tracking*pooledCeilingSlack, PooledBufferCeiling(tracking),
		"between the clamps the ceiling is slack times the hint")

	require.EqualValues(t, PooledCeilingMax, PooledBufferCeiling(PooledCeilingMax),
		"a hint at the max clamps rather than overflowing past it")
	require.EqualValues(t, PooledCeilingMax, PooledBufferCeiling(^uint64(0)),
		"an absurd hint clamps instead of wrapping")
}

// TestBufferHintRecyclesCapacity is the property the pool exists for: a buffer
// that grew to carry a body comes back with that capacity, so the next frame's
// body does not re-grow from zero by doubling.
func TestBufferHintRecyclesCapacity(t *testing.T) {
	bh := RegisterBufferHint("test-recycles-capacity")

	buf := bh.Acquire()
	require.GreaterOrEqual(t, buf.Cap(), detachedBufferFloor,
		"a cold buffer starts at the floor at least")
	buf.Write(make([]byte, 64*1024))
	grown := buf.Cap()
	bh.Release(buf)

	next := bh.Acquire()
	require.Zero(t, next.Len(), "a recycled buffer is handed back empty")
	require.EqualValues(t, grown, next.Cap(), "...but keeps the capacity it grew to")
	bh.Release(next)
}

// TestBufferHintColdBufferStaysAtTheFloor pins the deliberate asymmetry with
// ScopeHint: a BufferHint's kind can cover regions of very different sizes
// (every dock tab shares "dockTabBody"), so a drained pool must not hand each
// of them the largest region's capacity. ADR-0049's 2026-08-19 Update
// measured that shape and rejected it.
func TestBufferHintColdBufferStaysAtTheFloor(t *testing.T) {
	bh := RegisterBufferHint("test-cold-sizing")

	buf := bh.Acquire()
	buf.Write(make([]byte, 64*1024))
	bh.Release(buf)
	require.GreaterOrEqual(t, bh.hint.Load(), uint64(64*1024),
		"the release must have folded a large observation into the hint")

	// Drain the pool the way the collector would, leaving only the hint.
	bh.pool = sync.Pool{}

	cold := bh.Acquire()
	require.EqualValues(t, detachedBufferFloor, cold.Cap(),
		"a cold buffer starts at the floor regardless of what the hint says")
}

// TestBufferHintDropsOutlier pins the insurance: a buffer that grew far past
// what this kind normally costs goes to the collector instead of pinning its
// allocation in the pool for the process's lifetime.
func TestBufferHintDropsOutlier(t *testing.T) {
	bh := RegisterBufferHint("test-drops-outlier")

	// Establish a small working set, then hand back one pathological buffer.
	for range 4 {
		buf := bh.Acquire()
		buf.Write(make([]byte, 1024))
		bh.Release(buf)
	}
	outlier := bytes.NewBuffer(make([]byte, 0, 8*PooledCeilingMax))
	outlier.Write(make([]byte, 4*PooledCeilingMax))
	bh.Release(outlier)

	// Everything the pool hands back now must be a normal buffer. Acquire
	// enough times to exhaust what a per-P pool could be holding.
	for range 16 {
		buf := bh.Acquire()
		require.Less(t, uint64(buf.Cap()), uint64(8*PooledCeilingMax),
			"the outlier must not have been retained")
	}
}

// TestRegisterBufferHintIsIdempotent pins the singleton contract: two call
// sites naming the same kind share one hint and one pool.
func TestRegisterBufferHintIsIdempotent(t *testing.T) {
	a := RegisterBufferHint("test-idempotent")
	b := RegisterBufferHint("test-idempotent")
	require.Same(t, a, b)
	require.NotSame(t, a, RegisterBufferHint("test-idempotent-other"))
}
