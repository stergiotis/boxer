package vocab_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/assignments"
)

// The committed id-stream claims (ADR-0319 §SD2). A moved tag value
// compiles, mints and reads, and every id already stored under the old value
// then belongs to no stream; the golden makes that edit a changed line.
func TestIdStreamClaimsMatchTheGolden(t *testing.T) {
	if os.Getenv(assignments.RegenEnvVar) != "" {
		require.NoError(t, assignments.WriteStreamGoldenFile(".", vocab.IdStreams))
		t.Skip("golden rewritten; unset " + assignments.RegenEnvVar + " to compare against it")
	}
	differences, err := assignments.CompareToStreamGoldenFile(".", vocab.IdStreams)
	require.NoError(t, err)
	assert.Empty(t, differences, "the claims and the committed table disagree; a `!` line moves every id already minted")
}

func TestIdStreamsSitInTheirAdvisedClass(t *testing.T) {
	claims, err := assignments.SnapshotStreams(vocab.IdStreams)
	require.NoError(t, err)
	for _, c := range claims {
		assert.NoError(t, assignments.CheckStreamClass(c), c.Name)
	}
}
