package compression

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_ngramWindowed(t *testing.T) {
	require.EqualValues(t, []string{"a", "b", "c"}, ngramWindowed("abc", 1))
	require.EqualValues(t, []string{"ab", "c", "a", "bc"}, ngramWindowed("abc", 2))
	require.EqualValues(t, []string{"abc", "a", "bc", "ab", "c"}, ngramWindowed("abc", 3))
}
