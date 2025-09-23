package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRandomAccessLookupAccel(t *testing.T) {
	accel := NewRandomAccessLookupAccel[int, int](1)
	accel.LoadCardinalities([]uint64{2, 0, 1, 3})
	b, e := accel.LookupForward(0)
	require.EqualValues(t, 0, b)
	require.EqualValues(t, 2, e)
	b, e = accel.LookupForward(1)
	require.EqualValues(t, 2, b)
	require.EqualValues(t, 2, e)
	b, e = accel.LookupForward(2)
	require.EqualValues(t, 2, b)
	require.EqualValues(t, 3, e)
	b, e = accel.LookupForward(3)
	require.EqualValues(t, 3, b)
	require.EqualValues(t, 6, e)
}
