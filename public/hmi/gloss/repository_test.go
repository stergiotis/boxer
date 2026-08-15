package gloss

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A rule set declared in code registers on a repository, ranks between the
// buffer's directives and the affinities, and carries its provenance.
func TestRepository(t *testing.T) {
	repo := NewRepository(nil)
	assert.Same(t, repo.Catalog(), repo.Catalog())
	assert.Len(t, repo.Rules(), len(repo.Catalog().AffinityRules()), "empty: the affinities alone")

	sensors := Rules("acme-sensors").
		Rule("kelvin readings").
		When(Section("sensor"), NameMatches(`^temp`)).
		Show(MediaTypeTemperature, Unit("K")).
		Rule("secrets stay masked").
		When(Sem(valueaspects.AspectSecret)).
		Show(MediaTypeMasked)
	require.NoError(t, repo.Register(sensors))
	assert.Equal(t, 2, sensors.Len())

	rules := repo.Rules()
	require.Len(t, rules, 2+len(repo.Catalog().AffinityRules()))
	assert.Equal(t, "set acme-sensors: kelvin readings", rules[0].Source)
	assert.Equal(t, "acme-sensors", rules[0].Set)
	assert.Equal(t, "kelvin readings", rules[0].Name)
	assert.Equal(t, "section=sensor ∧ name~^temp", rules[0].Pattern)
	assert.Equal(t, MediaTypeTemperature, rules[0].MediaType)
	assert.Equal(t, map[string]string{ParamUnit: "K"}, rules[0].Params)
	assert.NotNil(t, rules[0].Instance, "bound once at registration")
	assert.Equal(t, "gloss/temperature;unit=K", rules[0].Token())
	assert.Equal(t, SourceAffinity, rules[len(rules)-1].Source, "affinities last")

	// The set's rule outranks the affinity it duplicates — list order.
	r, ok := MatchFirst(rules, "name:pw section:auth role:val ct:s sem:secret arrow:utf8")
	require.True(t, ok)
	assert.Equal(t, "secrets stay masked", r.Name)
	all := MatchAll(rules, "name:pw section:auth role:val ct:s sem:secret arrow:utf8")
	require.Len(t, all, 2)
	assert.Equal(t, SourceAffinity, all[1].Source, "…and shadows it")
	r, ok = MatchFirst(rules, "name:temperature section:sensor role:val ct:f64 arrow:list<item: float64>")
	require.True(t, ok)
	assert.Equal(t, "K", r.Params[ParamUnit])
	_, ok = MatchFirst(rules, "name:temperature section:room role:val ct:f64 arrow:float64")
	assert.False(t, ok, "wrong section")

	// A second set ranks below the first; a duplicate name is refused.
	require.NoError(t, repo.Register(Rules("site").Rule("everything is raw").When(NameMatches(`.`)).Show(MediaTypeRaw)))
	require.Len(t, repo.Sets(), 2)
	r, _ = MatchFirst(repo.Rules(), "name:pw section:auth role:val ct:s sem:secret arrow:utf8")
	assert.Equal(t, "secrets stay masked", r.Name, "the earlier set still wins")
	assert.ErrorContains(t, repo.Register(Rules("site").Rule("x").When(Name("x")).Show(MediaTypeRaw)), "already registered")
	assert.Error(t, repo.Register(nil))
}

// A set that does not validate is refused whole, with the set and rule
// named: unknown type, undeclared or missing parameter, bad pattern, no
// condition, no name, duplicate name, an unclosed rule.
func TestRuleSetValidation(t *testing.T) {
	cases := []struct {
		set  *RuleSet
		want string
	}{
		{Rules("s").Rule("r").When(Name("x")).Show("gloss/temperatur", Unit("K")), "unknown media type"},
		{Rules("s").Rule("r").When(Name("x")).Show(MediaTypeTemperature, P("unti", "K")), "unknown parameter"},
		{Rules("s").Rule("r").When(Name("x")).Show(MediaTypeTemperature), "requires unit="},
		{Rules("s").Rule("r").When(Name("x")).Show(MediaTypeTemperature, Unit("k")), "not allowed"},
		{Rules("s").Rule("r").When(NameMatches("(")).Show(MediaTypeRaw), "does not compile"},
		{Rules("s").Rule("r").Show(MediaTypeRaw), "no condition"},
		{Rules("s").Rule("").When(Name("x")).Show(MediaTypeRaw), "needs a name"},
		{Rules("").Rule("r").When(Name("x")).Show(MediaTypeRaw), "needs a name"},
		{Rules("s").Rule("r").When(Name("x")).Show(MediaTypeRaw).Rule("r").When(Name("y")).Show(MediaTypeRaw), "declared twice"},
	}
	for _, tc := range cases {
		repo := NewRepository(nil)
		err := repo.Register(tc.set)
		require.Error(t, err, tc.want)
		assert.ErrorContains(t, err, tc.want)
		assert.Empty(t, repo.Sets(), "nothing registered: %s", tc.want)
	}

	// An opened rule that never reached Show.
	open := Rules("s")
	open.Rule("dangling").When(Name("x"))
	err := NewRepository(nil).Register(open)
	assert.ErrorContains(t, err, "never closed with Show")
}
