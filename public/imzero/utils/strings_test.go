package utils

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTruncateDescriptiveNameLeft(t *testing.T) {
	require.Equal(t, "", TruncateDescriptiveNameLeft("abcdef", 0, "_"))
	require.Equal(t, "_", TruncateDescriptiveNameLeft("abcdef", 1, "_"))
	require.Equal(t, "_", TruncateDescriptiveNameLeft("abcdef", 1, "_*"))
	require.Equal(t, "_f", TruncateDescriptiveNameLeft("abcdef", 2, "_"))
	require.Equal(t, "_ef", TruncateDescriptiveNameLeft("abcdef", 3, "_"))
	require.Equal(t, "_def", TruncateDescriptiveNameLeft("abcdef", 4, "_"))
	require.Equal(t, "_cdef", TruncateDescriptiveNameLeft("abcdef", 5, "_"))
	require.Equal(t, "abcdef", TruncateDescriptiveNameLeft("abcdef", 6, "_"))
	require.Equal(t, "abcdef", TruncateDescriptiveNameLeft("abcdef", 7, "_"))
	require.Equal(t, "_*def", TruncateDescriptiveNameLeft("abcdef", 5, "_*"))
}
