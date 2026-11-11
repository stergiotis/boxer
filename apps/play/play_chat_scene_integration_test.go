//go:build integration

package play

import (
	"slices"
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
{"do":"wait","value":"6 rows","role":"label","comment":"the run's announcement; the status line holds the same words and more, so a contains-match is ambiguous"}
{"do":"click","name":"Chat","role":"button"}
{"do":"sleep","settleMs":1500}`)

	// The transcript lays out only the messages near its view (chatview's
	// virtualization), and a message not laid out is not in the tree. The
	// pane opens at the tail, so the ceiling is measured there and the gaps
	// between whole messages are gathered while scrolling up.
	//
	// The session's own connection: a second one would start a second video
	// decoder on the carrier's stream for nothing.
	type textBox struct{ top, h float32 }
	transcript := func() (byPrefix map[string]textBox) {
		snap, err := s.Client.Tree(10 * time.Second)
		require.NoError(t, err)
		// One text label per message, by its prefix. The Detail pane repeats
		// the selected row's body further right, so only labels in the left
		// half are the transcript's.
		byPrefix = map[string]textBox{}
		for _, n := range snap.Nodes {
			if n.Role != "label" || n.X > 700 {
				continue
			}
			prefix, _, found := strings.Cut(n.Value, ".")
			if !found || !slices.Contains([]string{"Grow 0", "Grow 1", "Grow 2", "Grow 3", "Huge", "Tail"}, prefix) {
				continue
			}
			byPrefix[prefix] = textBox{top: n.Y, h: n.H}
		}
		return
	}
	// Whole bubbles: the gap to the next message is the text's own height
	// plus one constant chrome. A clipped bubble makes the gap constant
	// instead, so the chrome would shrink as the text grows. A gap is taken
	// from any view that lays out both of its messages, on the way from the
	// tail to the top.
	order := []string{"Grow 0", "Grow 1", "Grow 2", "Grow 3", "Huge"}
	gaps := map[int]float32{}
	note := func(view map[string]textBox) {
		for i := range 4 {
			cur, hasCur := view[order[i]]
			next, hasNext := view[order[i+1]]
			if hasCur && hasNext {
				gaps[i] = next.top - cur.top - cur.h
			}
		}
	}
	atTail := transcript()
	for _, p := range []string{"Grow 3", "Huge", "Tail"} {
		require.Contains(t, atTail, p, "at the tail, the last messages are laid out")
	}
	note(atTail)
	heights := map[string]float32{}
	for range 8 {
		scenetest.Run(t, s, `
{"do":"hover","x":400,"y":770,"comment":"near the pane's foot: the huge message has a scroll area of its own that would take the wheel"}
{"do":"scroll","x":0,"y":300}
{"do":"sleep","settleMs":600}`)
		view := transcript()
		note(view)
		for p, b := range view {
			heights[p] = b.h
		}
	}
	var chrome []float32
	for i := range 4 {
		require.Contains(t, gaps, i, "no view laid out both %s and %s", order[i], order[i+1])
		require.Greater(t, heights[order[i]], float32(3*cardBlockLineHeight), "message %d must be several lines long to test anything", i)
		chrome = append(chrome, gaps[i])
	}
	for i, c := range chrome {
		assert.InDelta(t, chrome[0], c, 2, "message %d is not shown whole: its bubble is %.0f points short of its text", i, chrome[0]-c)
	}

	// The ceiling: the huge text is thousands of points tall, and its bubble
	// shows the ceiling's worth of it.
	huge, tail := atTail["Huge"], atTail["Tail"]
	require.Greater(t, huge.h, float32(4*chatBlockMaxHeight), "the huge message must be far past the ceiling to test it")
	shown := tail.top - huge.top - chrome[0]
	assert.InDelta(t, chatBlockMaxHeight, shown, 12, "a long body scrolls at the ceiling rather than growing or collapsing")
}
