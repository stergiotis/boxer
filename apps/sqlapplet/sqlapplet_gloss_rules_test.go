package sqlapplet

import (
	"testing"

	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The books' standing rules: the suffixes the books use, and only those.
func TestBookRules(t *testing.T) {
	require.Len(t, bookRepository.Sets(), 1)
	rules := bookRepository.Rules()
	first := func(spec string) (gloss.Rule, bool) { return gloss.MatchFirst(rules, spec) }

	r, ok := first("name:text_bytes arrow:uint64")
	require.True(t, ok)
	assert.Equal(t, gloss.MediaTypeBytes, r.MediaType)
	assert.Equal(t, "set sqlapplet-books: byte counts", r.Source)
	r, ok = first("name:bytes arrow:uint64")
	require.True(t, ok)
	assert.Equal(t, gloss.MediaTypeBytes, r.MediaType)
	_, ok = first("name:bytes_total arrow:uint64")
	assert.False(t, ok, "a suffix, not a substring")

	r, ok = first("name:self_ns arrow:int64")
	require.True(t, ok)
	assert.Equal(t, gloss.MediaTypeDuration, r.MediaType)
	assert.Equal(t, "ns", r.Params[gloss.ParamUnit])
	r, ok = first("name:age_ms arrow:float64")
	require.True(t, ok)
	assert.Equal(t, "ms", r.Params[gloss.ParamUnit])
	_, ok = first("name:module_path arrow:utf8")
	assert.False(t, ok)

	// The instances render the books' shapes.
	r, _ = first("name:cum_ms arrow:float64")
	assert.Equal(t, "12.3 ms", r.Instance.Inline(gloss.TextCell{S: "12.3", K: gloss.ValueKindNumeric}).Text)
	r, _ = first("name:text_bytes arrow:uint64")
	assert.Equal(t, "1.2 MiB", r.Instance.Inline(gloss.TextCell{S: "1258291", K: gloss.ValueKindNumeric}).Text)
}
