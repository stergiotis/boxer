package promptbook

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

const goodDoc = `---
title: Improve style
summary: Tighten the prose.
icon: "X"
scope: selection
temperature: 0.3
max-tokens: 2048
---

You are a copy editor.
`

func TestParseDocSource_ReadsTheWholeShape(t *testing.T) {
	def, err := ParseDocSource("b", "sub/improve-style.md", []byte(goodDoc))
	require.NoError(t, err)
	assert.Equal(t, "b", def.BookId)
	assert.Equal(t, "improve-style", def.Slug, "the filename base is the slug, directories stripped")
	assert.Equal(t, "Improve style", def.Title)
	assert.Equal(t, "Tighten the prose.", def.Summary)
	assert.Equal(t, "X", def.Icon)
	assert.Equal(t, ScopeSelection, def.Scope)
	require.NotNil(t, def.Temperature)
	assert.InDelta(t, 0.3, float64(*def.Temperature), 1e-6)
	assert.Equal(t, int32(2048), def.MaxTokens)
	assert.Equal(t, "You are a copy editor.", def.System, "the whole body after frontmatter, trimmed")
}

func TestParseDocSource_Errors(t *testing.T) {
	cases := []struct {
		name string
		path string
		src  string
	}{
		{"missing title", "a.md", "---\nsummary: s\n---\nbody\n"},
		{"missing summary", "a.md", "---\ntitle: t\n---\nbody\n"},
		{"unknown scope", "a.md", "---\ntitle: t\nsummary: s\nscope: paragraph\n---\nbody\n"},
		{"empty body", "a.md", "---\ntitle: t\nsummary: s\n---\n\n"},
		{"no frontmatter at all", "a.md", "just a prompt\n"},
		{"bad slug", "Not_A_Slug.md", "---\ntitle: t\nsummary: s\n---\nbody\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDocSource("b", tc.path, []byte(tc.src))
			assert.Error(t, err)
		})
	}
}

func TestParseDocSource_DefaultsAndCRLF(t *testing.T) {
	src := "---\r\ntitle: t\r\nsummary: s\r\n---\r\nbody line\r\n"
	def, err := ParseDocSource("b", "a.md", []byte(src))
	require.NoError(t, err)
	assert.Equal(t, ScopeSelection, def.Scope, "selection is the default scope")
	assert.Nil(t, def.Temperature)
	assert.Zero(t, def.MaxTokens)
	assert.Equal(t, "body line", def.System)
}

func TestParseBook_WalksOnlyMarkdown(t *testing.T) {
	fsys := fstest.MapFS{
		"one.md":    {Data: []byte("---\ntitle: One\nsummary: s\n---\nbody\n")},
		"two.md":    {Data: []byte("---\ntitle: Two\nsummary: s\n---\nbody\n")},
		"README":    {Data: []byte("not a prompt")},
		"broken.md": {Data: []byte("---\ntitle: t\n---\nbody\n")},
	}
	defs, errs := ParseBook("b", fsys)
	assert.Len(t, defs, 2, "the broken sibling costs itself, not the book")
	assert.Len(t, errs, 1)
}

// Purpose is the spelling keelson('llm_calls').purpose carries, so a call
// joins keelson('llm_prompts') on it.
func TestPurposeIsBookSlashSlug(t *testing.T) {
	assert.Equal(t, "b/p", PromptDef{BookId: "b", Slug: "p"}.Purpose())
}

// ---------------------------------------------------------------------------
// The registry
// ---------------------------------------------------------------------------

func TestRegister_RefusesDuplicatesAndEmpties(t *testing.T) {
	assert.Error(t, Register("", fstest.MapFS{}))
	assert.Error(t, Register("x", nil))
	require.NoError(t, Register("test-dup", fstest.MapFS{}))
	assert.Error(t, Register("test-dup", fstest.MapFS{}), "a registered book already claimed the id")
}
