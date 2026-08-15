package gloss

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roleT string // a string-kinded type, as common.ColumnRoleE is

func TestPredicates(t *testing.T) {
	spec := ParseSpec("name:pw section:auth role:val ct:s sem:secret sem:canonicalized-value use:data arrow:list<item: utf8>")
	plain := ParseSpec("name:temp_c arrow:float64")

	assert.True(t, Name("pw").Matches(&spec))
	assert.False(t, Name("p").Matches(&spec), "exact, not prefix")
	assert.True(t, NameMatches(`^p`).Matches(&spec))
	assert.True(t, Section("auth").Matches(&spec))
	assert.True(t, Role(roleT("val")).Matches(&spec), "any string-kinded type")
	assert.True(t, CT("s").Matches(&spec))
	assert.True(t, Sem(valueaspects.AspectSecret).Matches(&spec))
	assert.False(t, Sem(valueaspects.AspectUrl).Matches(&spec))
	assert.True(t, Arrow("list<").Matches(&spec))
	assert.False(t, Arrow("list<").Matches(&plain))
	assert.True(t, Item("").Matches(&plain), "a plain column has no item — matching the empty item is the way to say so")
	assert.True(t, SpecMatches(`\bsem:secret\b`).Matches(&spec), "the directive's own matcher")

	// Text forms, and how they compose.
	assert.Equal(t, "section=auth ∧ name~^p", All(Section("auth"), NameMatches(`^p`)).String())
	assert.Equal(t, "(sem=secret ∨ sem=url)", Any(Sem(valueaspects.AspectSecret), Sem(valueaspects.AspectUrl)).String())
	assert.Equal(t, "¬(section=auth)", Not(Section("auth")).String())
	assert.Equal(t, "ct=s ∧ (sem=secret ∨ sem=url)", All(CT("s"), Any(Sem(valueaspects.AspectSecret), Sem(valueaspects.AspectUrl))).String())
	assert.Equal(t, "name=pw", All(Name("pw")).String(), "one predicate: no wrapper")

	// Semantics of the combinators.
	assert.True(t, All(Section("auth"), Sem(valueaspects.AspectSecret)).Matches(&spec))
	assert.False(t, All(Section("auth"), Sem(valueaspects.AspectUrl)).Matches(&spec))
	assert.False(t, All().Matches(&spec), "nothing holds nothing")
	assert.True(t, Any(Sem(valueaspects.AspectUrl), Sem(valueaspects.AspectSecret)).Matches(&spec))
	assert.True(t, Not(Sem(valueaspects.AspectUrl)).Matches(&spec))
	assert.False(t, Predicate{}.Matches(&spec), "the zero predicate matches nothing")

	// A bad pattern is carried, not panicked, and poisons what combines it.
	bad := NameMatches("(")
	require.Error(t, bad.Err())
	assert.False(t, bad.Matches(&spec))
	assert.Error(t, All(Section("auth"), bad).Err())
	assert.Error(t, Not(bad).Err())
	assert.Equal(t, "name~(", bad.String(), "the text still says what was meant")
}
