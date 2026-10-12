package attention

import (
	"hash/fnv"
	"image/color"
	"math"
)

// A conversation's colour (ADR-0324 background §3.12): one per conversation
// id, the same on every surface that shows the conversation — the chat's
// roster, the queue's rows and cards, the highlights — so the person tells
// streams apart at a glance. It is never the only cue: a surface that shows
// the colour shows the conversation's name beside it.

// conversationHues are twelve hues spread around the wheel; an id hashes to
// one.
var conversationHues = [...]float64{200, 140, 280, 30, 175, 320, 95, 245, 55, 340, 120, 220}

// ConversationColor is conversation's colour; the zero colour for none. Its
// lightness sits mid-scale, to read on a dark panel and a light one.
func ConversationColor(conversation string) (c color.RGBA) {
	if conversation == "" {
		return
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(conversation))
	return hsl(conversationHues[h.Sum32()%uint32(len(conversationHues))], 0.62, 0.56)
}

// hsl is an opaque colour from hue in degrees, saturation and lightness in
// [0,1].
func hsl(h, s, l float64) color.RGBA {
	c := (1 - math.Abs(2*l-1)) * s
	hp := math.Mod(h, 360) / 60
	x := c * (1 - math.Abs(math.Mod(hp, 2)-1))
	var r, g, b float64
	switch {
	case hp < 1:
		r, g = c, x
	case hp < 2:
		r, g = x, c
	case hp < 3:
		g, b = c, x
	case hp < 4:
		g, b = x, c
	case hp < 5:
		r, b = x, c
	default:
		r, b = c, x
	}
	m := l - c/2
	u8 := func(v float64) uint8 { return uint8(math.Round((v + m) * 255)) }
	return color.RGBA{R: u8(r), G: u8(g), B: u8(b), A: 255}
}
