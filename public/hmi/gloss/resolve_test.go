package gloss

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A declaration in the name wins and shadows the rules; otherwise the first
// matching rule binds; a value kind the gloss refuses keeps the values
// plain, with the reason on hover.
func TestResolveTakesTheDeclarationThenTheFirstRule(t *testing.T) {
	cat := Default()
	bytesRule, err := cat.CompileRule(MediaTypeBytes, `name:size`, "test")
	require.NoError(t, err)
	idRule, err := cat.CompileRule(MediaTypeTaggedId, `name:size`, "test")
	require.NoError(t, err)
	rules := []Rule{bytesRule, idRule}

	r := Resolve(cat, "size", "name:size", rules, ValueKindNumeric, ValueKindNumeric)
	assert.Equal(t, MediaTypeBytes, r.MediaType)
	assert.True(t, r.Glossed())
	assert.Len(t, r.Shadowed, 1, "the later match is shadowed")
	assert.Contains(t, r.Hover(), "glossed as gloss/bytes")
	face, glossed := r.Face(TextCell{S: "2048", K: ValueKindNumeric})
	assert.True(t, glossed)
	assert.NotEqual(t, "2048", face.Text)

	r = Resolve(cat, "id@gloss/taggedid", "name:size", rules, ValueKindNumeric, ValueKindNumeric)
	assert.Equal(t, MediaTypeTaggedId, r.MediaType)
	assert.Equal(t, SourceAlias, r.Source)
	assert.Equal(t, "id", r.Label)
	assert.Len(t, r.Shadowed, 2, "a declaration is never offered to the rules")

	r = Resolve(cat, "size", "name:size", rules, ValueKindText, ValueKindText)
	assert.False(t, r.Glossed(), "bytes refuses text")
	assert.Contains(t, r.Hover(), "not applied")
	face, glossed = r.Face(TextCell{S: "abc", K: ValueKindText})
	assert.False(t, glossed)
	assert.Equal(t, "abc", face.Text, "a value that is not glossed shows its text")

	r = Resolve(cat, "other", "name:other", rules, ValueKindNumeric, ValueKindNumeric)
	assert.Empty(t, r.MediaType)
	assert.Equal(t, "spec: name:other", r.Hover())

	r = Resolve(cat, "x@gloss/nosuch", "", nil, ValueKindText, ValueKindText)
	assert.NotEmpty(t, r.Reason)
	assert.False(t, r.Glossed())
}

// Only a glossed gloss/url value is a link, trimmed.
func TestResolutionLinkIsAGlossedURLOnly(t *testing.T) {
	cat := Default()
	r := Resolve(cat, "u@gloss/url", "", nil, ValueKindText, ValueKindText)
	assert.Equal(t, "https://example.org", r.Link(TextCell{S: " https://example.org ", K: ValueKindText}))
	r = Resolve(cat, "plain", "", nil, ValueKindText, ValueKindText)
	assert.Empty(t, r.Link(TextCell{S: "https://example.org", K: ValueKindText}))
}

func TestDefaultRepositoryIsOneValue(t *testing.T) {
	assert.Same(t, DefaultRepository(), DefaultRepository())
}
