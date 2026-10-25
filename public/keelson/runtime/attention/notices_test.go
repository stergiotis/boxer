package attention

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

func TestOnlyAHighlightIsKept(t *testing.T) {
	levels := &Levels{}
	n := NewNotices(levels)
	now := time.Now()
	assert.Equal(t, LevelMessage, n.Notify("chat", 1, Notice{Conversation: "c1", Kind: "turn"}, now), "a turn's end is a message by default")
	assert.Empty(t, n.List())

	require.NoError(t, levels.Set(AuthorApp, "chat", "c1", Key{Kind: "turn"}, LevelHighlight), "watch c1")
	assert.Equal(t, LevelHighlight, n.Notify("chat", 1, Notice{Conversation: "c1", Kind: "turn", Text: "the turn ended"}, now))
	assert.Equal(t, LevelHighlight, n.Notify("chat", 1, Notice{Conversation: "c1", Kind: "turn", Text: "the turn failed"}, now.Add(time.Second)))
	got := n.List()
	require.Len(t, got, 1, "one per conversation")
	assert.Equal(t, "the turn failed", got[0].Text)
	assert.Equal(t, uint64(1), got[0].Instance)

	assert.Equal(t, LevelUnspecified, n.Notify("chat", 1, Notice{Conversation: "c1", Kind: KindQuestion}, now), "a waiting kind goes to the queue")
	assert.Equal(t, LevelUnspecified, n.Notify("chat", 1, Notice{Kind: "turn"}, now), "a notice belongs to a conversation")

	n.Seen("c1")
	assert.Empty(t, n.List())
}

func TestNoticesAreBounded(t *testing.T) {
	levels := &Levels{}
	require.NoError(t, levels.Set(AuthorPerson, "", "", Key{Kind: "turn"}, LevelHighlight))
	n := NewNotices(levels)
	for i := range MaxNotices + 3 {
		n.Notify("chat", 1, Notice{Conversation: "c" + itoa(uint64(i)), Kind: "turn"}, time.Now())
	}
	got := n.List()
	require.Len(t, got, MaxNotices)
	assert.Equal(t, "c"+itoa(MaxNotices+2), got[0].Conversation, "newest first; the oldest went")
}

func TestNoticesOverTheBus(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	svc := &Service{Questions: NewQuestions(nil), Levels: &Levels{}}
	svc.Notices = NewNotices(svc.Levels)
	bs, err := NewBusService(bus, svc, zerolog.Nop())
	require.NoError(t, err)
	defer bs.Close()
	cli := bus.NewClient("chat", ClientCaps("test"))

	require.NoError(t, SetLevel(cli, "c1", Key{Kind: "turn"}, LevelHighlight))
	l, err := Notify(cli, Notice{Conversation: "c1", Title: "Wind field", Kind: "turn", Text: "the turn ended"})
	require.NoError(t, err)
	assert.Equal(t, LevelHighlight, l)
	got, err := FetchNotices(cli)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Wind field", got[0].Title)
	assert.Equal(t, "chat", string(got[0].App), "the sender is the bus's, not the payload's")

	require.NoError(t, Seen(cli, "c1"))
	got, err = FetchNotices(cli)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// A job's terminal state is a notice keyed by its owner and id (ADR-0323
// §SD6, §SD7): data or message by default, a highlight once the person or
// the owning app's key raises it; the latest per job is kept, and it goes
// when seen.
func TestAJobsEndIsKeptBySource(t *testing.T) {
	levels := &Levels{}
	n := NewNotices(levels)
	now := time.Now()
	assert.Equal(t, LevelData, n.NotifyJob("play", "j1", KindJobFailed, "attempt failed", now), "a retried attempt is data")
	assert.Equal(t, LevelMessage, n.NotifyJob("play", "j1", KindJobDiscarded, "attempts exhausted", now), "a discarded job is a message")
	assert.Equal(t, LevelMessage, n.NotifyJob("play", "j1", KindJobAbandoned, "no life", now))
	assert.Empty(t, n.List())
	assert.Equal(t, LevelUnspecified, n.NotifyJob("play", "", KindJobDiscarded, "", now), "a job notice names its job")
	assert.Equal(t, LevelUnspecified, n.NotifyJob("play", "j1", KindQuestion, "", now), "a waiting kind is no notice")

	require.NoError(t, levels.Set(AuthorPerson, "", "", Key{Kind: KindJobDiscarded, App: "play"}, LevelHighlight), "the owning app's key")
	assert.Equal(t, LevelHighlight, n.NotifyJob("play", "j1", KindJobDiscarded, "attempts exhausted", now))
	assert.Equal(t, LevelHighlight, n.NotifyJob("play", "j1", KindJobDiscarded, "discarded again", now.Add(time.Second)))
	assert.Equal(t, LevelMessage, n.NotifyJob("chat", "j1", KindJobDiscarded, "another owner", now), "the key narrows by the owner")
	got := n.List()
	require.Len(t, got, 1, "one per job")
	assert.Equal(t, "discarded again", got[0].Text)
	assert.Equal(t, "j1", got[0].Job)
	assert.Equal(t, "job j1", got[0].Title)
	assert.Empty(t, got[0].Conversation)

	n.Seen("")
	assert.Len(t, n.List(), 1, "a conversation seen drops no job's notice")
	n.SeenJob("chat", "j1")
	assert.Len(t, n.List(), 1, "another owner's job is not this one")
	n.SeenJob("play", "j1")
	assert.Empty(t, n.List())

	var kinds []string
	for _, r := range GridRows() {
		kinds = append(kinds, r.Key.Kind)
	}
	assert.Subset(t, kinds, []string{KindJobFailed, KindJobDiscarded, KindJobAbandoned}, "the person's grid offers the job kinds")
}
