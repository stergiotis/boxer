package fsmview

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAMachineReportsItselfByItsLabels(t *testing.T) {
	m := NewMachine("idle", 8, MachineOptions[string]{Label: func(s string) string { return "<" + s + ">" }})
	m.AddRule("idle", "running").AddRule("running", "done", "failed").AddRule("done", "running")
	m.EdgeLabel("idle", "running", "Run")
	require.NoError(t, m.Transition("running"))
	require.NoError(t, m.Transition("done"))
	require.NoError(t, m.Transition("running"))

	assert.Equal(t, "<running>", m.OpsCurrent())
	v := m.OpsView(2)
	assert.Equal(t, "<running>", v.Current)
	assert.Contains(t, v.States, "<failed>")
	assert.Len(t, v.Edges, 4)
	require.Len(t, v.History, 2, "at most the steps asked for")
	assert.Equal(t, "<running>", v.History[0].From, "oldest first")
	assert.Equal(t, "<done>", v.History[1].From)
	assert.Empty(t, m.OpsView(0).History)
}
