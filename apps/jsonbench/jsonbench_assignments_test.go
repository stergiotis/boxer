package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/assignments"
)

// The committed assignment table for the jsonbench vocabulary (ADR-0183 D1).
//
// This one is an app's, not a platform vocabulary's, and it is here for the
// same reason the others are: it mints version-controlled ids, so the union
// check has to be able to see them. It is also the vocabulary that showed why
// the check is needed — it picked the runtime's tag value, which stayed latent
// only because the two share no table.
func TestMembershipAssignmentsMatchTheGolden(t *testing.T) {
	if os.Getenv(assignments.RegenEnvVar) != "" {
		require.NoError(t, assignments.WriteGoldenFile(".", NkRegistry))
		t.Skip("golden rewritten; unset " + assignments.RegenEnvVar + " to compare against it")
	}
	differences, err := assignments.CompareToGoldenFile(".", NkRegistry)
	require.NoError(t, err)
	assert.Empty(t, differences,
		"the vocabulary and its committed table disagree; a `!` line is a re-pointed id, not a new membership")
}
