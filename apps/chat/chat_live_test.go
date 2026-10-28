package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// The pacer reveals a backlog over about liveLag, frame by frame and at
// an even rate, rather than at once; it never runs past the text, cuts at
// rune starts, and a new step starts it over.
func TestThePacerRevealsABacklogSteadily(t *testing.T) {
	var p pacer
	at := time.Unix(0, 0)
	text := strings.Repeat("é", 100)
	assert.Empty(t, p.advance(1, text, at), "the first frame shows nothing yet")
	var shown []int
	for i := 1; i <= 30; i++ {
		got := p.advance(1, text, at.Add(time.Duration(i)*16*time.Millisecond))
		assert.True(t, strings.HasPrefix(text, got))
		assert.True(t, len(got) == 0 || len(got)%2 == 0, "cut at a rune start")
		shown = append(shown, len(got))
	}
	assert.Greater(t, shown[0], 0)
	assert.Less(t, shown[0], len(text)/4, "not all at once")
	for i := 1; i < len(shown); i++ {
		assert.GreaterOrEqual(t, shown[i], shown[i-1])
	}
	assert.InDelta(t, shown[0], shown[4]-shown[3], 2, "an even rate")
	assert.Equal(t, len(text), shown[20], "caught up after about liveLag")

	got := p.advance(2, "new", at.Add(time.Second))
	assert.Empty(t, got, "another step starts over")
}
