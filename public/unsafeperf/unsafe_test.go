package unsafeperf_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/unsafeperf"
)

// The conversions hold the same bytes under either build; only whether they
// alias differs, which unsafe_alias_test.go and unsafe_extrasafe_test.go check.
func TestStringBytesRoundTrip(t *testing.T) {
	for _, s := range []string{"", "a", "héllo", "\x00\xff"} {
		b := unsafeperf.UnsafeStringToBytes(s)
		// bytes.Equal, not assert.Equal: the default build returns nil for
		// "" where extrasafe returns an empty slice.
		assert.True(t, bytes.Equal([]byte(s), b), "%q", s)
		assert.Equal(t, s, unsafeperf.UnsafeBytesToString(b))
	}
	assert.Equal(t, "", unsafeperf.UnsafeBytesToString(nil))
}

func TestSliceToBytesMatchesNativeEncoding(t *testing.T) {
	s := []uint32{1, 0xdeadbeef, math.MaxUint32}
	b, ok := unsafeperf.UnsafeSliceToBytes(s)
	if !ok {
		assert.Nil(t, b, "no view is made when ok is false")
		return
	}
	want := make([]byte, 0, 4*len(s))
	for _, v := range s {
		want = binary.NativeEndian.AppendUint32(want, v)
	}
	assert.Equal(t, want, b)

	empty, ok := unsafeperf.UnsafeSliceToBytes([]float64{})
	assert.True(t, ok)
	assert.Empty(t, empty)
}

func TestSliceReinterpret(t *testing.T) {
	f := []float32{0, 1.5, float32(math.Inf(-1))}
	u, ok := unsafeperf.UnsafeSliceReinterpret[uint32](f)
	if ok {
		require.Len(t, u, len(f))
		for i := range f {
			assert.Equal(t, math.Float32bits(f[i]), u[i])
		}
		nilView, nilOk := unsafeperf.UnsafeSliceReinterpret[uint32]([]float32(nil))
		assert.True(t, nilOk)
		assert.Nil(t, nilView, "a nil slice stays nil")
	} else {
		assert.Nil(t, u)
	}

	// Different element sizes never reinterpret, under either build.
	wide, ok := unsafeperf.UnsafeSliceReinterpret[uint64](f)
	assert.False(t, ok)
	assert.Nil(t, wide)
}
