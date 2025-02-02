package imgui

import (
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIdStack(t *testing.T) {
	stack := NewIdStack(true)
	require.EqualValues(t, 0, stack.GetCurrent())
	stack.SetSeed(1)
	require.EqualValues(t, 1, stack.GetCurrent())
	stack.SetSeed(0)
	stack.AddIDString("stats")
	require.Equal(t, imgui.ImGuiID(0x574767aa), stack.GetCurrent())
	stack.AddIDString("go render")
	require.Equal(t, imgui.ImGuiID(0x066099e9), stack.GetCurrent())
	stack.AddIDString("Δt histogram")
	require.Equal(t, imgui.ImGuiID(0xc3950749), stack.GetCurrent())
	stack.RemoveID()
	require.Equal(t, imgui.ImGuiID(0x066099e9), stack.GetCurrent())
	stack.RemoveID()
	require.Equal(t, imgui.ImGuiID(0x574767aa), stack.GetCurrent())
	stack.RemoveID()
	require.EqualValues(t, 0, stack.GetCurrent())
}
