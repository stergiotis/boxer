package ladingvocab_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/fs/lading/ladingvocab"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/assignments"
)

// The committed assignment table for the fs-snapshot-store vocabulary (ADR-0183 D1).
//
// The registry refuses a repeated ordinal at init; this catches the edit it
// cannot see — an ordinal changed in place, which compiles and reads while
// making rows already stored under the old id mean something else.
func TestMembershipAssignmentsMatchTheGolden(t *testing.T) {
	if os.Getenv(assignments.RegenEnvVar) != "" {
		require.NoError(t, assignments.WriteGoldenFile(".", ladingvocab.NkRegistry))
		t.Skip("golden rewritten; unset " + assignments.RegenEnvVar + " to compare against it")
	}
	differences, err := assignments.CompareToGoldenFile(".", ladingvocab.NkRegistry)
	require.NoError(t, err)
	assert.Empty(t, differences,
		"the vocabulary and its committed table disagree; a `!` line is a re-pointed id, not a new membership")
}
