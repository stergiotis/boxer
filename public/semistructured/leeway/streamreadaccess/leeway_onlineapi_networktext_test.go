package streamreadaccess

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func uint32Array(t *testing.T, vals ...uint32) arrow.Array {
	t.Helper()
	b := array.NewUint32Builder(memory.DefaultAllocator)
	t.Cleanup(b.Release)
	b.AppendValues(vals, nil)
	arr := b.NewArray()
	t.Cleanup(arr.Release)
	return arr
}

func fixedBinaryArray(t *testing.T, width int, vals ...[]byte) arrow.Array {
	t.Helper()
	b := array.NewFixedSizeBinaryBuilder(memory.DefaultAllocator, &arrow.FixedSizeBinaryType{ByteWidth: width})
	t.Cleanup(b.Release)
	for _, v := range vals {
		require.Len(t, v, width)
		b.Append(v)
	}
	arr := b.NewArray()
	t.Cleanup(arr.Release)
	return arr
}

// The text lane writes a network column out as its address. Without this it
// formats through arrow's ValueStr, which renders an IPv4 host as the decimal
// of its big-endian uint32 and every packed shape as base64 — what the leeway
// card and the per-attribute grid used to show.
func TestNetworkValueText(t *testing.T) {
	v4 := uint32Array(t, 0x01020304, 0)
	assert.Equal(t, "1.2.3.4", valueText(v4, 0, ctabb.V))
	assert.Equal(t, "0.0.0.0", valueText(v4, 1, ctabb.V))
	assert.Equal(t, "16909060", v4.ValueStr(0), "the rendering the lane used to write")

	v6 := fixedBinaryArray(t, 16,
		[]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		[]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 1, 2, 3, 4})
	assert.Equal(t, "2001:db8::1", valueText(v6, 0, ctabb.W))
	assert.Equal(t, "::ffff:1.2.3.4", valueText(v6, 1, ctabb.W), "an IPv4 kept 4-in-6, as ClickHouse shows it")
	assert.Equal(t, "IAENuAAAAAAAAAAAAAAAAQ==", v6.ValueStr(0), "the base64 the lane used to write")

	// The CIDR forms carry the prefix length in a trailing byte.
	v4c := fixedBinaryArray(t, 5, []byte{10, 0, 0, 0, 8}, []byte{10, 0, 0, 0, 255})
	assert.Equal(t, "10.0.0.0/8", valueText(v4c, 0, ctabb.Vc))
	v6c := fixedBinaryArray(t, 17, []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 32})
	assert.Equal(t, "2001:db8::/32", valueText(v6c, 0, ctabb.Wc))

	// An array or set column holds the same element shape; the driver reads
	// the inner array, so the modifier does not change the element rendering.
	assert.Equal(t, "1.2.3.4", valueText(v4, 0, ctabb.Vh))
	assert.Equal(t, "2001:db8::1", valueText(v6, 0, ctabb.Wm))

	// A value the type cannot explain keeps arrow's rendering rather than
	// becoming an invented address: a prefix length no IPv4 can carry, and a
	// column whose Arrow type is not the one the canonical type implies.
	assert.Equal(t, v4c.ValueStr(1), valueText(v4c, 1, ctabb.Vc))
	assert.Equal(t, v6.ValueStr(0), valueText(v6, 0, ctabb.V))
	assert.Equal(t, v4.ValueStr(0), valueText(v4, 0, ctabb.W))

	// A null reads as arrow's null spelling, as every other column does.
	nb := array.NewUint32Builder(memory.DefaultAllocator)
	defer nb.Release()
	nb.AppendNull()
	nulls := nb.NewArray()
	defer nulls.Release()
	assert.Equal(t, nulls.ValueStr(0), valueText(nulls, 0, ctabb.V))

	// Everything that is not a network type is untouched.
	assert.Equal(t, "16909060", valueText(v4, 0, ctabb.U32))
	assert.Equal(t, "16909060", valueText(v4, 0, nil))
	var ct canonicaltypes.PrimitiveAstNodeI
	assert.Equal(t, "16909060", valueText(v4, 0, ct))
}
