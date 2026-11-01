package appops

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCutTextCutsAtNRunes(t *testing.T) {
	assert.Equal(t, "abc", CutText("abc", 3))
	assert.Equal(t, "ab…", CutText("abc", 2))
	assert.Equal(t, "", CutText("", 0))
	assert.Equal(t, "…", CutText("a", 0))
	assert.Equal(t, strings.Repeat("x", 5)+"…", CutText(strings.Repeat("x", 9), 5))
	// Multibyte: counted in runes, never split mid-sequence.
	assert.Equal(t, "äöü", CutText("äöü", 3), "six bytes, three runes")
	assert.Equal(t, "äö…", CutText("äöüß", 2))
	assert.Equal(t, "日本…", CutText("日本語", 2))
}
