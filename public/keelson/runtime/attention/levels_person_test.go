package attention

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

type memStore map[string][]byte

func (inst memStore) Get(key string) ([]byte, bool, error) { v, ok := inst[key]; return v, ok, nil }
func (inst memStore) Set(key string, value []byte) error   { inst[key] = value; return nil }

func TestThePersonsTableIsKept(t *testing.T) {
	store := memStore{}
	levels := &Levels{}
	require.NoError(t, AttachPersonStore(levels, store, zerolog.Nop()))
	require.NoError(t, levels.Set(AuthorPerson, "", "", Key{Kind: "reply"}, LevelMessage))
	require.NoError(t, levels.Set(AuthorPerson, "", "", Key{Kind: KindQuestion}, LevelCritical))
	require.NoError(t, levels.Set(AuthorApp, "chat", "c1", Key{Kind: "turn"}, LevelHighlight), "a conversation's override")

	again := &Levels{}
	var trail []Change
	require.NoError(t, AttachPersonStore(again, store, zerolog.Nop()))
	again.SetOnWaitingChange(func(ch Change) { trail = append(trail, ch) })
	assert.Equal(t, LevelMessage, again.Classify(Event{Kind: "reply"}))
	assert.Equal(t, LevelCritical, again.Classify(Event{Kind: KindQuestion}))
	assert.Equal(t, LevelMessage, again.Classify(Event{Kind: "turn", Conversation: "c1"}), "a conversation's override is not kept")
	assert.Empty(t, trail, "loading is not the person's change")

	require.NoError(t, again.Clear(AuthorPerson, "", "", Key{Kind: "reply"}))
	third := &Levels{}
	require.NoError(t, AttachPersonStore(third, store, zerolog.Nop()))
	assert.Equal(t, LevelData, third.Classify(Event{Kind: "reply"}), "a cleared row is gone from what is kept")
}

func TestAKeptRowBelowTheFloorIsDropped(t *testing.T) {
	store := memStore{personStoreKey: []byte(`{"v":1,"rows":[{"kind":"question","level":1},{"kind":"reply","level":2},{"kind":"nonsense","level":2}]}`)}
	levels := &Levels{}
	require.NoError(t, AttachPersonStore(levels, store, zerolog.Nop()))
	assert.Equal(t, LevelYourTurn, levels.Classify(Event{Kind: KindQuestion}), "a waiting kind kept at data is refused")
	assert.Equal(t, LevelMessage, levels.Classify(Event{Kind: "reply"}))
}

func TestTheGridRowsCarryTheirDefaults(t *testing.T) {
	rows := GridRows()
	byKey := make(map[Key]GridRow, len(rows))
	for _, r := range rows {
		_, known := ScaleOf(r.Key.Kind)
		assert.True(t, known, r.Key.Kind)
		assert.Equal(t, r.Default.Scale(), func() ScaleE { s, _ := ScaleOf(r.Key.Kind); return s }(), "a row's default is on its scale: %v", r.Key)
		byKey[r.Key] = r
	}
	assert.Equal(t, LevelData, byKey[Key{Kind: "intent", Effect: app.OperationEffectView}].Default)
	assert.Equal(t, LevelMessage, byKey[Key{Kind: "intent", Effect: app.OperationEffectRun}].Default)
	assert.Equal(t, LevelCritical, byKey[Key{Kind: "asked", Effect: app.OperationEffectConsequential}].Default)
	assert.Equal(t, LevelCritical, byKey[Key{Kind: KindConfirmation}].Default)
}
