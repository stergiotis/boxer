package imgui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIdStack(t *testing.T) {
	stack := NewIdStack(true)
	require.EqualValues(t, 0, stack.GetCurrent())
	stack.SetSeed(1)
	require.EqualValues(t, 1, stack.GetCurrent())
	stack.SetSeed(0)
	stack.AddIDString("stats")
	require.Equal(t, ImGuiID(0x50244858), stack.GetCurrent())
	stack.AddIDString("go render")
	require.Equal(t, ImGuiID(0x18ece0d0), stack.GetCurrent())
	stack.AddIDString("Δt histogram")
	require.Equal(t, ImGuiID(0xa29198a9), stack.GetCurrent())
	stack.RemoveID()
	require.Equal(t, ImGuiID(0x18ece0d0), stack.GetCurrent())
	stack.RemoveID()
	require.Equal(t, ImGuiID(0x50244858), stack.GetCurrent())
	stack.RemoveID()
	require.EqualValues(t, 0, stack.GetCurrent())
}
