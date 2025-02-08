package math32

import (
	"github.com/stretchr/testify/require"
	"math"
	"testing"
)

func TestAbs(t *testing.T) {
	require.Equal(t, float32(1.0), Abs(-1.0))
	require.Equal(t, float32(1.0), Abs(+1.0))
	require.Equal(t, float32(1.1), Abs(-1.1))
	require.Equal(t, float32(1.1), Abs(+1.1))
	require.Equal(t, float32(math.Inf(+1)), Abs(float32(math.Inf(+1))))
	require.Equal(t, float32(math.Inf(+1)), Abs(float32(math.Inf(-1))))
	require.Equal(t, float32(+0.0), Abs(-0.0))
}
