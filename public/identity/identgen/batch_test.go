package identgen

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKeysColumn_AppendAndAt(t *testing.T) {
	var c KeysColumn
	require.Equal(t, 0, c.Len())

	for _, k := range []string{"a", "bb", "ccc", ""} {
		c = c.AppendKey([]byte(k))
	}
	require.Equal(t, 4, c.Len())
	require.Equal(t, []byte("a"), c.At(0))
	require.Equal(t, []byte("bb"), c.At(1))
	require.Equal(t, []byte("ccc"), c.At(2))
	require.Empty(t, c.At(3)) // a zero-length key is a valid column entry
	require.Equal(t, "abbccc", string(c.Data))
	require.Equal(t, []uint32{1, 3, 6, 6}, c.Ends)
}
