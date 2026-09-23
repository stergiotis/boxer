package stevedorevocab_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/assignments"
	"github.com/stergiotis/boxer/public/streaming/stevedore/stevedorevocab"
)

// The committed assignment table for the stevedore vocabulary (ADR-0183 D1).
//
// The registry refuses a repeated ordinal at init; this catches the edit it
// cannot see — an ordinal changed in place, which compiles and reads while
// making rows already stored under the old id mean something else.
func TestMembershipAssignmentsMatchTheGolden(t *testing.T) {
	if os.Getenv(assignments.RegenEnvVar) != "" {
		require.NoError(t, assignments.WriteGoldenFile(".", stevedorevocab.NkRegistry))
		t.Skip("golden rewritten; unset " + assignments.RegenEnvVar + " to compare against it")
	}
	differences, err := assignments.CompareToGoldenFile(".", stevedorevocab.NkRegistry)
	require.NoError(t, err)
	assert.Empty(t, differences,
		"the vocabulary and its committed table disagree; a `!` line is a re-pointed id, not a new membership")
}

func TestAllMembsCoversTheRegistry(t *testing.T) {
	seen := make(map[uint64]struct{}, len(stevedorevocab.AllMembs))
	for _, m := range stevedorevocab.AllMembs {
		id := m.GetId().Value()
		require.NotZero(t, id)
		_, dup := seen[id]
		require.False(t, dup, "two memberships share one id")
		seen[id] = struct{}{}
	}
	n := 0
	for range stevedorevocab.NkRegistry.IterateAll() {
		n++
	}
	require.Equal(t, n, len(stevedorevocab.AllMembs), "AllMembs and the registry disagree")
}
