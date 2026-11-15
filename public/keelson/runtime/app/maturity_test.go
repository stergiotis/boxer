package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The grades are ordered weakest first, and the rank is that order: a
// predicate asking for a grade or better depends on it.
func TestMaturity_RankFollowsTheOrder(t *testing.T) {
	require.Equal(t, []MaturityE{MaturityUnstated, MaturityIncubating, MaturitySupported, MaturityCore}, AllMaturities)
	for i, g := range AllMaturities {
		assert.EqualValues(t, i, g.Rank(), g.String())
		assert.True(t, g.IsValid(), g.String())
	}
	assert.Equal(t, MaturityUnstated, Manifest{}.Maturity, "a manifest that states no grade is unstated")
}

// Every grade's token parses back to it; a token that is no grade is not
// read as unstated.
func TestMaturity_ParseRoundTrips(t *testing.T) {
	for _, g := range AllMaturities {
		got, ok := ParseMaturity(g.String())
		require.True(t, ok, g.String())
		assert.Equal(t, g, got)
	}
	for _, s := range []string{"", "beta", "Incubating", "stable"} {
		_, ok := ParseMaturity(s)
		assert.False(t, ok, "%q is no grade", s)
	}
	assert.Equal(t, "unstated", MaturityE(99).String(), "a value outside the grades prints as the weakest")
}

// A manifest is not refused for stating no grade, only for one outside the
// grades.
func TestManifest_Validate_Maturity(t *testing.T) {
	m := Manifest{
		Id: "test.graded", Display: "Graded", Summary: "fixture summary",
		Surface: SurfaceWindowed, Topics: []TopicT{TopicRuntime},
	}
	require.NoError(t, m.Validate(), "no grade stated")
	for _, g := range AllMaturities {
		m.Maturity = g
		require.NoError(t, m.Validate(), g.String())
	}
	m.Maturity = MaturityCore + 1
	err := m.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid Maturity")
}
