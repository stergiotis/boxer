package chat

import (
	"slices"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/lifetime"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
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

// The roster's title reaches the host's store: the manifest declares the
// prefix, the host injects the persist cap for it, and the write storeTitle
// hands off the frame lands under the window's scope. Without the
// declaration the bus refuses the write, which is what a manifest with no
// PersistedKeys did.
func TestTheRostersTitleReachesTheStore(t *testing.T) {
	assert.Equal(t, []string{titleStorePrefix}, manifest.PersistedKeys)
	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(2 * time.Second)
	backend := persist.NewMemoryBackend()
	svc, err := persist.NewService(bus, zerolog.Nop(), backend)
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	alias := ManifestId.SubjectAlias()
	clientOver := func(caps []app.SubjectFilter) (storage app.StorageI) {
		storage, cErr := persist.NewClient(bus.NewClient(ManifestId, caps), ManifestId)
		require.NoError(t, cErr)
		return
	}
	titled := func(scope *lifetime.Scope, storage app.StorageI) (inst *App) {
		inst = newApp()
		inst.scope, inst.storage = scope, storage
		inst.conv.started, inst.conv.title = true, "Wind field over the Aegean"
		return
	}

	// The cap the window host injects for a manifest with PersistedKeys.
	injected := append(slices.Clone(manifest.Caps), app.SubjectFilter{Pattern: persist.SubjectPrefix + alias + ".>", Direction: app.CapDirectionPub})
	scope := lifetime.New(nil)
	inst := titled(scope, clientOver(injected))
	inst.storeTitle()
	require.Eventually(t, func() bool {
		v, found, _ := backend.Get(persist.StorageRef{Alias: alias}, titleStorePrefix+inst.conv.id)
		return found && string(v) == inst.conv.title
	}, 5*time.Second, 5*time.Millisecond)
	scope.Retire()
	<-scope.Done()

	// The manifest's own caps alone: the write is refused at the bus.
	refused := titled(lifetime.New(nil), clientOver(manifest.Caps))
	refused.storeTitle()
	refused.scope.Retire()
	<-refused.scope.Done()
	_, found, _ := backend.Get(persist.StorageRef{Alias: alias}, titleStorePrefix+refused.conv.id)
	assert.False(t, found, "without the injected cap nothing reaches the store")
}
