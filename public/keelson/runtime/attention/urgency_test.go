package attention

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// A window asks; only the person's decision in the host's card changes the
// levels, as the person's, and an urgent conversation's waiting items are
// critical (ADR-0323 §SD4, update of 2026-10-12).
func TestUrgencyIsTheHostsDecision(t *testing.T) {
	svc := NewService()
	var authors []AuthorE
	svc.Levels.SetOnWaitingChange(func(ch Change) { authors = append(authors, ch.Author) })
	question := Event{Kind: KindQuestion, Conversation: "c1"}
	require.Equal(t, LevelYourTurn, svc.Levels.Classify(question))

	require.Empty(t, svc.Urgencies.Request("chat", 3, "c1", "Wind field", true, t0))
	var items []Item
	for it := range svc.Urgencies.WaitingItems() {
		items = append(items, it)
	}
	require.Len(t, items, 1)
	assert.Equal(t, KindUrgency, items[0].Kind)
	assert.Equal(t, uint64(3), items[0].Instance)
	assert.Contains(t, items[0].Summary, "Wind field")
	assert.Equal(t, LevelYourTurn, svc.Levels.Classify(question), "asking changes nothing")

	ok, err := svc.Urgencies.Decide(items[0].Key, false)
	require.True(t, ok)
	require.NoError(t, err)
	assert.Equal(t, LevelYourTurn, svc.Levels.Classify(question), "declined")

	require.Empty(t, svc.Urgencies.Request("chat", 3, "c1", "Wind field", true, t0))
	ok, err = svc.Urgencies.Decide(urgencyKeyPrefix+"c1", true)
	require.True(t, ok)
	require.NoError(t, err)
	assert.Equal(t, LevelCritical, svc.Levels.Classify(question))
	assert.Equal(t, LevelCritical, svc.Levels.Classify(Event{Kind: KindGrantRequest, Conversation: "c1"}))
	assert.Equal(t, LevelYourTurn, svc.Levels.Classify(Event{Kind: KindQuestion, Conversation: "c2"}), "one conversation only")
	assert.True(t, IsUrgent(svc.Levels.Snapshot("c1").Conversation))
	require.NotEmpty(t, authors)
	for _, a := range authors {
		assert.Equal(t, AuthorPerson, a, "on the trail as the person's")
	}

	require.Empty(t, svc.Urgencies.Request("chat", 3, "c1", "", false, t0))
	_, err = svc.Urgencies.Decide(urgencyKeyPrefix+"c1", true)
	require.NoError(t, err)
	assert.Equal(t, LevelYourTurn, svc.Levels.Classify(question), "stopped")
	ok, _ = svc.Urgencies.Decide(urgencyKeyPrefix+"c1", true)
	assert.False(t, ok, "decided once")
}

func TestUrgencyOverTheBus(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	svc := NewService()
	bs, err := NewBusService(bus, svc, zerolog.Nop())
	require.NoError(t, err)
	defer bs.Close()
	cli := bus.NewClient("chat", ClientCaps("test"))
	refused, err := RequestUrgent(cli, "c1", "Wind field", true)
	require.NoError(t, err)
	require.Empty(t, refused)
	refused, err = RequestUrgent(cli, "", "", true)
	require.NoError(t, err)
	assert.NotEmpty(t, refused)
	assert.Error(t, svc.Levels.Set(AuthorApp, "chat", "c1", Key{Kind: KindQuestion}, LevelCritical), "an app still cannot set it")
	n := 0
	for range svc.Urgencies.WaitingItems() {
		n++
	}
	assert.Equal(t, 1, n)
}
