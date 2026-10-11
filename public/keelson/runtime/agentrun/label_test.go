package agentrun

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"pgregory.net/rapid"
)

func genLabel(t *rapid.T) Label {
	return Label{Level: LevelE(rapid.IntRange(0, 4).Draw(t, "level")), Tier: TierE(rapid.IntRange(0, 4).Draw(t, "tier")),
		Exposure: rapid.SliceOfN(rapid.SampledFrom([]string{"", "openai", "anthropic", "local"}), 0, 3).Draw(t, "exposure"),
		Origin:   rapid.SliceOfN(rapid.SampledFrom([]string{"", "r1", "r2"}), 0, 3).Draw(t, "origin")}
}

// Join is a join-semilattice: associative, commutative, idempotent, with
// the zero label as identity, and every label at or below its join.
func TestTheLabelIsAJoinSemilattice(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b, c := genLabel(t), genLabel(t), genLabel(t)
		assert.True(t, a.Join(b).Equal(b.Join(a)), "commutative")
		assert.True(t, a.Join(b).Join(c).Equal(a.Join(b.Join(c))), "associative")
		assert.True(t, a.Join(a).Equal(a), "idempotent")
		assert.True(t, a.Join(Label{}).Equal(a), "identity")
		assert.True(t, a.AtOrBelow(a.Join(b)))
		assert.True(t, b.AtOrBelow(a.Join(b)))
		assert.Equal(t, a.Join(b).Join(c), JoinAll(a, b, c))
	})
}

func TestEndorsementLowersIntegrityOnly(t *testing.T) {
	l := Label{Level: LevelOtherModel, Tier: TierAmber, Origin: []string{"r1"}}
	e := l.Endorsed(LevelPerson)
	assert.Equal(t, LevelPerson, e.Level)
	assert.Equal(t, TierAmber, e.Tier, "confidentiality never lowers")
	assert.Equal(t, []string{"r1"}, e.Origin, "marks stay")
	assert.Equal(t, LevelExternal, Label{Level: LevelExternal}.Endorsed(LevelOwnModel).Level, "an endorsement never raises")
	assert.Equal(t, []string{"a", "b"}, Label{}.WithExposure("b").WithExposure("a").WithExposure("a").Exposure)
	tier, ok := ParseTier("amber+strict")
	assert.True(t, ok)
	assert.Equal(t, TierAmberStrict, tier)
	assert.Equal(t, "other-model/AMBER origin=r1", l.String())
	assert.Equal(t, []string{"x", "y"}, ParseMarks(MarksString([]string{"y", "x", "y"})))
}
