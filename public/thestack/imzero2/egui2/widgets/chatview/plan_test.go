package chatview

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A minute in ms, and a day.
const (
	minute int64 = 60 * 1000
	day    int64 = 24 * 60 * minute
)

// twoParty is a dialogue over two days: three messages of A in one cluster,
// one of B, one of A the next day, and a system line between.
func twoParty() *Model {
	base := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC).UnixMilli()
	m := &Model{
		TimeMS:  []int64{base, base + minute, base + 3*minute, base + 4*minute, base + 20*minute, base + day},
		Sender:  []int32{0, 0, 0, 1, -1, 0},
		Body:    []string{"hi", "are you there?", "ping", "yes", "B joined", "morning"},
		ReplyTo: []int32{-1, -1, -1, 1, -1, -1},
		Flags:   []FlagsE{0, 0, 0, 0, FlagSystem, 0},
		Status:  []StatusE{StatusRead, StatusRead, StatusDelivered, 0, 0, StatusSent},
		Participants: []Participant{
			{Name: "Ada Lovelace"}, {Name: "Bob"},
		},
	}
	return m
}

func TestPlanClustersSeparatorsAndSystemLines(t *testing.T) {
	m := twoParty()
	require.NoError(t, m.Validate())
	rows := plan(m, time.UTC, 0)
	kinds := make([]rowKindE, 0, len(rows))
	for _, r := range rows {
		kinds = append(kinds, r.kind)
	}
	assert.Equal(t, []rowKindE{rowDay, rowMessage, rowMessage, rowMessage, rowMessage, rowSystem, rowDay, rowMessage}, kinds)

	// A's three messages are one cluster: name on the first, tail on the last.
	assert.True(t, rows[1].first)
	assert.False(t, rows[1].last)
	assert.False(t, rows[2].first)
	assert.False(t, rows[2].last)
	assert.False(t, rows[3].first)
	assert.True(t, rows[3].last)
	// B's single message is a cluster of one.
	assert.True(t, rows[4].first)
	assert.True(t, rows[4].last)
	// Day ordinals count from the first drawn day.
	assert.Equal(t, int32(0), rows[0].msg)
	assert.Equal(t, int32(1), rows[6].msg)
}

func TestPlanWindowStartsMidTranscript(t *testing.T) {
	m := twoParty()
	rows := plan(m, time.UTC, 2)
	// The window opens on a day separator even mid-day, and the first drawn
	// message begins a cluster — its earlier siblings are not drawn.
	require.Len(t, rows, 6)
	assert.Equal(t, rowDay, rows[0].kind)
	assert.Equal(t, int32(2), rows[1].msg)
	assert.True(t, rows[1].first)
	assert.True(t, rows[1].last)
}

func TestPlanGapBreaksACluster(t *testing.T) {
	base := int64(1_700_000_000_000)
	m := &Model{
		TimeMS:       []int64{base, base + clusterGapMS, base + 2*clusterGapMS + 1},
		Sender:       []int32{0, 0, 0},
		Body:         []string{"a", "b", "c"},
		ReplyTo:      []int32{-1, -1, -1},
		Flags:        []FlagsE{0, 0, 0},
		Status:       []StatusE{0, 0, 0},
		Participants: []Participant{{Name: "A"}},
	}
	rows := plan(m, time.UTC, 0)
	require.Len(t, rows, 4)
	assert.False(t, rows[2].first, "exactly the gap still continues")
	assert.True(t, rows[3].first, "one past the gap starts anew")
}

func TestChooseLayout(t *testing.T) {
	m := twoParty()
	assert.Equal(t, LayoutDialogue, chooseLayout(m, 0, LayoutAuto))
	assert.Equal(t, LayoutDialogue, chooseLayout(m, 1, LayoutAuto))
	assert.Equal(t, LayoutGroup, chooseLayout(m, -1, LayoutAuto), "no viewer: a group of two")
	assert.Equal(t, LayoutGroup, chooseLayout(m, 0, LayoutGroup), "an explicit layout stands")

	// A third speaker makes a group whoever the viewer is.
	m.Participants = append(m.Participants, Participant{Name: "Cy"})
	m.TimeMS = append(m.TimeMS, m.TimeMS[len(m.TimeMS)-1]+minute)
	m.Sender = append(m.Sender, 2)
	m.Body = append(m.Body, "hello all")
	m.ReplyTo = append(m.ReplyTo, -1)
	m.Flags = append(m.Flags, 0)
	m.Status = append(m.Status, 0)
	require.NoError(t, m.Validate())
	assert.Equal(t, LayoutGroup, chooseLayout(m, 0, LayoutAuto))

	// A viewer who never speaks is a reader of a two-party chat, not a party.
	m2 := twoParty()
	m2.Participants = append(m2.Participants, Participant{Name: "Reader"})
	assert.Equal(t, LayoutGroup, chooseLayout(m2, 2, LayoutAuto))
}

func TestValidateRejectsMismatches(t *testing.T) {
	m := twoParty()
	require.NoError(t, m.Validate())

	bad := twoParty()
	bad.Sender = bad.Sender[:1]
	assert.Error(t, bad.Validate())

	bad = twoParty()
	bad.Sender[0] = 5
	assert.Error(t, bad.Validate())

	bad = twoParty()
	bad.ReplyTo[0] = 99
	assert.Error(t, bad.Validate())

	bad = twoParty()
	bad.TimeMS[1] = bad.TimeMS[0] - 1
	assert.Error(t, bad.Validate())

	bad = twoParty()
	bad.ReactionOff = []int32{0, 1}
	assert.Error(t, bad.Validate(), "offsets need n+1 entries")

	ok := twoParty()
	ok.ReactionOff = []int32{0, 0, 2, 2, 2, 2, 3}
	ok.ReactionKey = []string{"👍", "❤️", "👍"}
	ok.ReactionCount = []int32{2, 1, 1}
	ok.ReactionWho = []string{"Bob, Ada", "Bob", ""}
	require.NoError(t, ok.Validate())
	keys, counts, _ := ok.Reactions(1)
	assert.Equal(t, []string{"👍", "❤️"}, keys)
	assert.Equal(t, []int32{2, 1}, counts)
	keys, _, _ = ok.Reactions(0)
	assert.Empty(t, keys)
}

func TestStateZeroValue(t *testing.T) {
	var st State
	assert.Equal(t, int32(-1), st.Selected())
	assert.True(t, st.Follow())
	assert.Equal(t, defaultWindow, st.Window())
	st.SetSelected(3)
	assert.Equal(t, int32(3), st.Selected())
	st.SetSelected(-1)
	assert.Equal(t, int32(-1), st.Selected())
	st.JumpTo(7)
	assert.False(t, st.Follow(), "a jump releases the tail")
	assert.Equal(t, int32(8), st.jump)
}

func TestInitialsAndFirstLine(t *testing.T) {
	assert.Equal(t, "AL", initials("Ada Lovelace"))
	assert.Equal(t, "B", initials("bob"))
	assert.Equal(t, "?", initials("  "))
	assert.Equal(t, "ÉM", initials("élodie martin"))
	assert.Equal(t, "first", firstLine("first\nsecond"))
	long := "0123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890"
	got := firstLine(long)
	assert.Equal(t, quoteMaxRunes+1, len([]rune(got)))
	assert.Equal(t, "…", string([]rune(got)[quoteMaxRunes:]))
}
