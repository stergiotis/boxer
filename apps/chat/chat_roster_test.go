package chat

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
)

func TestTheRosterGroupsConversations(t *testing.T) {
	t0 := time.Date(2026, 10, 11, 14, 0, 0, 0, time.UTC)
	runs := []agentrun.Run{
		{Conversation: "here-old", Coordinator: "1", State: agentrun.StateOpen, Created: t0},
		{Conversation: "here-now", Coordinator: "1", State: agentrun.StateOpen, Created: t0.Add(time.Hour)},
		{Conversation: "other", Coordinator: "5", State: agentrun.StateOpen, Created: t0.Add(2 * time.Hour)},
		{Conversation: "orphan", Coordinator: "9", State: agentrun.StateOpen, Created: t0},
		{Conversation: "done", Coordinator: "1", State: agentrun.StateClosed, Created: t0},
		{Conversation: "pinned", Coordinator: "7", State: agentrun.StateStranded, Created: t0},
		{Conversation: "gone", Coordinator: "1", State: agentrun.StateDeleted, Created: t0},
	}
	extra := rosterExtra{
		windows: []rosterWindow{
			{Key: "1", AppId: "chat"}, {Key: "5", AppId: "chat"},
			{Key: "2", AppId: "play", Display: "SQL playground", AgentTasks: []string{"task-a"}},
			{Key: "3", AppId: "map", Display: "Map", AgentTasks: []string{"task-b"}},
			{Key: "4", AppId: "mdedit", Display: "mdedit", AgentTasks: []string{"task-old"}},
		},
		grants: []rosterGrant{
			{Task: "task-a", ActorInstance: "1"},
			{Task: "task-b", ActorInstance: "5"},
			{Task: "task-old", ActorInstance: "1", Revoked: "ended"},
		},
		titles: map[string]string{"other": "Wind field"},
	}
	live, resumable, stranded, closed := buildRoster(runs, extra, "chat", "1")

	require.Len(t, live, 2)
	assert.Equal(t, "here-now", live[0].run.Conversation, "this window's newest open run, first")
	assert.True(t, live[0].here)
	require.Len(t, live[0].working, 1, "a revoked task's window is not its")
	assert.Equal(t, "2", live[0].working[0].Key)
	assert.Equal(t, "other", live[1].run.Conversation)
	assert.Equal(t, "Wind field", live[1].title)
	assert.Equal(t, "3", live[1].working[0].Key)

	var res []string
	for _, r := range resumable {
		res = append(res, r.run.Conversation)
	}
	assert.ElementsMatch(t, []string{"here-old", "orphan"}, res, "an open run no window holds now resumes")
	var cl []string
	for _, r := range closed {
		cl = append(cl, r.run.Conversation)
	}
	assert.ElementsMatch(t, []string{"done"}, cl, "deleted runs are left out")
	require.Len(t, stranded, 1)
	assert.Equal(t, "pinned", stranded[0].run.Conversation, "a stranded run has its own group")
}
