package imgui

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestColorU32(t *testing.T) {
	if ImguiUsesBGRAColorFormat {
		require.Equal(t, uint32(0xff6d6027), ColorU32(0x6D6027ff))
	} else {
		require.Equal(t, uint32(0xff27606d), ColorU32(0x6D6027ff))
	}
}
