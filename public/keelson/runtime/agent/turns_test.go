package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThePersonsChangePausesTheTaskUntilTurn(t *testing.T) {
	r := newRig(t, true)
	ctx := context.Background()
	g := r.grant(ModeAct)
	r.call(g, "q1", "get_text", "{}")
	r.host.person(7, func(d *doc) { d.text = "the person's" })
	r.host.frame(7)

	out := r.call(g, "w1", "set_text", `{"text":"agent"}`)
	require.Equal(t, "refused", out.Phase)
	assert.Contains(t, out.Reason, "paused")
	assert.Contains(t, out.Reason, "person changed text")
	assert.Equal(t, "completed", r.call(g, "q2", "get_text", "{}").Phase, "reading is not paused")

	changes, err := r.cli.Turn(ctx, g.Handle)
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, "person", changes[0].Writer)
	assert.Equal(t, []string{"text"}, changes[0].Resources)

	r.call(g, "q3", "get_text", "{}")
	require.Equal(t, "accepted", r.call(g, "w2", "set_text", `{"text":"agent"}`).Phase)
	r.host.frame(7)
	assert.Equal(t, "agent", r.host.docs[7].text)

	again, err := r.cli.Turn(ctx, g.Handle)
	require.NoError(t, err)
	assert.Empty(t, again, "the task's own change is not news to it")
}

func TestAChangeTheTaskDidNotReadDoesNotPause(t *testing.T) {
	r := newRig(t, true)
	g := r.grant(ModeAct)
	r.host.person(7, func(d *doc) { d.text = "the person's" })
	r.host.frame(7)
	r.call(g, "q", "get_text", "{}")
	assert.Equal(t, "accepted", r.call(g, "w", "set_text", `{"text":"agent"}`).Phase)
}

func TestEventsAnnounceChangesWithoutContent(t *testing.T) {
	r := newRig(t, true)
	g := r.grant(ModeAct)
	var mu sync.Mutex
	var got []Event
	unsub, err := r.cli.SubscribeEvents(g.Task, func(ev Event) {
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
	})
	require.NoError(t, err)
	defer unsub()
	r.call(g, "q", "get_text", "{}")
	r.host.person(7, func(d *doc) { d.text = "the person's" })
	r.host.frame(7)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) > 0
	}, 2*time.Second, 10*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "person", got[0].Writer)
	assert.True(t, got[0].Pauses)
	assert.Equal(t, []string{"text"}, got[0].Resources)
}
