//go:build extrasafe

package unsafeperf_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stergiotis/boxer/public/unsafeperf"
)

// Under extrasafe the string conversions copy, and the slice views are
// refused so callers take their copying paths.
func TestExtrasafeBuildCopies(t *testing.T) {
	// Pointer identity cannot show a copy: the compiler may share a
	// conversion's memory when the result neither escapes nor is written.
	// Writing through the result forces the question.
	s := strings.Clone("copied")
	b := unsafeperf.UnsafeStringToBytes(s)
	b[0] = 'X'
	assert.Equal(t, "copied", s)

	raw := []byte("copy")
	str := unsafeperf.UnsafeBytesToString(raw)
	raw[0] = 'X'
	assert.Equal(t, "copy", str)

	_, ok := unsafeperf.UnsafeSliceToBytes([]uint16{1, 2})
	assert.False(t, ok)
	_, ok = unsafeperf.UnsafeSliceReinterpret[uint32]([]int32{-1, 7})
	assert.False(t, ok)
}
