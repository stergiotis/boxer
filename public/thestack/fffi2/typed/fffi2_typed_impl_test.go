package typed

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
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

// TestPoolRetainsRealisticFrameBuffer pins the sizing contract that
// largestPooledBuffer exists to satisfy: a builder grown to the size a real
// frame reaches must still be *retained* by putInPool, not dropped.
//
// This is the regression the 4 KiB ceiling caused (ADR-0049 Update 2026-08-18):
// a frame's spliced deferred-block maps run to a few hundred KiB, so every
// buffer that mattered failed the retention predicate and was re-grown by
// doubling on the next frame. Asserting the predicate rather than a
// Put/Get round-trip keeps the test deterministic — sync.Pool is free to
// drop entries at any GC, so pool identity is not a testable property.
func TestPoolRetainsRealisticFrameBuffer(t *testing.T) {
	// Representative of one frame's spliced deferred-block map; the
	// 2026-08-18 measurement put a whole frame's wire bytes at ~460 KiB
	// spread over several such scopes.
	const realisticFrameBytes = 128 * 1024

	inst := NewRetainedFffiBuilder()
	inst.builder.buf.Write(make([]byte, realisticFrameBytes))

	require.Greater(t, inst.builder.buf.Cap(), 4096,
		"test is vacuous unless the buffer actually grew past the old ceiling")
	require.LessOrEqual(t, inst.builder.buf.Cap(), largestPooledBuffer,
		"a realistically-sized frame buffer must stay poolable, else it is "+
			"discarded and re-grown by doubling every frame")
}

// TestDefaultBufferSizeIsIndependentOfCeiling guards the decoupling:
// defaultBufferSize sizes a *fresh* buffer on a pool miss and must stay small,
// so raising the retention ceiling never inflates the common small-widget
// allocation. It was previously derived as largestPooledBuffer/8.
func TestDefaultBufferSizeIsIndependentOfCeiling(t *testing.T) {
	require.Equal(t, 512, defaultBufferSize)
	require.Less(t, defaultBufferSize, largestPooledBuffer/8,
		"defaultBufferSize must not track the ceiling")
}
