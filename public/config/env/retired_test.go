package env

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckEnviron(t *testing.T) {
	findings := CheckEnviron([]string{
		"SPINNAKER_PLAY_SQL=SELECT 1",
		"BOXER_PLAY_EXPERIMENTS=1",
		"BOXER_NO_SUCH_KNOB_EVER=1",
		"IMZERO2_NO_SUCH_KNOB_EVER=",
		"PATH=/bin",
		"HOME",
	})
	require.Len(t, findings, 3)
	require.Equal(t, EnvFinding{Kind: EnvFindingUnregistered, Name: "BOXER_NO_SUCH_KNOB_EVER"}, findings[0])
	require.Equal(t, EnvFindingRetired, findings[1].Kind)
	require.Equal(t, "BOXER_PLAY_EXPERIMENTS", findings[1].Name)
	require.Empty(t, findings[1].Retired.ReplacedBy)
	require.Equal(t, "SPINNAKER_PLAY_SQL", findings[2].Name)
	require.Equal(t, "BOXER_PLAY_SQL", findings[2].Retired.ReplacedBy)
}

func TestCheckEnvironKnowsNamesThisBinaryDoesNotLink(t *testing.T) {
	require.NotEmpty(t, knownNames)
	require.Empty(t, CheckEnviron([]string{knownNames[0] + "=1"}))
}

func TestRetiredNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range RetiredVars() {
		require.False(t, seen[r.Name], r.Name)
		seen[r.Name] = true
	}
}
