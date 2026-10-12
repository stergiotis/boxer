package attention

import (
	"image/color"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAConversationKeepsItsColour(t *testing.T) {
	a := ConversationColor("chat-dm2s6rb8dhho-2")
	assert.Equal(t, a, ConversationColor("chat-dm2s6rb8dhho-2"))
	assert.Equal(t, uint8(255), a.A)
	assert.Equal(t, color.RGBA{}, ConversationColor(""), "none for no conversation")
	seen := make(map[color.RGBA]bool, 12)
	for _, c := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p"} {
		seen[ConversationColor(c)] = true
	}
	assert.GreaterOrEqual(t, len(seen), 6, "ids spread over the hues")
}

func TestHsl(t *testing.T) {
	assert.Equal(t, color.RGBA{R: 255, A: 255}, hsl(0, 1, 0.5))
	assert.Equal(t, color.RGBA{G: 255, A: 255}, hsl(120, 1, 0.5))
	assert.Equal(t, color.RGBA{B: 255, A: 255}, hsl(240, 1, 0.5))
}
