package imgui

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestColorU32(t *testing.T) {
	require.Equal(t, uint32(0xff27606d), ColorU32(0x6D6027ff))
}
