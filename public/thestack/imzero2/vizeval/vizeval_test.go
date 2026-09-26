package vizeval

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSinkDefaultsResolve(t *testing.T) {
	seen := make(map[string]bool, len(Sinks()))
	for _, s := range Sinks() {
		assert.False(t, seen[s.ID], "sink ids are unique: %s", s.ID)
		seen[s.ID] = true
		assert.Positive(t, s.RowCap, s.ID)
		_, err := s.Space.Resolve(nil)
		require.NoError(t, err, "every default of %s validates", s.ID)
	}
}

func TestResolveRefusesRatherThanRepairs(t *testing.T) {
	sp := Space{
		{Name: "n", Kind: OptionKindInt, Min: 1, Max: 10, Default: int64(5)},
		{Name: "e", Kind: OptionKindEnum, Choices: []string{"a", "b"}, Default: "a"},
		{Name: "f", Kind: OptionKindFloat, Min: 0, Max: 1, Default: 0.5},
		{Name: "b", Kind: OptionKindBool, Default: false},
	}
	vals, err := sp.Resolve(map[string]any{"n": float64(7)})
	require.NoError(t, err)
	assert.Equal(t, Values{"n": int64(7), "e": "a", "f": 0.5, "b": false}, vals)

	for name, raw := range map[string]map[string]any{
		"unknown option":  {"zz": 1},
		"fractional int":  {"n": 2.5},
		"above max":       {"n": 11},
		"not a choice":    {"e": "c"},
		"enum not string": {"e": 1},
		"bool not bool":   {"b": "true"},
	} {
		_, err = sp.Resolve(raw)
		assert.Error(t, err, name)
	}
}

func TestCandidateIdentityIgnoresSpelling(t *testing.T) {
	a, err := NewCandidate(SinkCard, nil)
	require.NoError(t, err)
	b, err := UnmarshalCandidate([]byte(`{"sink":"card","options":{"palette":"viridis"}}`))
	require.NoError(t, err)
	assert.Equal(t, a.ID(), b.ID(), "an option at its default and one left out are the same candidate")
	assert.JSONEq(t, `{"sink":"card","options":{"palette":"viridis"}}`, string(a.Canonical()))

	c, err := NewCandidate(SinkCard, map[string]any{OptionPalette: "magma"})
	require.NoError(t, err)
	assert.NotEqual(t, a.ID(), c.ID())

	u, err := UnmarshalCandidate([]byte(`{"sink":"unicode","options":{"width":120}}`))
	require.NoError(t, err)
	assert.Equal(t, int64(120), u.Options[OptionWidth])

	_, err = UnmarshalCandidate([]byte(`{"sink":"card","extra":1}`))
	assert.Error(t, err, "unknown members are refused")
	_, err = UnmarshalCandidate([]byte(`{"sink":"nope"}`))
	assert.Error(t, err)
}
