package chat

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

func TestTheMarkShowsWhatLandedWhileAway(t *testing.T) {
	inst := newApp()
	inst.conv.entries = []entry{{speaker: speakerUser}, {speaker: speakerModel}}
	inst.focused = true
	inst.syncLooked()
	assert.Equal(t, -1, inst.looked.mark, "nothing new while looking")

	inst.focused = false
	inst.syncLooked()
	inst.conv.entries = append(inst.conv.entries, entry{speaker: speakerUser}, entry{speaker: speakerTool}, entry{speaker: speakerModel})
	inst.syncLooked()
	assert.Equal(t, 3, inst.looked.mark, "marked while away too, for a window seen beside another; past the person's own message")

	inst.focused = true
	inst.syncLooked()
	assert.Equal(t, 3, inst.looked.mark, "kept while they read")
	inst.conv.entries = append(inst.conv.entries, entry{speaker: speakerUser})
	inst.syncLooked()
	assert.Equal(t, 3, inst.looked.mark, "what lands while they look moves nothing")

	inst.focused = false
	inst.syncLooked()
	assert.Equal(t, -1, inst.looked.mark, "gone once they leave")

	inst.conv.entries = inst.conv.entries[:1]
	inst.syncLooked()
	assert.Equal(t, 1, inst.looked.to, "an edit shrinks what was seen")
}

func TestAWatchedTurnEndingUnseenReachesTheHost(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	svc := attention.NewService()
	bs, err := attention.NewBusService(bus, svc, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(bs.Close)
	inst := newApp()
	inst.bus = bus.NewClient(ManifestId, manifest.Caps)
	inst.conv.title = "Wind field"

	inst.turnEnded("the turn ended")
	assert.Empty(t, svc.Notices.List(), "a turn's end is a message; the window has focus anyway")

	inst.focused = false
	inst.turnEnded("the turn ended")
	assert.Equal(t, attention.LevelMessage, inst.looked.level, "counted unseen")
	assert.Empty(t, svc.Notices.List(), "unwatched: not the host's to show")

	inst.levels.fetched, inst.levels.conv = true, inst.conv.id
	inst.setWatch(true)
	assert.True(t, inst.watching(), "the window's copy changes at once")
	require.Eventually(t, func() bool {
		return svc.Levels.Classify(attention.Event{Kind: "turn", Conversation: inst.conv.id}) == attention.LevelHighlight
	}, time.Second, 5*time.Millisecond, "the host takes the watch")

	inst.turnEnded("the turn ended")
	require.Eventually(t, func() bool { return len(svc.Notices.List()) == 1 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, "Wind field", svc.Notices.List()[0].Title)

	inst.focused = true
	inst.syncLooked()
	require.Eventually(t, func() bool { return len(svc.Notices.List()) == 0 }, time.Second, 5*time.Millisecond, "looking clears it")

	inst.setWatch(false)
	assert.False(t, inst.watching())
	require.Eventually(t, func() bool {
		return svc.Levels.Classify(attention.Event{Kind: "turn", Conversation: inst.conv.id}) == attention.LevelMessage
	}, time.Second, 5*time.Millisecond, "unwatched")
}

func TestTheRosterShowsAHighlight(t *testing.T) {
	inst := newApp()
	at := time.Now().Add(-3 * time.Minute)
	runs := []agentrun.Run{{Conversation: "away", Coordinator: "9", State: agentrun.StateOpen, Created: at}}
	extra := rosterExtra{notices: []attention.Notice{{Conversation: "away", Text: "the turn ended", Level: attention.LevelHighlight, Created: at}}}
	_, resumable, _ := buildRoster(runs, extra, "chat", "1")
	require.Len(t, resumable, 1)
	line, level := inst.rosterStatus(&resumable[0])
	assert.Equal(t, attention.LevelHighlight, level)
	assert.Equal(t, "the turn ended · 3 min ago", line)
}
