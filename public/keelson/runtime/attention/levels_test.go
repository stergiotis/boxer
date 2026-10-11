package attention

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func TestDefaultLevels(t *testing.T) {
	cases := []struct {
		ev   Event
		want LevelE
	}{
		{Event{Kind: "user"}, LevelData},
		{Event{Kind: "reply"}, LevelData},
		{Event{Kind: "intent", Effect: app.OperationEffectView}, LevelData},
		{Event{Kind: "intent", Effect: app.OperationEffectRun}, LevelMessage},
		{Event{Kind: "outcome"}, LevelData},
		{Event{Kind: "outcome", Refused: true}, LevelMessage},
		{Event{Kind: "turn"}, LevelMessage},
		{Event{Kind: "asked"}, LevelYourTurn},
		{Event{Kind: "asked", Effect: app.OperationEffectConsequential}, LevelCritical},
		{Event{Kind: KindGrantRequest}, LevelYourTurn},
		{Event{Kind: KindQuestion}, LevelYourTurn},
		{Event{Kind: KindConfirmation}, LevelCritical},
		{Event{Kind: KindModeratorQuestion}, LevelCritical},
		{Event{Kind: "no-such-kind"}, LevelData},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, DefaultLevel(c.ev), c.ev.Kind)
	}
}

// Every journal kind and host kind lands on its scale by default: no
// default crosses the floor (ADR-0323 §SD2, §SD6).
func TestDefaultsRespectScales(t *testing.T) {
	kinds := append([]string{}, hostKinds...)
	for _, k := range agentrun.AllKinds {
		kinds = append(kinds, k.String())
	}
	for _, k := range kinds {
		scale, known := ScaleOf(k)
		require.True(t, known, k)
		for _, eff := range []app.OperationEffectE{0, app.OperationEffectNone, app.OperationEffectRun, app.OperationEffectConsequential} {
			assert.Equal(t, scale, DefaultLevel(Event{Kind: k, Effect: eff}).Scale(), k)
		}
	}
}

func TestFloorRefusesCrossing(t *testing.T) {
	var l Levels
	assert.Error(t, l.Set(AuthorPerson, "", "", Key{Kind: "asked"}, LevelData))
	assert.Error(t, l.Set(AuthorPerson, "", "", Key{Kind: "turn"}, LevelYourTurn))
	assert.Error(t, l.Set(AuthorPerson, "", "", Key{Kind: "nope"}, LevelData))
	assert.NoError(t, l.Set(AuthorPerson, "", "", Key{Kind: "asked"}, LevelCritical))
	assert.NoError(t, l.Set(AuthorPerson, "", "", Key{Kind: "turn"}, LevelHighlight))
}

func TestAppMayNotSetWaiting(t *testing.T) {
	var l Levels
	assert.Error(t, l.Set(AuthorApp, "chat", "c1", Key{Kind: KindQuestion}, LevelCritical))
	assert.Error(t, l.Clear(AuthorApp, "chat", "c1", Key{Kind: KindQuestion}))
	assert.Error(t, l.Set(AuthorApp, "chat", "", Key{Kind: "turn"}, LevelHighlight))
	assert.NoError(t, l.Set(AuthorApp, "chat", "c1", Key{Kind: "turn"}, LevelHighlight))
}

func TestMostSpecificWins(t *testing.T) {
	var l Levels
	require.NoError(t, l.Set(AuthorPerson, "", "", Key{Kind: "intent"}, LevelHighlight))
	require.NoError(t, l.Set(AuthorPerson, "", "", Key{Kind: "intent", App: "mdedit"}, LevelData))
	require.NoError(t, l.Set(AuthorApp, "chat", "c1", Key{Kind: "intent", Effect: app.OperationEffectDocument}, LevelMessage))
	assert.Equal(t, LevelHighlight, l.Classify(Event{Kind: "intent", App: "play"}))
	assert.Equal(t, LevelData, l.Classify(Event{Kind: "intent", App: "mdedit"}))
	assert.Equal(t, LevelMessage, l.Classify(Event{Kind: "intent", App: "mdedit", Effect: app.OperationEffectDocument, Conversation: "c1"}))
	assert.Equal(t, LevelData, l.Classify(Event{Kind: "intent", App: "mdedit", Effect: app.OperationEffectDocument, Conversation: "c2"}))
	require.NoError(t, l.Clear(AuthorPerson, "", "", Key{Kind: "intent", App: "mdedit"}))
	assert.Equal(t, LevelHighlight, l.Classify(Event{Kind: "intent", App: "mdedit"}))
}

func TestWaitingChangeIsReported(t *testing.T) {
	var l Levels
	var got []Change
	l.SetOnWaitingChange(func(c Change) { got = append(got, c) })
	require.NoError(t, l.Set(AuthorPerson, "", "c1", Key{Kind: KindQuestion}, LevelCritical))
	require.NoError(t, l.Set(AuthorPerson, "", "", Key{Kind: "turn"}, LevelHighlight))
	require.Len(t, got, 1)
	assert.Equal(t, "c1", got[0].Conversation)
	assert.Equal(t, LevelCritical, got[0].Level)
	assert.Equal(t, uint64(2), l.Version())
}
