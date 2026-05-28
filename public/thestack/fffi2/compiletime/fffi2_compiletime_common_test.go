package compiletime

import (
	"slices"
	"testing"

	"github.com/stergiotis/boxer/public/functional"
	"github.com/stretchr/testify/require"
)

func Test_iterateHighBits(t *testing.T) {
	require.EqualValues(t, []uint8{1, 3, 5}, slices.Collect(functional.IterLeftOnly(iterateHighBits(0b101010, 0))))
	require.EqualValues(t, []uint64{1 << 1, 1 << 3, 1 << 5}, slices.Collect(functional.IterRightOnly(iterateHighBits(0b101010, 0))))
	require.EqualValues(t, []uint8(nil), slices.Collect(functional.IterLeftOnly(iterateHighBits(0, 0))))
}
