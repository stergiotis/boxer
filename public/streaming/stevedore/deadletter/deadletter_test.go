package deadletter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIdentityIsStable(t *testing.T) {
	a, ka := Identity("t", 3, 99)
	b, kb := Identity("t", 3, 99)
	require.Equal(t, a, b)
	require.Equal(t, ka, kb)
	c, _ := Identity("t", 3, 100)
	require.NotEqual(t, a, c)
	d, _ := Identity("u", 3, 99)
	require.NotEqual(t, a, d)
}
