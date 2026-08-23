package typed

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stretchr/testify/require"
)

func TestNewRetainedFffi(t *testing.T) {
	r1 := NewRetainedFffiBuilder()
	r1.WriteString("uniq1")
	r1.WriteString("uniq2")
	h1 := r1.BuildRetained()
	r2 := NewRetainedFffiBuilder()
	r2.WriteString("uniq1")
	r2.WriteString("uniq2")
	h2 := r2.BuildRetained()
	require.EqualValues(t, h1.GetRetainedElementId(), h2.GetRetainedElementId())
	require.NotZero(t, h1.GetRetainedElementId())
	require.EqualValues(t, h1, h2)
}

type testWidgetTag struct{}

func TestWidgetHandleRoundTrip(t *testing.T) {
	// Build a retained holder that contains a widget ID at a known offset.
	r := NewRetainedFffiBuilder()
	r.WriteUint32(0xdeadbeef) // opcode (4 bytes)
	expectedId := uint64(0x123456789abcdef0)
	r.WriteWidgetId(expectedId)
	r.WriteUint32(0x11223344) // trailing data
	holder := r.BuildRetained()

	typed := NewRetainedFffiHolderTyped[testWidgetTag](holder)
	h := typed.GetWidgetHandle()
	require.Equal(t, expectedId, h.Resolve(), "WidgetHandle should resolve back to the original ID")

	// Round-trip via Untype
	untyped := typed.Untype()
	require.Equal(t, expectedId, untyped.GetWidgetHandle().Resolve())
}

func TestWidgetHandleWithoutWidgetIdReturnsNoWidget(t *testing.T) {
	// Build a retained holder without calling WriteWidgetId.
	r := NewRetainedFffiBuilder()
	r.WriteUint32(0x11223344)
	holder := r.BuildRetained()

	typed := NewRetainedFffiHolderTyped[testWidgetTag](holder)
	h := typed.GetWidgetHandle()
	// widgetIdOffset is 0 (the default). Offset 0 is valid only if the
	// holder has at least 8 bytes of content. In this case it has 4 bytes,
	// so GetWidgetHandle must return NoWidget.
	require.Equal(t, widgethandle.NoWidget, h)
}

// withCleanBuilderHint isolates a test from builderSizeHint's process-global
// state (and leaves it as it found it), so retention assertions do not depend
// on what other tests in this package folded into the hint first.
func withCleanBuilderHint(t *testing.T) {
	t.Helper()
	prev := builderSizeHint.Load()
	builderSizeHint.Store(0)
	t.Cleanup(func() { builderSizeHint.Store(prev) })
}

// TestPoolRetainsRealisticFrameBuffer pins the sizing contract the retention
// ceiling exists to satisfy: a builder grown to the size a real frame reaches
// must still be *retained* by putInPool, not dropped.
//
// This is the regression a FIXED ceiling caused twice — at 4 KiB (ADR-0049
// Update 2026-08-18) and again at 256 KiB (Update 2026-08-23): a frame's
// spliced deferred-block maps run to a few hundred KiB, so every buffer that
// mattered failed the retention predicate and was re-grown by doubling on the
// next frame. Asserting the predicate rather than a Put/Get round-trip keeps
// the test deterministic — sync.Pool is free to drop entries at any GC, so
// pool identity is not a testable property.
func TestPoolRetainsRealisticFrameBuffer(t *testing.T) {
	withCleanBuilderHint(t)

	// Representative of one frame's spliced deferred-block map; the
	// 2026-08-18 measurement put a whole frame's wire bytes at ~460 KiB
	// spread over several such scopes.
	const realisticFrameBytes = 128 * 1024

	inst := NewRetainedFffiBuilder()
	inst.builder.buf.Write(make([]byte, realisticFrameBytes))
	grown := uint64(inst.builder.buf.Cap())

	require.Greater(t, grown, uint64(4096),
		"test is vacuous unless the buffer actually grew past the old ceiling")
	inst.putInPool()
	require.LessOrEqual(t, grown, runtime.PooledBufferCeiling(builderSizeHint.Load()),
		"a realistically-sized frame buffer must stay poolable, else it is "+
			"discarded and re-grown by doubling every frame")
}

// TestCeilingTracksTheWorkingSet is the property the adaptive ceiling adds
// over any fixed one: a payload well past the ceiling's MINIMUM — the dock
// area's spliced block map is the real case — raises the ceiling that judges
// it, so it is retained rather than discarded and re-grown every frame.
func TestCeilingTracksTheWorkingSet(t *testing.T) {
	withCleanBuilderHint(t)

	const bigFrameBytes = 3 * runtime.PooledCeilingMin // ~768 KiB
	inst := NewRetainedFffiBuilder()
	inst.builder.buf.Write(make([]byte, bigFrameBytes))
	grown := uint64(inst.builder.buf.Cap())

	require.Greater(t, grown, uint64(runtime.PooledCeilingMin),
		"test is vacuous unless the buffer grew past the ceiling's floor")
	inst.putInPool()
	require.LessOrEqual(t, grown, runtime.PooledBufferCeiling(builderSizeHint.Load()),
		"the ceiling must follow the working set up, not discard it")
}

// TestSmallBuildersDoNotMoveTheHint pins the participation floor. The pool's
// population is overwhelmingly small widget builders; if every one folded,
// they would drag the hint to their own size — leaving the ceiling pinned at
// its minimum, which is the fixed-ceiling bug wearing an adaptive hat — and
// pay an atomic write per opcode to do it.
func TestSmallBuildersDoNotMoveTheHint(t *testing.T) {
	withCleanBuilderHint(t)

	inst := NewRetainedFffiBuilder()
	inst.builder.buf.Write(make([]byte, 512*1024))
	inst.putInPool()
	big := builderSizeHint.Load()
	require.NotZero(t, big, "a large builder must fold into the hint")

	for range 1000 {
		small := NewRetainedFffiBuilder()
		small.WriteString("a typical widget's worth of opcodes")
		small.putInPool()
	}
	require.EqualValues(t, big, builderSizeHint.Load(),
		"small builders must not decay the hint the big ones set")
}

// TestDefaultBufferSizeIsIndependentOfCeiling guards the decoupling:
// defaultBufferSize sizes a *fresh* buffer on a pool miss and must stay small,
// so a ceiling that rises with the working set never inflates the common
// small-widget allocation. It was previously derived as the ceiling / 8.
func TestDefaultBufferSizeIsIndependentOfCeiling(t *testing.T) {
	require.Equal(t, 512, defaultBufferSize)
	require.Less(t, uint64(defaultBufferSize), uint64(runtime.PooledCeilingMin/8),
		"defaultBufferSize must not track the ceiling")
}
