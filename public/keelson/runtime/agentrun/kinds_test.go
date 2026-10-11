package agentrun

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The kind enum is closed and its ordinals never retire: the golden pins
// every name to its ordinal (ADR-0308 §SD2).
func TestTheKindEnumMatchesItsGolden(t *testing.T) {
	var b strings.Builder
	for _, k := range AllKinds {
		b.WriteString(strconv.Itoa(int(k)) + " " + k.String() + "\n")
		assert.True(t, k.IsValid())
		parsed, ok := ParseKind(k.String())
		assert.True(t, ok)
		assert.Equal(t, k, parsed)
	}
	path := filepath.Join("testdata", "kinds.golden")
	if os.Getenv("AGENTRUN_UPDATE_GOLDEN") != "" {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "no golden yet: run with AGENTRUN_UPDATE_GOLDEN=1")
	assert.Equal(t, string(want), b.String(), "a kind moved or vanished; ordinals never retire")
	assert.False(t, KindE(99).IsValid())
	assert.Equal(t, "unspecified", KindUnspecified.String())
}

func TestKindsThatMoveTheProjection(t *testing.T) {
	for _, k := range []KindE{KindUser, KindReply, KindOutcome, KindNote, KindSection, KindTools, KindCompact, KindEdit} {
		assert.True(t, k.MovesProjection(), k.String())
	}
	for _, k := range []KindE{KindRequest, KindIntent, KindProfile, KindHead, KindIgnorable, KindDeleted} {
		assert.False(t, k.MovesProjection(), k.String())
	}
	for _, k := range []KindE{KindSection, KindTools, KindCompact, KindEdit} {
		assert.True(t, k.Rewrites(), k.String())
	}
	assert.False(t, KindUser.Rewrites())
}
