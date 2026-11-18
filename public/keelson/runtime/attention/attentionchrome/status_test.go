package attentionchrome

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/attention"
)

// Entries alike but for their time and key make one line, counted with its
// time span; the lines keep the card's order, level first, then time.
func TestTheAwayCardFoldsAlikeEntriesIntoOneLine(t *testing.T) {
	t0 := time.Date(2026, 11, 18, 15, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	slow := attention.AwayEntry{What: attention.AwayHighlight, Kind: "slow-window", Instance: 7, Summary: "a window is slow", Level: attention.LevelHighlight}
	ask := attention.AwayEntry{What: attention.AwayArrived, Kind: attention.KindGrantRequest, Conversation: "c1", Summary: "read a table", Level: attention.LevelYourTurn}
	var entries []attention.AwayEntry
	for m := range 20 {
		e := slow
		e.At = at(m)
		entries = append(entries, e)
	}
	for i, m := range []int{12, 3} {
		e := ask
		e.At, e.Key = at(m), []string{"k-late", "k-early"}[i]
		entries = append(entries, e)
	}
	other := ask
	other.At, other.Key, other.Summary = at(5), "k-other", "write a file"
	entries = append(entries, other)

	lines := awayLines(entries)
	require.Len(t, lines, 3)

	assert.Equal(t, "read a table", lines[0].Summary, "the higher level first, then the earlier")
	assert.Equal(t, 2, lines[0].Count)
	assert.Equal(t, at(3), lines[0].At)
	assert.Equal(t, at(12), lines[0].Last)
	assert.Equal(t, []string{"k-late", "k-early"}, lines[0].Keys, "every key in the log's order, for Open")

	assert.Equal(t, "write a file", lines[1].Summary, "a different summary is its own line")
	assert.Equal(t, 1, lines[1].Count)

	assert.Equal(t, 20, lines[2].Count)
	assert.Equal(t, at(0), lines[2].At)
	assert.Equal(t, at(19), lines[2].Last)
	assert.Empty(t, lines[2].Keys)
}
