//go:build llm_generated_opus47

package h3

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnyFailure(t *testing.T) {
	require.False(t, AnyFailure(nil))
	require.False(t, AnyFailure([]StatusE{}))
	require.False(t, AnyFailure([]StatusE{StatusOk, StatusOk, StatusOk}))
	require.True(t, AnyFailure([]StatusE{StatusOk, StatusInvalidCell}))
	require.True(t, AnyFailure([]StatusE{StatusInvalidLatLng}))
}

func TestFirstFailure(t *testing.T) {
	{
		idx, code, ok := FirstFailure(nil)
		require.False(t, ok)
		require.Zero(t, idx)
		require.Equal(t, StatusOk, code)
	}
	{
		idx, code, ok := FirstFailure([]StatusE{StatusOk, StatusOk})
		require.False(t, ok)
		require.Zero(t, idx)
		require.Equal(t, StatusOk, code)
	}
	{
		idx, code, ok := FirstFailure([]StatusE{StatusOk, StatusInvalidCell, StatusOk, StatusInvalidLatLng})
		require.True(t, ok)
		require.Equal(t, 1, idx)
		require.Equal(t, StatusInvalidCell, code)
	}
	{
		idx, code, ok := FirstFailure([]StatusE{StatusInvalidResolution})
		require.True(t, ok)
		require.Equal(t, 0, idx)
		require.Equal(t, StatusInvalidResolution, code)
	}
}

func TestCountFailures(t *testing.T) {
	require.Equal(t, 0, CountFailures(nil))
	require.Equal(t, 0, CountFailures([]StatusE{StatusOk, StatusOk}))
	require.Equal(t, 2, CountFailures([]StatusE{StatusOk, StatusInvalidCell, StatusOk, StatusInvalidLatLng}))
	require.Equal(t, 3, CountFailures([]StatusE{StatusInvalidCell, StatusInvalidLatLng, StatusInternal}))
}
