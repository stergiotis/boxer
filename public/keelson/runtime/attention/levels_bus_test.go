package attention

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

func TestLevelsOverTheBus(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	levels := &Levels{}
	require.NoError(t, levels.Set(AuthorPerson, "", "", Key{Kind: "intent", Effect: app.OperationEffectRun}, LevelHighlight))
	svc, err := NewBusService(bus, &Service{Questions: NewQuestions(nil), Levels: levels}, zerolog.Nop())
	require.NoError(t, err)
	defer svc.Close()
	cli := bus.NewClient("chat", ClientCaps("test"))

	var seen atomic.Uint64
	unsub, err := SubscribeChanges(cli, func(v uint64) { seen.Store(v) })
	require.NoError(t, err)
	defer unsub()

	s, err := FetchLevels(cli, "c1")
	require.NoError(t, err)
	assert.Equal(t, LevelHighlight, s.Classify(Event{Kind: "intent", Effect: app.OperationEffectRun}), "the person's table travels")
	assert.Equal(t, LevelData, s.Classify(Event{Kind: "reply"}), "defaults apply where nothing overrides")

	require.NoError(t, SetLevel(cli, "c1", Key{Kind: "turn"}, LevelHighlight), "an app sets its conversation's attention scale")
	assert.Error(t, SetLevel(cli, "c1", Key{Kind: KindQuestion}, LevelCritical), "the waiting scale is the person's")
	assert.Error(t, SetLevel(cli, "", Key{Kind: "turn"}, LevelHighlight), "an app sets levels for a conversation only")
	require.Eventually(t, func() bool { return seen.Load() == levels.Version() }, time.Second, 5*time.Millisecond, "the change is announced")

	s, err = FetchLevels(cli, "c1")
	require.NoError(t, err)
	assert.Equal(t, LevelHighlight, s.Classify(Event{Kind: "turn"}))
	other, err := FetchLevels(cli, "c2")
	require.NoError(t, err)
	assert.Equal(t, LevelMessage, other.Classify(Event{Kind: "turn"}), "another conversation does not see it")

	require.NoError(t, SetLevel(cli, "c1", Key{Kind: "turn"}, LevelUnspecified))
	s, err = FetchLevels(cli, "c1")
	require.NoError(t, err)
	assert.Equal(t, LevelMessage, s.Classify(Event{Kind: "turn"}), "cleared")
}

func TestTheZeroSnapshotIsTheDefaults(t *testing.T) {
	var s Snapshot
	assert.Equal(t, LevelYourTurn, s.Classify(Event{Kind: "asked"}))
	assert.Equal(t, LevelMessage, s.Classify(Event{Kind: "outcome", Refused: true}))
}
