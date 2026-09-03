package gloss

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The inline face is the pattern, and the verdict is a tone: neutral for a
// pattern Go's engine accepts, error plus ✗ for one it does not.
func TestRegexpFace(t *testing.T) {
	re := instFor(t, "rule@gloss/regexp")

	assert.Equal(t, Inline{Text: `^(\d{3})-(\d{4})$`}, re.Inline(txt(`^(\d{3})-(\d{4})$`)))
	assert.Equal(t, Inline{Text: `\d{3}-(\d{4} ✗`, Tone: ToneError}, re.Inline(txt(`\d{3}-(\d{4}`)))
	assert.Equal(t, Inline{Text: `a{2,1} ✗`, Tone: ToneError}, re.Inline(txt(`a{2,1}`)),
		"a repetition whose bounds are backwards compiles nowhere")
	assert.Equal(t, Inline{}, re.Inline(txt("")), "an empty cell has nothing to judge")
}

// The verdict is on the whole pattern, the display on its first line: an
// `(?x)`-style pattern laid out over lines is refused or accepted whole,
// and the cell still shows one line.
func TestRegexpFaceMultiLine(t *testing.T) {
	re := instFor(t, "rule@gloss/regexp")

	assert.Equal(t, Inline{Text: `(?s)^a$ ✗`, Tone: ToneError}, re.Inline(txt("(?s)^a$\n(")),
		"the broken half is out of sight, not out of the verdict")
	assert.Equal(t, Inline{Text: `(?s)^a$`}, re.Inline(txt("(?s)^a$\n(b)")))
}

// Past the compile bound the cell shows the pattern without a verdict
// rather than compiling a kilobyte of alternation on every frame.
func TestRegexpFaceOverBound(t *testing.T) {
	re := instFor(t, "rule@gloss/regexp")

	long := strings.Repeat("a|", regexpInlineMaxBytes) + "("
	face := re.Inline(txt(long))
	assert.Equal(t, ToneNeutral, face.Tone, "no verdict is claimed past the bound")
	assert.Len(t, face.Text, FirstLineMax)
}

// Bytes as well as text: a ClickHouse String arrives as an Arrow binary
// column unless the server was asked for `output_format_arrow_string_as_string`,
// and a pattern is no less a pattern for it. A number is refused with the
// reason the host shows beside the plain cell.
func TestRegexpAccepts(t *testing.T) {
	re := instFor(t, "rule@gloss/regexp")

	assert.Equal(t, Inline{Text: `^ab+$`}, re.Inline(TextCell{S: `^ab+$`, K: ValueKindBytes}))

	kinds, all := AcceptedKinds(re)
	assert.Equal(t, []ValueKindE{ValueKindText, ValueKindBytes}, kinds)
	assert.False(t, all)

	ok, reason := re.Accepts(ValueKindNumeric)
	assert.False(t, ok)
	assert.Contains(t, reason, MediaTypeRegexp)
	assert.Contains(t, reason, "numeric")
}

// It takes no parameters and brings no affinity: nothing in a spec line
// says a text column holds a pattern, so a column reaches it by alias, by
// `gloss(…)` or by rule.
func TestRegexpDeclaration(t *testing.T) {
	assert.Nil(t, instFor(t, "rule@gloss/regexp").Gloss().Affinities())

	d, declared := Default().ParseColumn("rule@gloss/regexp;flags=i")
	require.True(t, declared)
	assert.Contains(t, d.Reason, "takes no parameters")
	assert.Nil(t, d.Instance)
}
