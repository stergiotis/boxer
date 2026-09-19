//go:build integration

package play

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/scene"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene/scenetest"
)

// A message's body block inside the Chat pane (ADR-0239) takes the height of
// its text, up to [chatBlockMaxHeight], and scrolls only past that.
//
// The regression this guards: the body renders in a vertical scroll area
// inside the transcript's own, and egui sizes a nested scroll area from the
// space left below the cursor in its parent — about none — falling back to
// its 64-point minimum. Every long message then showed three lines. The
// accessibility tree still lists the whole text, so the check is geometric:
// the distance from one message's text to the next is that text's height plus
// a constant chrome while the bubble shows it whole, and a constant
// regardless of the text when it is clipped.
//
// A scene of the computed kind (ADR-0248 §SD5): the oracle is arithmetic over
// node bounds, which no trace step expresses.
func TestSceneChatBodiesTakeTheirHeightUpToTheCeiling(t *testing.T) {
	// Four messages of growing length, then one far past the ceiling, then a
	// one-liner below it so the long one's displayed height can be measured.
	const sql = "SELECT toDateTime64('2026-09-19 10:00:00', 3, 'UTC') + toIntervalMinute(number) AS ts, " +
		"if(number % 2 = 0, 'assistant', 'user') AS sender, " +
		"multiIf(number < 4, concat('Grow ', toString(number), '. ', repeat('lorem ipsum dolor sit amet consectetur adipiscing ', 6 + number * 10)), " +
		"number = 4, concat('Huge. ', repeat('lorem ipsum dolor sit amet consectetur adipiscing ', 600)), " +
		"'Tail. a single line') AS `body@text/markdown` FROM numbers(6)"
	s := scenetest.Launch(t, scene.Spec{
		Launch:   "play",
		Size:     "1400x900",
		Requires: []string{scene.RequireClickHouse},
		Env: map[string]string{
			"BOXER_PLAY_WINDOW_SIZE": "1350x850",
			"BOXER_PLAY_AUTORUN":     "1",
			"BOXER_PLAY_SQL":         sql,
		},
	})
	scenetest.Run(t, s, `
{"do":"wait","name":"Run","role":"button"}
{"do":"read","valueContains":"6 rows","role":"label","pattern":"^(?P<rows>6) rows"}
{"do":"click","name":"Chat","role":"button"}
{"do":"sleep","settleMs":1500}`)

	// The session's own connection: a second one would start a second video
	// decoder on the carrier's stream for nothing.
	snap, err := s.Client.Tree(10 * time.Second)
	require.NoError(t, err)

	// The transcript's text labels, one per message, by its prefix. The
	// Detail pane repeats the selected row's body further right, so the
	// leftmost label of each prefix is the bubble's.
	type textBox struct{ top, h float32 }
	byPrefix := map[string]textBox{}
	leftmost := map[string]float32{}
	for _, n := range snap.Nodes {
		if n.Role != "label" {
			continue
		}
		prefix, _, found := strings.Cut(n.Value, ".")
		if !found || !slices.Contains([]string{"Grow 0", "Grow 1", "Grow 2", "Grow 3", "Huge", "Tail"}, prefix) {
			continue
		}
		if x, seen := leftmost[prefix]; seen && x <= n.X {
			continue
		}
		leftmost[prefix] = n.X
		byPrefix[prefix] = textBox{top: n.Y, h: n.H}
	}
	require.Len(t, byPrefix, 6, "every message's text is in the tree")

	// Whole bubbles: the gap to the next message is the text's own height
	// plus one constant chrome. A clipped bubble makes the gap constant
	// instead, so the chrome would shrink as the text grows.
	var chrome []float32
	for i := range 4 {
		cur := byPrefix["Grow "+strconv.Itoa(i)]
		next := byPrefix["Huge"]
		if i < 3 {
			next = byPrefix["Grow "+strconv.Itoa(i+1)]
		}
		require.Greater(t, cur.h, float32(3*cardBlockLineHeight), "message %d must be several lines long to test anything", i)
		chrome = append(chrome, next.top-cur.top-cur.h)
	}
	for i, c := range chrome {
		assert.InDelta(t, chrome[0], c, 2, "message %d is not shown whole: its bubble is %.0f points short of its text", i, chrome[0]-c)
	}

	// The ceiling: the huge text is thousands of points tall, and its bubble
	// shows the ceiling's worth of it.
	huge, tail := byPrefix["Huge"], byPrefix["Tail"]
	require.Greater(t, huge.h, float32(4*chatBlockMaxHeight), "the huge message must be far past the ceiling to test it")
	shown := tail.top - huge.top - chrome[0]
	assert.InDelta(t, chatBlockMaxHeight, shown, 12, "a long body scrolls at the ceiling rather than growing or collapsing")
}
