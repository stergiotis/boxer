package pathfilter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestZeroValueAndEmptyMatchNothing(t *testing.T) {
	var m *Matcher
	assert.True(t, m.IsEmpty())
	assert.False(t, m.Match("anything.md"))

	m = NewMatcher(nil)
	assert.True(t, m.IsEmpty())
	assert.False(t, m.Match("anything.md"))

	m = NewMatcher([]string{"", "   "})
	assert.True(t, m.IsEmpty(), "blank patterns are dropped, not kept as match-all")
}

func TestBareNameMatchesBasenameAnywhere(t *testing.T) {
	m := NewMatcher([]string{"CLAUDE.md"})
	assert.True(t, m.Match("CLAUDE.md"))
	assert.True(t, m.Match("sub/dir/CLAUDE.md"))
	assert.False(t, m.Match("CLAUDE.md.bak"))
	assert.False(t, m.Match("AGENTS.md"))
}

func TestBareGlobMatchesBasename(t *testing.T) {
	m := NewMatcher([]string{"*.out.md", "*.gen.md"})
	assert.True(t, m.Match("a/b/table.out.md"))
	assert.True(t, m.Match("x.gen.md"))
	assert.False(t, m.Match("a/b/table.md"))
}

// The case the shell versions approximated with alternation: an attic anywhere.
func TestTrailingSlashMatchesDirectoryAtAnyDepth(t *testing.T) {
	m := NewMatcher([]string{"attic/"})
	assert.True(t, m.Match("attic/old.md"))
	assert.True(t, m.Match("public/x/attic/old.md"))
	assert.True(t, m.Match("attic/deep/deeper/old.md"))
	assert.False(t, m.Match("attics/old.md"))
	assert.False(t, m.Match("my-attic.md"))
	assert.False(t, m.Match("attic"), "the directory itself is not a file in it")
}

func TestMultiSegmentDirectoryPattern(t *testing.T) {
	m := NewMatcher([]string{"doc/changelog/summaries/"})
	assert.True(t, m.Match("doc/changelog/summaries/2026-01.md"))
	assert.False(t, m.Match("doc/changelog/index.md"))
}

func TestPatternWithSeparatorMatchesWholePath(t *testing.T) {
	m := NewMatcher([]string{"doc/adr/*.md"})
	assert.True(t, m.Match("doc/adr/0001-x.md"))
	assert.False(t, m.Match("doc/adr/sub/0001-x.md"), "* does not cross a separator")
	assert.False(t, m.Match("other/doc/adr/0001-x.md"), "a path pattern is root-anchored")
}

func TestNormalisesLeadingDotSlashAndBackslashes(t *testing.T) {
	m := NewMatcher([]string{"attic/"})
	assert.True(t, m.Match("./attic/old.md"))
	assert.True(t, m.Match("attic\\old.md"))
}

func TestTrailingSlashSurvivesCleaning(t *testing.T) {
	m := NewMatcher([]string{"attic//"})
	assert.True(t, m.Match("public/attic/x.md"))
}
