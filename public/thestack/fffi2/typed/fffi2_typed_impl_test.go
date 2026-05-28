//go:build llm_generated_opus47

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
