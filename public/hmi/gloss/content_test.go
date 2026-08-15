package gloss

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirstLine(t *testing.T) {
	assert.Equal(t, "first", FirstLine("first\nsecond\nthird"))
	assert.Equal(t, "no newline", FirstLine("no newline"))
	long := strings.Repeat("x", FirstLineMax+10)
	assert.Len(t, FirstLine(long), FirstLineMax)
	assert.True(t, utf8.ValidString(FirstLine("\xff\xfe")), "invalid UTF-8 is repaired, never shipped")
}

// The content family's inline faces: the first line for text, type and size
// for images — no decode, no parse, cheap enough for a table cell.
func TestContentInlineFaces(t *testing.T) {
	c := Default()
	inst := func(name string) InstanceI {
		d, declared := c.ParseColumn(name)
		require.True(t, declared)
		require.Empty(t, d.Reason)
		return d.Instance
	}
	md := inst("notes@text/markdown")
	assert.Equal(t, Inline{Text: "# Title"}, md.Inline(TextCell{S: "# Title\n\nbody", K: ValueKindText}))

	png := inst("shot@image/png")
	face := png.Inline(TextCell{S: strings.Repeat("\x00", 359), K: ValueKindBytes})
	assert.Equal(t, "[image/png · 359 B]", face.Text)
	assert.Equal(t, ToneNeutral, face.Tone)

	ok, reason := md.Accepts(ValueKindText)
	assert.True(t, ok)
	assert.Empty(t, reason)
	ok, reason = md.Accepts(ValueKindBytes)
	assert.True(t, ok, "ClickHouse String arrives as Arrow String or Binary; both are text to a content face")
	ok, reason = md.Accepts(ValueKindNumeric)
	assert.False(t, ok)
	assert.Contains(t, reason, "text/markdown expects text or bytes, got numeric")

	raw := inst("x@gloss/raw")
	assert.Equal(t, Inline{Text: "42"}, raw.Inline(TextCell{S: "42", K: ValueKindNumeric}))
	ok, _ = raw.Accepts(ValueKindOther)
	assert.True(t, ok, "raw accepts anything")
}
