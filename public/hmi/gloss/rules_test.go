package gloss

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileRule(t *testing.T) {
	c := Default()
	r, err := c.CompileRule("gloss/temperature;unit=K", `name:.*temp\b`, "directive line 3")
	require.NoError(t, err)
	assert.Equal(t, MediaTypeTemperature, r.MediaType)
	assert.Equal(t, "K", r.Params[ParamUnit])
	assert.Equal(t, "gloss/temperature;unit=K", r.Token())
	assert.True(t, r.Match("name:temp section:sensor role:val ct:f64 arrow:float64"))
	assert.False(t, r.Match("name:temperature_id"), "the author's own boundary holds")
	assert.NotNil(t, r.Instance)
	assert.Equal(t, "293.7 K", r.Instance.Inline(TextCell{S: "293.7", K: ValueKindNumeric}).Text)

	// Every failure is an error, never a rule that matches nothing.
	_, err = c.CompileRule("celsius", "name:x", "")
	assert.ErrorContains(t, err, "no slash")
	_, err = c.CompileRule("gloss/temperatur;unit=K", "name:x", "")
	assert.ErrorContains(t, err, "unknown media type")
	_, err = c.CompileRule("gloss/temperature;unti=K", "name:x", "")
	assert.ErrorContains(t, err, "unknown parameter")
	_, err = c.CompileRule("gloss/temperature", "name:x", "")
	assert.ErrorContains(t, err, "requires unit=")
	_, err = c.CompileRule("gloss/raw", "  ", "")
	assert.ErrorContains(t, err, "needs a pattern")
	_, err = c.CompileRule("gloss/raw", "(", "")
	assert.ErrorContains(t, err, "does not compile")
}

// The affinities: masked (sem:secret), url, the json family and the network
// canonical types — narrow, in catalog order (content family first, so
// application/json precedes gloss/masked).
func TestAffinityRules(t *testing.T) {
	c := Default()
	rules := c.AffinityRules()
	var got []string
	for _, r := range rules {
		got = append(got, r.MediaType+" ← "+r.Pattern)
		assert.Equal(t, SourceAffinity, r.Source)
	}
	assert.Equal(t, []string{
		MediaTypeJSON + ` ← \bsem:json(-scalar|-array|-object)?\b`,
		MediaTypeMasked + ` ← \bsem:secret\b`,
		MediaTypeURL + ` ← \bsem:url\b`,
		MediaTypeIPAddr + ` ← \bct:[vw]c?[hm]?\b`,
	}, got)

	r, ok := MatchFirst(rules, "name:pw section:auth role:val ct:s sem:secret arrow:utf8")
	require.True(t, ok)
	assert.Equal(t, MediaTypeMasked, r.MediaType)
	_, ok = MatchFirst(rules, "name:pw section:auth role:val ct:s sem:secretive")
	assert.False(t, ok, "word boundary: sem:secretive is not sem:secret")
	_, ok = MatchFirst(rules, "name:body ct:s sem:json-object")
	assert.True(t, ok)
	_, ok = MatchFirst(rules, "name:temp_c arrow:float64")
	assert.False(t, ok, "units have no affinity — that is what the directive is for")
	r, ok = MatchFirst(rules, "name:peer section:conn role:val ct:wc arrow:fixed_size_binary[17]")
	require.True(t, ok)
	assert.Equal(t, MediaTypeIPAddr, r.MediaType, "a network canonical type carries its own affinity")
	_, ok = MatchFirst(rules, "name:weight ct:f64 arrow:float64")
	assert.False(t, ok, "ct:[vw] is the whole type, not a prefix of another one")

	// Directive rules precede affinities by list order; gloss/raw overrides.
	raw, err := c.CompileRule("gloss/raw", `\bsem:secret\b`, "directive line 1")
	require.NoError(t, err)
	r, ok = MatchFirst(append([]Rule{raw}, rules...), "name:pw sem:secret")
	require.True(t, ok)
	assert.Equal(t, MediaTypeRaw, r.MediaType)
	assert.Same(t, rules[0].Instance, c.AffinityRules()[0].Instance, "affinities compile once")
}

// MatchAll keeps list order — the winner first, then what it shadows — so
// the Glosses tab can show a rule that never fires (ADR-0186 §SD3).
func TestMatchAll(t *testing.T) {
	c := Default()
	raw, err := c.CompileRule("gloss/raw", `\bsem:secret\b`, "directive line 1")
	require.NoError(t, err)
	temp, err := c.CompileRule("gloss/temperature;unit=C", `name:pw`, "directive line 2")
	require.NoError(t, err)
	rules := append([]Rule{raw, temp}, c.AffinityRules()...)

	all := MatchAll(rules, "name:pw section:auth role:val ct:s sem:secret arrow:utf8")
	require.Len(t, all, 3)
	assert.Equal(t, MediaTypeRaw, all[0].MediaType, "the winner, as MatchFirst returns it")
	first, ok := MatchFirst(rules, "name:pw section:auth role:val ct:s sem:secret arrow:utf8")
	require.True(t, ok)
	assert.Equal(t, first.Source, all[0].Source)
	assert.Equal(t, "directive line 2", all[1].Source, "shadowed by the earlier directive")
	assert.Equal(t, SourceAffinity, all[2].Source, "the affinity, shadowed by both")
	assert.Equal(t, MediaTypeMasked, all[2].MediaType)

	assert.Nil(t, MatchAll(rules, "name:temp_c arrow:float64"), "nothing matches: nil, not empty")
}
