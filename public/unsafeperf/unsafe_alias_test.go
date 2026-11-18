//go:build !extrasafe

package unsafeperf_test

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/unsafeperf"
)

// In the default build the conversions are views: they share the argument's
// memory instead of copying it.
func TestDefaultBuildAliases(t *testing.T) {
	s := "aliased"
	b := unsafeperf.UnsafeStringToBytes(s)
	require.Len(t, b, len(s))
	assert.Equal(t, unsafe.StringData(s), unsafe.SliceData(b))

	raw := []byte("view")
	str := unsafeperf.UnsafeBytesToString(raw)
	assert.Equal(t, unsafe.SliceData(raw), unsafe.StringData(str))

	u := []uint16{1, 2}
	ub, ok := unsafeperf.UnsafeSliceToBytes(u)
	require.True(t, ok)
	assert.Equal(t, unsafe.Pointer(unsafe.SliceData(u)), unsafe.Pointer(unsafe.SliceData(ub)))

	i := []int32{-1, 7}
	r, ok := unsafeperf.UnsafeSliceReinterpret[uint32](i)
	require.True(t, ok)
	assert.Equal(t, unsafe.Pointer(unsafe.SliceData(i)), unsafe.Pointer(unsafe.SliceData(r)))
}
