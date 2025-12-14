package slices

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCopySliceInt(t *testing.T) {
	require.EqualValues(t, []uint16(nil), CopySliceInt[uint8, uint16]([]uint8{}, nil))
	require.EqualValues(t, []uint16{1}, CopySliceInt[uint8, uint16]([]uint8{1}, nil))
	require.EqualValues(t, []uint16{1, 2, 3}, CopySliceInt[uint8, uint16]([]uint8{1, 2, 3}, nil))
	require.EqualValues(t, []uint16{1, 2, 3}, CopySliceInt[uint8, uint16]([]uint8{1, 2, 3}, []uint16{}))
	require.EqualValues(t, []uint16{0, 1, 2, 3}, CopySliceInt[uint8, uint16]([]uint8{1, 2, 3}, []uint16{0}))
}
