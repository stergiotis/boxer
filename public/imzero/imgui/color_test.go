package imgui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestColorU32(t *testing.T) {
	if ImguiUsesBGRAColorFormat {
		require.Equal(t, uint32(0xff6d6027), ColorU32(0x6D6027ff))
	} else {
		require.Equal(t, uint32(0xff27606d), ColorU32(0x6D6027ff))
	}
}

func TestColor32ToU8(t *testing.T) {
	r, g, b, a := Color32ToU8(Color32U8(1, 2, 3, 4))
	require.Equal(t, uint8(1), r)
	require.Equal(t, uint8(2), g)
	require.Equal(t, uint8(3), b)
	require.Equal(t, uint8(4), a)
}
