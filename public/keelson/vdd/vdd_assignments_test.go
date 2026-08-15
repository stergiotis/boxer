package vdd_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/vdd"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/assignments"
)

// The committed assignment table for the keelson vocabulary (ADR-0183 D1).
//
// The registry refuses a repeated ordinal at init, so what this catches is the
// remaining silent edit: an ordinal changed in place, which compiles and reads
// while making rows already in `boxer.facts` mean something else. Until this
// landed, vdd had no test file at all — the largest vocabulary in the tree was
// also the only one with nothing pinning it.
func TestMembershipAssignmentsMatchTheGolden(t *testing.T) {
	if os.Getenv(assignments.RegenEnvVar) != "" {
		require.NoError(t, assignments.WriteGoldenFile(".", vdd.KeelsonHrNkRegistry))
		t.Skip("golden rewritten; unset " + assignments.RegenEnvVar + " to compare against it")
	}
	differences, err := assignments.CompareToGoldenFile(".", vdd.KeelsonHrNkRegistry)
	require.NoError(t, err)
	assert.Empty(t, differences,
		"the vocabulary and its committed table disagree; a `!` line is a re-pointed id, not a new membership")
}
