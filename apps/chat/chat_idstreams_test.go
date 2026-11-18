package chat

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/namemint/assignments"
)

// The committed id-stream claims (ADR-0319 §SD2): a moved tag value compiles
// and mints, and every mount already minted under the old value then belongs
// to no stream; the golden makes that edit a changed line.
func TestIdStreamClaimsMatchTheGolden(t *testing.T) {
	if os.Getenv(assignments.RegenEnvVar) != "" {
		require.NoError(t, assignments.WriteStreamGoldenFile(".", idStreams))
		t.Skip("golden rewritten; unset " + assignments.RegenEnvVar + " to compare against it")
	}
	differences, err := assignments.CompareToStreamGoldenFile(".", idStreams)
	require.NoError(t, err)
	assert.Empty(t, differences, "the claims and the committed table disagree; a `!` line moves every id already minted")
}
