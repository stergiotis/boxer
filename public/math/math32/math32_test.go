package math32

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAbs(t *testing.T) {
	require.Equal(t, float32(1.0), Abs(-1.0))
	require.Equal(t, float32(1.0), Abs(+1.0))
	require.Equal(t, float32(1.1), Abs(-1.1))
	require.Equal(t, float32(1.1), Abs(+1.1))
	require.Equal(t, float32(math.Inf(+1)), Abs(float32(math.Inf(+1))))
	require.Equal(t, float32(math.Inf(+1)), Abs(float32(math.Inf(-1))))
	// In Go, the literal -0.0 evaluates to +0.0. Construct true negative zero
	// via Copysign so this asserts the sign-bit clearing behavior of Abs.
	require.Equal(t, float32(+0.0), Abs(float32(math.Copysign(0, -1))))
}
